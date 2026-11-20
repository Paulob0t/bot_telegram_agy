import 'package:flutter/material.dart';

import 'screens/companion_screen.dart';
import 'services/notifications.dart';
import 'services/ws_client.dart';

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  LocalNotifier.init();

  const defaultUrl = String.fromEnvironment(
    'AGY_WS_URL',
    defaultValue: 'ws://100.90.208.26:8080/ws',
  );
  const defaultToken = String.fromEnvironment(
    'AGY_WS_TOKEN',
    defaultValue: 'agy_ws_0f7f9d32ac8e0cd248569b8c43f0ada3',
  );

  final client = AgyWsClient(
    wsUrl: defaultUrl,
    bearerToken: defaultToken,
  );
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
      title: 'Paulobot AGY',
      debugShowCheckedModeBanner: false,
      theme: ThemeData.dark(useMaterial3: true).copyWith(
        scaffoldBackgroundColor: const Color(0xFF111315),
      ),
      home: CompanionScreen(client: client),
    );
  }
}
