import 'package:flutter/material.dart';

/// Shared palette for the dark UI; sheets and widgets reference these instead of raw hex.
abstract final class AppColors {
  static const text = Color(0xFFE6EDF3);
  static const textMuted = Color(0xFF8B949E);
  static const textFaint = Color(0xFF6E7681);
  static const sheet = Color(0xFF1A1D21);
  static const sheetDeep = Color(0xFF15181C);
  static const card = Color(0xFF111315);
  static const divider = Color(0xFF21262D);
  static const border = Color(0xFF30363D);
  static const accent = Color(0xFFD97757);
  static const success = Color(0xFF3FB950);
  static const info = Color(0xFF58A6FF);
  static const warning = Color(0xFFD29922);
  static const danger = Color(0xFFF85149);
  static const violet = Color(0xFFA371F7);
  static const avatarBoxDefault = Color(0xFF111418);
}

/// Rounded top corners shared by every bottom sheet.
const sheetShape = RoundedRectangleBorder(
  borderRadius: BorderRadius.vertical(top: Radius.circular(12)),
);

/// Sheet title style.
const sheetTitleStyle = TextStyle(
  color: AppColors.text,
  fontSize: 16,
  fontWeight: FontWeight.w600,
);

/// Outlined text field decoration used by the settings forms.
InputDecoration appFieldDecoration(String label) => InputDecoration(
      labelText: label,
      labelStyle: const TextStyle(color: AppColors.textMuted),
      enabledBorder: const OutlineInputBorder(
        borderSide: BorderSide(color: AppColors.border),
      ),
      focusedBorder: const OutlineInputBorder(
        borderSide: BorderSide(color: AppColors.accent),
      ),
    );
