import 'package:flutter/material.dart';

import '../services/ws_client.dart';

/// Displays the background-session completion banner and optional live debug log strip.
class SessionStatusBanners extends StatelessWidget {
  final AgyWsClient client;

  const SessionStatusBanners({super.key, required this.client});

  @override
  Widget build(BuildContext context) {
    final unread = client.sessions.where((s) => s.hasUnread).toList();
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        if (unread.isNotEmpty)
          Container(
            width: double.infinity,
            margin: const EdgeInsets.fromLTRB(12, 2, 12, 4),
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
            decoration: BoxDecoration(
              color: const Color(0xFF16261B),
              borderRadius: BorderRadius.circular(8),
              border: Border.all(
                color: const Color(0xFF3FB950).withValues(alpha: 0.5),
              ),
            ),
            child: Row(
              children: [
                const Icon(
                  Icons.check_circle_rounded,
                  color: Color(0xFF3FB950),
                  size: 16,
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    'Terminó en ${unread.first.displayLabel}',
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(
                      color: Color(0xFFE6EDF3),
                      fontSize: 12,
                      fontWeight: FontWeight.w500,
                    ),
                  ),
                ),
                TextButton(
                  onPressed: () => client.openSession(
                    id: unread.first.id,
                    project: unread.first.project,
                    title: unread.first.title,
                  ),
                  style: TextButton.styleFrom(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 10,
                      vertical: 2,
                    ),
                    minimumSize: Size.zero,
                    tapTargetSize: MaterialTapTargetSize.shrinkWrap,
                    foregroundColor: const Color(0xFF3FB950),
                  ),
                  child: const Text(
                    'Ver',
                    style: TextStyle(fontSize: 12, fontWeight: FontWeight.w700),
                  ),
                ),
              ],
            ),
          ),
        if (client.debugMode)
          Container(
            width: double.infinity,
            constraints: const BoxConstraints(maxHeight: 96),
            margin: const EdgeInsets.symmetric(horizontal: 12, vertical: 2),
            padding: const EdgeInsets.all(8),
            decoration: BoxDecoration(
              color: const Color(0xFF0D1117),
              borderRadius: BorderRadius.circular(8),
              border: Border.all(
                color: const Color(0xFFD29922).withValues(alpha: 0.4),
              ),
            ),
            child: ListView(
              reverse: true,
              shrinkWrap: true,
              children: client.debugLogs.reversed
                  .map(
                    (l) => Text(
                      l,
                      style: const TextStyle(
                        color: Color(0xFF8B949E),
                        fontSize: 10,
                        fontFamily: 'monospace',
                      ),
                    ),
                  )
                  .toList(),
            ),
          ),
      ],
    );
  }
}
