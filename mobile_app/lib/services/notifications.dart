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

  static Future<void> show(String title, String body) async {
    if (!_initialized) return;
    try {
      const androidDetails = AndroidNotificationDetails(
        'paulobot_tasks',
        'Tareas AGY y Claude',
        channelDescription: 'Notificaciones cuando termina una tarea en tu PC',
        icon: '@drawable/ic_notification',
        importance: Importance.high,
        priority: Priority.high,
      );
      const details = NotificationDetails(android: androidDetails);
      final cleanBody = body.length > 240 ? '${body.substring(0, 240)}…' : body;
      await _plugin.show(_seq++, title, cleanBody, details);
    } catch (_) {}
  }
}
