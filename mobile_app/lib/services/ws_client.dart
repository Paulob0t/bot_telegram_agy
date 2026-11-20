import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import 'package:open_filex/open_filex.dart';
import 'package:path_provider/path_provider.dart';
import '../models/agent_event.dart';
import 'notifications.dart';

const int currentAppBuildNumber = 21;

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
  final List<ChatMessage> _notifications = [];
  int _unreadNotifications = 0;
  String? _activeStreamingMsgId;
  String? _lastError;
  bool isAppInForeground = true;

  bool _updateAvailable = false;
  String _availableVersion = '';

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
        _userId = userId {
    unawaited(_loadLocalHistory());
  }

  String get wsUrl => _wsUrl;
  String get bearerToken => _bearerToken;
  bool get isConnected => _socket != null && _socket!.readyState == WebSocket.open;
  bool get isConnecting => _isConnecting;
  int get retryAttempt => _retryAttempt;
  CharacterState get characterState => _characterState;
  List<ChatMessage> get messages => List.unmodifiable(_messages);
  List<ChatMessage> get notifications => List.unmodifiable(_notifications);
  int get unreadNotifications => _unreadNotifications;
  String? get lastError => _lastError;
  bool get updateAvailable => _updateAvailable;
  String get availableVersion => _availableVersion;

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
      WebSocket ws;
      try {
        ws = await WebSocket.connect(
          _wsUrl,
          headers: {
            'Authorization': 'Bearer $_bearerToken',
          },
        ).timeout(connectTimeout);
      } catch (_) {
        // Si Tailscale no está encendido en el cel pero está por USB (adb reverse), probar 127.0.0.1
        if (!_wsUrl.contains('127.0.0.1') && !_wsUrl.contains('localhost')) {
          ws = await WebSocket.connect(
            'ws://127.0.0.1:8080/ws',
            headers: {
              'Authorization': 'Bearer $_bearerToken',
            },
          ).timeout(const Duration(seconds: 2));
        } else {
          rethrow;
        }
      }

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
      unawaited(_checkAndInstallUpdate());

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

    if (text == '/new' || text.startsWith('/resume ')) {
      _messages.clear();
    }

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

  /// Sube una foto, PDF o archivo al servidor Go (`POST /upload`) y opcionalmente lo analiza con IA.
  Future<bool> uploadFile(
    String filePath, {
    String target = 'cache',
    bool analyzeWithAI = false,
    String caption = '',
  }) async {
    try {
      final wsUri = Uri.parse(_wsUrl);
      final scheme = wsUri.scheme == 'wss' ? 'https' : 'http';
      final uploadUri = wsUri.replace(scheme: scheme, path: '/upload');

      final req = http.MultipartRequest('POST', uploadUri);
      req.headers['Authorization'] = 'Bearer $_bearerToken';
      req.fields['target'] = target;
      req.files.add(await http.MultipartFile.fromPath('file', filePath));

      final streamed = await req.send().timeout(const Duration(seconds: 30));
      final body = await streamed.stream.bytesToString();
      if (streamed.statusCode >= 400) {
        _lastError = 'Error subiendo archivo (${streamed.statusCode})';
        notifyListeners();
        return false;
      }
      final decoded = jsonDecode(body) as Map<String, dynamic>;
      final savedPath = decoded['path'] as String? ?? filePath;
      final fileName = decoded['file_name'] as String? ?? 'archivo';

      if (analyzeWithAI) {
        final instr = caption.trim().isNotEmpty
            ? caption.trim()
            : 'Por favor analiza este archivo.';
        return sendPrompt('[Archivo adjunto guardado en: $savedPath]\n\n$instr');
      }

      _messages.add(
        ChatMessage(
          id: 'up_${DateTime.now().microsecondsSinceEpoch}',
          isUser: false,
          text: '✅ Archivo `$fileName` guardado en:\n`$savedPath`',
          timestamp: DateTime.now(),
        ),
      );
      notifyListeners();
      return true;
    } catch (err) {
      _lastError = err.toString();
      notifyListeners();
      return false;
    }
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

        case AgentEventType.notify:
          final title = ev.title.isNotEmpty ? ev.title : 'HyprAgent';
          final body = ev.text.isNotEmpty ? ev.text : 'Tarea completada en tu PC';
          _notifications.insert(
            0,
            ChatMessage(
              id: 'n_${DateTime.now().microsecondsSinceEpoch}',
              isUser: false,
              text: '$title\n\n$body',
              timestamp: DateTime.now(),
            ),
          );
          if (_notifications.length > 50) {
            _notifications.removeLast();
          }
          _unreadNotifications++;
          notifyListeners();
          LocalNotifier.show(title, body);
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
      unawaited(_saveLocalHistory());
      if (!isAppInForeground && ev.text.trim().isNotEmpty) {
        final lines = ev.text.trim().split('\n');
        final title = lines.first.length > 50
            ? '${lines.first.substring(0, 50)}…'
            : lines.first;
        final body = lines.length > 1 ? lines.sublist(1).join(' ').trim() : ev.text.trim();
        LocalNotifier.show(title, body.isEmpty ? 'Tarea completada' : body);
      }
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

  void markNotificationsRead() {
    if (_unreadNotifications > 0) {
      _unreadNotifications = 0;
      notifyListeners();
    }
  }

  void clearNotifications() {
    _notifications.clear();
    _unreadNotifications = 0;
    notifyListeners();
  }

  void deleteMessage(String id) {
    _messages.removeWhere((m) => m.id == id);
    unawaited(_saveLocalHistory());
    notifyListeners();
  }

  void clearHistory() {
    _messages.clear();
    unawaited(_saveLocalHistory());
    notifyListeners();
  }

  Future<void> _loadLocalHistory() async {
    try {
      final dir = await getApplicationDocumentsDirectory();
      final f = File('${dir.path}/chat_history.json');
      if (!await f.exists()) return;
      final raw = await f.readAsString();
      final list = jsonDecode(raw) as List<dynamic>;
      if (_messages.isEmpty && list.isNotEmpty) {
        for (final item in list) {
          final m = item as Map<String, dynamic>;
          _messages.add(ChatMessage(
            id: m['id'] as String? ?? 'h_${_messages.length}',
            isUser: m['is_user'] as bool? ?? false,
            text: m['text'] as String? ?? '',
            isError: m['is_error'] as bool? ?? false,
            timestamp: DateTime.tryParse(m['ts'] as String? ?? '') ?? DateTime.now(),
          ));
        }
        notifyListeners();
      }
    } catch (_) {}
  }

  Future<void> _saveLocalHistory() async {
    try {
      final dir = await getApplicationDocumentsDirectory();
      final f = File('${dir.path}/chat_history.json');
      final tail = _messages.length > 80
          ? _messages.sublist(_messages.length - 80)
          : _messages;
      final data = tail
          .map((m) => {
                'id': m.id,
                'is_user': m.isUser,
                'text': m.text,
                'is_error': m.isError,
                'ts': m.timestamp.toIso8601String(),
              })
          .toList();
      await f.writeAsString(jsonEncode(data), flush: true);
    } catch (_) {}
  }

  Future<Map<String, dynamic>?> fetchOptions() async {
    try {
      final wsUri = Uri.parse(_wsUrl);
      final scheme = wsUri.scheme == 'wss' ? 'https' : 'http';
      final optUri = wsUri.replace(scheme: scheme, path: '/options');
      final res = await http
          .get(optUri, headers: {'Authorization': 'Bearer $_bearerToken'})
          .timeout(const Duration(seconds: 5));
      if (res.statusCode == 200) {
        return jsonDecode(res.body) as Map<String, dynamic>;
      }
    } catch (_) {}
    return null;
  }

  Future<void> _checkAndInstallUpdate() async {
    try {
      final wsUri = Uri.parse(_wsUrl);
      final scheme = wsUri.scheme == 'wss' ? 'https' : 'http';
      final verUri = wsUri.replace(scheme: scheme, path: '/version');

      final res = await http
          .get(verUri, headers: {'Authorization': 'Bearer $_bearerToken'})
          .timeout(const Duration(seconds: 4));
      if (res.statusCode != 200) return;

      final data = jsonDecode(res.body) as Map<String, dynamic>;
      final serverBuild = (data['build'] as num?)?.toInt() ?? 0;
      final apkExists = data['apk_exists'] as bool? ?? false;
      if (!apkExists || serverBuild <= currentAppBuildNumber) {
        if (_updateAvailable) {
          _updateAvailable = false;
          notifyListeners();
        }
        return;
      }

      _updateAvailable = true;
      _availableVersion = data['version'] as String? ?? 'v$serverBuild';
      notifyListeners();
    } catch (_) {
      // Silencioso en tests o si no hay actualización disponible
    }
  }

  Future<void> installAvailableUpdate() async {
    try {
      final wsUri = Uri.parse(_wsUrl);
      final scheme = wsUri.scheme == 'wss' ? 'https' : 'http';
      final apkUri = wsUri.replace(scheme: scheme, path: '/apk');

      _messages.add(
        ChatMessage(
          id: 'ota_${DateTime.now().microsecondsSinceEpoch}',
          isUser: false,
          text: '⏳ Descargando actualización ($_availableVersion)...',
          timestamp: DateTime.now(),
        ),
      );
      notifyListeners();

      final apkRes = await http
          .get(apkUri, headers: {'Authorization': 'Bearer $_bearerToken'})
          .timeout(const Duration(seconds: 90));
      if (apkRes.statusCode != 200) {
        throw Exception('HTTP ${apkRes.statusCode}');
      }

      final dir = await getTemporaryDirectory();
      final apkFile = File('${dir.path}/paulobot-update.apk');
      await apkFile.writeAsBytes(apkRes.bodyBytes, flush: true);

      final result = await OpenFilex.open(
        apkFile.path,
        type: 'application/vnd.android.package-archive',
      );
      if (result.type != ResultType.done) {
        throw Exception('${result.type.name}: ${result.message}');
      }
    } catch (err) {
      _messages.add(
        ChatMessage(
          id: 'ota_err_${DateTime.now().microsecondsSinceEpoch}',
          isUser: false,
          text: '⚠️ No se pudo abrir el instalador: $err',
          isError: true,
          timestamp: DateTime.now(),
        ),
      );
      notifyListeners();
    }
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
