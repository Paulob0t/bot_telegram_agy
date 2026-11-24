import 'package:flutter_local_notifications/flutter_local_notifications.dart';

class LocalNotifier {
  static final FlutterLocalNotificationsPlugin _plugin =
      FlutterLocalNotificationsPlugin();
  static bool _initialized = false;
  static int _seq = 1;

  static Future<void> init() async {
    if (_initialized) return;
    try {
      const androidInit = AndroidInitializationSettings('@drawable/ic_notification');
      const initSettings = InitializationSettings(android: androidInit);
      await _plugin.initialize(initSettings);

      final androidImpl = _plugin.resolvePlatformSpecificImplementation<
          AndroidFlutterLocalNotificationsPlugin>();
      await androidImpl?.requestNotificationsPermission();
      _initialized = true;
    } catch (_) {
      // Ignorar en entornos de test sin canal de plataforma Android
    }
  }

  /// Strips raw Markdown (`**`, `*`, `` ` ``, `[label](url)`, headings) so system
  /// push notifications read cleanly without syntax noise.
  static String cleanNotificationText(String raw) {
    var s = raw;
    s = s.replaceAllMapped(
      RegExp(r'\[([^\]]+)\]\([^\)]+\)'),
      (m) => m.group(1) ?? '',
    );
    s = s.replaceAll('**', '');
    s = s.replaceAll('`', '');
    s = s.replaceAll(RegExp(r'^#{1,4}\s+', multiLine: true), '');
    s = s.replaceAll(RegExp(r'^\s*[-*]\s+', multiLine: true), '• ');
    s = s.replaceAllMapped(
      RegExp(r'(?<!\w)\*([^\*\n]+)\*(?!\w)'),
      (m) => m.group(1) ?? '',
    );
    return s.trim();
  }

  static Future<void> show(String title, String body) async {
    if (!_initialized) return;
    try {
      final cleanTitle = cleanNotificationText(title);
      final cleanFull = cleanNotificationText(body);
      final expandedText =
          cleanFull.length > 700 ? '${cleanFull.substring(0, 700)}…' : cleanFull;
      final collapsedText =
          cleanFull.length > 140 ? '${cleanFull.substring(0, 140)}…' : cleanFull;

      final androidDetails = AndroidNotificationDetails(
        'paulobot_tasks',
        'Tareas AGY y Claude',
        channelDescription: 'Notificaciones cuando termina una tarea en tu PC',
        icon: '@drawable/ic_notification',
        importance: Importance.high,
        priority: Priority.high,
        styleInformation: BigTextStyleInformation(
          expandedText,
          contentTitle: cleanTitle,
          summaryText: 'HyprAgent',
        ),
      );
      final details = NotificationDetails(android: androidDetails);
      await _plugin.show(_seq++, cleanTitle, collapsedText, details);
    } catch (_) {}
  }
}
