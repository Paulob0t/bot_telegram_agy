import 'dart:convert';
import 'dart:io';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:paulobot_mobile/models/agent_event.dart';
import 'package:paulobot_mobile/services/ws_client.dart';

void main() {
  // Direct mode: with the hub enabled (the default) connect() would look up a Gist over the network.
  setUp(() => FlutterSecureStorage.setMockInitialValues({'hub_relay_enabled': 'false'}));

  test('AgyWsClient inicia en sleeping y maneja ciclo completo sobre WebSocket', () async {
    late HttpServer server;
    String? receivedAuth;
    final serverClients = <WebSocket>[];

    server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    server.listen((HttpRequest req) async {
      receivedAuth = req.headers.value('authorization');
      if (receivedAuth != 'Bearer token-secreto') {
        req.response.statusCode = HttpStatus.unauthorized;
        await req.response.close();
        return;
      }
      if (req.uri.path == '/version') {
        req.response.statusCode = HttpStatus.ok;
        req.response.write(jsonEncode({'ok': true, 'build': 1, 'apk_exists': false}));
        await req.response.close();
        return;
      }
      final ws = await WebSocketTransformer.upgrade(req);
      serverClients.add(ws);

      // Estado inicial al conectar
      ws.add(jsonEncode({'type': 'state', 'state': 'idle'}));

      ws.listen((dynamic data) {
        final decoded = jsonDecode(data as String) as Map<String, dynamic>;
        if (decoded['text'] == 'hola rive') {
          ws.add(jsonEncode({'type': 'state', 'state': 'thinking'}));
          ws.add(jsonEncode({
            'type': 'chunk',
            'text': 'Hola ',
            'tool_status': '⚙ *Comando:* `ls`',
            'done': false,
          }));
          ws.add(jsonEncode({'type': 'state', 'state': 'speaking'}));
          ws.add(jsonEncode({
            'type': 'chunk',
            'text': 'Hola desde Go en Tailscale',
            'done': true,
          }));
          ws.add(jsonEncode({'type': 'state', 'state': 'idle'}));
        }
      });
    });

    final client = AgyWsClient(
      wsUrl: 'ws://127.0.0.1:${server.port}/ws',
      bearerToken: 'token-secreto',
      maxBackoffSeconds: 2,
    );

    // Antes de conectar, la PC se asume offline -> sleeping
    expect(client.characterState, CharacterState.sleeping);
    expect(client.isConnected, isFalse);

    await client.connect();
    await Future<void>.delayed(const Duration(milliseconds: 80));

    expect(client.isConnected, isTrue);
    expect(receivedAuth, 'Bearer token-secreto');
    expect(client.characterState, CharacterState.idle);

    // Simular que el usuario escribe -> listening
    client.setUserListening(true);
    expect(client.characterState, CharacterState.listening);

    // Enviar mensaje al servidor Go
    final sent = client.sendPrompt('hola rive');
    expect(sent, isTrue);

    await Future<void>.delayed(const Duration(milliseconds: 120));

    expect(client.messages.length, 2);
    expect(client.messages.first.isUser, isTrue);
    expect(client.messages.first.text, 'hola rive');
    expect(client.messages.last.isUser, isFalse);
    expect(client.messages.last.text, 'Hola desde Go en Tailscale');
    expect(client.messages.last.isStreaming, isFalse);
    expect(client.characterState, CharacterState.idle);

    // Simular suspensión/apagado de la PC cerrando el servidor
    for (final c in serverClients) {
      await c.close();
    }
    await server.close(force: true);
    await Future<void>.delayed(const Duration(milliseconds: 80));

    // Debe pasar automáticamente a sleeping y programar reconexión exponencial
    expect(client.isConnected, isFalse);
    expect(client.characterState, CharacterState.sleeping);
    expect(client.retryAttempt, greaterThanOrEqualTo(1));

    client.dispose();
  });
}
