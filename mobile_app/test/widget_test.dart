import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:paulobot_mobile/main.dart';
import 'package:paulobot_mobile/models/agent_event.dart';
import 'package:paulobot_mobile/services/ws_client.dart';
import 'package:paulobot_mobile/widgets/rive_avatar.dart';

void main() {
  testWidgets('CompanionScreen renderiza RiveCompanionAvatar y estado sleeping cuando PC está offline',
      (WidgetTester tester) async {
    final client = AgyWsClient(
      wsUrl: 'ws://100.64.0.1:8080/ws',
      bearerToken: 'test-token',
    );

    await tester.pumpWidget(PaulobotCompanionApp(client: client));

    expect(find.byType(RiveCompanionAvatar), findsOneWidget);
    expect(find.textContaining('Paulobot · AGY Core'), findsOneWidget);
    expect(find.textContaining('PC suspendida / desconectada'), findsOneWidget);
    expect(client.characterState, CharacterState.sleeping);

    // Abrir hoja de configuración de Tailscale
    await tester.tap(find.byIcon(Icons.tune));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 350));

    expect(find.text('Nodo Tailscale (Go Core)'), findsOneWidget);
    expect(find.text('Guardar y Reconectar'), findsOneWidget);

    client.dispose();
  });
}
