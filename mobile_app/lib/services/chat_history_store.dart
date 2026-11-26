import 'dart:convert';
import 'dart:io';

import 'package:path_provider/path_provider.dart';

import '../models/agent_event.dart';
import 'ws_client.dart' show SessionEntry;

/// Keeps the last messages of the active chat on the phone between app restarts.
class ChatHistoryStore {
  static const _maxMessages = 80;

  static Future<File> _file() async {
    final dir = await getApplicationDocumentsDirectory();
    return File('${dir.path}/chat_history.json');
  }

  static Future<File> _sessionsFile() async {
    final dir = await getApplicationDocumentsDirectory();
    return File('${dir.path}/chat_sessions.json');
  }

  static List<Map<String, dynamic>> _encode(List<ChatMessage> messages) {
    final tail = messages.length > _maxMessages
        ? messages.sublist(messages.length - _maxMessages)
        : messages;
    return [
      for (final m in tail)
        {
          'id': m.id,
          'is_user': m.isUser,
          'text': m.text,
          'is_error': m.isError,
          'ts': m.timestamp.toIso8601String(),
        },
    ];
  }

  static List<ChatMessage> _decode(List<dynamic> list) {
    var i = 0;
    return [
      for (final item in list.cast<Map<String, dynamic>>())
        ChatMessage(
          id: item['id'] as String? ?? 'h_${i++}',
          isUser: item['is_user'] as bool? ?? false,
          text: item['text'] as String? ?? '',
          isError: item['is_error'] as bool? ?? false,
          timestamp:
              DateTime.tryParse(item['ts'] as String? ?? '') ?? DateTime.now(),
        ),
    ];
  }

  static Future<List<ChatMessage>> load() async {
    try {
      final f = await _file();
      if (!await f.exists()) return const [];
      return _decode(jsonDecode(await f.readAsString()) as List<dynamic>);
    } catch (_) {
      return const [];
    }
  }

  static Future<void> save(List<ChatMessage> messages) async {
    try {
      await (await _file()).writeAsString(
        jsonEncode(_encode(messages)),
        flush: true,
      );
    } catch (_) {}
  }

  /// Open chats survive Android killing the app while the phone is offline.
  static Future<({List<SessionEntry> sessions, String? activeId})>
  loadSessions() async {
    try {
      final f = await _sessionsFile();
      if (!await f.exists())
        return (sessions: <SessionEntry>[], activeId: null);
      final data = jsonDecode(await f.readAsString()) as Map<String, dynamic>;
      final sessions = [
        for (final item
            in (data['sessions'] as List<dynamic>).cast<Map<String, dynamic>>())
          SessionEntry(
            id: item['id'] as String,
            project: item['project'] as String? ?? '',
            title: item['title'] as String? ?? '',
            lastActivity: DateTime.tryParse(item['last'] as String? ?? ''),
            messages: _decode(item['messages'] as List<dynamic>? ?? const []),
          ),
      ];
      return (sessions: sessions, activeId: data['active'] as String?);
    } catch (_) {
      return (sessions: <SessionEntry>[], activeId: null);
    }
  }

  static Future<void> saveSessions(
    List<SessionEntry> sessions,
    String? activeId,
  ) async {
    try {
      final data = {
        'active': activeId,
        'sessions': [
          for (final s in sessions)
            {
              'id': s.id,
              'project': s.project,
              'title': s.title,
              'last': s.lastActivity.toIso8601String(),
              'messages': _encode(s.messages),
            },
        ],
      };
      await (await _sessionsFile()).writeAsString(
        jsonEncode(data),
        flush: true,
      );
    } catch (_) {}
  }
}
