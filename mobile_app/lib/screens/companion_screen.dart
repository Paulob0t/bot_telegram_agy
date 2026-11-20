import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../models/agent_event.dart';
import '../services/ws_client.dart';
import '../widgets/rive_avatar.dart';

class CompanionScreen extends StatefulWidget {
  final AgyWsClient client;

  const CompanionScreen({
    super.key,
    required this.client,
  });

  @override
  State<CompanionScreen> createState() => _CompanionScreenState();
}

class _CompanionScreenState extends State<CompanionScreen> {
  final TextEditingController _inputController = TextEditingController();
  final ScrollController _scrollController = ScrollController();
  int _lastMsgCount = 0;
  String _assistantName = 'HyprAgent';
  AvatarStyle _avatarStyle = AvatarStyle.cubeBot;
  AvatarAccessory _avatarAccessory = AvatarAccessory.none;
  Color? _avatarAccent;
  String? _customRivePath;
  String _slashQuery = '';

  static const List<(String, String)> _allSlashCommands = [
    ('/scan', 'Escanear PC y proyectos'),
    ('/status', 'Estado del sistema'),
    ('/project', 'Cambiar proyecto'),
    ('/model', 'Cambiar modelo'),
    ('/resume', 'Historial de chats'),
    ('/new', 'Nuevo chat limpio'),
    ('/tokens', 'Cuotas de tokens'),
    ('/stop', 'Detener ejecución'),
  ];

