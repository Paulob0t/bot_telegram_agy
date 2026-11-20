import 'dart:io';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:rive/rive.dart' hide LinearGradient, RadialGradient;

import '../models/agent_event.dart';

enum AvatarStyle { cubeBot, orbCore, visorMech, ghostCat }

enum AvatarAccessory { none, hair, antenna, crown }

class RiveCompanionAvatar extends StatefulWidget {
  final CharacterState state;
  final String assetPath;
  final String stateMachineName;
  final AvatarStyle style;
  final AvatarAccessory accessory;
  final Color? customAccent;
  final String? customRivePath;

  const RiveCompanionAvatar({
    super.key,
    required this.state,
    this.assetPath = 'assets/rive/companion.riv',
    this.stateMachineName = 'CompanionStateMachine',
    this.style = AvatarStyle.cubeBot,
    this.accessory = AvatarAccessory.none,
    this.customAccent,
    this.customRivePath,
  });

  @override
  State<RiveCompanionAvatar> createState() => _RiveCompanionAvatarState();
}

class _RiveCompanionAvatarState extends State<RiveCompanionAvatar>
    with SingleTickerProviderStateMixin {
  Artboard? _artboard;
  SMIBool? _isIdle;
  SMIBool? _isListening;
  SMIBool? _isThinking;
  SMIBool? _isSpeaking;
  SMIBool? _isSleeping;
  SMINumber? _stateIndex;

  late final AnimationController _pulseController;

  @override
  void initState() {
    super.initState();
    _pulseController = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 1800),
    )..repeat();
    _tryLoadRiveAsset();
  }

  Future<void> _tryLoadRiveAsset() async {
    try {
      RiveFile file;
      if (widget.customRivePath != null && widget.customRivePath!.isNotEmpty) {
        final bytes = await File(widget.customRivePath!).readAsBytes();
        file = RiveFile.import(ByteData.sublistView(bytes));
      } else {
        final data = await rootBundle.load(widget.assetPath);
        file = RiveFile.import(data);
      }
      final artboard = file.mainArtboard;
      StateMachineController? controller =
          StateMachineController.fromArtboard(artboard, widget.stateMachineName);
      controller ??= artboard.stateMachines.isNotEmpty
          ? StateMachineController.fromArtboard(
              artboard,
              artboard.stateMachines.first.name,
            )
          : null;

      if (controller != null) {
        artboard.addController(controller);
        _isIdle = controller.findInput<bool>('isIdle') as SMIBool?;
        _isListening = controller.findInput<bool>('isListening') as SMIBool?;
        _isThinking = controller.findInput<bool>('isThinking') as SMIBool?;
        _isSpeaking = controller.findInput<bool>('isSpeaking') as SMIBool?;
        _isSleeping = controller.findInput<bool>('isSleeping') as SMIBool?;
        _stateIndex = controller.findInput<double>('stateIndex') as SMINumber?;
      }

      if (mounted) {
        setState(() => _artboard = artboard);
        _syncRiveInputs(widget.state);
      }
    } catch (_) {
      if (mounted && _artboard != null) {
        setState(() => _artboard = null);
      }
    }
  }

  @override
  void didUpdateWidget(covariant RiveCompanionAvatar oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.customRivePath != widget.customRivePath) {
      _tryLoadRiveAsset();
    }
    if (oldWidget.state != widget.state) {
      _syncRiveInputs(widget.state);
    }
  }

  void _syncRiveInputs(CharacterState s) {
    _isIdle?.value = s == CharacterState.idle;
    _isListening?.value = s == CharacterState.listening;
    _isThinking?.value = s == CharacterState.thinking;
    _isSpeaking?.value = s == CharacterState.speaking;
    _isSleeping?.value = s == CharacterState.sleeping;
    _stateIndex?.value = s.index.toDouble();
  }

  @override
  void dispose() {
    _pulseController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (_artboard != null) {
      return Rive(
        artboard: _artboard!,
        fit: BoxFit.contain,
      );
    }

    return AnimatedBuilder(
      animation: _pulseController,
      builder: (context, _) {
        return CustomPaint(
          size: const Size(160, 160),
          painter: _ProceduralCharacterPainter(
            state: widget.state,
            phase: _pulseController.value,
            style: widget.style,
            accessory: widget.accessory,
            customAccent: widget.customAccent,
          ),
        );
      },
    );
  }
}

