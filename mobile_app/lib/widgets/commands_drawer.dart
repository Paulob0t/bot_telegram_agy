import 'package:flutter/material.dart';

import '../services/ws_client.dart';

/// Lateral drawer showing active sessions, Debug switch, quick slash commands, and avatar customizer.
class CommandsDrawer extends StatelessWidget {
  final AgyWsClient client;
  final VoidCallback onOpenObsidianVault;
  final ValueChanged<String> onOpenOptionPicker;
  final VoidCallback onOpenSettings;
  final VoidCallback onOpenAvatarCustomizer;

  const CommandsDrawer({
    super.key,
    required this.client,
    required this.onOpenObsidianVault,
    required this.onOpenOptionPicker,
    required this.onOpenAvatarCustomizer,
    required this.onOpenSettings,
  });

  @override
  Widget build(BuildContext context) {
    final commands = [
      (Icons.add_comment_outlined, 'Nuevo chat', '/new'),
      (Icons.menu_book_rounded, 'Obsidian · Prompts', '/obsidian'),
      (Icons.info_outline, 'Estado del sistema', '/status'),
      (Icons.memory, 'Cambiar modelo', '/model'),
      (Icons.data_usage, 'Cuotas de tokens', '/tokens'),
      (Icons.verified_user_outlined, 'Aprobar herramientas (on/off)', '/approvals'),
      (Icons.stop_circle_outlined, 'Detener ejecución', '/stop'),
    ];

    return Drawer(
      backgroundColor: const Color(0xFF15181C),
      child: SafeArea(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const Padding(
              padding: EdgeInsets.fromLTRB(20, 20, 20, 12),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    'Comandos rápidos',
                    style: TextStyle(
                      color: Color(0xFFE6EDF3),
                      fontSize: 16,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                  SizedBox(height: 2),
                  Text(
                    'Build $currentAppBuildNumber',
                    style: TextStyle(color: Color(0xFF6E7681), fontSize: 11),
                  ),
                ],
              ),
            ),
            SwitchListTile(
              dense: true,
              secondary: const Icon(
                Icons.smartphone_rounded,
                color: Color(0xFF3FB950),
                size: 20,
              ),
              title: const Text(
                'Conexión Cel Home',
                style: TextStyle(color: Color(0xFFE6EDF3), fontSize: 14),
              ),
              value: client.forceCelHome,
              activeThumbColor: const Color(0xFF3FB950),
              onChanged: (v) => client.setForceCelHome(v),
            ),
            SwitchListTile(
              dense: true,
              secondary: const Icon(
                Icons.bug_report_outlined,
                color: Color(0xFFD29922),
                size: 20,
              ),
              title: const Text(
                'Modo Debug',
                style: TextStyle(color: Color(0xFFE6EDF3), fontSize: 14),
              ),
              value: client.debugMode,
              activeThumbColor: const Color(0xFFD29922),
              onChanged: (v) => client.setDebugMode(v),
            ),
            const Divider(color: Color(0xFF21262D), height: 1),
            Expanded(
              child: ListView(
                padding: const EdgeInsets.symmetric(vertical: 8),
                children: [
                  if (client.sessions.isNotEmpty) ...[
                    const Padding(
                      padding: EdgeInsets.fromLTRB(16, 6, 16, 4),
                      child: Text(
                        'Sesiones activas (TTL 1h)',
                        style: TextStyle(
                          color: Color(0xFF8B949E),
                          fontSize: 11,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                    ),
                    for (final s in client.sessions)
                      ListTile(
                        dense: true,
                        leading: Icon(
                          Icons.terminal_rounded,
                          size: 18,
                          color: client.activeSession?.id == s.id
                              ? const Color(0xFF3FB950)
                              : const Color(0xFF8B949E),
                        ),
                        title: Text(
                          s.displayLabel,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(
                            color: Color(0xFFE6EDF3),
                            fontSize: 13,
                          ),
                        ),
                        trailing: Row(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            if (s.isWorking)
                              const Padding(
                                padding: EdgeInsets.only(right: 6),
                                child: SizedBox(
                                  width: 12,
                                  height: 12,
                                  child: CircularProgressIndicator(
                                    strokeWidth: 2,
                                    color: Color(0xFFD29922),
                                  ),
                                ),
                              )
                            else if (s.hasUnread)
                              Container(
                                margin: const EdgeInsets.only(right: 6),
                                padding: const EdgeInsets.symmetric(
                                  horizontal: 6,
                                  vertical: 2,
                                ),
                                decoration: BoxDecoration(
                                  color: const Color(0xFF3FB950)
                                      .withValues(alpha: 0.2),
                                  borderRadius: BorderRadius.circular(6),
                                  border: Border.all(
                                    color: const Color(0xFF3FB950),
                                  ),
                                ),
                                child: const Text(
                                  'Listo',
                                  style: TextStyle(
                                    color: Color(0xFF3FB950),
                                    fontSize: 10,
                                    fontWeight: FontWeight.w700,
                                  ),
                                ),
                              ),
                            IconButton(
                              tooltip: 'Terminar sesión y proceso',
                              icon: const Icon(
                                Icons.close,
                                size: 16,
                                color: Color(0xFFF85149),
                              ),
                              onPressed: () => client.killSession(s.id),
                            ),
                          ],
                        ),
                        onTap: () {
                          Navigator.of(context).pop();
                          client.openSession(
                            id: s.id,
                            project: s.project,
                            title: s.title,
                          );
                        },
                      ),
                    const Divider(color: Color(0xFF21262D), height: 12),
                  ],
                  for (final item in commands)
                    ListTile(
                      leading: Icon(
                        item.$1,
                        color: const Color(0xFF8B949E),
                        size: 20,
                      ),
                      title: Text(
                        item.$2,
                        style: const TextStyle(
                          color: Color(0xFFE6EDF3),
                          fontSize: 14,
                        ),
                      ),
                      onTap: () {
                        Navigator.of(context).pop();
                        if (item.$3 == '/stop') {
                          client.stopExecution();
                        } else if (item.$3 == '/obsidian') {
                          onOpenObsidianVault();
                        } else if (item.$3 == '/model' ||
                            item.$3 == '/new' ||
                            item.$3 == '/resume') {
                          onOpenOptionPicker(item.$3);
                        } else {
                          client.sendPrompt(item.$3);
                        }
                      },
                    ),
                  const Divider(color: Color(0xFF21262D), height: 16),
                  ListTile(
                    leading: const Icon(
                      Icons.face_retouching_natural,
                      color: Color(0xFF8B949E),
                      size: 20,
                    ),
                    title: const Text(
                      'Personalizar personaje',
                      style: TextStyle(color: Color(0xFFE6EDF3), fontSize: 14),
                    ),
                    subtitle: const Text(
                      'Estilo, accesorios, color o .riv',
                      style: TextStyle(color: Color(0xFF6E7681), fontSize: 12),
                    ),
                    onTap: () {
                      Navigator.of(context).pop();
                      onOpenAvatarCustomizer();
                    },
                  ),
                  ListTile(
                    leading: const Icon(
                      Icons.settings_outlined,
                      color: Color(0xFF8B949E),
                      size: 20,
                    ),
                    title: const Text(
                      'Ajustes y conexión',
                      style: TextStyle(color: Color(0xFFE6EDF3), fontSize: 14),
                    ),
                    onTap: () {
                      Navigator.of(context).pop();
                      onOpenSettings();
                    },
                  ),
                  if (client.updateAvailable) ...[
                    const Divider(color: Color(0xFF21262D), height: 16),
                    ListTile(
                      leading: const Icon(
                        Icons.system_update,
                        color: Color(0xFF3FB950),
                        size: 20,
                      ),
                      title: Text(
                        'Actualizar App (${client.availableVersion})',
                        style: const TextStyle(
                          color: Color(0xFF3FB950),
                          fontSize: 14,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                      subtitle: const Text(
                        'Nueva versión disponible por Tailscale',
                        style: TextStyle(
                          color: Color(0xFF8B949E),
                          fontSize: 11,
                        ),
                      ),
                      onTap: () {
                        Navigator.of(context).pop();
                        client.installAvailableUpdate();
                      },
                    ),
                  ],
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}
