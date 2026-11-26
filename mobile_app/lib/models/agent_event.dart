import 'dart:convert';

import '../features/avatar3d/models/avatar3d_state.dart';

/// Estados de la State Machine de Rive.
/// Cuando la PC está apagada, suspendida o sin red Tailscale, pasa a [sleeping].
enum CharacterState {
  idle,
  listening,
  thinking,
  speaking,
  sleeping;

  static CharacterState fromWire(String? raw) {
    switch (raw?.toLowerCase().trim()) {
      case 'listening':
        return CharacterState.listening;
      case 'thinking':
        return CharacterState.thinking;
      case 'speaking':
        return CharacterState.speaking;
      case 'sleeping':
        return CharacterState.sleeping;
      case 'idle':
      default:
        return CharacterState.idle;
    }
  }
}

enum AgentEventType {
  state,
  chunk,
  notify,
  error,
  agentStatus,
  permissionRequest,
  unknown,
}

/// Evento recibido desde el servidor Go sobre WebSocket.
class AgentEvent {
  final AgentEventType type;
  final CharacterState? state;
  final String title;
  final String text;
  final String toolStatus;
  final bool done;
  final String message;
  final String sessionId;
  final AgentActivity? activity;

  const AgentEvent({
    this.activity,
    required this.type,
    this.state,
    this.title = '',
    this.text = '',
    this.toolStatus = '',
    this.done = false,
    this.message = '',
    this.sessionId = '',
  });

  factory AgentEvent.fromJson(Map<String, dynamic> json) {
    final rawType = (json['type'] as String? ?? '').toLowerCase().trim();
    final sid = json['session_id'] as String? ?? '';
    switch (rawType) {
      case 'state':
        return AgentEvent(
          type: AgentEventType.state,
          state: CharacterState.fromWire(json['state'] as String?),
          sessionId: sid,
        );
      case 'chunk':
        return AgentEvent(
          type: AgentEventType.chunk,
          text: json['text'] as String? ?? '',
          toolStatus: json['tool_status'] as String? ?? '',
          done: json['done'] as bool? ?? false,
          sessionId: sid,
        );
      case 'notify':
        return AgentEvent(
          type: AgentEventType.notify,
          title: json['title'] as String? ?? 'HyprAgent',
          text: json['text'] as String? ?? '',
          done: true,
          sessionId: sid,
        );
      case 'agent_status':
        return AgentEvent(
          type: AgentEventType.agentStatus,
          activity: AgentActivity.fromJson(json),
          sessionId: sid,
        );
      // title = workspace, text = command/file, toolStatus = tool, message = request id.
      // done = true means the request was answered or expired elsewhere.
      case 'permission_request':
        return AgentEvent(
          type: AgentEventType.permissionRequest,
          title: json['title'] as String? ?? '',
          text: json['detail'] as String? ?? '',
          toolStatus: json['label'] as String? ?? '',
          message: json['message'] as String? ?? '',
          done: json['done'] as bool? ?? false,
        );
      case 'error':
        return AgentEvent(
          type: AgentEventType.error,
          message: json['message'] as String? ?? 'Error desconocido',
          sessionId: sid,
        );
      default:
        return const AgentEvent(type: AgentEventType.unknown);
    }
  }

  factory AgentEvent.fromRawJson(String raw) {
    final decoded = jsonDecode(raw) as Map<String, dynamic>;
    return AgentEvent.fromJson(decoded);
  }
}

/// Tool call waiting for approval from the phone.
class PermissionRequest {
  final String id;
  final String tool;
  final String detail;
  final String workspace;
  final DateTime receivedAt;

  const PermissionRequest({
    required this.id,
    required this.tool,
    required this.detail,
    required this.workspace,
    required this.receivedAt,
  });
}

/// Mensaje en el historial conversacional del cliente móvil.
class ChatMessage {
  final String id;
  final bool isUser;
  final String text;
  final String toolStatus;
  final bool isStreaming;
  final bool isError;
  final DateTime timestamp;

  const ChatMessage({
    required this.id,
    required this.isUser,
    required this.text,
    this.toolStatus = '',
    this.isStreaming = false,
    this.isError = false,
    required this.timestamp,
  });

  ChatMessage copyWith({
    String? text,
    String? toolStatus,
    bool? isStreaming,
    bool? isError,
  }) {
    return ChatMessage(
      id: id,
      isUser: isUser,
      text: text ?? this.text,
      toolStatus: toolStatus ?? this.toolStatus,
      isStreaming: isStreaming ?? this.isStreaming,
      isError: isError ?? this.isError,
      timestamp: timestamp,
    );
  }
}
