import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../models/avatar3d_state.dart';

const _surface = Color(0xFF15181C);
const _border = Color(0xFF262B32);
const _text = Color(0xFFE6EDF3);
const _muted = Color(0xFF8B949E);

Color avatarAccentForState(Avatar3DState state) {
  switch (state) {
    case Avatar3DState.starting:
    case Avatar3DState.thinking:
      return const Color(0xFFD29922);
    case Avatar3DState.reading:
      return const Color(0xFF58A6FF);
    case Avatar3DState.codingEdit:
    case Avatar3DState.wrappingUp:
      return const Color(0xFFD97757);
    case Avatar3DState.executing:
    case Avatar3DState.success:
      return const Color(0xFF3FB950);
    case Avatar3DState.error:
      return const Color(0xFFF85149);
    case Avatar3DState.awaitingApproval:
      return const Color(0xFFE3B341);
    case Avatar3DState.idle:
      return const Color(0xFF8B949E);
  }
}

IconData avatarIconForState(Avatar3DState state) {
  switch (state) {
    case Avatar3DState.starting:
      return Icons.bolt_rounded;
    case Avatar3DState.thinking:
      return Icons.psychology_alt_rounded;
    case Avatar3DState.reading:
      return Icons.manage_search_rounded;
    case Avatar3DState.codingEdit:
      return Icons.code_rounded;
    case Avatar3DState.executing:
      return Icons.terminal_rounded;
    case Avatar3DState.wrappingUp:
      return Icons.edit_note_rounded;
    case Avatar3DState.success:
      return Icons.check_rounded;
    case Avatar3DState.error:
      return Icons.warning_amber_rounded;
    case Avatar3DState.awaitingApproval:
      return Icons.front_hand_rounded;
    case Avatar3DState.idle:
      return Icons.circle_outlined;
  }
}

/// Clean status pill with pulsing status dot, vector micro-icon, and two-tone
/// typography (`Action · target`) with zero emoji clutter.
class CleanStatusPill extends StatefulWidget {
  final AgentActivity activity;

  const CleanStatusPill({super.key, required this.activity});

  @override
  State<CleanStatusPill> createState() => _CleanStatusPillState();
}

class _CleanStatusPillState extends State<CleanStatusPill>
    with SingleTickerProviderStateMixin {
  late final AnimationController _pulse;

  @override
  void initState() {
    super.initState();
    _pulse = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 1100),
    )..repeat(reverse: true);
  }

  @override
  void dispose() {
    _pulse.dispose();
    super.dispose();
  }

  (String, String) _splitLabel(String raw) {
    final cleaned = raw
        .replaceAll(
          RegExp(r'^[\u{1F300}-\u{1FAFF}\u{2600}-\u{27BF}\u{FE0F}◈⚙✓]+\s*',
              unicode: true),
          '',
        )
        .trim();
    final idx = cleaned.indexOf('·');
    if (idx == -1) return (cleaned, '');
    return (
      cleaned.substring(0, idx).trim(),
      cleaned.substring(idx + 1).trim(),
    );
  }

  @override
  Widget build(BuildContext context) {
    final act = widget.activity;
    final accent = avatarAccentForState(act.state);
    final icon = avatarIconForState(act.state);
    final (action, target) = _splitLabel(act.label);

    return Container(
      constraints: const BoxConstraints(maxWidth: 290),
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 5),
      decoration: BoxDecoration(
        color: _surface,
        borderRadius: BorderRadius.circular(8),
        border: Border.all(
          color: act.state == Avatar3DState.success
              ? accent.withValues(alpha: 0.55)
              : _border,
        ),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          AnimatedBuilder(
            animation: _pulse,
            builder: (context, _) {
              final alpha = act.state.isWorking
                  ? 0.45 + (_pulse.value * 0.55)
                  : 1.0;
              return Container(
                width: 6,
                height: 6,
                decoration: BoxDecoration(
                  color: accent.withValues(alpha: alpha),
                  shape: BoxShape.circle,
                ),
              );
            },
          ),
          const SizedBox(width: 7),
          Icon(icon, size: 13, color: accent),
          const SizedBox(width: 6),
          Flexible(
            child: RichText(
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              text: TextSpan(
                style: const TextStyle(fontSize: 11.5, height: 1.2),
                children: [
                  TextSpan(
                    text: action,
                    style: TextStyle(
                      color: target.isEmpty ? _text : _muted,
                      fontWeight: FontWeight.w500,
                    ),
                  ),
                  if (target.isNotEmpty) ...[
                    const TextSpan(text: '  '),
                    TextSpan(
                      text: target,
                      style: const TextStyle(
                        color: _text,
                        fontFamily: 'monospace',
                        fontSize: 11,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                  ],
                ],
              ),
            ),
          ),
          if (act.engine.isNotEmpty) ...[
            const SizedBox(width: 8),
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 5, vertical: 1.5),
              decoration: BoxDecoration(
                color: const Color(0xFF21262D),
                borderRadius: BorderRadius.circular(4),
              ),
              child: Text(
                act.engine.toUpperCase(),
                style: const TextStyle(
                  color: _muted,
                  fontSize: 9,
                  fontWeight: FontWeight.w600,
                  letterSpacing: 0.5,
                ),
              ),
            ),
          ],
        ],
      ),
    );
  }
}

class AvatarDetailCard extends StatelessWidget {
  final String detail;

  const AvatarDetailCard({super.key, required this.detail});

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      onLongPress: () {
        Clipboard.setData(ClipboardData(text: detail));
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Copiado'), duration: Duration(seconds: 1)),
        );
      },
      child: Container(
        constraints: const BoxConstraints(maxWidth: 300),
        padding: const EdgeInsets.all(8),
        decoration: BoxDecoration(
          color: const Color(0xFF111316),
          borderRadius: BorderRadius.circular(6),
          border: Border.all(color: _border),
        ),
        child: SelectableText(
          detail,
          maxLines: 4,
          style: const TextStyle(
            color: _text,
            fontSize: 11,
            fontFamily: 'monospace',
            height: 1.35,
          ),
        ),
      ),
    );
  }
}
