import 'dart:async';

import 'package:flutter/foundation.dart';

import '../models/avatar3d_state.dart';

/// Holds the avatar's current activity and returns to idle after a success.
class Avatar3DController extends ChangeNotifier {
  AgentActivity _activity = AgentActivity.idle;
  AgentActivity get activity => _activity;
  Avatar3DState get state => _activity.state;

  Timer? _successTimer;

  /// Applies a new `agent_status`. Same state with a new label only updates text.
  void apply(AgentActivity next) {
    final changed = next.state != _activity.state;
    _activity = next;
    notifyListeners();
    if (!changed) return;

    _successTimer?.cancel();
    if (next.state == Avatar3DState.success) {
      _successTimer = Timer(kAvatarSuccessHold, () {
        apply(const AgentActivity(state: Avatar3DState.idle));
      });
    }
  }

  @override
  void dispose() {
    _successTimer?.cancel();
    super.dispose();
  }
}
