import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../models/avatar3d_state.dart';
import 'clean_status_pill.dart';

/// Draws the animated mini-laptop / terminal prop in front of the avatar
/// whenever the agent is actively coding, reading, executing, or wrapping up.
class WorkingPropOverlay extends StatefulWidget {
  final Avatar3DState state;

  const WorkingPropOverlay({super.key, required this.state});

  @override
  State<WorkingPropOverlay> createState() => _WorkingPropOverlayState();
}

class _WorkingPropOverlayState extends State<WorkingPropOverlay>
    with SingleTickerProviderStateMixin {
  late final AnimationController _tick;

  @override
  void initState() {
    super.initState();
    _tick = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 1400),
    )..repeat();
  }

  @override
  void dispose() {
    _tick.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (!widget.state.isUsingLaptop &&
        widget.state != Avatar3DState.success &&
        widget.state != Avatar3DState.thinking &&
        widget.state != Avatar3DState.starting) {
      return const SizedBox.shrink();
    }
    return AnimatedBuilder(
      animation: _tick,
      builder: (context, _) {
        return CustomPaint(
          painter: _MiniLaptopPainter(
            state: widget.state,
            phase: _tick.value,
          ),
        );
      },
    );
  }
}

class _MiniLaptopPainter extends CustomPainter {
  final Avatar3DState state;
  final double phase;

  const _MiniLaptopPainter({required this.state, required this.phase});

  @override
  void paint(Canvas canvas, Size size) {
    final accent = avatarAccentForState(state);
    final cx = size.width / 2;
    final bottomY = size.height - 6;

    if (state == Avatar3DState.thinking || state == Avatar3DState.starting) {
      final paint = Paint()..color = accent.withValues(alpha: 0.75);
      for (int i = 0; i < 3; i++) {
        final a = (phase * 2 * math.pi) + (i * 2.1);
        final dx = cx + math.cos(a) * 28;
        final dy = size.height * 0.22 + math.sin(a) * 5;
        canvas.drawCircle(Offset(dx, dy), 2.2, paint);
      }
      return;
    }

    if (state == Avatar3DState.success) {
      final badgeCenter = Offset(cx + 26, bottomY - 14);
      canvas.drawCircle(
        badgeCenter,
        10,
        Paint()..color = const Color(0xFF15181C),
      );
      canvas.drawCircle(
        badgeCenter,
        10,
        Paint()
          ..color = accent
          ..style = PaintingStyle.stroke
          ..strokeWidth = 1.8,
      );
      final check = Path()
        ..moveTo(badgeCenter.dx - 4, badgeCenter.dy)
        ..lineTo(badgeCenter.dx - 1, badgeCenter.dy + 3)
        ..lineTo(badgeCenter.dx + 4.5, badgeCenter.dy - 3);
      canvas.drawPath(
        check,
        Paint()
          ..color = accent
          ..style = PaintingStyle.stroke
          ..strokeWidth = 1.8
          ..strokeCap = StrokeCap.round
          ..strokeJoin = StrokeJoin.round,
      );
      return;
    }

    final bounce = math.sin(phase * 2 * math.pi * 2) * 0.8;
    final lidRect = RRect.fromRectAndRadius(
      Rect.fromCenter(
        center: Offset(cx, bottomY - 13 + bounce),
        width: 44,
        height: 24,
      ),
      const Radius.circular(4),
    );
    final baseRect = RRect.fromRectAndRadius(
      Rect.fromCenter(
        center: Offset(cx, bottomY - 1 + bounce),
        width: 52,
        height: 3.5,
      ),
      const Radius.circular(2),
    );

    final bodyPaint = Paint()..color = const Color(0xFF1B1F24);
    final rimPaint = Paint()
      ..color = accent.withValues(alpha: 0.85)
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1.4;

    final glowRect = Rect.fromCenter(
      center: Offset(cx, bottomY - 24),
      width: 46,
      height: 18,
    );
    canvas.drawOval(
      glowRect,
      Paint()..color = accent.withValues(alpha: 0.12),
    );

    canvas.drawRRect(lidRect, bodyPaint);
    canvas.drawRRect(lidRect, rimPaint);
    canvas.drawRRect(baseRect, Paint()..color = const Color(0xFF2D333B));

    final linePaint = Paint()
      ..color = accent.withValues(alpha: 0.9)
      ..strokeWidth = 1.6
      ..strokeCap = StrokeCap.round;

    final left = cx - 15;
    final top = bottomY - 20 + bounce;
    final activeRow = (phase * 3).floor() % 3;
    final widths = [18.0, 26.0, 14.0];
    for (int r = 0; r < 3; r++) {
      final y = top + r * 5.0;
      final w = r == activeRow
          ? widths[r] * (0.4 + 0.6 * ((phase * 3) % 1.0))
          : widths[r];
      canvas.drawLine(Offset(left, y), Offset(left + w, y), linePaint);
    }

    final handPaint = Paint()..color = accent;
    final leftTap = math.sin(phase * 2 * math.pi * 3) * 1.8;
    final rightTap = -leftTap;
    canvas.drawCircle(Offset(cx - 18, bottomY - 3 + leftTap), 2.8, handPaint);
    canvas.drawCircle(Offset(cx + 18, bottomY - 3 + rightTap), 2.8, handPaint);
  }

  @override
  bool shouldRepaint(covariant _MiniLaptopPainter oldDelegate) =>
      oldDelegate.state != state || oldDelegate.phase != phase;
}
