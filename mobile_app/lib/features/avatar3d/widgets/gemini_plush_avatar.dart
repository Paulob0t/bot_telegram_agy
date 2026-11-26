import 'dart:io';
import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../models/avatar3d_state.dart';
import 'clean_status_pill.dart';

/// Public preview widget for a Gemini plush/vinyl character variant (used in the 4-option picker).
class GeminiCharacterPreview extends StatelessWidget {
  final GeminiAvatarProfile profile;
  final Avatar3DState state;
  final double size;
  final Color? boxBackgroundColor;

  const GeminiCharacterPreview({
    super.key,
    required this.profile,
    this.state = Avatar3DState.idle,
    this.size = 96,
    this.boxBackgroundColor,
  });

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      width: size,
      height: size,
      child: GeminiCharacterAvatar(
        profile: profile,
        state: state,
        boxBackgroundColor: boxBackgroundColor,
      ),
    );
  }
}

/// Renders a 60 FPS 3D-shaded plush/vinyl companion inside a clean square card
/// with user traits extracted by Gemini Vision.
class GeminiCharacterAvatar extends StatefulWidget {
  final GeminiAvatarProfile profile;
  final Avatar3DState state;
  final Color? boxBackgroundColor;

  /// Animates the mouth while the reply is streaming in.
  final bool speaking;

  const GeminiCharacterAvatar({
    super.key,
    required this.profile,
    required this.state,
    this.boxBackgroundColor,
    this.speaking = false,
  });

  @override
  State<GeminiCharacterAvatar> createState() => _GeminiCharacterAvatarState();
}

class _GeminiCharacterAvatarState extends State<GeminiCharacterAvatar>
    with SingleTickerProviderStateMixin {
  late final AnimationController _anim;

  @override
  void initState() {
    super.initState();
    _anim = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 3200),
    )..repeat();
  }

  @override
  void dispose() {
    _anim.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: _anim,
      builder: (context, _) {
        return CustomPaint(
          painter: _GeminiCharacterPainter(
            profile: widget.profile,
            state: widget.state,
            phase: _anim.value,
            boxBackgroundColor: widget.boxBackgroundColor,
            speaking: widget.speaking,
          ),
        );
      },
    );
  }
}

class _GeminiCharacterPainter extends CustomPainter {
  final GeminiAvatarProfile profile;
  final Avatar3DState state;
  final double phase;
  final Color? boxBackgroundColor;
  final bool speaking;

  const _GeminiCharacterPainter({
    required this.profile,
    required this.state,
    required this.phase,
    this.boxBackgroundColor,
    this.speaking = false,
  });

  Color _shiftLightness(Color c, double delta) {
    final hsl = HSLColor.fromColor(c);
    return hsl
        .withLightness((hsl.lightness + delta).clamp(0.06, 0.96))
        .toColor();
  }

