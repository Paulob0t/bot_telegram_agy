import 'package:flutter/material.dart';

import '../models/agent_event.dart';
import '../services/ws_client.dart';

/// Shows the oldest pending tool approval with Approve / Reject buttons.
class PermissionRequestCard extends StatelessWidget {
  final AgyWsClient client;

  const PermissionRequestCard({super.key, required this.client});

  @override
  Widget build(BuildContext context) {
    final pending = client.pendingPermissions;
    if (pending.isEmpty) return const SizedBox.shrink();
    final req = pending.first;

    return Container(
      width: double.infinity,
      margin: const EdgeInsets.fromLTRB(12, 4, 12, 6),
      padding: const EdgeInsets.fromLTRB(12, 10, 12, 8),
      decoration: BoxDecoration(
        color: const Color(0xFF2A2112),
        borderRadius: BorderRadius.circular(10),
        border: Border.all(color: const Color(0xFFD29922)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(
            _title(req, pending.length),
            style: const TextStyle(
              color: Color(0xFFE6EDF3),
              fontSize: 13,
              fontWeight: FontWeight.w600,
            ),
          ),
          if (req.detail.isNotEmpty) ...[
            const SizedBox(height: 6),
            Container(
              width: double.infinity,
              constraints: const BoxConstraints(maxHeight: 120),
              padding: const EdgeInsets.all(8),
              decoration: BoxDecoration(
                color: const Color(0xFF111315),
                borderRadius: BorderRadius.circular(6),
              ),
              child: SingleChildScrollView(
                child: SelectableText(
                  req.detail,
                  style: const TextStyle(
                    color: Color(0xFFE6EDF3),
                    fontFamily: 'monospace',
                    fontSize: 12,
                  ),
                ),
              ),
            ),
          ],
          const SizedBox(height: 6),
          Row(
            mainAxisAlignment: MainAxisAlignment.end,
            children: [
              TextButton(
                onPressed: () => _answer(context, req.id, false),
                style: TextButton.styleFrom(
                  foregroundColor: const Color(0xFFF85149),
                  minimumSize: const Size(48, 44),
                ),
                child: const Text('Rechazar'),
              ),
              const SizedBox(width: 8),
              FilledButton(
                onPressed: () => _answer(context, req.id, true),
                style: FilledButton.styleFrom(
                  backgroundColor: const Color(0xFF238636),
                  minimumSize: const Size(48, 44),
                ),
                child: const Text('Aprobar'),
              ),
            ],
          ),
        ],
      ),
    );
  }

  String _title(PermissionRequest req, int count) {
    final where = req.workspace.isEmpty ? '' : ' en ${req.workspace}';
    final more = count > 1 ? ' (+${count - 1} en cola)' : '';
    return 'Claude quiere usar ${req.tool}$where$more';
  }

  void _answer(BuildContext context, String id, bool allow) {
    if (!client.respondPermission(id, allow)) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Sin conexión con la PC; no se envió la respuesta')),
      );
    }
  }
}
