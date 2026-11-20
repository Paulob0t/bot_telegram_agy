import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'package:flutter/foundation.dart';
import '../models/agent_event.dart';

/// Cliente WebSocket resiliente con reconexión exponencial y sincronización
/// directa con la State Machine de Rive (`idle`, `listening`, `thinking`, `speaking`, `sleeping`).
class AgyWsClient extends ChangeNotifier {
  String _wsUrl;
  String _bearerToken;
  String _userId;

  WebSocket? _socket;
  StreamSubscription<dynamic>? _subscription;
  Timer? _reconnectTimer;

  bool _disposed = false;
  bool _manualDisconnect = false;
  bool _isConnecting = false;
  int _retryAttempt = 0;

  CharacterState _characterState = CharacterState.sleeping;
  final List<ChatMessage> _messages = [];
  String? _activeStreamingMsgId;
  String? _lastError;

  final Duration connectTimeout;
  final Duration pingInterval;
  final int maxBackoffSeconds;

  AgyWsClient({
    required String wsUrl,
    required String bearerToken,
    String userId = 'paulo_mobile',
    this.connectTimeout = const Duration(seconds: 5),
    this.pingInterval = const Duration(seconds: 20),
    this.maxBackoffSeconds = 30,
  })  : _wsUrl = wsUrl.trim(),
        _bearerToken = bearerToken.trim(),
        _userId = userId;

  String get wsUrl => _wsUrl;
  String get bearerToken => _bearerToken;
  bool get isConnected => _socket != null && _socket!.readyState == WebSocket.open;
  bool get isConnecting => _isConnecting;
  int get retryAttempt => _retryAttempt;
  CharacterState get characterState => _characterState;
  List<ChatMessage> get messages => List.unmodifiable(_messages);
  String? get lastError => _lastError;

  /// Actualiza endpoint de Tailscale y Bearer Token en caliente y reconecta.
  Future<void> updateCredentials({
    required String wsUrl,
    required String bearerToken,
    String? userId,
  }) async {
    _wsUrl = wsUrl.trim();
    _bearerToken = bearerToken.trim();
    if (userId != null && userId.trim().isNotEmpty) {
      _userId = userId.trim();
    }
    _retryAttempt = 0;
    await disconnect();
    await connect();
  }

  /// Conecta al servidor Go en Tailscale. Si la PC está suspendida o apagada,
  /// pasa automáticamente a [CharacterState.sleeping] y programa reconexión exponencial.
  Future<void> connect() async {
    if (_disposed || _isConnecting || isConnected) return;
    _manualDisconnect = false;
    _isConnecting = true;
    notifyListeners();

    try {
      final ws = await WebSocket.connect(
        _wsUrl,
        headers: {
          'Authorization': 'Bearer $_bearerToken',
        },
      ).timeout(connectTimeout);

      if (_disposed || _manualDisconnect) {
        await ws.close();
        return;
      }

      ws.pingInterval = pingInterval;
      _socket = ws;
      _isConnecting = false;
      _retryAttempt = 0;
      _lastError = null;
      _setCharacterState(CharacterState.idle);

      _subscription = ws.listen(
        _onRawFrame,
        onError: (Object err) {
          _lastError = err.toString();
          _handleDisconnect();
        },
        onDone: _handleDisconnect,
        cancelOnError: true,
      );
    } catch (err) {
      _isConnecting = false;
      _lastError = err.toString();
      _handleDisconnect();
    }
  }

  /// Cierra la conexión voluntariamente sin disparar reconexión automática.
  Future<void> disconnect() async {
    _manualDisconnect = true;
    _reconnectTimer?.cancel();
    _reconnectTimer = null;
    await _subscription?.cancel();
    _subscription = null;
    final old = _socket;
    _socket = null;
    _isConnecting = false;
    _setCharacterState(CharacterState.sleeping);
    if (old != null) {
      try {
        await old.close();
      } catch (_) {}
    }
  }

  /// Cambia a `listening` cuando el usuario está escribiendo o hablando en el micrófono,
  /// siempre que el agente esté conectado y en reposo (`idle`).
  void setUserListening(bool listening) {
    if (!isConnected) return;
    if (listening && _characterState == CharacterState.idle) {
      _setCharacterState(CharacterState.listening);
    } else if (!listening && _characterState == CharacterState.listening) {
      _setCharacterState(CharacterState.idle);
    }
  }

