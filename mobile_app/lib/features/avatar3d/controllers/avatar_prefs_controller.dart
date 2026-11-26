import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:path_provider/path_provider.dart';

import '../../../widgets/rive_avatar.dart';
import '../models/avatar3d_state.dart';

/// Look of the assistant (name, style, colors, custom character) persisted on the phone.
class AvatarPrefsController extends ChangeNotifier {
  String assistantName = 'HyprAgent';
  AvatarStyle style = AvatarStyle.cubeBot;
  AvatarAccessory accessory = AvatarAccessory.none;
  Color? accent;
  Color? boxBackground;
  String? customRivePath;
  String? customPhotoPath;
  GeminiAvatarProfile? geminiProfile;
  List<GeminiAvatarProfile> geminiVariants = const [];

  /// True while the PC is designing variants from a photo.
  bool isGenerating = false;

  static Future<File> _file() async {
    final dir = await getApplicationDocumentsDirectory();
    return File('${dir.path}/companion_prefs.json');
  }

  Future<void> load() async {
    try {
      final file = await _file();
      if (!await file.exists()) return;
      final data = jsonDecode(await file.readAsString()) as Map<String, dynamic>;
      final name = data['assistantName'] as String?;
      if (name != null && name.isNotEmpty) assistantName = name;
      style = _enumAt(AvatarStyle.values, data['avatarStyle']) ?? style;
      accessory = _enumAt(AvatarAccessory.values, data['avatarAccessory']) ?? accessory;
      final accentVal = data['avatarAccent'] as int?;
      if (accentVal != null) accent = Color(accentVal);
      final bgVal = data['avatarBoxBg'] as int?;
      if (bgVal != null) boxBackground = Color(bgVal);
      customRivePath = _existingFile(data['customRivePath']);
      customPhotoPath = _existingFile(data['customPhotoPath']);
      final gem = data['geminiProfile'] as Map<String, dynamic>?;
      if (gem != null) geminiProfile = GeminiAvatarProfile.fromJson(gem);
      final gemList = data['geminiVariants'] as List?;
      if (gemList != null && gemList.isNotEmpty) {
        geminiVariants = gemList
            .whereType<Map<String, dynamic>>()
            .map(GeminiAvatarProfile.fromJson)
            .toList();
      }
      notifyListeners();
    } catch (_) {}
  }

  Future<void> _save() async {
    try {
      final file = await _file();
      await file.writeAsString(jsonEncode({
        'assistantName': assistantName,
        'avatarStyle': style.index,
        'avatarAccessory': accessory.index,
        'avatarAccent': accent?.toARGB32(),
        'avatarBoxBg': boxBackground?.toARGB32(),
        'customRivePath': customRivePath,
        'customPhotoPath': customPhotoPath,
        'geminiProfile': geminiProfile?.toJson(),
        'geminiVariants': geminiVariants.map((e) => e.toJson()).toList(),
      }));
    } catch (_) {}
  }

  void _commit() {
    notifyListeners();
    _save();
  }

  void setAssistantName(String name) {
    if (name.trim().isEmpty) return;
    assistantName = name.trim();
    _commit();
  }

  /// A built-in style replaces any custom character.
  void selectStyle(AvatarStyle next) {
    style = next;
    customRivePath = null;
    customPhotoPath = null;
    geminiProfile = null;
    _commit();
  }

  bool isBuiltInStyle(AvatarStyle s) =>
      style == s && customRivePath == null && customPhotoPath == null && geminiProfile == null;

  void setAccessory(AvatarAccessory next) {
    accessory = next;
    _commit();
  }

  void setAccent(Color next) {
    accent = next;
    _commit();
  }

  void setBoxBackground(Color next) {
    boxBackground = next;
    _commit();
  }

  void setCustomRive(String path) {
    customRivePath = path;
    customPhotoPath = null;
    _commit();
  }

  void selectVariant(GeminiAvatarProfile v) {
    geminiProfile = v;
    customPhotoPath = null;
    customRivePath = null;
    _commit();
  }

  /// Applies [edit] to every variant and to the active profile, e.g. a hair change.
  void editVariants(GeminiAvatarProfile Function(GeminiAvatarProfile) edit) {
    geminiVariants = geminiVariants.map(edit).toList();
    if (geminiProfile != null) geminiProfile = edit(geminiProfile!);
    _commit();
  }

  /// Stores freshly generated variants and activates the first one.
  void setGeneratedVariants(List<GeminiAvatarProfile> variants) {
    isGenerating = false;
    if (variants.isEmpty) {
      notifyListeners();
      return;
    }
    geminiVariants = variants;
    selectVariant(variants.first);
  }

  void markGenerating() {
    isGenerating = true;
    notifyListeners();
  }

  static T? _enumAt<T>(List<T> values, Object? raw) {
    final idx = raw as int?;
    if (idx == null || idx < 0 || idx >= values.length) return null;
    return values[idx];
  }

  static String? _existingFile(Object? raw) {
    final path = raw as String?;
    if (path == null || path.isEmpty || !File(path).existsSync()) return null;
    return path;
  }
}
