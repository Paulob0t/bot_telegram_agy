import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:path_provider/path_provider.dart';
import 'package:speech_to_text/speech_to_text.dart' as stt;

import '../models/agent_event.dart';
import '../services/hub_credentials_store.dart';
import '../services/ws_client.dart';
import '../widgets/permission_request_card.dart';
import '../features/avatar3d/models/avatar3d_state.dart';
import '../features/avatar3d/widgets/avatar3d_viewport.dart';
import '../widgets/chat_message_bubble.dart';
import '../widgets/obsidian_vault_sheet.dart';
import '../widgets/commands_drawer.dart';
import '../widgets/rive_avatar.dart';
import '../widgets/session_status_banners.dart';
import '../widgets/worker_status_pill.dart';

class CompanionScreen extends StatefulWidget {
  final AgyWsClient client;

  const CompanionScreen({
    super.key,
    required this.client,
  });

  @override
  State<CompanionScreen> createState() => _CompanionScreenState();
}

class _CompanionScreenState extends State<CompanionScreen> with WidgetsBindingObserver {
  final TextEditingController _inputController = TextEditingController();
  final ScrollController _scrollController = ScrollController();
  final stt.SpeechToText _speech = stt.SpeechToText();
  bool _isListeningVoice = false;
  int _lastMsgCount = 0;
  String _assistantName = 'HyprAgent';
  AvatarStyle _avatarStyle = AvatarStyle.cubeBot;
  AvatarAccessory _avatarAccessory = AvatarAccessory.none;
  Color? _avatarAccent;
  Color? _avatarBoxBg;
  String? _customRivePath;
  String? _customPhotoPath;
  GeminiAvatarProfile? _geminiProfile;
  List<GeminiAvatarProfile> _geminiVariants = const [];
  bool _isGeneratingAvatar = false;
  final Set<String> _selectedMsgIds = {};
  final List<String> _stagedAttachments = [];
  String _slashQuery = '';

  bool get _isSelectionMode => _selectedMsgIds.isNotEmpty;

  void _toggleMsgSelection(String msgId) {
    setState(() {
      if (_selectedMsgIds.contains(msgId)) {
        _selectedMsgIds.remove(msgId);
      } else {
        _selectedMsgIds.add(msgId);
      }
    });
  }

  void _selectAllMessages() {
    setState(() {
      if (_selectedMsgIds.length == widget.client.messages.length) {
        _selectedMsgIds.clear();
      } else {
        _selectedMsgIds
          ..clear()
          ..addAll(widget.client.messages.map((m) => m.id));
      }
    });
  }