  @override
  void initState() {
    super.initState();
    widget.client.addListener(_onClientUpdated);
    _inputController.addListener(_onTextChanged);
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

  void _handleSend() {
    final text = _inputController.text.trim();
    if (text.isEmpty) return;
    final lower = text.toLowerCase();
    if (lower == '/model' || lower == '/modelo') {
      _inputController.clear();
      _openOptionPicker('/model');
      return;
    }
    if (lower == '/project' || lower == '/cd') {
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
                leading: const Icon(Icons.auto_awesome, color: Color(0xFFD97757)),
                title: const Text('Analizar con IA', style: TextStyle(color: Color(0xFFE6EDF3))),
                onTap: () {
                  Navigator.of(ctx).pop();
                  widget.client.uploadFile(path, target: 'cache', analyzeWithAI: true);
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

    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      backgroundColor: const Color(0xFF1A1D21),
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(12)),
      ),
      builder: (ctx) {
        return Padding(
          padding: EdgeInsets.only(
            left: 20,
            right: 20,
            top: 20,
            bottom: MediaQuery.of(ctx).viewInsets.bottom + 24,
          ),
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
                  }
                  widget.client.updateCredentials(
                    wsUrl: urlCtrl.text,
                    bearerToken: tokCtrl.text,
                  );
                },
                child: const Text(
                  'Guardar y Reconectar',
                  style: TextStyle(fontWeight: FontWeight.w600),
                ),
              ),
            ],
          ),
        );
      },
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
            // Header minimalista fijo centrado con Stack: [=]  [Cuadrito + Nombre]  [🔔] [⚙️]
            Padding(
              padding: const EdgeInsets.fromLTRB(12, 10, 12, 8),
              child: Stack(
                alignment: Alignment.topCenter,
                children: [
                  Align(
                    alignment: Alignment.topLeft,
                    child: Builder(
                      builder: (btnCtx) => IconButton(
                        tooltip: 'Menú de comandos',
                        onPressed: () => Scaffold.of(btnCtx).openDrawer(),
                        style: IconButton.styleFrom(
                          backgroundColor: const Color(0xFF1A1D21),
                        ),
                        icon: const Icon(Icons.drag_handle, color: Color(0xFFE6EDF3), size: 20),
                      ),
                    ),
                  ),
                  GestureDetector(
                    onTap: () => _onAvatarTap(state),
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        SizedBox(
                          width: 76,
                          height: 66,
                          child: RiveCompanionAvatar(
                            state: state,
                            style: _avatarStyle,
                            accessory: _avatarAccessory,
                            customAccent: _avatarAccent,
                            customRivePath: _customRivePath,
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
                        return _MessageBubble(
                          message: msg,
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

            // Barra de entrada
            Container(
              padding: const EdgeInsets.fromLTRB(12, 8, 12, 12),
              decoration: const BoxDecoration(
                color: Color(0xFF15181C),
                border: Border(
                  top: BorderSide(color: Color(0xFF21262D)),
                ),
              ),
              child: Row(
                children: [
                  IconButton(
                    tooltip: 'Adjuntar foto o PDF',
                    onPressed: widget.client.isConnected ? _pickAndUploadFile : null,
                    icon: const Icon(Icons.attach_file, color: Color(0xFF8B949E), size: 20),
                  ),
                  Expanded(
                    child: TextField(
                      controller: _inputController,
                      onSubmitted: (_) => _handleSend(),
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
                  IconButton(
                    key: const Key('send_prompt_btn'),
                    onPressed: widget.client.isConnected ? _handleSend : null,
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
              ),
            ),
          ],
        ),
      ),
    );
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
        final notifs = widget.client.notifications;
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
    final data = await widget.client.fetchOptions();
    if (!mounted || data == null) return;

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

  Widget _buildCommandsDrawer() {
    final commands = [
      (Icons.radar, 'Escanear PC y proyectos', '/scan'),
      (Icons.info_outline, 'Estado del sistema', '/status'),
      (Icons.folder_outlined, 'Cambiar proyecto', '/project'),
      (Icons.memory, 'Cambiar modelo', '/model'),
      (Icons.history, 'Historial de chats', '/resume'),
      (Icons.add_comment_outlined, 'Nuevo chat limpio', '/new'),
      (Icons.data_usage, 'Cuotas de tokens', '/tokens'),
      (Icons.stop_circle_outlined, 'Detener ejecución', '/stop'),
    ];

    return Drawer(
      backgroundColor: const Color(0xFF15181C),
      child: SafeArea(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const Padding(
              padding: EdgeInsets.fromLTRB(20, 20, 20, 12),
              child: Text(
                'Comandos rápidos',
                style: TextStyle(
                  color: Color(0xFFE6EDF3),
                  fontSize: 16,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ),
            const Divider(color: Color(0xFF21262D), height: 1),
            Expanded(
              child: ListView(
                padding: const EdgeInsets.symmetric(vertical: 8),
                children: [
                  for (final item in commands)
                    ListTile(
                      leading: Icon(item.$1, color: const Color(0xFF8B949E), size: 20),
                      title: Text(
                        item.$2,
                        style: const TextStyle(color: Color(0xFFE6EDF3), fontSize: 14),
                      ),
                      subtitle: Text(
                        item.$3,
                        style: const TextStyle(color: Color(0xFF6E7681), fontSize: 12),
                      ),
                      onTap: () {
                        Navigator.of(context).pop();
                        if (item.$3 == '/stop') {
                          widget.client.stopExecution();
                        } else if (item.$3 == '/model' ||
                            item.$3 == '/project' ||
                            item.$3 == '/resume') {
                          _openOptionPicker(item.$3);
                        } else {
                          widget.client.sendPrompt(item.$3);
                        }
                      },
                    ),
                  const Divider(color: Color(0xFF21262D), height: 16),
                  ListTile(
                    leading: const Icon(Icons.face_retouching_natural, color: Color(0xFF8B949E), size: 20),
                    title: const Text(
                      'Personalizar personaje',
                      style: TextStyle(color: Color(0xFFE6EDF3), fontSize: 14),
                    ),
                    subtitle: const Text(
                      'Estilo, accesorios, color o .riv',
                      style: TextStyle(color: Color(0xFF6E7681), fontSize: 12),
                    ),
                    onTap: () {
                      Navigator.of(context).pop();
                      _openAvatarCustomizer();
                    },
                  ),
                  ListTile(
                    leading: const Icon(Icons.tune, color: Color(0xFF8B949E), size: 20),
                    title: const Text(
                      'Ajustes y conexión',
                      style: TextStyle(color: Color(0xFFE6EDF3), fontSize: 14),
                    ),
                    subtitle: const Text(
                      'Nombre, URL Tailscale y Token',
                      style: TextStyle(color: Color(0xFF6E7681), fontSize: 12),
                    ),
                    onTap: () {
                      Navigator.of(context).pop();
                      _openConnectionSheet();
                    },
                  ),
                  if (widget.client.updateAvailable) ...[
                    const Divider(color: Color(0xFF21262D), height: 16),
                    ListTile(
                      leading: const Icon(Icons.system_update, color: Color(0xFF3FB950), size: 20),
                      title: Text(
                        'Actualizar App (${widget.client.availableVersion})',
                        style: const TextStyle(
                          color: Color(0xFF3FB950),
                          fontSize: 14,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                      subtitle: const Text(
                        'Nueva versión disponible por Tailscale',
                        style: TextStyle(color: Color(0xFF8B949E), fontSize: 11),
                      ),
                      onTap: () {
                        Navigator.of(context).pop();
                        widget.client.installAvailableUpdate();
                      },
                    ),
                  ],
                ],
              ),
            ),
          ],
        ),
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
                      selected: _avatarStyle == s && _customRivePath == null,
                      onSelected: (_) {
                        setState(() {
                          _avatarStyle = s;
                          _customRivePath = null;
                        });
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
                        setSheetState(() {});
                      },
                    ),
                ],
              ),
              const SizedBox(height: 12),
              const Text('Color', style: TextStyle(color: Color(0xFF8B949E), fontSize: 12)),
              const SizedBox(height: 6),
              Row(
                children: [
                  for (final c in colors)
                    GestureDetector(
                      onTap: () {
                        setState(() => _avatarAccent = c);
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
              const SizedBox(height: 14),
              OutlinedButton.icon(
                onPressed: () async {
                  final picked = await FilePicker.pickFiles(type: FileType.any);
                  if (picked.isEmpty) return;
                  final path = picked.first.path;
                  if (path == null || !path.toLowerCase().endsWith('.riv')) return;
                  setState(() => _customRivePath = path);
                  if (sheetCtx.mounted) Navigator.of(sheetCtx).pop();
                },
                icon: const Icon(Icons.upload_file, size: 18),
                label: Text(
                  _customRivePath != null
                      ? 'Rive activo: ${_customRivePath!.split('/').last}'
                      : 'Importar animación .riv',
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _MessageBubble extends StatelessWidget {
  final ChatMessage message;
  final VoidCallback? onCopy;
  final VoidCallback? onDelete;

  const _MessageBubble({
    required this.message,
    this.onCopy,
    this.onDelete,
  });

  void _showActions(BuildContext context) {
    showModalBottomSheet<void>(
      context: context,
      backgroundColor: const Color(0xFF1A1D21),
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(12)),
      ),
      builder: (ctx) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (message.text.isNotEmpty)
              ListTile(
                leading: const Icon(Icons.copy, color: Color(0xFF58A6FF)),
                title: const Text('Copiar texto', style: TextStyle(color: Color(0xFFE6EDF3))),
                onTap: () {
                  Navigator.of(ctx).pop();
                  onCopy?.call();
                },
              ),
            ListTile(
              leading: const Icon(Icons.delete_outline, color: Color(0xFFF85149)),
              title: const Text('Eliminar mensaje', style: TextStyle(color: Color(0xFFF85149))),
              onTap: () {
                Navigator.of(ctx).pop();
                onDelete?.call();
              },
            ),
          ],
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final bgColor = message.isUser
        ? const Color(0xFF21262D)
        : (message.isError ? const Color(0xFF2D1619) : const Color(0xFF161B22));
    final borderColor = message.isUser
        ? const Color(0xFF30363D)
        : (message.isError ? const Color(0xFFF85149) : const Color(0xFF21262D));

    return Align(
      alignment: message.isUser ? Alignment.centerRight : Alignment.centerLeft,
      child: GestureDetector(
        onLongPress: () => _showActions(context),
        child: Container(
        margin: const EdgeInsets.symmetric(vertical: 5),
        padding: const EdgeInsets.symmetric(horizontal: 13, vertical: 10),
        constraints: const BoxConstraints(maxWidth: 340),
        decoration: BoxDecoration(
          color: bgColor,
          borderRadius: BorderRadius.circular(8),
          border: Border.all(color: borderColor),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (message.text.isNotEmpty)
              SelectableText(
                message.text,
                style: const TextStyle(
                  color: Color(0xFFE6EDF3),
                  fontSize: 13.5,
                  height: 1.4,
                ),
              ),
            if (message.toolStatus.isNotEmpty) ...[
              if (message.text.isNotEmpty) const SizedBox(height: 6),
              Text(
                message.toolStatus,
                style: const TextStyle(
                  color: Color(0xFFD29922),
                  fontSize: 12,
                  fontFamily: 'monospace',
                ),
              ),
            ],
          ],
        ),
        ),
      ),
    );
  }
}
