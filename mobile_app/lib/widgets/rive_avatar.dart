import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:rive/rive.dart' hide LinearGradient, RadialGradient;

import '../models/agent_event.dart';

/// Controlador visual del asistente Rive.
///
/// Conecta el stream de [CharacterState] (`idle`, `listening`, `thinking`, `speaking`, `sleeping`)
/// directamente a los inputs booleanos y numéricos de la State Machine de `assets/rive/companion.riv`:
/// - `isIdle` (bool)
/// - `isListening` (bool)
/// - `isThinking` (bool)
/// - `isSpeaking` (bool)
/// - `isSleeping` (bool)
/// - `stateIndex` (number: 0=idle, 1=listening, 2=thinking, 3=speaking, 4=sleeping)
///
/// Si el archivo `.riv` aún no se ha colocado en `assets/rive/companion.riv`, renderiza
/// un autómata vectorial determinista en `CustomPainter` que responde a los mismos 5 estados.
class RiveCompanionAvatar extends StatefulWidget {
  final CharacterState state;
  final String assetPath;
  final String stateMachineName;

  const RiveCompanionAvatar({
    super.key,
    required this.state,
    this.assetPath = 'assets/rive/companion.riv',
    this.stateMachineName = 'CompanionStateMachine',
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
      final data = await rootBundle.load(widget.assetPath);
      final file = RiveFile.import(data);
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
      // Sin archivo .riv externo: usa el renderizador vectorial integrado.
    }
  }

  @override
  void didUpdateWidget(covariant RiveCompanionAvatar oldWidget) {
    super.didUpdateWidget(oldWidget);
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
          ),
        );
      },
    );
  }
}

class _ProceduralCharacterPainter extends CustomPainter {
  final CharacterState state;
  final double phase;

  const _ProceduralCharacterPainter({
    required this.state,
    required this.phase,
  });

  @override
  void paint(Canvas canvas, Size size) {
    final center = Offset(size.width / 2, size.height / 2);
    final radius = math.min(size.width, size.height) * 0.36;
    final angle = phase * 2 * math.pi;

    // Desplazamiento vertical de respiración según el estado
    final breathY = state == CharacterState.sleeping
        ? math.sin(angle * 0.5) * 2.0
        : math.sin(angle) * 4.0;
    final headCenter = Offset(center.dx, center.dy + breathY);

    final Color accentColor;
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

    // Chasis del núcleo
    final shellPaint = Paint()
      ..color = const Color(0xFF1A1D21)
      ..style = PaintingStyle.fill;
    final borderPaint = Paint()
      ..color = accentColor.withValues(alpha: 0.75)
      ..style = PaintingStyle.stroke
      ..strokeWidth = 2.2;

    final headRect = RRect.fromRectAndRadius(
      Rect.fromCenter(
        center: headCenter,
        width: radius * 1.95,
        height: radius * 1.95,
      ),
      const Radius.circular(16),
    );
    canvas.drawRRect(headRect, shellPaint);
    canvas.drawRRect(headRect, borderPaint);

    // Ojos expresivos dirigidos por la State Machine
    final eyePaint = Paint()
      ..color = accentColor
      ..style = PaintingStyle.fill
      ..strokeCap = StrokeCap.round
      ..strokeWidth = 3.2;

    final leftEye = Offset(headCenter.dx - radius * 0.42, headCenter.dy - 4);
    final rightEye = Offset(headCenter.dx + radius * 0.42, headCenter.dy - 4);

    if (state == CharacterState.sleeping) {
      // Ojos cerrados en reposo (PC suspendida / desconectada)
      canvas.drawLine(
        leftEye.translate(-10, 0),
        leftEye.translate(10, 0),
        eyePaint,
      );
      canvas.drawLine(
        rightEye.translate(-10, 0),
        rightEye.translate(10, 0),
        eyePaint,
      );
    } else if (state == CharacterState.thinking) {
      // Ojos orbitando mientras procesa herramientas
      final shift = math.cos(angle) * 4.0;
      canvas.drawCircle(leftEye.translate(shift, -2), 5.5, eyePaint);
      canvas.drawCircle(rightEye.translate(shift, -2), 5.5, eyePaint);
    } else {
      final eyeHeight = state == CharacterState.listening ? 15.0 : 12.0;
      canvas.drawRRect(
        RRect.fromRectAndRadius(
          Rect.fromCenter(center: leftEye, width: 9, height: eyeHeight),
          const Radius.circular(4),
        ),
        eyePaint,
      );
      canvas.drawRRect(
        RRect.fromRectAndRadius(
          Rect.fromCenter(center: rightEye, width: 9, height: eyeHeight),
          const Radius.circular(4),
        ),
        eyePaint,
      );
    }

    // Boca / ecualizador de voz cuando está en speaking
    final mouthCenter = Offset(headCenter.dx, headCenter.dy + radius * 0.42);
    if (state == CharacterState.speaking) {
      final openH = 4.0 + (math.sin(angle * 3).abs() * 10.0);
      canvas.drawRRect(
        RRect.fromRectAndRadius(
          Rect.fromCenter(center: mouthCenter, width: 26, height: openH),
          const Radius.circular(5),
        ),
        eyePaint,
      );
    } else {
      canvas.drawLine(
        mouthCenter.translate(-9, 0),
        mouthCenter.translate(9, 0),
        eyePaint,
      );
    }
  }

  @override
  bool shouldRepaint(covariant _ProceduralCharacterPainter oldDelegate) {
    return oldDelegate.state != state || oldDelegate.phase != phase;
  }
}
