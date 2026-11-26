import 'dart:async';

import 'package:flutter/material.dart';

import '../../services/hub_credentials_store.dart';
import '../../services/ws_client.dart';
import '../../theme/app_colors.dart';
import '../avatar3d/controllers/avatar_prefs_controller.dart';

const _fieldStyle = TextStyle(color: AppColors.text, fontSize: 14);

/// Assistant name plus direct and Hub connection settings.
void showConnectionSettingsSheet(
  BuildContext context,
  AgyWsClient client,
  AvatarPrefsController prefs,
) {
  final nameCtrl = TextEditingController(text: prefs.assistantName);
  final urlCtrl = TextEditingController(text: client.wsUrl);
  final tokCtrl = TextEditingController(text: client.bearerToken);
  final hub = client.hubCredentials;
  var hubEnabled = hub?.enabled ?? false;
  final gistIdCtrl = TextEditingController(text: hub?.gistId ?? '');
  final gistTokCtrl = TextEditingController(text: hub?.gistToken ?? '');
  final hubSecretCtrl = TextEditingController(text: hub?.hubSecret ?? '');

  void save() {
    prefs.setAssistantName(nameCtrl.text);
    final hubCreds = HubCredentials(
      enabled: hubEnabled,
      gistToken: gistTokCtrl.text.trim(),
      gistId: gistIdCtrl.text.trim(),
      hubSecret: hubSecretCtrl.text.trim(),
    );
    // Persist direct-mode values first, then reconnect through the hub when on.
    unawaited(() async {
      await client.updateCredentials(wsUrl: urlCtrl.text, bearerToken: tokCtrl.text);
      await client.configureHubRelay(hubCreds);
    }());
  }

  showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    backgroundColor: AppColors.sheet,
    shape: sheetShape,
    builder: (ctx) => StatefulBuilder(
      builder: (ctx, setSheetState) => Padding(
        padding: EdgeInsets.only(
          left: 20,
          right: 20,
          top: 20,
          bottom: MediaQuery.of(ctx).viewInsets.bottom + 24,
        ),
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const Text('Configuración del Asistente', style: sheetTitleStyle),
              const SizedBox(height: 14),
              TextField(
                controller: nameCtrl,
                style: _fieldStyle,
                decoration: appFieldDecoration('Nombre del asistente'),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: urlCtrl,
                style: _fieldStyle,
                decoration: appFieldDecoration('WebSocket URL (ws://100.x.y.z:8080/ws)'),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: tokCtrl,
                obscureText: true,
                style: _fieldStyle,
                decoration: appFieldDecoration('Bearer Token (MOBILE_WS_TOKEN)'),
              ),
              const SizedBox(height: 8),
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                value: hubEnabled,
                onChanged: (v) => setSheetState(() => hubEnabled = v),
                title: const Text('Conectar por Hub (Gist)', style: _fieldStyle),
                subtitle: const Text(
                  'Encuentra la PC aunque cambie de red',
                  style: TextStyle(color: AppColors.textMuted, fontSize: 12),
                ),
              ),
              if (hubEnabled) ...[
                const SizedBox(height: 8),
                TextField(
                  controller: gistIdCtrl,
                  style: _fieldStyle,
                  decoration: appFieldDecoration('Gist ID'),
                ),
                const SizedBox(height: 12),
                TextField(
                  controller: gistTokCtrl,
                  obscureText: true,
                  style: _fieldStyle,
                  decoration: appFieldDecoration('Token de GitHub (solo permiso gist)'),
                ),
                const SizedBox(height: 12),
                TextField(
                  controller: hubSecretCtrl,
                  obscureText: true,
                  style: _fieldStyle,
                  decoration: appFieldDecoration('Secreto del Hub'),
                ),
              ],
              const SizedBox(height: 16),
              FilledButton(
                style: FilledButton.styleFrom(
                  backgroundColor: AppColors.accent,
                  foregroundColor: const Color(0xFF0D1117),
                  shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
                ),
                onPressed: () {
                  Navigator.of(ctx).pop();
                  save();
                },
                child: const Text('Guardar y Reconectar', style: TextStyle(fontWeight: FontWeight.w600)),
              ),
            ],
          ),
        ),
      ),
    ),
  );
}
