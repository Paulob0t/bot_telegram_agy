import 'package:flutter/material.dart';

import '../models/agent_event.dart';
import '../theme/app_colors.dart';

/// Small pill under the avatar: thinking, replying, typing or PC offline.
/// Tap toggles the full text of long tool statuses.
class StatusBubble extends StatefulWidget {
  final CharacterState state;

  /// Live tool status of the last reply, shown instead of "Pensando...".
  final String liveTool;

  const StatusBubble({super.key, required this.state, this.liveTool = ''});

  @override
  State<StatusBubble> createState() => _StatusBubbleState();
}

class _StatusBubbleState extends State<StatusBubble> {
  bool _expanded = false;

  @override
  Widget build(BuildContext context) {
    final state = widget.state;
    final liveTool = widget.liveTool;
    final (String label, Color dotColor) = switch (state) {
      CharacterState.thinking => (
        liveTool.isNotEmpty ? liveTool : 'Pensando...',
        AppColors.warning,
      ),
      CharacterState.speaking => ('Respondiendo...', AppColors.success),
      CharacterState.listening => ('Escribiendo...', AppColors.info),
      CharacterState.sleeping => ('PC offline', AppColors.textFaint),
      CharacterState.idle => ('', Colors.transparent),
    };
    if (state == CharacterState.idle) return const SizedBox.shrink();

    return GestureDetector(
      onTap: () => setState(() => _expanded = !_expanded),
      child: AnimatedContainer(
        duration: const Duration(milliseconds: 180),
        constraints: BoxConstraints(maxWidth: _expanded ? 320 : 220),
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
        decoration: BoxDecoration(
          color: AppColors.sheetDeep,
          borderRadius: BorderRadius.circular(12),
          border: Border.all(color: dotColor.withValues(alpha: 0.45)),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: _expanded
              ? CrossAxisAlignment.start
              : CrossAxisAlignment.center,
          children: [
            Container(
              width: 7,
              height: 7,
              decoration: BoxDecoration(
                color: dotColor,
                shape: BoxShape.circle,
              ),
            ),
            const SizedBox(width: 6),
            Flexible(
              child: Text(
                label,
                maxLines: _expanded ? 6 : 1,
                overflow: TextOverflow.ellipsis,
                style: const TextStyle(color: AppColors.text, fontSize: 11),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