class _ProceduralCharacterPainter extends CustomPainter {
  final CharacterState state;
  final double phase;
  final AvatarStyle style;
  final AvatarAccessory accessory;
  final Color? customAccent;

  const _ProceduralCharacterPainter({
    required this.state,
    required this.phase,
    required this.style,
    required this.accessory,
    this.customAccent,
  });

  @override
  void paint(Canvas canvas, Size size) {
    final center = Offset(size.width / 2, size.height / 2);
    final radius = math.min(size.width, size.height) * 0.36;
    final angle = phase * 2 * math.pi;

    final breathY = state == CharacterState.sleeping
        ? math.sin(angle * 0.5) * 1.5
        : math.sin(angle) * 2.5;
    final headCenter = Offset(center.dx, center.dy + breathY);

    Color accentColor;
    if (customAccent != null && state != CharacterState.sleeping) {
      accentColor = customAccent!;
    } else {
      switch (state) {
        case CharacterState.sleeping:
          accentColor = const Color(0xFF6E7681);
          break;
        case CharacterState.listening:
          accentColor = const Color(0xFF58A6FF);
          break;
        case CharacterState.thinking:
          accentColor = const Color(0xFFD29922);
          break;
        case CharacterState.speaking:
          accentColor = const Color(0xFFD97757);
          break;
        case CharacterState.idle:
          accentColor = const Color(0xFF3FB950);
          break;
      }
    }

    final shellPaint = Paint()
      ..color = const Color(0xFF15181C)
      ..style = PaintingStyle.fill;
    final borderPaint = Paint()
      ..color = accentColor.withValues(alpha: 0.85)
      ..style = PaintingStyle.stroke
      ..strokeWidth = 2.2;

    // 1. Cuerpo según estilo
    if (style == AvatarStyle.orbCore) {
      canvas.drawCircle(headCenter, radius * 0.96, shellPaint);
      canvas.drawCircle(headCenter, radius * 0.96, borderPaint);
    } else {
      final headRect = RRect.fromRectAndRadius(
        Rect.fromCenter(
          center: headCenter,
          width: radius * 2.1,
          height: radius * 1.75,
        ),
        Radius.zero,
      );
      canvas.drawRRect(headRect, shellPaint);
      canvas.drawRRect(headRect, borderPaint);

      if (style == AvatarStyle.ghostCat) {
        final topY = headCenter.dy - radius * 0.87;
        final leftEar = Path()
          ..moveTo(headCenter.dx - radius * 0.72, topY + 2)
          ..lineTo(headCenter.dx - radius * 0.88, topY - 10)
          ..lineTo(headCenter.dx - radius * 0.38, topY);
        final rightEar = Path()
          ..moveTo(headCenter.dx + radius * 0.72, topY + 2)
          ..lineTo(headCenter.dx + radius * 0.88, topY - 10)
          ..lineTo(headCenter.dx + radius * 0.38, topY);
        canvas.drawPath(leftEar, borderPaint);
        canvas.drawPath(rightEar, borderPaint);
      }
    }

    // 2. Accesorios (Pelo, Antena, Corona)
    final topEdge = headCenter.dy - (style == AvatarStyle.orbCore ? radius * 0.96 : radius * 0.87);
    final accPaint = Paint()
      ..color = accentColor
      ..style = PaintingStyle.stroke
      ..strokeCap = StrokeCap.round
      ..strokeWidth = 2.2;

    if (accessory == AvatarAccessory.hair) {
      final hair = Path()
        ..moveTo(headCenter.dx - 10, topEdge)
        ..quadraticBezierTo(headCenter.dx - 4, topEdge - 10, headCenter.dx + 2, topEdge - 3)
        ..quadraticBezierTo(headCenter.dx + 8, topEdge - 11, headCenter.dx + 12, topEdge);
      canvas.drawPath(hair, accPaint);
    } else if (accessory == AvatarAccessory.antenna) {
      canvas.drawLine(Offset(headCenter.dx, topEdge), Offset(headCenter.dx, topEdge - 9), accPaint);
      canvas.drawCircle(Offset(headCenter.dx, topEdge - 11), 3.0, Paint()..color = accentColor);
    } else if (accessory == AvatarAccessory.crown) {
      final crown = Path()
        ..moveTo(headCenter.dx - 11, topEdge - 1)
        ..lineTo(headCenter.dx - 11, topEdge - 9)
        ..lineTo(headCenter.dx - 4, topEdge - 4)
        ..lineTo(headCenter.dx, topEdge - 11)
        ..lineTo(headCenter.dx + 4, topEdge - 4)
        ..lineTo(headCenter.dx + 11, topEdge - 9)
        ..lineTo(headCenter.dx + 11, topEdge - 1);
      canvas.drawPath(crown, accPaint);
    }

    // 3. Ojos o Visor
    final eyePaint = Paint()
      ..color = accentColor
      ..style = PaintingStyle.fill
      ..strokeCap = StrokeCap.round
      ..strokeWidth = 3.0;

    final leftEye = Offset(headCenter.dx - radius * 0.42, headCenter.dy - 3);
    final rightEye = Offset(headCenter.dx + radius * 0.42, headCenter.dy - 3);

    if (style == AvatarStyle.visorMech && state != CharacterState.sleeping) {
      final visorW = radius * 1.4;
      canvas.drawRRect(
        RRect.fromRectAndRadius(
          Rect.fromCenter(center: Offset(headCenter.dx, headCenter.dy - 3), width: visorW, height: 8),
          const Radius.circular(4),
        ),
        eyePaint,
      );
    } else if (state == CharacterState.sleeping) {
      canvas.drawLine(leftEye.translate(-7, 0), leftEye.translate(7, 0), eyePaint);
      canvas.drawLine(rightEye.translate(-7, 0), rightEye.translate(7, 0), eyePaint);
    } else if (state == CharacterState.thinking) {
      final shift = math.cos(angle) * 3.5;
      canvas.drawCircle(leftEye.translate(shift, -1), 4.5, eyePaint);
      canvas.drawCircle(rightEye.translate(shift, -1), 4.5, eyePaint);
    } else {
      final eyeHeight = state == CharacterState.listening ? 12.0 : 10.0;
      canvas.drawRRect(
        RRect.fromRectAndRadius(
          Rect.fromCenter(center: leftEye, width: 7.5, height: eyeHeight),
          const Radius.circular(4),
        ),
        eyePaint,
      );
      canvas.drawRRect(
        RRect.fromRectAndRadius(
          Rect.fromCenter(center: rightEye, width: 7.5, height: eyeHeight),
          const Radius.circular(4),
        ),
        eyePaint,
      );
    }

    // 4. Boca
    final mouthCenter = Offset(headCenter.dx, headCenter.dy + radius * 0.42);
    if (state == CharacterState.speaking) {
      final openH = 3.5 + (math.sin(angle * 3).abs() * 7.5);
      canvas.drawRRect(
        RRect.fromRectAndRadius(
          Rect.fromCenter(center: mouthCenter, width: 20, height: openH),
          const Radius.circular(4),
        ),
        eyePaint,
      );
    } else {
      canvas.drawLine(mouthCenter.translate(-7, 0), mouthCenter.translate(7, 0), eyePaint);
    }
  }

  @override
  bool shouldRepaint(covariant _ProceduralCharacterPainter oldDelegate) {
    return oldDelegate.state != state ||
        oldDelegate.phase != phase ||
        oldDelegate.style != style ||
        oldDelegate.accessory != accessory ||
        oldDelegate.customAccent != customAccent;
  }
}