  void _copySelectedMessages() {
    final selected = widget.client.messages
        .where((m) => _selectedMsgIds.contains(m.id) && m.text.trim().isNotEmpty)
        .toList();
    if (selected.isEmpty) return;

    final text = selected.length == 1
        ? selected.first.text.trim()
        : selected
            .map((m) => m.text.trim())
            .join('\n\n');

    Clipboard.setData(ClipboardData(text: text));
    final count = selected.length;
    setState(() => _selectedMsgIds.clear());
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text('$count ${count == 1 ? "mensaje copiado" : "mensajes copiados"} al portapapeles'),
        duration: const Duration(seconds: 2),
      ),
    );
  }

  void _deleteSelectedMessages() {
    final ids = _selectedMsgIds.toList();
    setState(() => _selectedMsgIds.clear());
    for (final id in ids) {
      widget.client.deleteMessage(id);
    }
  }

  static const List<(String, String)> _allSlashCommands = [
    ('/obsidian', 'Bóveda de Obsidian y Prompts'),
    ('/scan', 'Escanear PC y proyectos'),
    ('/status', 'Estado del sistema'),
    ('/model', 'Cambiar modelo'),
    ('/resume', 'Historial de chats'),
    ('/new', 'Nuevo chat (elige proyecto)'),
    ('/tokens', 'Cuotas de tokens'),
    ('/stop', 'Detener ejecución'),
  ];

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    widget.client.addListener(_onClientUpdated);
    _inputController.addListener(_onTextChanged);
    _loadAvatarPrefs();
  }

  bool _setupPrompted = false;

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    widget.client.isAppInForeground = state == AppLifecycleState.resumed;
  }

  Future<File> _prefsFile() async {
    final dir = await getApplicationDocumentsDirectory();
    return File('${dir.path}/companion_prefs.json');
  }

  Future<void> _loadAvatarPrefs() async {
    try {
      final file = await _prefsFile();
      if (!await file.exists()) return;
      final data = jsonDecode(await file.readAsString()) as Map<String, dynamic>;
      if (!mounted) return;
      setState(() {
        final name = data['assistantName'] as String?;
        if (name != null && name.isNotEmpty) _assistantName = name;
        final styleIdx = data['avatarStyle'] as int?;
        if (styleIdx != null && styleIdx >= 0 && styleIdx < AvatarStyle.values.length) {
          _avatarStyle = AvatarStyle.values[styleIdx];
        }
        final accIdx = data['avatarAccessory'] as int?;
        if (accIdx != null && accIdx >= 0 && accIdx < AvatarAccessory.values.length) {
          _avatarAccessory = AvatarAccessory.values[accIdx];
        }
        final accentVal = data['avatarAccent'] as int?;
        if (accentVal != null) _avatarAccent = Color(accentVal);
        final bgVal = data['avatarBoxBg'] as int?;
        if (bgVal != null) _avatarBoxBg = Color(bgVal);
        final riv = data['customRivePath'] as String?;
        if (riv != null && riv.isNotEmpty && File(riv).existsSync()) {
          _customRivePath = riv;
        }
        final photo = data['customPhotoPath'] as String?;
        if (photo != null && photo.isNotEmpty && File(photo).existsSync()) {
          _customPhotoPath = photo;
        }
        final gem = data['geminiProfile'] as Map<String, dynamic>?;
        if (gem != null) {
          _geminiProfile = GeminiAvatarProfile.fromJson(gem);
        }
        final gemList = data['geminiVariants'] as List?;
        if (gemList != null && gemList.isNotEmpty) {
          _geminiVariants = gemList
              .whereType<Map<String, dynamic>>()
              .map(GeminiAvatarProfile.fromJson)
              .toList();
        }
      });
    } catch (_) {}
  }

  Future<void> _saveAvatarPrefs() async {
    try {
      final file = await _prefsFile();
      await file.writeAsString(jsonEncode({
        'assistantName': _assistantName,
        'avatarStyle': _avatarStyle.index,
        'avatarAccessory': _avatarAccessory.index,
        'avatarAccent': _avatarAccent?.toARGB32(),
        'avatarBoxBg': _avatarBoxBg?.toARGB32(),
        'customRivePath': _customRivePath,
        'customPhotoPath': _customPhotoPath,
        'geminiProfile': _geminiProfile?.toJson(),
        'geminiVariants': _geminiVariants.map((e) => e.toJson()).toList(),
      }));
    } catch (_) {}
  }

  Future<void> _generateGeminiAvatarFromPhoto(String photoPath) async {
    if (_isGeneratingAvatar) return;
    setState(() => _isGeneratingAvatar = true);
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(
        content: Text('Diseñando 4 opciones de personaje desde tu foto…'),
        duration: Duration(seconds: 4),
      ),
    );
    final variants = await widget.client.generateAvatarVariantsFromPhoto(photoPath);
    if (!mounted) return;
    setState(() {
      _isGeneratingAvatar = false;
      if (variants.isNotEmpty) {
        _geminiVariants = variants;
        _geminiProfile = variants.first;
        _customPhotoPath = null;
        _customRivePath = null;
      }
    });
    if (variants.isNotEmpty) {
      await _saveAvatarPrefs();
      if (!mounted) return;
      _openGeminiVariantPicker(variants);
    }
  }

  void _openGeminiVariantPicker(List<GeminiAvatarProfile> variants) {
    showModalBottomSheet<void>(
      context: context,
      backgroundColor: const Color(0xFF15181C),
      isScrollControlled: true,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (sheetCtx) {
        return SafeArea(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(18, 18, 18, 22),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                const Text(
                  'Elige tu versión del personaje',
                  style: TextStyle(
                    color: Color(0xFFE6EDF3),
                    fontSize: 16,
                    fontWeight: FontWeight.w600,
                  ),
                ),
                const SizedBox(height: 4),
                  const Text(
                    '4 estilos creados con los rasgos y paleta de tu foto',
                    style: TextStyle(color: Color(0xFF8B949E), fontSize: 12),
                  ),
                  const SizedBox(height: 10),
                  StatefulBuilder(
                    builder: (ctx, setGridState) {
                      final isFem = _geminiProfile?.isFeminine ?? variants.first.isFeminine;
                      final activeHair = _geminiProfile?.hairStyle ?? variants.first.hairStyle;
                      return Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [
                          SingleChildScrollView(
                            scrollDirection: Axis.horizontal,
                            child: Row(
                              children: [
                                const Text(
                                  'Rasgos:',
                                  style: TextStyle(color: Color(0xFF8B949E), fontSize: 12),
                                ),
                                const SizedBox(width: 8),
                                ChoiceChip(
                                  label: const Text('Masculino'),
                                  selected: !isFem,
                                  onSelected: (_) {
                                    setState(() {
                                      _geminiVariants = _geminiVariants
                                          .map((e) => e.copyWith(
                                                genderStyle: 'masculine',
                                                eyelashes: false,
                                              ))
                                          .toList();
                                      if (_geminiProfile != null) {
                                        _geminiProfile = _geminiProfile!.copyWith(
                                          genderStyle: 'masculine',
                                          eyelashes: false,
                                        );
                                      }
                                    });
                                    _saveAvatarPrefs();
                                    setGridState(() {});
                                  },
                                ),
                                const SizedBox(width: 8),
                                ChoiceChip(
                                  label: const Text('Femenino'),
                                  selected: isFem,
                                  onSelected: (_) {
                                    setState(() {
                                      _geminiVariants = _geminiVariants
                                          .map((e) => e.copyWith(
                                                genderStyle: 'feminine',
                                                eyelashes: true,
                                              ))
                                          .toList();
                                      if (_geminiProfile != null) {
                                        _geminiProfile = _geminiProfile!.copyWith(
                                          genderStyle: 'feminine',
                                          eyelashes: true,
                                        );
                                      }
                                    });
                                    _saveAvatarPrefs();
                                    setGridState(() {});
                                  },
                                ),
                              ],
                            ),
                          ),
                          const SizedBox(height: 6),
                          SingleChildScrollView(
                            scrollDirection: Axis.horizontal,
                            child: Row(
                              children: [
                                const Text(
                                  'Cabello:',
                                  style: TextStyle(color: Color(0xFF8B949E), fontSize: 12),
                                ),
                                const SizedBox(width: 8),
                                for (final h in const [
                                  ('curly', 'Rizado'),
                                  ('short', 'Lacio corto'),
                                  ('long', 'Largo'),
                                  ('afro', 'Afro / Rizo grande'),
                                ])
                                  Padding(
                                    padding: const EdgeInsets.only(right: 6),
                                    child: ChoiceChip(
                                      label: Text(h.$2),
                                      selected: activeHair == h.$1,
                                      onSelected: (_) {
                                        setState(() {
                                          _geminiVariants = _geminiVariants
                                              .map((e) => e.copyWith(hairStyle: h.$1))
                                              .toList();
                                          if (_geminiProfile != null) {
                                            _geminiProfile =
                                                _geminiProfile!.copyWith(hairStyle: h.$1);
                                          }
                                        });
                                        _saveAvatarPrefs();
                                        setGridState(() {});
                                      },
                                    ),
                                  ),
                              ],
                            ),
                          ),
                          const SizedBox(height: 8),
                          SingleChildScrollView(
                            scrollDirection: Axis.horizontal,
                            child: Row(
                              children: [
                                const Text(
                                  'Fondo cuadro:',
                                  style: TextStyle(color: Color(0xFF8B949E), fontSize: 12),
                                ),
                                const SizedBox(width: 8),
                                for (final bg in const [
                                  Color(0xFF111418),
                                  Color(0xFF050608),
                                  Color(0xFF1B2432),
                                  Color(0xFF1A2821),
                                  Color(0xFF2B1F1D),
                                  Color(0xFF241E2E),
                                  Color(0xFFE8DFD5),
                                ])
                                  GestureDetector(
                                    onTap: () {
                                      setState(() => _avatarBoxBg = bg);
                                      _saveAvatarPrefs();
                                      setGridState(() {});
                                    },
                                    child: Container(
                                      margin: const EdgeInsets.only(right: 8),
                                      width: 24,
                                      height: 24,
                                      decoration: BoxDecoration(
                                        color: bg,
                                        borderRadius: BorderRadius.circular(6),
                                        border: Border.all(
                                          color: (_avatarBoxBg ?? const Color(0xFF111418)) == bg
                                              ? const Color(0xFFD97757)
                                              : const Color(0xFF30363D),
                                          width: (_avatarBoxBg ?? const Color(0xFF111418)) == bg ? 2 : 1,
                                        ),
                                      ),
                                    ),
                                  ),
                              ],
                            ),
                          ),
                          const SizedBox(height: 12),
                          GridView.builder(
                            shrinkWrap: true,
                            physics: const NeverScrollableScrollPhysics(),
                            itemCount: _geminiVariants.length,
                            gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
                              crossAxisCount: 2,
                              mainAxisSpacing: 12,
                              crossAxisSpacing: 12,
                              childAspectRatio: 1.05,
                            ),
                            itemBuilder: (ctx, idx) {
                              final v = _geminiVariants[idx];
                              final isSelected = _geminiProfile?.variant == v.variant;
                              return GestureDetector(
                                onTap: () {
                                  setState(() {
                                    _geminiProfile = v;
                                    _customPhotoPath = null;
                                    _customRivePath = null;
                                  });
                                  _saveAvatarPrefs();
                                  Navigator.of(sheetCtx).pop();
                                },
                                child: Container(
                                  padding: const EdgeInsets.all(10),
                                  decoration: BoxDecoration(
                                    color: const Color(0xFF0E1013),
                                    borderRadius: BorderRadius.circular(12),
                                    border: Border.all(
                                      color: isSelected
                                          ? const Color(0xFFD97757)
                                          : const Color(0xFF262B32),
                                      width: isSelected ? 2 : 1,
                                    ),
                                  ),
                                  child: Column(
                                    mainAxisAlignment: MainAxisAlignment.center,
                                    children: [
                                      Expanded(
                                        child: Center(
                                          child: GeminiCharacterPreview(
                                            profile: v,
                                            size: 86,
                                            boxBackgroundColor: _avatarBoxBg,
                                          ),
                                        ),
                                      ),
                                      const SizedBox(height: 6),
                                      Text(
                                        v.vibe,
                                        style: TextStyle(
                                          color: isSelected
                                              ? const Color(0xFFD97757)
                                              : const Color(0xFFE6EDF3),
                                          fontSize: 12.5,
                                          fontWeight: FontWeight.w600,
                                        ),
                                      ),
                                    ],
                                  ),
                                ),
                              );
                            },
                          ),
                        ],
                      );
                    },
                  ),
              ],
            ),
          ),
        );
      },
    );
  }

  void _onTextChanged() {
    final text = _inputController.text;
    widget.client.setUserListening(text.trim().isNotEmpty);
    final nextQuery = text.startsWith('/') && !text.contains(' ')
        ? text.toLowerCase()
        : '';
    if (nextQuery != _slashQuery) {
      setState(() => _slashQuery = nextQuery);
    }
  }

  void _onClientUpdated() {
    if (!mounted) return;
    setState(() {});
    if (widget.client.needsSetup && !_setupPrompted) {
      _setupPrompted = true;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) _openConnectionSheet();
      });
    }
    if (widget.client.messages.length != _lastMsgCount) {
      _lastMsgCount = widget.client.messages.length;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (_scrollController.hasClients) {
          _scrollController.animateTo(
            _scrollController.position.maxScrollExtent,
            duration: const Duration(milliseconds: 180),
            curve: Curves.easeOut,
          );
        }
      });
    }
  }

  @override
  void dispose() {
    widget.client.removeListener(_onClientUpdated);
    _inputController.removeListener(_onTextChanged);
    _inputController.dispose();
    _scrollController.dispose();
    super.dispose();
  }

  Future<void> _handleSend() async {
    final text = _inputController.text.trim();
    if (text.isEmpty && _stagedAttachments.isEmpty) return;

    if (_stagedAttachments.isNotEmpty) {
      final filesToSend = List<String>.from(_stagedAttachments);
      setState(() => _stagedAttachments.clear());
      _inputController.clear();
      await widget.client.uploadAttachmentsWithPrompt(filesToSend, text);
      return;
    }

    final lower = text.toLowerCase();
    if (lower == '/obsidian' || lower == '/prompts') {
      _inputController.clear();
      _openObsidianVault();
      return;
    }
    if (lower == '/model' || lower == '/modelo') {
      _inputController.clear();
      _openOptionPicker('/model');
      return;
    }
    if (lower == '/project' || lower == '/cd' || lower == '/new' || lower == '/nuevo') {
      _inputController.clear();
      _openOptionPicker('/project');
      return;
    }
    if (lower == '/resume' || lower == '/chats') {
      _inputController.clear();
      _openOptionPicker('/resume');
      return;
    }
    if (widget.client.sendPrompt(text)) {
      _inputController.clear();
    }
  }

  Future<void> _pickImagesForPrompt() async {
    if (!widget.client.isConnected) return;
    final res = await FilePicker.pickFiles(
      type: FileType.image,
    );
    if (res.isEmpty || !mounted) return;
    setState(() {
      for (final f in res) {
        final p = f.path;
        if (p != null && !_stagedAttachments.contains(p)) {
          _stagedAttachments.add(p);
        }
      }
    });
  }

  Future<void> _pickAndUploadFile() async {
    if (!widget.client.isConnected) return;
    final res = await FilePicker.pickFiles(type: FileType.any);
    if (res.isEmpty) return;
    final file = res.first;
    final path = file.path;
    if (path == null || !mounted) return;

    showModalBottomSheet<void>(
      context: context,
      backgroundColor: const Color(0xFF1A1D21),
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(12)),
      ),
      builder: (ctx) {
        return SafeArea(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(20, 16, 20, 8),
                child: Text(
                  'Archivo: ${file.name}',
                  style: const TextStyle(
                    color: Color(0xFFE6EDF3),
                    fontSize: 15,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ),
              ListTile(
                leading: const Icon(Icons.add_photo_alternate_rounded, color: Color(0xFFD97757)),
                title: const Text('Adjuntar al mensaje para escribir texto', style: TextStyle(color: Color(0xFFE6EDF3))),
                onTap: () {
                  Navigator.of(ctx).pop();
                  setState(() {
                    if (!_stagedAttachments.contains(path)) {
                      _stagedAttachments.add(path);
                    }
                  });
                },
              ),
              ListTile(
                leading: const Icon(Icons.auto_awesome, color: Color(0xFFD97757)),
                title: const Text('Enviar directo para analizar con IA', style: TextStyle(color: Color(0xFFE6EDF3))),
                onTap: () {
                  Navigator.of(ctx).pop();
                  widget.client.uploadFile(path, target: 'cache', analyzeWithAI: true);
                },
              ),
              if (path.toLowerCase().endsWith('.png') ||
                  path.toLowerCase().endsWith('.jpg') ||
                  path.toLowerCase().endsWith('.jpeg') ||
                  path.toLowerCase().endsWith('.webp'))
                ListTile(
                  leading: const Icon(Icons.face_retouching_natural, color: Color(0xFFA371F7)),
                  title: const Text('Crear personaje con Gemini (con laptop)', style: TextStyle(color: Color(0xFFE6EDF3))),
                  onTap: () {
                    Navigator.of(ctx).pop();
                    _generateGeminiAvatarFromPhoto(path);
                  },
                ),
              ListTile(
                leading: const Icon(Icons.folder_outlined, color: Color(0xFF58A6FF)),
                title: const Text('Guardar en proyecto (cwd)', style: TextStyle(color: Color(0xFFE6EDF3))),
                onTap: () {
                  Navigator.of(ctx).pop();
                  widget.client.uploadFile(path, target: 'cwd');
                },
              ),
              ListTile(
                leading: const Icon(Icons.download_outlined, color: Color(0xFF3FB950)),
                title: const Text('Guardar en ~/Downloads', style: TextStyle(color: Color(0xFFE6EDF3))),
                onTap: () {
                  Navigator.of(ctx).pop();
                  widget.client.uploadFile(path, target: 'downloads');
                },
              ),
            ],
          ),
        );
      },
    );
  }

  void _openConnectionSheet() {
    final nameCtrl = TextEditingController(text: _assistantName);
    final urlCtrl = TextEditingController(text: widget.client.wsUrl);
    final tokCtrl = TextEditingController(text: widget.client.bearerToken);
    final hub = widget.client.hubCredentials;
    var hubEnabled = hub?.enabled ?? false;
    final gistIdCtrl = TextEditingController(text: hub?.gistId ?? '');
    final gistTokCtrl = TextEditingController(text: hub?.gistToken ?? '');
    final hubSecretCtrl = TextEditingController(text: hub?.hubSecret ?? '');
    InputDecoration field(String label) => InputDecoration(
          labelText: label,
          labelStyle: const TextStyle(color: Color(0xFF8B949E)),
          enabledBorder: const OutlineInputBorder(
            borderSide: BorderSide(color: Color(0xFF30363D)),
          ),
          focusedBorder: const OutlineInputBorder(
            borderSide: BorderSide(color: Color(0xFFD97757)),
          ),
        );

    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      backgroundColor: const Color(0xFF1A1D21),
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(12)),
      ),
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setSheetState) => Padding(
          padding: EdgeInsets.only(
            left: 20,
            right: 20,
            top: 20,
            bottom: MediaQuery.of(ctx).viewInsets.bottom + 24,
          ),
          child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const Text(
                'Configuración del Asistente',
                style: TextStyle(
                  color: Color(0xFFE6EDF3),
                  fontSize: 16,
                  fontWeight: FontWeight.w600,
                ),
              ),
              const SizedBox(height: 14),
              TextField(
                controller: nameCtrl,
                style: const TextStyle(color: Color(0xFFE6EDF3), fontSize: 14),
                decoration: const InputDecoration(
                  labelText: 'Nombre del asistente',
                  labelStyle: TextStyle(color: Color(0xFF8B949E)),
                  enabledBorder: OutlineInputBorder(
                    borderSide: BorderSide(color: Color(0xFF30363D)),
                  ),
                  focusedBorder: OutlineInputBorder(
                    borderSide: BorderSide(color: Color(0xFFD97757)),
                  ),
                ),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: urlCtrl,
                style: const TextStyle(color: Color(0xFFE6EDF3), fontSize: 14),
                decoration: const InputDecoration(
                  labelText: 'WebSocket URL (ws://100.x.y.z:8080/ws)',
                  labelStyle: TextStyle(color: Color(0xFF8B949E)),
                  enabledBorder: OutlineInputBorder(
                    borderSide: BorderSide(color: Color(0xFF30363D)),
                  ),
                  focusedBorder: OutlineInputBorder(
                    borderSide: BorderSide(color: Color(0xFFD97757)),
                  ),
                ),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: tokCtrl,
                obscureText: true,
                style: const TextStyle(color: Color(0xFFE6EDF3), fontSize: 14),
                decoration: const InputDecoration(
                  labelText: 'Bearer Token (MOBILE_WS_TOKEN)',
                  labelStyle: TextStyle(color: Color(0xFF8B949E)),
                  enabledBorder: OutlineInputBorder(
                    borderSide: BorderSide(color: Color(0xFF30363D)),
                  ),
                  focusedBorder: OutlineInputBorder(
                    borderSide: BorderSide(color: Color(0xFFD97757)),
                  ),
                ),
              ),
              const SizedBox(height: 8),
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                value: hubEnabled,
                onChanged: (v) => setSheetState(() => hubEnabled = v),
                title: const Text(
                  'Conectar por Hub (Gist)',
                  style: TextStyle(color: Color(0xFFE6EDF3), fontSize: 14),
                ),
                subtitle: const Text(
                  'Encuentra la PC aunque cambie de red',
                  style: TextStyle(color: Color(0xFF8B949E), fontSize: 12),
                ),
              ),
              if (hubEnabled) ...[
                const SizedBox(height: 8),
                TextField(
                  controller: gistIdCtrl,
                  style: const TextStyle(color: Color(0xFFE6EDF3), fontSize: 14),
                  decoration: field('Gist ID'),
                ),
                const SizedBox(height: 12),
                TextField(
                  controller: gistTokCtrl,
                  obscureText: true,
                  style: const TextStyle(color: Color(0xFFE6EDF3), fontSize: 14),
                  decoration: field('Token de GitHub (solo permiso gist)'),
                ),
                const SizedBox(height: 12),
                TextField(
                  controller: hubSecretCtrl,
                  obscureText: true,
                  style: const TextStyle(color: Color(0xFFE6EDF3), fontSize: 14),
                  decoration: field('Secreto del Hub'),
                ),
              ],
              const SizedBox(height: 16),
              FilledButton(
                style: FilledButton.styleFrom(
                  backgroundColor: const Color(0xFFD97757),
                  foregroundColor: const Color(0xFF0D1117),
                  shape: RoundedRectangleBorder(
                    borderRadius: BorderRadius.circular(8),
                  ),
                ),
                onPressed: () {
                  Navigator.of(ctx).pop();
                  final nextName = nameCtrl.text.trim();
                  if (nextName.isNotEmpty) {
                    setState(() => _assistantName = nextName);
                    _saveAvatarPrefs();
                  }
                  final client = widget.client;
                  final hubCreds = HubCredentials(
                    enabled: hubEnabled,
                    gistToken: gistTokCtrl.text.trim(),
                    gistId: gistIdCtrl.text.trim(),
                    hubSecret: hubSecretCtrl.text.trim(),
                  );
                  // Persist direct-mode values first, then reconnect through the hub when on.
                  unawaited(() async {
                    await client.updateCredentials(
                      wsUrl: urlCtrl.text,
                      bearerToken: tokCtrl.text,
                    );
                    await client.configureHubRelay(hubCreds);
                  }());
                },
                child: const Text(
                  'Guardar y Reconectar',
                  style: TextStyle(fontWeight: FontWeight.w600),
                ),
              ),
            ],
          ),
          ),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final state = widget.client.characterState;

    return Scaffold(
      backgroundColor: const Color(0xFF0B0C0E),
      drawer: _buildCommandsDrawer(),
      body: SafeArea(
        child: Column(
          children: [
            PermissionRequestCard(client: widget.client),
            SessionStatusBanners(client: widget.client),
            // Header minimalista fijo centrado con Stack: [=]  [Cuadrito + Nombre]  [🔔] [⚙️]
            Padding(
              padding: const EdgeInsets.fromLTRB(12, 10, 12, 8),
              child: Stack(
                alignment: Alignment.topCenter,
                children: [
                  Align(
                    alignment: Alignment.topLeft,
                    child: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Builder(
                          builder: (btnCtx) {
                            final hasUnread = widget.client.sessions.any((s) => s.hasUnread);
                            return Stack(
                              clipBehavior: Clip.none,
                              children: [
                                IconButton(
                                  tooltip: 'Menú de comandos',
                                  onPressed: () => Scaffold.of(btnCtx).openDrawer(),
                                  style: IconButton.styleFrom(
                                    backgroundColor: const Color(0xFF1A1D21),
                                  ),
                                  icon: const Icon(Icons.drag_handle, color: Color(0xFFE6EDF3), size: 20),
                                ),
                                if (hasUnread)
                                  Positioned(
                                    right: 6,
                                    top: 6,
                                    child: Container(
                                      width: 9,
                                      height: 9,
                                      decoration: const BoxDecoration(
                                        color: Color(0xFF3FB950),
                                        shape: BoxShape.circle,
                                      ),
                                    ),
                                  ),
                              ],
                            );
                          },
                        ),
                        if (widget.client.activeWorker.isNotEmpty) ...[
                          const SizedBox(width: 6),
                          WorkerStatusPill(activeWorker: widget.client.activeWorker),
                        ],
                      ],
                    ),
                  ),
                  GestureDetector(
                    onTap: () => _onAvatarTap(state),
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Avatar3DViewport(
                          activity: widget.client.activity,
                          speaking: state == CharacterState.speaking,
                          size: 112,
                          customPhotoPath: _customPhotoPath,
                          geminiProfile: _geminiProfile,
                          boxBackgroundColor: _avatarBoxBg,
                          onLongPress: _confirmStop,
                          onTapIdleFallback: () => _onAvatarTap(state),
                          fallback: Center(
                            child: SizedBox(
                              width: 76,
                              height: 66,
                              child: RiveCompanionAvatar(
                                state: state,
                                style: _avatarStyle,
                                accessory: _avatarAccessory,
                                customAccent: _avatarAccent,
                                customBgColor: _avatarBoxBg,
                                customRivePath: _customRivePath,
                              ),
                            ),
                          ),
                        ),
                        const SizedBox(height: 6),
                        Container(
                          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 3),
                          decoration: BoxDecoration(
                            color: const Color(0xFF1A1D21),
                            borderRadius: BorderRadius.circular(14),
                          ),
                          child: Text(
                            _assistantName,
                            style: const TextStyle(
                              color: Color(0xFFE6EDF3),
                              fontSize: 12,
                              fontWeight: FontWeight.w500,
                            ),
                          ),
                        ),
                        if (state != CharacterState.idle && widget.client.activity.label.isEmpty) ...[
                          const SizedBox(height: 5),
                          _buildStatusBubble(state),
                        ],
                      ],
                    ),
                  ),
                  Align(
                    alignment: Alignment.topRight,
                    child: IconButton(
                      tooltip: 'Buzón de notificaciones',
                      onPressed: _openNotificationsInbox,
                      style: IconButton.styleFrom(
                        backgroundColor: const Color(0xFF1A1D21),
                      ),
                      icon: Badge(
                        isLabelVisible: widget.client.unreadNotifications > 0,
                        label: Text('${widget.client.unreadNotifications}'),
                        child: const Icon(Icons.inbox_rounded, color: Color(0xFFE6EDF3), size: 20),
                      ),
                    ),
                  ),
                ],
              ),
            ),

            if (_isSelectionMode)
              Container(
                width: double.infinity,
                padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
                decoration: const BoxDecoration(
                  color: Color(0xFF161B22),
                  border: Border(
                    top: BorderSide(color: Color(0xFF262B32)),
                    bottom: BorderSide(color: Color(0xFF262B32)),
                  ),
                ),
                child: Row(
                  children: [
                    IconButton(
                      visualDensity: VisualDensity.compact,
                      tooltip: 'Cancelar selección',
                      onPressed: () => setState(() => _selectedMsgIds.clear()),
                      icon: const Icon(Icons.close_rounded, color: Color(0xFFE6EDF3), size: 19),
                    ),
                    const SizedBox(width: 4),
                    Expanded(
                      child: Text(
                        '${_selectedMsgIds.length} ${_selectedMsgIds.length == 1 ? "seleccionado" : "seleccionados"}',
                        style: const TextStyle(
                          color: Color(0xFFE6EDF3),
                          fontSize: 13,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                    ),
                    TextButton(
                      onPressed: _selectAllMessages,
                      child: Text(
                        _selectedMsgIds.length == widget.client.messages.length ? 'Ninguno' : 'Todos',
                        style: const TextStyle(color: Color(0xFF8B949E), fontSize: 12),
                      ),
                    ),
                    FilledButton.tonalIcon(
                      style: FilledButton.styleFrom(
                        backgroundColor: const Color(0xFFD97757),
                        foregroundColor: const Color(0xFF0D1117),
                        visualDensity: VisualDensity.compact,
                        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
                      ),
                      onPressed: _copySelectedMessages,
                      icon: const Icon(Icons.copy_rounded, size: 15),
                      label: const Text(
                        'Copiar',
                        style: TextStyle(fontSize: 12, fontWeight: FontWeight.w600),
                      ),
                    ),
                    const SizedBox(width: 4),
                    IconButton(
                      visualDensity: VisualDensity.compact,
                      tooltip: 'Eliminar seleccionados',
                      onPressed: _deleteSelectedMessages,
                      icon: const Icon(Icons.delete_outline_rounded, color: Color(0xFFF85149), size: 19),
                    ),
                  ],
                ),
              ),

            // Área principal del chat sin choques
            Expanded(
              child: widget.client.messages.isEmpty
                  ? Center(
                      child: Padding(
                        padding: const EdgeInsets.symmetric(horizontal: 32),
                        child: Text(
                          state == CharacterState.sleeping
                              ? 'Esperando conexión con tu PC por Tailscale (${widget.client.wsUrl}).'
                              : 'Escribe una tarea para ejecutarla en tu sesión activa de AGY o Claude.',
                          textAlign: TextAlign.center,
                          style: const TextStyle(
                            color: Color(0xFF8B949E),
                            fontSize: 13,
                            height: 1.4,
                          ),
                        ),
                      ),
                    )
                  : ListView.builder(
                      controller: _scrollController,
                      padding: const EdgeInsets.fromLTRB(16, 8, 16, 12),
                      itemCount: widget.client.messages.length,
                      itemBuilder: (context, index) {
                        final msg = widget.client.messages[index];
                        final isSelected = _selectedMsgIds.contains(msg.id);
                        return ChatMessageBubble(
                          message: msg,
                          selectionMode: _isSelectionMode,
                          isSelected: isSelected,
                          onToggleSelect: () => _toggleMsgSelection(msg.id),
                          onCopy: () {
                            Clipboard.setData(ClipboardData(text: msg.text));
                            ScaffoldMessenger.of(context).showSnackBar(
                              const SnackBar(
                                content: Text('Texto copiado'),
                                duration: Duration(seconds: 1),
                              ),
                            );
                          },
                          onDelete: () => widget.client.deleteMessage(msg.id),
                        );
                      },
                    ),
            ),

            if (_slashQuery.isNotEmpty)
              Container(
                width: double.infinity,
                padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
                decoration: const BoxDecoration(
                  color: Color(0xFF15181C),
                  border: Border(
                    top: BorderSide(color: Color(0xFF21262D)),
                  ),
                ),
                child: SingleChildScrollView(
                  scrollDirection: Axis.horizontal,
                  child: Row(
                    children: [
                      for (final cmd in _allSlashCommands.where(
                        (c) => c.$1.startsWith(_slashQuery),
                      ))
                        Padding(
                          padding: const EdgeInsets.only(right: 8),
                          child: ActionChip(
                            backgroundColor: const Color(0xFF1F242A),
                            side: const BorderSide(color: Color(0xFF30363D)),
                            label: Text(
                              '${cmd.$1} · ${cmd.$2}',
                              style: const TextStyle(
                                color: Color(0xFFE6EDF3),
                                fontSize: 12,
                              ),
                            ),
                            onPressed: () {
                              _inputController.text = '${cmd.$1} ';
                              _inputController.selection = TextSelection.fromPosition(
                                TextPosition(offset: _inputController.text.length),
                              );
                            },
                          ),
                        ),
                    ],
                  ),
                ),
              ),

            if (_slashQuery.isEmpty && widget.client.isConnected)
              Container(
                width: double.infinity,
                padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
                color: const Color(0xFF0B0C0E),
                child: SingleChildScrollView(
                  scrollDirection: Axis.horizontal,
                  child: Row(
                    children: [
                      for (final quick in const ['Sí', 'Dale', 'Cancela', '/status'])
                        Padding(
                          padding: const EdgeInsets.only(right: 6),
                          child: ActionChip(
                            backgroundColor: const Color(0xFF15181C),
                            side: const BorderSide(color: Color(0xFF21262D)),
                            label: Text(
                              quick,
                              style: const TextStyle(color: Color(0xFF8B949E), fontSize: 11),
                            ),
                            onPressed: () => widget.client.sendPrompt(quick),
                          ),
                        ),
                    ],
                  ),
                ),
              ),

            // Barra de capturas/imágenes adjuntas listas para enviar con tu texto
            if (_stagedAttachments.isNotEmpty)
              Container(
                width: double.infinity,
                padding: const EdgeInsets.fromLTRB(12, 8, 12, 4),
                decoration: const BoxDecoration(
                  color: Color(0xFF15181C),
                  border: Border(
                    top: BorderSide(color: Color(0xFF21262D)),
                  ),
                ),
                child: SizedBox(
                  height: 62,
                  child: ListView.separated(
                    scrollDirection: Axis.horizontal,
                    itemCount: _stagedAttachments.length,
                    separatorBuilder: (_, __) => const SizedBox(width: 10),
                    itemBuilder: (ctx, idx) {
                      final path = _stagedAttachments[idx];
                      final isImg = path.toLowerCase().endsWith('.png') ||
                          path.toLowerCase().endsWith('.jpg') ||
                          path.toLowerCase().endsWith('.jpeg') ||
                          path.toLowerCase().endsWith('.webp');
                      return Stack(
                        clipBehavior: Clip.none,
                        children: [
                          Container(
                            width: 58,
                            height: 58,
                            decoration: BoxDecoration(
                              color: const Color(0xFF0D1117),
                              borderRadius: BorderRadius.circular(10),
                              border: Border.all(color: const Color(0xFFD97757)),
                            ),
                            clipBehavior: Clip.antiAlias,
                            child: isImg && File(path).existsSync()
                                ? Image.file(File(path), fit: BoxFit.cover)
                                : Center(
                                    child: Text(
                                      path.split('/').last,
                                      maxLines: 2,
                                      textAlign: TextAlign.center,
                                      style: const TextStyle(
                                        color: Color(0xFFE6EDF3),
                                        fontSize: 9,
                                      ),
                                    ),
                                  ),
                          ),
                          Positioned(
                            top: -5,
                            right: -5,
                            child: GestureDetector(
                              onTap: () => setState(() => _stagedAttachments.removeAt(idx)),
                              child: Container(
                                padding: const EdgeInsets.all(2),
                                decoration: const BoxDecoration(
                                  color: Color(0xFF21262D),
                                  shape: BoxShape.circle,
                                ),
                                child: const Icon(
                                  Icons.close_rounded,
                                  size: 13,
                                  color: Color(0xFFE6EDF3),
                                ),
                              ),
                            ),
                          ),
                        ],
                      );
                    },
                  ),
                ),
              ),

            // Barra de entrada
            Container(
              padding: const EdgeInsets.fromLTRB(8, 8, 12, 12),
              decoration: BoxDecoration(
                color: const Color(0xFF15181C),
                border: _stagedAttachments.isEmpty
                    ? const Border(top: BorderSide(color: Color(0xFF21262D)))
                    : null,
              ),
              child: Row(
                children: [
                  PopupMenuButton<String>(
                    enabled: widget.client.isConnected,
                    tooltip: 'Adjuntar o dictar',
                    color: const Color(0xFF1A1D21),
                    shape: RoundedRectangleBorder(
                      borderRadius: BorderRadius.circular(12),
                      side: const BorderSide(color: Color(0xFF30363D)),
                    ),
                    icon: Icon(
                      _isListeningVoice ? Icons.mic : Icons.add_box_outlined,
                      color: _isListeningVoice
                          ? const Color(0xFFF85149)
                          : const Color(0xFFD97757),
                      size: 22,
                    ),
                    onSelected: (value) {
                      if (value == 'image') {
                        _pickImagesForPrompt();
                      } else if (value == 'file') {
                        _pickAndUploadFile();
                      } else if (value == 'voice') {
                        _toggleVoiceInput();
                      }
                    },
                    itemBuilder: (ctx) => [
                      const PopupMenuItem(
                        value: 'image',
                        child: Row(
                          children: [
                            Icon(Icons.image_outlined, color: Color(0xFFD97757), size: 18),
                            SizedBox(width: 10),
                            Text('Imagen o captura', style: TextStyle(color: Color(0xFFE6EDF3), fontSize: 13)),
                          ],
                        ),
                      ),
                      const PopupMenuItem(
                        value: 'file',
                        child: Row(
                          children: [
                            Icon(Icons.attach_file, color: Color(0xFF8B949E), size: 18),
                            SizedBox(width: 10),
                            Text('Archivo o PDF', style: TextStyle(color: Color(0xFFE6EDF3), fontSize: 13)),
                          ],
                        ),
                      ),
                      PopupMenuItem(
                        value: 'voice',
                        child: Row(
                          children: [
                            Icon(
                              _isListeningVoice ? Icons.mic : Icons.mic_none,
                              color: _isListeningVoice ? const Color(0xFFF85149) : const Color(0xFF8B949E),
                              size: 18,
                            ),
                            const SizedBox(width: 10),
                            Text(
                              _isListeningVoice ? 'Detener voz' : 'Dictar por voz',
                              style: const TextStyle(color: Color(0xFFE6EDF3), fontSize: 13),
                            ),
                          ],
                        ),
                      ),
                    ],
                  ),
                  Expanded(
                    child: TextField(
                      controller: _inputController,
                      minLines: 1,
                      maxLines: 8,
                      keyboardType: TextInputType.multiline,
                      textInputAction: TextInputAction.newline,
                      style: const TextStyle(
                        color: Color(0xFFE6EDF3),
                        fontSize: 14,
                      ),
                      decoration: InputDecoration(
                        hintText: widget.client.isConnected
                            ? 'Escribe un mensaje...'
                            : 'Sin conexión con el servidor Go...',
                        hintStyle: const TextStyle(
                          color: Color(0xFF6E7681),
                          fontSize: 14,
                        ),
                        isDense: true,
                        contentPadding: const EdgeInsets.symmetric(
                          horizontal: 12,
                          vertical: 11,
                        ),
                        filled: true,
                        fillColor: const Color(0xFF0D1117),
                        enabledBorder: OutlineInputBorder(
                          borderRadius: BorderRadius.circular(8),
                          borderSide: const BorderSide(color: Color(0xFF30363D)),
                        ),
                        focusedBorder: OutlineInputBorder(
                          borderRadius: BorderRadius.circular(8),
                          borderSide: const BorderSide(color: Color(0xFFD97757)),
                        ),
                      ),
                    ),
                  ),
                  const SizedBox(width: 8),
                  Builder(
                    builder: (_) {
                      final actState = widget.client.activity.state;
                      final isRunning = actState != Avatar3DState.idle &&
                          actState != Avatar3DState.success &&
                          actState != Avatar3DState.error;
                      return Row(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          if (isRunning) ...[
                            IconButton(
                              key: const Key('stop_prompt_btn'),
                              tooltip: 'Cancelar proceso',
                              onPressed: widget.client.isConnected
                                  ? widget.client.stopExecution
                                  : null,
                              style: IconButton.styleFrom(
                                backgroundColor: const Color(0xFFF85149),
                                foregroundColor: Colors.white,
                                shape: RoundedRectangleBorder(
                                  borderRadius: BorderRadius.circular(8),
                                ),
                              ),
                              icon: const Icon(Icons.stop_rounded, size: 20),
                            ),
                            const SizedBox(width: 6),
                          ],
                          IconButton(
                            key: const Key('send_prompt_btn'),
                            onPressed:
                                widget.client.isConnected ? _handleSend : null,
                            style: IconButton.styleFrom(
                              backgroundColor: widget.client.isConnected
                                  ? const Color(0xFFD97757)
                                  : const Color(0xFF21262D),
                              foregroundColor: const Color(0xFF0D1117),
                              shape: RoundedRectangleBorder(
                                borderRadius: BorderRadius.circular(8),
                              ),
                            ),
                            icon: const Icon(Icons.send_rounded, size: 18),
                          ),
                        ],
                      );
                    },
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
  Future<void> _confirmStop() async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('¿Detener la tarea?'),
        content: const Text('Se mata el proceso del agente en tu PC (/stop).'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Cancelar')),
          FilledButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('Detener')),
        ],
      ),
    );
    if (ok == true) widget.client.stopExecution();
  }

  void _onAvatarTap(CharacterState state) {
    if (state == CharacterState.thinking || state == CharacterState.speaking) {
      widget.client.stopExecution();
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text('Deteniendo ejecución actual…'),
          duration: Duration(seconds: 1),
        ),
      );
      return;
    }
    if (state == CharacterState.sleeping) {
      widget.client.connect();
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text('Reconectando a Tailscale…'),
          duration: Duration(seconds: 1),
        ),
      );
      return;
    }
    _openOptionPicker('/model');
  }

  void _openNotificationsInbox() {
    widget.client.markNotificationsRead();
    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      backgroundColor: const Color(0xFF1A1D21),
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(12)),
      ),
      builder: (ctx) {
        // Newest first.
        final notifs = widget.client.notifications.reversed.toList();
        return SafeArea(
          child: SizedBox(
            height: MediaQuery.of(ctx).size.height * 0.65,
            child: Column(
              children: [
                Padding(
                  padding: const EdgeInsets.fromLTRB(20, 14, 12, 8),
                  child: Row(
                    children: [
                      const Expanded(
                        child: Text(
                          'Buzón de Tareas (AGY / Claude)',
                          style: TextStyle(
                            color: Color(0xFFE6EDF3),
                            fontSize: 16,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                      ),
                      if (notifs.isNotEmpty)
                        TextButton(
                          onPressed: () {
                            widget.client.clearNotifications();
                            Navigator.of(ctx).pop();
                          },
                          child: const Text(
                            'Limpiar',
                            style: TextStyle(color: Color(0xFFF85149)),
                          ),
                        ),
                    ],
                  ),
                ),
                const Divider(color: Color(0xFF21262D), height: 1),
                Expanded(
                  child: notifs.isEmpty
                      ? const Center(
                          child: Text(
                            'Sin notificaciones de terminal/IDE aún.',
                            style: TextStyle(color: Color(0xFF8B949E), fontSize: 13),
                          ),
                        )
                      : ListView.separated(
                          padding: const EdgeInsets.all(14),
                          itemCount: notifs.length,
                          separatorBuilder: (_, __) => const SizedBox(height: 10),
                          itemBuilder: (_, idx) {
                            final n = notifs[idx];
                            return Container(
                              padding: const EdgeInsets.all(12),
                              decoration: BoxDecoration(
                                color: const Color(0xFF111315),
                                borderRadius: BorderRadius.circular(8),
                                border: Border.all(color: const Color(0xFF21262D)),
                              ),
                              child: SelectableText(
                                n.text,
                                style: const TextStyle(
                                  color: Color(0xFFE6EDF3),
                                  fontSize: 13,
                                  height: 1.4,
                                ),
                              ),
                            );
                          },
                        ),
                ),
              ],
            ),
          ),
        );
      },
    );
  }

  Future<void> _openOptionPicker(String cmd) async {
    if (cmd == '/new') cmd = '/project';
    var data = await widget.client.fetchOptionsOverWs(cmd);
    if (!mounted) return;
    // The resume list must always appear so a clean chat can be picked even when
    // the PC returns no history or the options request fails.
    if (data == null && cmd == '/resume') data = const {};
    if (data == null) return;

    final String title;
    final List<({String label, String sub, String command, bool active})> items = [];

    if (cmd == '/model') {
      title = 'Seleccionar Modelo';
      final activeMod = data['active_model'] as String? ?? '';
      for (final m in (data['models'] as List? ?? [])) {
        final id = m['id'] as String? ?? '';
        final name = m['name'] as String? ?? id;
        items.add((
          label: name,
          sub: id,
          command: '/model $id',
          active: id == activeMod,
        ));
      }
    } else if (cmd == '/project') {
      title = 'Seleccionar Proyecto';
      final activeCwd = data['active_cwd'] as String? ?? '';
      for (final p in (data['projects'] as List? ?? [])) {
        final name = p['name'] as String? ?? '';
        final path = p['path'] as String? ?? '';
        items.add((
          label: name,
          sub: path,
          command: '/cd $name',
          active: path == activeCwd,
        ));
      }
    } else {
      title = 'Seleccionar Chat';
      final raw = data['chats'] as List? ?? [];
      if (!raw.any((c) => c['id'] == 'NEW')) {
        items.add((
          label: '➕ Nuevo chat limpio en este proyecto',
          sub: 'NEW',
          command: '/resume NEW',
          active: false,
        ));
      }
      for (final c in (data['chats'] as List? ?? [])) {
        final id = c['id'] as String? ?? '';
        final cTitle = c['title'] as String? ?? id;
        items.add((
          label: cTitle,
          sub: id,
          command: '/resume $id',
          active: false,
        ));
      }
    }

    if (!mounted) return;
    showModalBottomSheet<void>(
      context: context,
      backgroundColor: const Color(0xFF1A1D21),
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(12)),
      ),
      builder: (ctx) {
        return SafeArea(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(20, 16, 20, 10),
                child: Text(
                  title,
                  style: const TextStyle(
                    color: Color(0xFFE6EDF3),
                    fontSize: 16,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ),
              const Divider(color: Color(0xFF21262D), height: 1),
              Flexible(
                child: ListView.builder(
                  shrinkWrap: true,
                  itemCount: items.length,
                  itemBuilder: (_, idx) {
                    final it = items[idx];
                    return ListTile(
                      leading: Icon(
                        it.active ? Icons.check_circle : Icons.circle_outlined,
                        color: it.active
                            ? const Color(0xFF3FB950)
                            : const Color(0xFF6E7681),
                        size: 20,
                      ),
                      title: Text(
                        it.label,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(color: Color(0xFFE6EDF3), fontSize: 14),
                      ),
                      subtitle: Text(
                        it.sub,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(color: Color(0xFF6E7681), fontSize: 11),
                      ),
                      onTap: () {
                        Navigator.of(ctx).pop();
                        if (it.command.startsWith('/cd ')) {
                          widget.client.openSession(
                            id: '${it.command}_${DateTime.now().millisecondsSinceEpoch}',
                            project: it.label,
                            title: it.label,
                          );
                          widget.client.sendPrompt(it.command);
                          // The worker handles messages in order, so this list is
                          // already filtered to the project chosen by `/cd`.
                          _openOptionPicker('/resume');
                          return;
                        }
                        widget.client.sendPrompt(it.command);
                      },
                    );
                  },
                ),
              ),
            ],
          ),
        );
      },
    );
  }

  void _openObsidianVault() {
    ObsidianVaultSheet.show(
      context,
      client: widget.client,
      onInsertIntoInput: (text) {
        setState(() {
          _inputController.text = text;
          _inputController.selection = TextSelection.fromPosition(
            TextPosition(offset: _inputController.text.length),
          );
        });
      },
    );
  }

  Widget _buildCommandsDrawer() {
    return CommandsDrawer(
      client: widget.client,
      onOpenObsidianVault: _openObsidianVault,
      onOpenOptionPicker: _openOptionPicker,
      onOpenAvatarCustomizer: _openAvatarCustomizer,
      onOpenSettings: _openConnectionSheet,
    );
  }
  Future<void> _toggleVoiceInput() async {
    if (_isListeningVoice) {
      await _speech.stop();
      if (mounted) setState(() => _isListeningVoice = false);
      return;
    }
    final available = await _speech.initialize();
    if (!available) return;
    if (mounted) setState(() => _isListeningVoice = true);
    await _speech.listen(
      localeId: 'es_MX',
      onResult: (res) {
        if (!mounted) return;
        _inputController.text = res.recognizedWords;
        _inputController.selection = TextSelection.fromPosition(
          TextPosition(offset: _inputController.text.length),
        );
        if (res.finalResult) {
          setState(() => _isListeningVoice = false);
        }
      },
    );
  }

  Widget _buildStatusBubble(CharacterState state) {
    String label;
    Color dotColor;
    final lastMsg = widget.client.messages.isNotEmpty ? widget.client.messages.last : null;
    final liveTool = (lastMsg != null && !lastMsg.isUser) ? lastMsg.toolStatus.trim() : '';

    switch (state) {
      case CharacterState.thinking:
        label = liveTool.isNotEmpty ? liveTool : 'Pensando...';
        dotColor = const Color(0xFFD29922);
      case CharacterState.speaking:
        label = 'Respondiendo...';
        dotColor = const Color(0xFF3FB950);
      case CharacterState.listening:
        label = 'Escribiendo...';
        dotColor = const Color(0xFF58A6FF);
      case CharacterState.sleeping:
        label = 'PC offline';
        dotColor = const Color(0xFF6E7681);
      case CharacterState.idle:
        return const SizedBox.shrink();
    }

    return Container(
      constraints: const BoxConstraints(maxWidth: 220),
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
      decoration: BoxDecoration(
        color: const Color(0xFF15181C),
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: dotColor.withValues(alpha: 0.45)),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(
            width: 7,
            height: 7,
            decoration: BoxDecoration(color: dotColor, shape: BoxShape.circle),
          ),
          const SizedBox(width: 6),
          Flexible(
            child: Text(
              label,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: const TextStyle(color: Color(0xFFE6EDF3), fontSize: 11),
            ),
          ),
        ],
      ),
    );
  }

  void _openAvatarCustomizer() {
    const colors = [
      Color(0xFF3FB950),
      Color(0xFF58A6FF),
      Color(0xFFD97757),
      Color(0xFFA371F7),
      Color(0xFFD29922),
    ];
    showModalBottomSheet<void>(
      context: context,
      backgroundColor: const Color(0xFF15181C),
      isScrollControlled: true,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (sheetCtx) => StatefulBuilder(
        builder: (ctx, setSheetState) => Padding(
          padding: const EdgeInsets.fromLTRB(20, 20, 20, 24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const Text(
                'Personalizar personaje',
                style: TextStyle(
                  color: Color(0xFFE6EDF3),
                  fontSize: 16,
                  fontWeight: FontWeight.w600,
                ),
              ),
              const SizedBox(height: 12),
              const Text('Estilo', style: TextStyle(color: Color(0xFF8B949E), fontSize: 12)),
              const SizedBox(height: 6),
              Wrap(
                spacing: 8,
                children: [
                  for (final s in AvatarStyle.values)
                    ChoiceChip(
                      label: Text(s.name),
                      selected: _avatarStyle == s &&
                          _customRivePath == null &&
                          _customPhotoPath == null &&
                          _geminiProfile == null,
                      onSelected: (_) {
                        setState(() {
                          _avatarStyle = s;
                          _customRivePath = null;
                          _customPhotoPath = null;
                          _geminiProfile = null;
                        });
                        _saveAvatarPrefs();
                        setSheetState(() {});
                      },
                    ),
                ],
              ),
              const SizedBox(height: 12),
              const Text('Accesorio', style: TextStyle(color: Color(0xFF8B949E), fontSize: 12)),
              const SizedBox(height: 6),
              Wrap(
                spacing: 8,
                children: [
                  for (final a in AvatarAccessory.values)
                    ChoiceChip(
                      label: Text(a.name),
                      selected: _avatarAccessory == a,
                      onSelected: (_) {
                        setState(() => _avatarAccessory = a);
                        _saveAvatarPrefs();
                        setSheetState(() {});
                      },
                    ),
                ],
              ),
              const SizedBox(height: 12),
              const Text('Color de acento', style: TextStyle(color: Color(0xFF8B949E), fontSize: 12)),
              const SizedBox(height: 6),
              Row(
                children: [
                  for (final c in colors)
                    GestureDetector(
                      onTap: () {
                        setState(() => _avatarAccent = c);
                        _saveAvatarPrefs();
                        setSheetState(() {});
                      },
                      child: Container(
                        margin: const EdgeInsets.only(right: 10),
                        width: 28,
                        height: 28,
                        decoration: BoxDecoration(
                          color: c,
                          shape: BoxShape.circle,
                          border: Border.all(
                            color: _avatarAccent == c ? Colors.white : Colors.transparent,
                            width: 2,
                          ),
                        ),
                      ),
                    ),
                ],
              ),
              const SizedBox(height: 12),
              const Text('Fondo del cuadro', style: TextStyle(color: Color(0xFF8B949E), fontSize: 12)),
              const SizedBox(height: 6),
              SingleChildScrollView(
                scrollDirection: Axis.horizontal,
                child: Row(
                  children: [
                    for (final bg in const [
                      Color(0xFF111418),
                      Color(0xFF050608),
                      Color(0xFF1B2432),
                      Color(0xFF1A2821),
                      Color(0xFF2B1F1D),
                      Color(0xFF241E2E),
                      Color(0xFFE8DFD5),
                    ])
                      GestureDetector(
                        onTap: () {
                          setState(() => _avatarBoxBg = bg);
                          _saveAvatarPrefs();
                          setSheetState(() {});
                        },
                        child: Container(
                          margin: const EdgeInsets.only(right: 10),
                          width: 28,
                          height: 28,
                          decoration: BoxDecoration(
                            color: bg,
                            borderRadius: BorderRadius.circular(8),
                            border: Border.all(
                              color: (_avatarBoxBg ?? const Color(0xFF111418)) == bg
                                  ? const Color(0xFFD97757)
                                  : const Color(0xFF30363D),
                              width: (_avatarBoxBg ?? const Color(0xFF111418)) == bg ? 2 : 1,
                            ),
                          ),
                        ),
                      ),
                  ],
                ),
              ),
              const SizedBox(height: 14),
              FilledButton.tonalIcon(
                onPressed: _isGeneratingAvatar
                    ? null
                    : () async {
                        final picked = await FilePicker.pickFiles(type: FileType.image);
                        if (picked.isEmpty) return;
                        final path = picked.first.path;
                        if (path == null) return;
                        if (sheetCtx.mounted) Navigator.of(sheetCtx).pop();
                        await _generateGeminiAvatarFromPhoto(path);
                      },
                icon: const Icon(Icons.auto_awesome_rounded, size: 18),
                label: Text(
                  _geminiProfile != null
                      ? 'Subir otra foto para crear 4 opciones'
                      : 'Crear personaje desde tu foto (4 opciones)',
                ),
              ),
              if (_geminiVariants.isNotEmpty) ...[
                const SizedBox(height: 8),
                OutlinedButton.icon(
                  onPressed: () {
                    Navigator.of(sheetCtx).pop();
                    _openGeminiVariantPicker(_geminiVariants);
                  },
                  icon: const Icon(Icons.grid_view_rounded, size: 18),
                  label: Text('Cambiar entre mis 4 opciones (${_geminiProfile?.vibe ?? "Plush"})'),
                ),
              ],
              const SizedBox(height: 8),
              OutlinedButton.icon(
                onPressed: () async {
                  final picked = await FilePicker.pickFiles(type: FileType.any);
                  if (picked.isEmpty) return;
                  final path = picked.first.path;
                  if (path == null) return;
                  final lower = path.toLowerCase();
                  if (lower.endsWith('.riv')) {
                    setState(() {
                      _customRivePath = path;
                      _customPhotoPath = null;
                    });
                  } else {
                    return;
                  }
                  _saveAvatarPrefs();
                  if (sheetCtx.mounted) Navigator.of(sheetCtx).pop();
                },
                icon: const Icon(Icons.view_in_ar_rounded, size: 18),
                label: Text(
                  _customRivePath != null
                      ? 'Rive activo: ${_customRivePath!.split('/').last}'
                      : 'Importar animación (.riv)',
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
