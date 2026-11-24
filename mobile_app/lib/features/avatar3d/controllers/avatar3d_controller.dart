import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/foundation.dart';
import 'package:thermion_flutter/thermion_flutter.dart';
import 'package:vector_math/vector_math_64.dart' as vm;

import '../models/avatar3d_state.dart';

/// State machine for the 3D avatar: clip cross-fades, blendshape easing,
/// procedural blink and the drag-to-orbit yaw with spring back.
///
/// Works without a viewer attached (2D fallback): state and label still
/// update, only the Filament calls are skipped.
class Avatar3DController extends ChangeNotifier {
  ThermionViewer? _viewer;
  ThermionAsset? _asset;
  MorphTargetSet? _face;
  List<String> _morphNames = const [];
  List<String> _clipOrder = const [];
  vm.Matrix4 _baseTransform = vm.Matrix4.identity();
  Set<String> _clips = const {};

  AgentActivity _activity = AgentActivity.idle;
  AgentActivity get activity => _activity;
  Avatar3DState get state => _activity.state;

  bool _waving = false;
  bool get waving => _waving;

  final Map<String, double> _weights = {};
  final math.Random _rng = math.Random();
  Timer? _tick;
  Timer? _successTimer;
  Timer? _waveTimer;
  DateTime _nextBlink = DateTime.now().add(const Duration(seconds: 3));
  DateTime? _blinkStart;

  double _yaw = 0;
  double _appliedYaw = double.nan;
  bool _dragging = false;

  bool get has3D => _viewer != null && _asset != null;

  Future<void> attach(ThermionViewer viewer, ThermionAsset asset) async {
    _viewer = viewer;
    _asset = asset;
    _baseTransform = await asset.getLocalTransform();
    _clipOrder = await asset.getGltfAnimationNames();
    _clips = _clipOrder.toSet();
    for (final set in await asset.getMorphTargetSets()) {
      final names = set.targets.map((t) => t.name ?? '').toList();
      if (names.any((n) => kBlinkShapes.contains(n))) {
        _face = set;
        _morphNames = names;
        break;
      }
    }
    await _playClip(_activity.state.clip, loop: _activity.state.loop);
    _tick ??= Timer.periodic(const Duration(milliseconds: 33), (_) => _onTick());
  }

  void detach() {
    _tick?.cancel();
    _tick = null;
    _viewer = null;
    _asset = null;
    _face = null;
  }

  /// Applies a new `agent_status`. Same state with a new label only updates text.
  void apply(AgentActivity next) {
    final changed = next.state != _activity.state;
    _activity = next;
    notifyListeners();
    if (!changed) return;

    _successTimer?.cancel();
    _waving = false;
    unawaited(_playClip(next.state.clip, loop: next.state.loop));
    if (next.state == Avatar3DState.success) {
      _successTimer = Timer(kAvatarSuccessHold, () {
        apply(const AgentActivity(state: Avatar3DState.idle));
      });
    }
  }

  /// Quick wave reaction; only while idle.
  Future<void> wave() async {
    if (state != Avatar3DState.idle || _waving) return;
    _waving = true;
    notifyListeners();
    await _playClip(kAvatarWaveClip, loop: false);
    double seconds = 1.6;
    final asset = _asset;
    final idx = _clipOrder.indexOf(kAvatarWaveClip);
    if (asset != null && idx >= 0) {
      seconds = await asset.getGltfAnimationDuration(idx);
    }
    _waveTimer?.cancel();
    _waveTimer = Timer(Duration(milliseconds: (seconds * 1000).round()), () {
      _waving = false;
      notifyListeners();
      if (state == Avatar3DState.idle) {
        unawaited(_playClip(Avatar3DState.idle.clip, loop: true));
      }
    });
  }

  void dragStart() => _dragging = true;

  /// [dx] in logical pixels; clamped to roughly ±35°.
  void dragUpdate(double dx) {
    _yaw = (_yaw + dx * 0.01).clamp(-0.6, 0.6);
  }

  void dragEnd() => _dragging = false;

  Future<void> _playClip(String name, {required bool loop}) async {
    final asset = _asset;
    if (asset == null || !_clips.contains(name)) return;
    await asset.playGltfAnimationByName(
      name,
      loop: loop,
      crossfade: kAvatarCrossFade.inMilliseconds / 1000,
    );
  }

  double _blinkValue(DateTime now) {
    if (_blinkStart == null && now.isAfter(_nextBlink)) {
      _blinkStart = now;
    }
    final start = _blinkStart;
    if (start == null) return 0;
    final t = now.difference(start).inMilliseconds / 150.0;
    if (t >= 1) {
      _blinkStart = null;
      _nextBlink = now.add(Duration(milliseconds: 3000 + _rng.nextInt(2000)));
      return 0;
    }
    return math.sin(t * math.pi);
  }

  double _targetFor(String morph, Map<String, double> logical) {
    for (final entry in logical.entries) {
      if (morph == entry.key ||
          morph == '${entry.key}Left' ||
          morph == '${entry.key}Right') {
        return entry.value;
      }
    }
    return 0;
  }

  void _onTick() {
    final asset = _asset;
    if (_viewer == null || asset == null) return;

    if (!_dragging && _yaw.abs() > 0.001) {
      _yaw *= 0.86;
    } else if (!_dragging) {
      _yaw = 0;
    }
    if (_yaw != _appliedYaw) {
      _appliedYaw = _yaw;
      unawaited(asset.setTransform(vm.Matrix4.rotationY(_yaw) * _baseTransform));
    }

    final face = _face;
    if (face == null || _morphNames.isEmpty) return;
    final blink = _blinkValue(DateTime.now());
    final logical = state.blendshapes;
    final out = List<double>.filled(_morphNames.length, 0);
    for (var i = 0; i < _morphNames.length; i++) {
      final name = _morphNames[i];
      final current = _weights[name] ?? 0;
      final eased = current + (_targetFor(name, logical) - current) * 0.2;
      _weights[name] = eased;
      out[i] = kBlinkShapes.contains(name) ? math.max(eased, blink) : eased;
    }
    unawaited(face.setAllWeights(out));
  }

  @override
  void dispose() {
    _successTimer?.cancel();
    _waveTimer?.cancel();
    detach();
    super.dispose();
  }
}
