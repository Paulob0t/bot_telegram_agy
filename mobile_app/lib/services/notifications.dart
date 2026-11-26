import 'package:flutter_local_notifications/flutter_local_notifications.dart';

class LocalNotifier {
  static final FlutterLocalNotificationsPlugin _plugin =
      FlutterLocalNotificationsPlugin();
  static bool _initialized = false;
  static int _seq = 1;

  static const _actionAllow = 'perm_allow';
  static const _actionDeny = 'perm_deny';

  /// Wired by main(): sends the decision over the live WebSocket.
  static bool Function(String id, bool allow)? onPermissionAction;

  static Future<void> init() async {
    if (_initialized) return;
    try {
      const androidInit = AndroidInitializationSettings('@drawable/ic_notification');
      const initSettings = InitializationSettings(android: androidInit);
      await _plugin.initialize(
        initSettings,
        onDidReceiveNotificationResponse: _onResponse,
      );

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

  static void _onResponse(NotificationResponse r) {
    final id = r.payload;
    if (id == null || id.isEmpty) return;
    if (r.actionId == _actionAllow) {
      onPermissionAction?.call(id, true);
    } else if (r.actionId == _actionDeny) {
      onPermissionAction?.call(id, false);
    }
  }

  // Stable per-request id so the notification can be cancelled once answered elsewhere.
  static int _permissionNotificationId(String id) =>
      0x40000000 | (id.hashCode & 0x3FFFFFFF);

  static Future<void> showPermission(
    String id,
    String tool,
    String detail,
    String workspace,
  ) async {
    if (!_initialized) return;
    try {
      final title = workspace.isEmpty
          ? 'Claude quiere usar $tool'
          : 'Claude quiere usar $tool · $workspace';
      final androidDetails = AndroidNotificationDetails(
        'paulobot_approvals',
        'Aprobaciones de herramientas',
        channelDescription: 'Claude espera tu permiso para ejecutar algo en tu PC',
        icon: '@drawable/ic_notification',
        importance: Importance.max,
        priority: Priority.max,
        category: AndroidNotificationCategory.reminder,
        ongoing: true,
        autoCancel: false,
        styleInformation: BigTextStyleInformation(detail, contentTitle: title),
        actions: const [
          AndroidNotificationAction(
            _actionAllow,
            'Aprobar',
            showsUserInterface: true,
            cancelNotification: true,
          ),
          AndroidNotificationAction(
            _actionDeny,
            'Rechazar',
            showsUserInterface: true,
            cancelNotification: true,
          ),
        ],
      );
      await _plugin.show(
        _permissionNotificationId(id),
        title,
        detail,
        NotificationDetails(android: androidDetails),
        payload: id,
      );
    } catch (_) {}
  }

  static Future<void> cancelPermission(String id) async {
    if (!_initialized) return;
    try {
      await _plugin.cancel(_permissionNotificationId(id));
    } catch (_) {}
  }
}