  @override
  void paint(Canvas canvas, Size size) {
    final cx = size.width / 2;
    final cy = size.height / 2;
    final s = math.min(size.width, size.height);
    final t = phase * 2 * math.pi;
    final breath = math.sin(t) * 1.6;

    final skin = Color(profile.skinColor);
    final hair = Color(profile.hairColor);
    final suitBase = Color(profile.hoodieColor);
    final accent = Color(profile.accentColor);
    final stateAccent = avatarAccentForState(state);

    // 1. Clean Minimalist Square Box (with customizable background color)
    final boxSize = s * 0.86;
    final boxRect = RRect.fromRectAndRadius(
      Rect.fromCenter(center: Offset(cx, cy), width: boxSize, height: boxSize),
      const Radius.circular(12),
    );
    canvas.drawRRect(
      boxRect,
      Paint()..color = boxBackgroundColor ?? const Color(0xFF111418),
    );
    canvas.drawRRect(
      boxRect,
      Paint()
        ..color = state.isWorking
            ? stateAccent.withValues(alpha: 0.75)
            : const Color(0xFF262B32)
        ..style = PaintingStyle.stroke
        ..strokeWidth = 1.4,
    );

    canvas.save();
    canvas.clipRRect(boxRect);

    final suitLight = _shiftLightness(suitBase, 0.12);
    final suitDark = _shiftLightness(suitBase, -0.14);
    final isHooded = profile.variant != 'vinyl_you';

    // 2. Plush Spirit Ears (for spirit_plush variant)
    final bodyCy = cy + 6 + breath;
    if (profile.variant == 'spirit_plush') {
      final earPaint = Paint()
        ..shader = RadialGradient(
          colors: [suitLight, suitDark],
        ).createShader(
            Rect.fromCircle(center: Offset(cx, bodyCy - 20), radius: 30));
      canvas.drawCircle(Offset(cx - 17, bodyCy - 24), 7.5, earPaint);
      canvas.drawCircle(Offset(cx + 17, bodyCy - 24), 7.5, earPaint);
      canvas.drawCircle(
        Offset(cx - 17, bodyCy - 24),
        3.8,
        Paint()..color = const Color(0xFFE8A598).withValues(alpha: 0.55),
      );
      canvas.drawCircle(
        Offset(cx + 17, bodyCy - 24),
        3.8,
        Paint()..color = const Color(0xFFE8A598).withValues(alpha: 0.55),
      );
    }

    // 3. Plush Bell-Shaped Body & Arms (3D volumetric radial shading)
    final plushShader = RadialGradient(
      center: const Alignment(-0.25, -0.35),
      radius: 0.95,
      colors: [suitLight, suitBase, suitDark],
      stops: const [0.0, 0.62, 1.0],
    ).createShader(
        Rect.fromCenter(center: Offset(cx, bodyCy), width: 64, height: 64));

    final plushPaint = Paint()..shader = plushShader;

    final armLift = state.isUsingLaptop ? -3.0 : 0.0;
    canvas.save();
    canvas.translate(cx - 21, bodyCy + 11 + armLift);
    canvas.rotate(0.42);
    canvas.drawRRect(
      RRect.fromRectAndRadius(
        Rect.fromCenter(center: Offset.zero, width: 14, height: 22),
        const Radius.circular(7),
      ),
      plushPaint,
    );
    canvas.restore();

    // Awaiting approval: right arm raised and waving, like asking for a turn.
    final raiseHand = state == Avatar3DState.awaitingApproval;
    canvas.save();
    if (raiseHand) {
      canvas.translate(cx + 24, bodyCy - 2);
      canvas.rotate(-2.75 + math.sin(t * 4) * 0.18);
    } else {
      canvas.translate(cx + 21, bodyCy + 11 + armLift);
      canvas.rotate(-0.42);
    }
    canvas.drawRRect(
      RRect.fromRectAndRadius(
        Rect.fromCenter(center: Offset.zero, width: 14, height: 22),
        const Radius.circular(7),
      ),
      plushPaint,
    );
    canvas.restore();

    if (isHooded) {
      final hoodPath = Path()
        ..moveTo(cx, bodyCy - 27)
        ..cubicTo(
            cx + 21, bodyCy - 27, cx + 22, bodyCy - 4, cx + 24, bodyCy + 14)
        ..cubicTo(cx + 25, bodyCy + 24, cx + 17, bodyCy + 27, cx, bodyCy + 27)
        ..cubicTo(
            cx - 17, bodyCy + 27, cx - 25, bodyCy + 24, cx - 24, bodyCy + 14)
        ..cubicTo(cx - 22, bodyCy - 4, cx - 21, bodyCy - 27, cx, bodyCy - 27)
        ..close();
      canvas.drawPath(hoodPath, plushPaint);
    } else {
      final torsoRect = RRect.fromRectAndRadius(
        Rect.fromCenter(center: Offset(cx, bodyCy + 14), width: 46, height: 28),
        const Radius.circular(14),
      );
      canvas.drawRRect(torsoRect, plushPaint);
    }

    // 4. Recessed Face Cutout with soft inner rim shadow & 3D skin gradient
    final faceCenter = Offset(
      cx,
      bodyCy - 8 + (state.isUsingLaptop ? 1.8 : 0),
    );
    final faceRect = RRect.fromRectAndRadius(
      Rect.fromCenter(center: faceCenter, width: 32, height: 23),
      const Radius.circular(11.5),
    );

    final hairLight = _shiftLightness(hair, 0.12);
    final hairDark = _shiftLightness(hair, -0.08);
    final isCurly = profile.hairStyle == 'curly' ||
        profile.hairStyle == 'afro' ||
        profile.hairStyle == 'wavy_long' ||
        profile.hairStyle == 'wavy';

    void draw3DCurl(Offset center, double radius) {
      canvas.drawCircle(
        center,
        radius,
        Paint()
          ..shader = RadialGradient(
            center: const Alignment(-0.3, -0.35),
            radius: 0.95,
            colors: [hairLight, hair, hairDark],
          ).createShader(Rect.fromCircle(center: center, radius: radius)),
      );
    }

    if (isHooded) {
      canvas.drawRRect(
        faceRect.inflate(1.4),
        Paint()..color = suitDark.withValues(alpha: 0.65),
      );
    } else if (isCurly) {
      // Full 3D curly halo behind the head for Vinyl Toy (unhooded)
      final backCurls = <Offset>[
        const Offset(-16, -9),
        const Offset(-18, -2),
        const Offset(-17, 5),
        const Offset(16, -9),
        const Offset(18, -2),
        const Offset(17, 5),
        const Offset(-12, -14),
        const Offset(-5, -16),
        const Offset(5, -16),
        const Offset(12, -14),
      ];
      for (final off in backCurls) {
        draw3DCurl(faceCenter + off, profile.hairStyle == 'afro' ? 7.2 : 5.8);
      }
    } else if (profile.hairStyle == 'long') {
      canvas.drawCircle(faceCenter.translate(0, -3), 21, Paint()..color = hair);
    }

    final skinLight = _shiftLightness(skin, 0.08);
    final skinDark = _shiftLightness(skin, -0.10);
    canvas.drawRRect(
      faceRect,
      Paint()
        ..shader = RadialGradient(
          center: const Alignment(-0.2, -0.3),
          radius: 0.9,
          colors: [skinLight, skin, skinDark],
        ).createShader(faceRect.outerRect),
    );

    // 5. Hair / 3D Curls / Feminine Side Locks
    final hairPaint = Paint()..color = hair;
    final hairTopY = faceCenter.dy - 11.5;
    final isFem = profile.isFeminine ||
        profile.hairStyle == 'long' ||
        profile.hairStyle == 'wavy_long' ||
        profile.hairStyle == 'ponytail';

    if (isCurly) {
      // Side curls framing both cheeks (even inside the plush hood!)
      final sideCurls = isHooded
          ? <Offset>[
              const Offset(-14.5, -6.5),
              const Offset(-15.2, -1.0),
              const Offset(-14.2, 4.2),
              const Offset(14.5, -6.5),
              const Offset(15.2, -1.0),
              const Offset(14.2, 4.2),
            ]
          : <Offset>[
              const Offset(-15.5, -7.5),
              const Offset(-16.5, -1.5),
              const Offset(-15.5, 4.5),
              const Offset(15.5, -7.5),
              const Offset(16.5, -1.5),
              const Offset(15.5, 4.5),
            ];
      for (final off in sideCurls) {
        draw3DCurl(faceCenter + off, isHooded ? 3.9 : 4.8);
      }

      // Forehead / top cluster of overlapping 3D curls
      final topCurls = isHooded
          ? <(double, double, double)>[
              (-11.0, 1.8, 4.4),
              (-5.5, 0.6, 4.9),
              (0.0, 1.2, 5.1),
              (5.5, 0.6, 4.9),
              (11.0, 1.8, 4.4),
            ]
          : <(double, double, double)>[
              (-13.5, 0.5, 5.2),
              (-8.0, -1.5, 5.8),
              (-2.5, -0.8, 6.0),
              (3.0, -1.4, 5.9),
              (8.5, -1.2, 5.7),
              (13.5, 0.5, 5.2),
            ];
      for (final c in topCurls) {
        draw3DCurl(Offset(faceCenter.dx + c.$1, hairTopY + c.$2), c.$3);
      }
    } else {
      if (isFem) {
        final leftLock = RRect.fromRectAndCorners(
          Rect.fromLTWH(faceCenter.dx - 16.5, faceCenter.dy - 9.5, 4.8, 19.5),
          topLeft: const Radius.circular(6),
          bottomLeft: const Radius.circular(6),
          bottomRight: const Radius.circular(4),
        );
        final rightLock = RRect.fromRectAndCorners(
          Rect.fromLTWH(faceCenter.dx + 11.7, faceCenter.dy - 9.5, 4.8, 19.5),
          topRight: const Radius.circular(6),
          bottomRight: const Radius.circular(6),
          bottomLeft: const Radius.circular(4),
        );
        canvas.drawRRect(leftLock, hairPaint);
        canvas.drawRRect(rightLock, hairPaint);
      }

      if (profile.hairStyle != 'none') {
        canvas.drawRRect(
          RRect.fromRectAndCorners(
            Rect.fromCenter(
              center: Offset(faceCenter.dx, hairTopY + (isHooded ? 1.5 : 0)),
              width: isHooded ? 24 : 32,
              height: isHooded ? 5.8 : 9,
            ),
            topLeft: const Radius.circular(8),
            topRight: const Radius.circular(8),
          ),
          hairPaint,
        );
      }
    }

    // 6. Soft Rosy Blush Cheeks
    final blushAlpha = profile.isFeminine ? 0.62 : 0.42;
    final blushPaint = Paint()
      ..shader = RadialGradient(
        colors: [
          const Color(0xFFE88D82).withValues(alpha: blushAlpha),
          const Color(0xFFE88D82).withValues(alpha: 0.0),
        ],
      ).createShader(Rect.fromCircle(
          center: faceCenter.translate(-9.5, 3.5), radius: 5.8));
    canvas.drawCircle(faceCenter.translate(-9.5, 3.5), 5.8, blushPaint);

    final blushRightPaint = Paint()
      ..shader = RadialGradient(
        colors: [
          const Color(0xFFE88D82).withValues(alpha: blushAlpha),
          const Color(0xFFE88D82).withValues(alpha: 0.0),
        ],
      ).createShader(
          Rect.fromCircle(center: faceCenter.translate(9.5, 3.5), radius: 5.8));
    canvas.drawCircle(faceCenter.translate(9.5, 3.5), 5.8, blushRightPaint);

    // 7. Glossy Bead Eyes + Feminine Curved Eyelashes + Blink + Eye Tracking
    final isBlinking = (phase > 0.46 && phase < 0.50);
    final gazeX = state.isUsingLaptop
        ? math.sin(t * 2) * 1.8
        : (state == Avatar3DState.thinking ? 1.8 : 0.0);
    final gazeY = state.isUsingLaptop
        ? 1.6
        : (state == Avatar3DState.thinking ? -1.4 : 0.0);

    final leftEye = faceCenter.translate(-6.8 + gazeX, -0.5 + gazeY);
    final rightEye = faceCenter.translate(6.8 + gazeX, -0.5 + gazeY);
    final eyePaint = Paint()
      ..color = const Color(0xFF141210)
      ..strokeWidth = 2.0
      ..strokeCap = StrokeCap.round;

    if (isBlinking) {
      canvas.drawLine(
          leftEye.translate(-2.8, 0), leftEye.translate(2.8, 0), eyePaint);
      canvas.drawLine(
          rightEye.translate(-2.8, 0), rightEye.translate(2.8, 0), eyePaint);
    } else {
      canvas.drawCircle(leftEye, 2.7, eyePaint);
      canvas.drawCircle(rightEye, 2.7, eyePaint);
      final shine = Paint()..color = Colors.white;
      canvas.drawCircle(leftEye.translate(-0.8, -0.9), 0.9, shine);
      canvas.drawCircle(rightEye.translate(-0.8, -0.9), 0.9, shine);

      if (profile.isFeminine) {
        final lashPaint = Paint()
          ..color = const Color(0xFF141210)
          ..strokeWidth = 1.45
          ..strokeCap = StrokeCap.round;
        canvas.drawLine(
          leftEye.translate(-2.0, -1.6),
          leftEye.translate(-4.1, -3.1),
          lashPaint,
        );
        canvas.drawLine(
          leftEye.translate(-1.0, -2.3),
          leftEye.translate(-2.4, -3.9),
          lashPaint,
        );
        canvas.drawLine(
          rightEye.translate(2.0, -1.6),
          rightEye.translate(4.1, -3.1),
          lashPaint,
        );
        canvas.drawLine(
          rightEye.translate(1.0, -2.3),
          rightEye.translate(2.4, -3.9),
          lashPaint,
        );
      }
    }

    // 8. Glasses & Cyber Headphones
    if (profile.glasses != 'none') {
      final gPaint = Paint()
        ..color = const Color(0xFF231F1C)
        ..style = PaintingStyle.stroke
        ..strokeWidth = 1.3;
      canvas.drawCircle(leftEye, 4.8, gPaint);
      canvas.drawCircle(rightEye, 4.8, gPaint);
      canvas.drawLine(
          leftEye.translate(4.8, 0), rightEye.translate(-4.8, 0), gPaint);
    }

    if (profile.variant == 'cyber_pod' || profile.headwear == 'headphones') {
      final cupPaint = Paint()..color = accent;
      canvas.drawRRect(
        RRect.fromRectAndRadius(
          Rect.fromCenter(
              center: faceCenter.translate(-21, 0), width: 5, height: 12),
          const Radius.circular(2.5),
        ),
        cupPaint,
      );
      canvas.drawRRect(
        RRect.fromRectAndRadius(
          Rect.fromCenter(
              center: faceCenter.translate(21, 0), width: 5, height: 12),
          const Radius.circular(2.5),
        ),
        cupPaint,
      );
    }

    // 9. Minimalist Smile / Rosy Lip / Facial Hair
    if (!profile.isFeminine && profile.facialHair != 'none') {
      canvas.drawRRect(
        RRect.fromRectAndRadius(
          Rect.fromCenter(
              center: faceCenter.translate(0, 7.2), width: 12, height: 3.2),
          const Radius.circular(2),
        ),
        Paint()..color = hair.withValues(alpha: 0.7),
      );
    }

    final mouthColor =
        profile.isFeminine ? const Color(0xFF8C3B3B) : const Color(0xFF1E1916);
    final mouthPaint = Paint()
      ..color = mouthColor
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1.5
      ..strokeCap = StrokeCap.round;
    if (speaking) {
      // Two overlapping frequencies so the mouth doesn't open in a mechanical beat.
      final open =
          (math.sin(t * 9).abs() * 0.7 + math.sin(t * 5.3).abs() * 0.3);
      canvas.drawOval(
        Rect.fromCenter(
          center: faceCenter.translate(0, 3.2),
          width: 5.5,
          height: 1.2 + open * 3.6,
        ),
        Paint()..color = mouthColor,
      );
    } else {
      canvas.drawArc(
        Rect.fromCenter(
          center: faceCenter.translate(0, 2.2),
          width: state == Avatar3DState.success ? 8.5 : 5.5,
          height: state == Avatar3DState.success ? 5.5 : 3.5,
        ),
        0.15,
        math.pi - 0.3,
        false,
        mouthPaint,
      );
    }

    canvas.restore();
  }

