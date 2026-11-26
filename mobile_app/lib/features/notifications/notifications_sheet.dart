import 'package:flutter/material.dart';

import '../../services/ws_client.dart';
import '../../theme/app_colors.dart';

/// Inbox of task-finished notifications, newest first.
void showNotificationsSheet(BuildContext context, AgyWsClient client) {
  client.markNotificationsRead();
  showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    backgroundColor: AppColors.sheet,
    shape: sheetShape,
    builder: (ctx) {
      final notifs = client.notifications.reversed.toList();
      return SafeArea(
        child: SizedBox(
          height: MediaQuery.of(ctx).size.height * 0.65,
          child: Column(
            children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(20, 14, 12, 8),
                child: Row(
                  children: [
                    const Expanded(
                      child: Text('Buzón de Tareas (AGY / Claude)', style: sheetTitleStyle),
                    ),
                    if (notifs.isNotEmpty)
                      TextButton(
                        onPressed: () {
                          client.clearNotifications();
                          Navigator.of(ctx).pop();
                        },
                        child: const Text('Limpiar', style: TextStyle(color: AppColors.danger)),
                      ),
                  ],
                ),
              ),
              const Divider(color: AppColors.divider, height: 1),
              Expanded(
                child: notifs.isEmpty
                    ? const Center(
                        child: Text(
                          'Sin notificaciones de terminal/IDE aún.',
                          style: TextStyle(color: AppColors.textMuted, fontSize: 13),
                        ),
                      )
                    : ListView.separated(
                        padding: const EdgeInsets.all(14),
                        itemCount: notifs.length,
                        separatorBuilder: (_, __) => const SizedBox(height: 10),
                        itemBuilder: (_, idx) => Container(
                          padding: const EdgeInsets.all(12),
                          decoration: BoxDecoration(
                            color: AppColors.card,
                            borderRadius: BorderRadius.circular(8),
                            border: Border.all(color: AppColors.divider),
                          ),
                          child: SelectableText(
                            notifs[idx].text,
                            style: const TextStyle(color: AppColors.text, fontSize: 13, height: 1.4),
                          ),
                        ),
                      ),
              ),
            ],
          ),
        ),
      );
    },
  );
}