  /// Envía un prompt con el contrato `{ "user_id", "text", "timestamp" }`.
  bool sendPrompt(String rawText) {
    final text = rawText.trim();
    if (text.isEmpty || !isConnected) return false;

    final now = DateTime.now();
    _messages.add(
      ChatMessage(
        id: 'u_${now.microsecondsSinceEpoch}',
        isUser: true,
        text: text,
        timestamp: now,
      ),
    );
    _activeStreamingMsgId = null;

    final payload = jsonEncode({
      'user_id': _userId,
      'text': text,
      'timestamp': now.millisecondsSinceEpoch ~/ 1000,
    });

    try {
      _socket!.add(payload);
      _setCharacterState(CharacterState.thinking);
      notifyListeners();
      return true;
    } catch (err) {
      _lastError = err.toString();
      _handleDisconnect();
      return false;
    }
  }

  /// Envía comando `/stop` para matar inmediatamente el árbol de procesos en Go.
  void stopExecution() {
    if (!isConnected) return;
    final payload = jsonEncode({
      'user_id': _userId,
      'text': '/stop',
      'timestamp': DateTime.now().millisecondsSinceEpoch ~/ 1000,
    });
    try {
      _socket!.add(payload);
    } catch (_) {}
  }

  void _onRawFrame(dynamic raw) {
    if (raw is! String || raw.trim().isEmpty) return;
    try {
      final ev = AgentEvent.fromRawJson(raw);
      switch (ev.type) {
        case AgentEventType.state:
          if (ev.state != null) {
            _setCharacterState(ev.state!);
          }
          break;

        case AgentEventType.chunk:
          _upsertStreamingMessage(ev);
          break;

        case AgentEventType.error:
          _lastError = ev.message;
          _messages.add(
            ChatMessage(
              id: 'err_${DateTime.now().microsecondsSinceEpoch}',
              isUser: false,
              text: ev.message,
              isError: true,
              timestamp: DateTime.now(),
            ),
          );
          _activeStreamingMsgId = null;
          _setCharacterState(CharacterState.idle);
          break;

        case AgentEventType.unknown:
          break;
      }
    } catch (err) {
      _lastError = 'Frame inválido: $err';
      notifyListeners();
    }
  }

  void _upsertStreamingMessage(AgentEvent ev) {
    if (ev.text.isEmpty && ev.toolStatus.isEmpty && !ev.done) return;

    if (_activeStreamingMsgId == null) {
      final id = 'a_${DateTime.now().microsecondsSinceEpoch}';
      _activeStreamingMsgId = id;
      _messages.add(
        ChatMessage(
          id: id,
          isUser: false,
          text: ev.text,
          toolStatus: ev.toolStatus,
          isStreaming: !ev.done,
          timestamp: DateTime.now(),
        ),
      );
    } else {
      final idx = _messages.lastIndexWhere((m) => m.id == _activeStreamingMsgId);
      if (idx != -1) {
        final prev = _messages[idx];
        _messages[idx] = prev.copyWith(
          text: ev.text.isNotEmpty ? ev.text : prev.text,
          toolStatus: ev.done ? '' : ev.toolStatus,
          isStreaming: !ev.done,
        );
      }
    }

    if (ev.done) {
      _activeStreamingMsgId = null;
    }
    notifyListeners();
  }

  void _handleDisconnect() {
    _subscription?.cancel();
    _subscription = null;
    _socket = null;
    _isConnecting = false;
    _activeStreamingMsgId = null;

    // Si la PC se suspende o pierde conexión, el personaje pasa a sleeping
    _setCharacterState(CharacterState.sleeping);

    if (!_disposed && !_manualDisconnect) {
      _scheduleExponentialReconnect();
    }
  }

  void _scheduleExponentialReconnect() {
    _reconnectTimer?.cancel();
    // Backoff exponencial: 1s, 2s, 4s, 8s, 16s, máx 30s
    final expSeconds = min(maxBackoffSeconds, 1 << min(_retryAttempt, 5));
    _retryAttempt++;
    _reconnectTimer = Timer(Duration(seconds: expSeconds), () {
      connect();
    });
    notifyListeners();
  }

  void _setCharacterState(CharacterState next) {
    if (_characterState != next) {
      _characterState = next;
    }
    notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    _reconnectTimer?.cancel();
    _subscription?.cancel();
    _socket?.close();
    super.dispose();
  }
}