  @override
  bool shouldRepaint(covariant _GeminiCharacterPainter oldDelegate) =>
      oldDelegate.profile != profile ||
      oldDelegate.state != state ||
      oldDelegate.phase != phase ||
      oldDelegate.speaking != speaking ||
      oldDelegate.boxBackgroundColor != boxBackgroundColor;
}

/// Renders a user-uploaded photo as an animated 2.5D character with breathing,
/// focus ring, and tilt towards the mini-laptop when coding/executing.
class PhotoCharacterAvatar extends StatefulWidget {
  final String photoPath;
  final Avatar3DState state;

  const PhotoCharacterAvatar({
    super.key,
    required this.photoPath,
    required this.state,
  });

  @override
  State<PhotoCharacterAvatar> createState() => _PhotoCharacterAvatarState();
}

class _PhotoCharacterAvatarState extends State<PhotoCharacterAvatar>
    with SingleTickerProviderStateMixin {
  late final AnimationController _anim;

  @override
  void initState() {
    super.initState();
    _anim = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 2200),
    )..repeat();
  }

  @override
  void dispose() {
    _anim.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final accent = avatarAccentForState(widget.state);
    return AnimatedBuilder(
      animation: _anim,
      builder: (context, _) {
        final t = _anim.value * 2 * math.pi;
        final dy = widget.state.isUsingLaptop
            ? math.sin(t * 2) * 1.4
            : math.sin(t) * 2.2;
        final tilt = widget.state.isUsingLaptop
            ? 0.05 + math.sin(t * 3) * 0.02
            : (widget.state == Avatar3DState.thinking ? -0.06 : 0.0);

        return Center(
          child: Transform.translate(
            offset: Offset(0, dy - (widget.state.isUsingLaptop ? 4 : 0)),
            child: Transform.rotate(
              angle: tilt,
              child: Container(
                width: 68,
                height: 68,
                decoration: BoxDecoration(
                  borderRadius: BorderRadius.circular(18),
                  border: Border.all(
                    color: accent.withValues(alpha: 0.85),
                    width: 2,
                  ),
                ),
                clipBehavior: Clip.antiAlias,
                child: ClipRRect(
                  borderRadius: BorderRadius.circular(16),
                  child: Image.file(
                    File(widget.photoPath),
                    fit: BoxFit.cover,
                  ),
                ),
              ),
            ),
          ),
        );
      },
    );
  }
}
