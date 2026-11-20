import 'package:flutter/material.dart';

import 'screens/companion_screen.dart';
import 'services/ws_client.dart';

void main() {
  WidgetsFlutterBinding.ensureInitialized();

  const defaultUrl = String.fromEnvironment(
    'AGY_WS_URL',
    defaultValue: 'ws://127.0.0.1:8080/ws',
  );
  const defaultToken = String.fromEnvironment(
    'AGY_WS_TOKEN',
    defaultValue: '',
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
