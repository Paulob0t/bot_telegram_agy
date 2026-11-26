import 'package:flutter/material.dart';

/// Compact read-only pill showing which worker node is currently active (`PC`, `Cel Home`, or `Offline`).
class WorkerStatusPill extends StatelessWidget {
  final String activeWorker;

  const WorkerStatusPill({super.key, required this.activeWorker});

  @override
  Widget build(BuildContext context) {
    if (activeWorker.isEmpty) return const SizedBox.shrink();

    final color = activeWorker == 'pc'
        ? const Color(0xFF3FB950)
        : activeWorker == 'cel'
            ? const Color(0xFFD29922)
            : const Color(0xFFF85149);
    final label = activeWorker == 'pc'
        ? 'PC'
        : activeWorker == 'cel'
            ? 'Cel Home'
            : 'Offline';

    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      decoration: BoxDecoration(
        color: const Color(0xFF1A1D21),
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: color.withValues(alpha: 0.5)),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(
            width: 7,
            height: 7,
            decoration: BoxDecoration(shape: BoxShape.circle, color: color),
          ),
          const SizedBox(width: 5),
          Text(
            label,
            style: const TextStyle(
              color: Color(0xFFE6EDF3),
              fontSize: 11,
              fontWeight: FontWeight.w600,
            ),
          ),
        ],
      ),
    );
  }
}
