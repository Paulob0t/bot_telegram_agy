import 'dart:async';
import 'dart:io';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:speech_to_text/speech_to_text.dart' as stt;

import '../models/agent_event.dart';
import '../services/ws_client.dart';
import '../features/avatar3d/controllers/avatar_prefs_controller.dart';
import '../features/avatar3d/widgets/avatar_customizer_sheet.dart';
import '../features/chat/file_actions_sheet.dart';
import '../features/notifications/notifications_sheet.dart';
import '../features/sessions/option_picker_sheet.dart';
import '../features/settings/connection_settings_sheet.dart';
import '../widgets/permission_request_card.dart';
import '../widgets/status_bubble.dart';
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
  final AvatarPrefsController _prefs = AvatarPrefsController();
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
    _prefs.addListener(_onClientUpdated);
    _prefs.load();
  }

  bool _setupPrompted = false;

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    widget.client.isAppInForeground = state == AppLifecycleState.resumed;
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
    _prefs
      ..removeListener(_onClientUpdated)
      ..dispose();
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
    showFileActionsSheet(
      context,
      client: widget.client,
      path: path,
      fileName: file.name,
      onAttach: () => setState(() {
        if (!_stagedAttachments.contains(path)) _stagedAttachments.add(path);
      }),
      onCreateAvatar: () => generateAvatarFromPhoto(context, _prefs, widget.client, path),
    );
  }

  void _openConnectionSheet() =>
      showConnectionSettingsSheet(context, widget.client, _prefs);

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
                          customPhotoPath: _prefs.customPhotoPath,
                          geminiProfile: _prefs.geminiProfile,
                          boxBackgroundColor: _prefs.boxBackground,
                          onLongPress: _confirmStop,
                          onTapIdleFallback: () => _onAvatarTap(state),
                          fallback: Center(
                            child: SizedBox(
                              width: 76,
                              height: 66,
                              child: RiveCompanionAvatar(
                                state: state,
                                style: _prefs.style,
                                accessory: _prefs.accessory,
                                customAccent: _prefs.accent,
                                customBgColor: _prefs.boxBackground,
                                customRivePath: _prefs.customRivePath,
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
                            _prefs.assistantName,
                            style: const TextStyle(
                              color: Color(0xFFE6EDF3),
                              fontSize: 12,
                              fontWeight: FontWeight.w500,
                            ),
                          ),
                        ),
                        if (state != CharacterState.idle && widget.client.activity.label.isEmpty) ...[
                          const SizedBox(height: 5),
                          StatusBubble(
                            state: state,
                            liveTool: _liveToolStatus(),
                          ),
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

  String _liveToolStatus() {
    final msgs = widget.client.messages;
    if (msgs.isEmpty || msgs.last.isUser) return '';
    return msgs.last.toolStatus.trim();
  }

  void _openNotificationsInbox() => showNotificationsSheet(context, widget.client);

  Future<void> _openOptionPicker(String cmd) => showOptionPicker(context, widget.client, cmd);

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

  void _openAvatarCustomizer() => showAvatarCustomizerSheet(context, _prefs, widget.client);
}
