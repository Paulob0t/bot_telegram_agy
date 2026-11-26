import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import 'package:open_filex/open_filex.dart';
import 'package:path_provider/path_provider.dart';
import '../features/avatar3d/models/avatar3d_state.dart';
import '../models/agent_event.dart';
import 'gist_discovery_service.dart';
import 'hub_credentials_store.dart';
import 'notifications.dart';

const int currentAppBuildNumber = int.fromEnvironment('APP_BUILD', defaultValue: 25);

/// Sesión aislada (proyecto + resume/chat) con su propio historial y TTL de 1h.
class SessionEntry {
  final String id;
  final String project;
  final String title;
  DateTime lastActivity;
  final List<ChatMessage> messages;
  bool isWorking;
  bool hasUnread;

  SessionEntry({
    required this.id,
    required this.project,
    required this.title,
    DateTime? lastActivity,
    List<ChatMessage>? messages,
    this.isWorking = false,
    this.hasUnread = false,
  })  : lastActivity = lastActivity ?? DateTime.now(),
        messages = messages ?? [];

  String get displayLabel =>
      project.isNotEmpty ? '$project · $title' : title;
}

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
  final List<PermissionRequest> _pendingPermissions = [];
  int _unreadNotifications = 0;
  String? _activeStreamingMsgId;
  String? _lastError;
  bool isAppInForeground = true;
  HubCredentials? _hubCreds;
  bool _needsSetup = false;

  /// True when neither the hub nor a direct URL is configured.
  bool get needsSetup => _needsSetup;
  HubCredentials? get hubCredentials => _hubCreds;
  String _hubStatusText = '';
  String _activeWorker = '';
  bool _debugMode = false;
  bool _forceCelHome = false;
  final List<String> _debugLogs = [];
  final List<SessionEntry> _sessions = [];
  String? _activeSessionId;
  String? _streamingTargetSessionId;
  final Map<String, String> _sessionStreamingMsgIds = {};
  Completer<Map<String, dynamic>>? _optionsCompleter;

  Future<Map<String, dynamic>?> fetchOptionsOverWs(String cmd) async {
    if (!isConnected) return null;
    _optionsCompleter?.completeError(Exception('cancelled'));
    final c = Completer<Map<String, dynamic>>();
    _optionsCompleter = c;
    _socket!.add(jsonEncode({
      'text': '__options__:$cmd',
      'session_id': _activeSessionId ?? 'default',
    }));
    try {
      return await c.future.timeout(const Duration(seconds: 8));
    } catch (_) {
      return null;
    } finally {
      if (_optionsCompleter == c) {
        _optionsCompleter = null;
      }
    }
  }

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

  AgentActivity _activity = AgentActivity.idle;
  /// A pending tool approval outranks whatever the PC reports: the agent is blocked on us.
  AgentActivity get activity => _pendingPermissions.isEmpty
      ? _activity
      : AgentActivity(
          state: Avatar3DState.awaitingApproval,
          label: 'Esperando tu OK · ${_pendingPermissions.first.tool}',
          detail: _pendingPermissions.first.detail,
          engine: _activity.engine,
        );
  List<ChatMessage> get messages => List.unmodifiable(_messages);
  List<ChatMessage> get notifications => List.unmodifiable(_notifications);
  List<PermissionRequest> get pendingPermissions =>
      List.unmodifiable(_pendingPermissions);

  /// Answers a tool approval request. Returns false when the socket is down.
  bool respondPermission(String id, bool allow) {
    if (!isConnected) return false;
    try {
      _socket!.add(jsonEncode({
        'text': '__perm__:$id:${allow ? 'allow' : 'deny'}',
        'session_id': _activeSessionId ?? 'default',
      }));
    } catch (_) {
      return false;
    }
    _pendingPermissions.removeWhere((p) => p.id == id);
    LocalNotifier.cancelPermission(id);
    notifyListeners();
    return true;
  }
  int get unreadNotifications => _unreadNotifications;
  String? get lastError => _lastError;
  String get hubStatusText => _hubStatusText;
  String get activeWorker => _activeWorker;
  bool get debugMode => _debugMode;
  bool get forceCelHome => _forceCelHome;
  List<String> get debugLogs => List.unmodifiable(_debugLogs);

  void setDebugMode(bool enabled) {
    _debugMode = enabled;
    if (enabled) {
      _addDebugLog('Debug activado (worker: ${_activeWorker.isEmpty ? "?" : _activeWorker})');
    }
    notifyListeners();
  }

  void setForceCelHome(bool enabled) {
    _forceCelHome = enabled;
    _addDebugLog(enabled ? '→ Forzando Cel Home' : '→ Volviendo a PC');
    if (isConnected) {
      try {
        _socket!.add(jsonEncode({'type': 'set_debug', 'enabled': enabled}));
      } catch (_) {}
    }
    notifyListeners();
  }

  void clearDebugLogs() {
    _debugLogs.clear();
    notifyListeners();
  }

  void _addDebugLog(String line) {
    if (!_debugMode) return;
    final now = DateTime.now();
    final ts = '${now.hour.toString().padLeft(2, '0')}:${now.minute.toString().padLeft(2, '0')}:${now.second.toString().padLeft(2, '0')}';
    _debugLogs.add('[$ts] $line');
    if (_debugLogs.length > 80) {
      _debugLogs.removeAt(0);
    }
  }
  List<SessionEntry> get sessions {
    _pruneIdleSessions();
    return List.unmodifiable(_sessions);
  }

  SessionEntry? get activeSession {
    if (_activeSessionId == null) return null;
    for (final s in _sessions) {
      if (s.id == _activeSessionId) return s;
    }
    return null;
  }

  void _pruneIdleSessions() {
    final cutoff = DateTime.now().subtract(const Duration(hours: 1));
    _sessions.removeWhere((s) => s.lastActivity.isBefore(cutoff));
    if (_activeSessionId != null &&
        !_sessions.any((s) => s.id == _activeSessionId)) {
      _activeSessionId = null;
    }
  }

  void openSession({
    required String id,
    required String project,
    required String title,
  }) {
    _pruneIdleSessions();
    if (_activeSessionId != null) {
      for (final s in _sessions) {
        if (s.id == _activeSessionId) {
          s.messages
            ..clear()
            ..addAll(_messages);
          s.lastActivity = DateTime.now();
          break;
        }
      }
    } else if (_messages.isNotEmpty) {
      final defEntry = SessionEntry(
        id: 'default',
        project: 'Principal',
        title: 'Chat inicial',
      )..messages.addAll(_messages);
      _sessions.insert(0, defEntry);
      if (_streamingTargetSessionId == null) {
        _streamingTargetSessionId = 'default';
      }
    }
    final existingIdx = _sessions.indexWhere((s) => s.id == id);
    if (existingIdx >= 0) {
      final s = _sessions[existingIdx];
      s.lastActivity = DateTime.now();
      s.hasUnread = false;
      _activeSessionId = s.id;
      _messages
        ..clear()
        ..addAll(s.messages);
    } else {
      final entry = SessionEntry(id: id, project: project, title: title);
      _sessions.insert(0, entry);
      _activeSessionId = entry.id;
      _messages.clear();
    }
    _activeStreamingMsgId = null;
    notifyListeners();
  }

  void killSession(String id) {
    _sessions.removeWhere((s) => s.id == id);
    if (_activeSessionId == id) {
      _activeSessionId = null;
      _messages.clear();
      _activeStreamingMsgId = null;
      stopExecution();
    }
    notifyListeners();
  }

  bool get updateAvailable => _updateAvailable;
  String get availableVersion => _availableVersion;

  /// Limpia los mensajes locales (al cambiar de proyecto o sesión /resume).
  void clearMessages() {
    _messages.clear();
    _activeStreamingMsgId = null;
    notifyListeners();
  }

  /// Guarda credenciales del Hub Relay y reconecta usando Gist Discovery.
  Future<void> configureHubRelay(HubCredentials creds) async {
    _hubCreds = creds;
    await HubCredentialsStore.save(creds);
    _retryAttempt = 0;
    await disconnect();
    await connect();
  }

  /// Actualiza endpoint de Tailscale y Bearer Token en caliente y reconecta.
  Future<void> updateCredentials({
    required String wsUrl,
    required String bearerToken,
    String? userId,
  }) async {
    _wsUrl = wsUrl.trim();
    _bearerToken = bearerToken.trim();
    await HubCredentialsStore.saveDirect(_wsUrl, _bearerToken);
    if (userId != null && userId.trim().isNotEmpty) {
      _userId = userId.trim();
    }
    _retryAttempt = 0;
    await disconnect();
    await connect();
  }

  /// Conecta al Hub Relay o al servidor Go configurado.
  Future<void> connect() async {
    if (_disposed || _isConnecting || isConnected) return;
    _manualDisconnect = false;
    _isConnecting = true;
    notifyListeners();

    try {
      _hubCreds ??= await HubCredentialsStore.load();
      if (_wsUrl.isEmpty) {
        final (url, tok) = await HubCredentialsStore.loadDirect();
        _wsUrl = url;
        if (_bearerToken.isEmpty) _bearerToken = tok;
      }
      final hubReady =
          _hubCreds != null && _hubCreds!.enabled && _hubCreds!.isComplete;
      if (!hubReady && _wsUrl.isEmpty) {
        // Nothing configured yet: wait for the settings sheet instead of retrying forever.
        _needsSetup = true;
        _isConnecting = false;
        _hubStatusText = 'Sin configurar';
        notifyListeners();
        return;
      }
      _needsSetup = false;
      WebSocket ws;
      if (hubReady) {
        _hubStatusText = 'Buscando Hub...';
        notifyListeners();
        final hubUrl = await GistDiscoveryService().fetchActiveHubUrl(
          gistId: _hubCreds!.gistId,
          gistToken: _hubCreds!.gistToken,
        );
        final clientWsUrl = GistDiscoveryService.toClientWsUrl(hubUrl);
        ws = await WebSocket.connect(
          clientWsUrl,
          headers: {
            'Authorization': 'Bearer ${_hubCreds!.hubSecret}',
          },
        ).timeout(connectTimeout);
        _hubStatusText = 'Conectado al Hub';
      } else {
        ws = await WebSocket.connect(
          _wsUrl,
          headers: {
            'Authorization': 'Bearer $_bearerToken',
          },
        ).timeout(connectTimeout);
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

    if (text == '/new') {
      _messages.clear();
      unawaited(_saveLocalHistory());
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
    final isControlCmd = text.startsWith('/cd ') ||
        text.startsWith('/resume ') ||
        text.startsWith('/model ') ||
        text == '/new' ||
        text == '/stop';
    if (!isControlCmd) {
      _activeStreamingMsgId = null;
      _streamingTargetSessionId = _activeSessionId;
      for (final s in _sessions) {
        if (s.id == (_activeSessionId ?? 'default')) {
          s.isWorking = true;
          s.hasUnread = false;
          break;
        }
      }
    }

    final payload = jsonEncode({
      'user_id': _userId,
      'text': text,
      'timestamp': now.millisecondsSinceEpoch ~/ 1000,
      'session_id': _activeSessionId ?? 'default',
    });

    try {
      _socket!.add(payload);
      _addDebugLog('→ send: ${text.length > 40 ? "${text.substring(0, 40)}…" : text}');
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
    _activeStreamingMsgId = null;
    _activity = AgentActivity.idle;
    _setCharacterState(CharacterState.idle);
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

  /// Sube múltiples fotos/capturas al servidor Go y envía un solo prompt con todas las rutas adjuntas.
  Future<bool> uploadAttachmentsWithPrompt(
    List<String> filePaths,
    String promptText,
  ) async {
    if (filePaths.isEmpty) {
      return sendPrompt(promptText);
    }
    try {
      final wsUri = Uri.parse(_wsUrl);
      final scheme = wsUri.scheme == 'wss' ? 'https' : 'http';
      final uploadUri = wsUri.replace(scheme: scheme, path: '/upload');

      final savedPaths = <String>[];
      for (final filePath in filePaths) {
        final req = http.MultipartRequest('POST', uploadUri);
        req.headers['Authorization'] = 'Bearer $_bearerToken';
        req.fields['target'] = 'cache';
        req.files.add(await http.MultipartFile.fromPath('file', filePath));

        final streamed = await req.send().timeout(const Duration(seconds: 30));
        final body = await streamed.stream.bytesToString();
        if (streamed.statusCode >= 400) {
          _lastError = 'Error subiendo captura (${streamed.statusCode})';
          notifyListeners();
          return false;
        }
        final decoded = jsonDecode(body) as Map<String, dynamic>;
        savedPaths.add(decoded['path'] as String? ?? filePath);
      }

      final header = savedPaths
          .map((p) => '[Imagen adjunta guardada en: $p]')
          .join('\n');
      final instr = promptText.trim().isNotEmpty
          ? promptText.trim()
          : 'Por favor analiza esta imagen.';
      return sendPrompt('$header\n\n$instr');
    } catch (err) {
      _lastError = err.toString();
      notifyListeners();
      return false;
    }
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
          text: 'Archivo `$fileName` guardado en:\n`$savedPath`',
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

  /// Envía una foto a `POST /avatar/from-photo` para que Gemini Vision extraiga
  /// los rasgos y devuelva 4 variantes del personaje animado con mini-laptop.
  Future<List<GeminiAvatarProfile>> generateAvatarVariantsFromPhoto(String filePath) async {
    try {
      final wsUri = Uri.parse(_wsUrl);
      final scheme = wsUri.scheme == 'wss' ? 'https' : 'http';
      final uri = wsUri.replace(scheme: scheme, path: '/avatar/from-photo');

      final req = http.MultipartRequest('POST', uri);
      req.headers['Authorization'] = 'Bearer $_bearerToken';
      req.files.add(await http.MultipartFile.fromPath('file', filePath));

      final streamed = await req.send().timeout(const Duration(seconds: 40));
      final body = await streamed.stream.bytesToString();
      if (streamed.statusCode >= 400) {
        _lastError = 'Error generando personaje (${streamed.statusCode})';
        notifyListeners();
        return const [];
      }
      final decoded = jsonDecode(body) as Map<String, dynamic>;
      final rawList = decoded['variants'] as List?;
      if (rawList != null && rawList.isNotEmpty) {
        return rawList
            .whereType<Map<String, dynamic>>()
            .map(GeminiAvatarProfile.fromJson)
            .toList();
      }
      final traits = decoded['traits'] as Map<String, dynamic>?;
      if (traits != null) {
        return [GeminiAvatarProfile.fromJson(traits)];
      }
      return const [];
    } catch (err) {
      _lastError = 'Error generando personaje con Gemini: $err';
      notifyListeners();
      return const [];
    }
  }

  void _onRawFrame(dynamic raw) {
    if (raw is! String || raw.trim().isEmpty) return;
    if (raw.contains('"worker_offline"')) {
      _hubStatusText = 'PC Desconectada / worker_offline';
      _lastError = _hubStatusText;
      notifyListeners();
    }
    try {
      final decoded = jsonDecode(raw);
      if (decoded is Map<String, dynamic> && decoded['type'] == 'hub_status') {
        _activeWorker = decoded['active_worker'] as String? ?? '';
        _addDebugLog('← hub_status: $_activeWorker');
        if (_activeWorker == 'pc') {
          _hubStatusText = 'PC conectada';
        } else if (_activeWorker == 'cel') {
          _hubStatusText = 'Cel Home (respaldo)';
        } else if (_activeWorker == 'offline') {
          _hubStatusText = 'PC Desconectada / worker_offline';
        }
        notifyListeners();
        return;
      }
      if (decoded is Map<String, dynamic> && decoded['type'] == 'options') {
        final data = decoded['data'];
        if (data is Map<String, dynamic> &&
            _optionsCompleter != null &&
            !_optionsCompleter!.isCompleted) {
          _optionsCompleter!.complete(data);
        }
        return;
      }
    } catch (_) {}
    try {
      final ev = AgentEvent.fromRawJson(raw);
      _addDebugLog('← ${ev.type.name}${ev.done ? " (done)" : ""}');
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
          _notifications.add(
            ChatMessage(
              id: 'n_${DateTime.now().microsecondsSinceEpoch}',
              isUser: false,
              text: '$title\n\n$body',
              timestamp: DateTime.now(),
            ),
          );
          if (_notifications.length > 50) {
            _notifications.removeAt(0);
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
          _activity = AgentActivity.idle;
          _setCharacterState(CharacterState.idle);
          break;

        case AgentEventType.agentStatus:
          if (ev.activity != null) {
            _activity = ev.activity!;
            notifyListeners();
          }
          break;

        case AgentEventType.permissionRequest:
          if (ev.message.isEmpty) break;
          if (ev.done) {
            _pendingPermissions.removeWhere((p) => p.id == ev.message);
            LocalNotifier.cancelPermission(ev.message);
          } else if (!_pendingPermissions.any((p) => p.id == ev.message)) {
            _pendingPermissions.add(PermissionRequest(
              id: ev.message,
              tool: ev.toolStatus,
              detail: ev.text,
              workspace: ev.title,
              receivedAt: DateTime.now(),
            ));
            LocalNotifier.showPermission(
              ev.message,
              ev.toolStatus,
              ev.text,
              ev.title,
            );
          }
          notifyListeners();
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

    final currentSid = _activeSessionId ?? 'default';
    final sid = ev.sessionId.isNotEmpty
        ? ev.sessionId
        : (_streamingTargetSessionId ?? currentSid);
    final targetList = (sid != currentSid)
        ? (_sessions
                .where((s) => s.id == sid)
                .firstOrNull
                ?.messages ??
            _messages)
        : _messages;

    final streamId = _sessionStreamingMsgIds[sid];
    if (streamId == null) {
      final id = 'a_${DateTime.now().microsecondsSinceEpoch}';
      if (!ev.done) {
        _sessionStreamingMsgIds[sid] = id;
      }
      targetList.add(
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
      final idx = targetList.lastIndexWhere((m) => m.id == streamId);
      if (idx != -1) {
        final prev = targetList[idx];
        targetList[idx] = prev.copyWith(
          text: ev.text.isNotEmpty ? ev.text : prev.text,
          toolStatus: ev.done ? '' : ev.toolStatus,
          isStreaming: !ev.done,
        );
      }
    }

    if (ev.done) {
      _sessionStreamingMsgIds.remove(sid);
      _activeStreamingMsgId = null;
      if (_streamingTargetSessionId == sid) {
        _streamingTargetSessionId = null;
      }
      for (final s in _sessions) {
        if (s.id == sid) {
          s.isWorking = false;
          if (sid != currentSid && !ev.text.startsWith('Proyecto cambiado a ') && !ev.text.startsWith('Sesión reanudada: ')) {
            s.hasUnread = true;
          }
          break;
        }
      }
    _streamingTargetSessionId = _activeSessionId;
      _activity = AgentActivity.idle;
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
    // The server only broadcasts once; after a reconnect these buttons could never resolve.
    for (final p in _pendingPermissions) {
      LocalNotifier.cancelPermission(p.id);
    }
    _pendingPermissions.clear();

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

  /// Navega la bóveda de Obsidian (`GET /obsidian?path=...`) con 0 tokens.
  /// Devuelve la lista de carpetas/archivos o el contenido del `.md`.
  Future<Map<String, dynamic>?> fetchObsidian({String relPath = ''}) async {
    try {
      final wsUri = Uri.parse(_wsUrl);
      final scheme = wsUri.scheme == 'wss' ? 'https' : 'http';
      final obsUri = wsUri.replace(
        scheme: scheme,
        path: '/obsidian',
        queryParameters: relPath.isEmpty ? null : {'path': relPath},
      );
      final res = await http
          .get(obsUri, headers: {'Authorization': 'Bearer $_bearerToken'})
          .timeout(const Duration(seconds: 6));
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
          .timeout(const Duration(minutes: 5));
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
    if (next == CharacterState.sleeping) {
      _activity = const AgentActivity(state: Avatar3DState.error, label: 'Reconectando · buscando túnel');
    } else if (_activity.state == Avatar3DState.error && _activity.label.startsWith('Reconectando')) {
      _activity = AgentActivity.idle;
    }
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
