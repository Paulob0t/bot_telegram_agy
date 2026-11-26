import 'package:flutter/material.dart';

import 'screens/companion_screen.dart';
import 'services/notifications.dart';
import 'services/ws_client.dart';

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  LocalNotifier.init();

  const defaultUrl = String.fromEnvironment(
    'AGY_WS_URL',
    defaultValue: '',
  );
  const defaultToken = String.fromEnvironment(
    'AGY_WS_TOKEN',
    defaultValue: '',
  );

  final client = AgyWsClient(
    wsUrl: defaultUrl,
    bearerToken: defaultToken,
  );
  LocalNotifier.onPermissionAction = client.respondPermission;
  client.connect();

  runApp(PaulobotCompanionApp(client: client));
}

class PaulobotCompanionApp extends StatelessWidget {
  final AgyWsClient client;

  const PaulobotCompanionApp({
    super.key,
    required this.client,
  });

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'HyprAgent',
      debugShowCheckedModeBanner: false,
      theme: ThemeData.dark(useMaterial3: true).copyWith(
        scaffoldBackgroundColor: const Color(0xFF111315),
      ),
      home: CompanionScreen(client: client),
    );
  }
}
