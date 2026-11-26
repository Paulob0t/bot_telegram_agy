import 'dart:io';

import 'package:flutter/material.dart';

import '../controllers/avatar3d_controller.dart';
import '../models/avatar3d_state.dart';
import 'clean_status_pill.dart';
import 'gemini_plush_avatar.dart';
import 'mini_laptop_overlay.dart';

export 'clean_status_pill.dart';
export 'gemini_plush_avatar.dart';
export 'mini_laptop_overlay.dart';

/// Avatar viewport with clean live status pill, photo/profile support,
/// and interactive mini-laptop overlay when working.
class Avatar3DViewport extends StatefulWidget {
  final AgentActivity activity;
  final Widget fallback;
  final double size;

  /// Optional user photo path: when set, renders an animated 2.5D character
  /// rigged from the user's photo with the interactive mini-laptop prop.
  final String? customPhotoPath;

  /// Optional Gemini Vision-generated stylized character profile built from a user photo.
  final GeminiAvatarProfile? geminiProfile;

  /// Optional background color for the square avatar card.
  final Color? boxBackgroundColor;

  /// Called on long press, meant to confirm `/stop`.
  final VoidCallback? onLongPress;

  /// True while the reply is streaming in; drives the mouth animation.
  final bool speaking;

  /// Called on tap when the avatar is idle and in fallback mode.
  final VoidCallback? onTapIdleFallback;

  const Avatar3DViewport({
    super.key,
    required this.activity,
    required this.fallback,
    this.size = 114,
    this.speaking = false,
    this.customPhotoPath,
    this.geminiProfile,
    this.boxBackgroundColor,
    this.onLongPress,
    this.onTapIdleFallback,
  });

  @override
  State<Avatar3DViewport> createState() => _Avatar3DViewportState();
}

class _Avatar3DViewportState extends State<Avatar3DViewport> {
  final Avatar3DController _controller = Avatar3DController();
  bool _showDetail = false;

  @override
  void initState() {
    super.initState();
    _controller.apply(widget.activity);
  }

  @override
  void didUpdateWidget(covariant Avatar3DViewport old) {
    super.didUpdateWidget(old);
    if (old.activity != widget.activity) {
      _controller.apply(widget.activity);
    }
  }

  void _onTap() {
    if (_controller.state == Avatar3DState.idle) {
      widget.onTapIdleFallback?.call();
      return;
    }
    setState(() => _showDetail = !_showDetail);
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final hasPhoto = widget.customPhotoPath != null &&
        widget.customPhotoPath!.isNotEmpty &&
        File(widget.customPhotoPath!).existsSync();

    return AnimatedBuilder(
      animation: _controller,
      builder: (context, _) {
        final act = _controller.activity;
        final showPill = act.state != Avatar3DState.idle && act.label.isNotEmpty;
        return Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            GestureDetector(
              behavior: HitTestBehavior.opaque,
              onTap: _onTap,
              onLongPress: widget.onLongPress,
              child: SizedBox(
                width: widget.size,
                height: widget.size * 0.92,
                child: Stack(
                  alignment: Alignment.center,
                  children: [
                    Positioned(
                      bottom: 2,
                      child: Container(
                        width: widget.size * 0.48,
                        height: widget.size * 0.06,
                        decoration: BoxDecoration(
                          borderRadius: BorderRadius.circular(widget.size),
                          gradient: RadialGradient(colors: [
                            Colors.black.withValues(alpha: 0.45),
                            Colors.black.withValues(alpha: 0),
                          ]),
                        ),
                      ),
                    ),
                    if (widget.geminiProfile != null)
                      Positioned.fill(
                        child: GeminiCharacterAvatar(
                          profile: widget.geminiProfile!,
                          state: act.state,
                          speaking: widget.speaking,
                          boxBackgroundColor: widget.boxBackgroundColor,
                        ),
                      )
                    else if (hasPhoto)
                      Positioned.fill(
                        child: PhotoCharacterAvatar(
                          photoPath: widget.customPhotoPath!,
                          state: act.state,
                        ),
                      )
                    else
                      Positioned.fill(child: widget.fallback),
                    Positioned.fill(
                      child: IgnorePointer(
                        child: WorkingPropOverlay(state: act.state),
                      ),
                    ),
                  ],
                ),
              ),
            ),
            AnimatedSize(
              duration: const Duration(milliseconds: 180),
              curve: Curves.easeOutCubic,
              child: showPill
                  ? Padding(
                      padding: const EdgeInsets.only(top: 6),
                      child: CleanStatusPill(activity: act),
                    )
                  : const SizedBox.shrink(),
            ),
            if (_showDetail && act.detail.isNotEmpty && act.state != Avatar3DState.idle)
              Padding(
                padding: const EdgeInsets.only(top: 6),
                child: AvatarDetailCard(detail: act.detail),
              ),
          ],
        );
      },
    );
  }
}
