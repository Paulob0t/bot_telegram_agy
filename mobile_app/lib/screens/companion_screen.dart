import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';

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
  String _assistantName = 'Asistente AGY';

  @override
  void initState() {
    super.initState();
    widget.client.addListener(_onClientUpdated);
    _inputController.addListener(_onTextChanged);
  }

  void _onTextChanged() {
    widget.client.setUserListening(_inputController.text.trim().isNotEmpty);
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

  String _stateLabel(CharacterState s) {
    switch (s) {
      case CharacterState.idle:
        return 'En línea · En espera';
      case CharacterState.listening:
        return 'Escuchando entrada';
      case CharacterState.thinking:
        return 'Razonando / Ejecutando';
      case CharacterState.speaking:
        return 'Respondiendo';
      case CharacterState.sleeping:
        return widget.client.isConnecting
            ? 'Conectando a Tailscale...'
            : 'PC suspendida / desconectada (reintento #${widget.client.retryAttempt})';
    }
  }

  @override
  Widget build(BuildContext context) {
    final state = widget.client.characterState;
    final busy = state == CharacterState.thinking || state == CharacterState.speaking;

    return Scaffold(
      backgroundColor: const Color(0xFF111315),
      drawer: _buildCommandsDrawer(),
      appBar: AppBar(
        backgroundColor: const Color(0xFF111315),
        elevation: 0,
        iconTheme: const IconThemeData(color: Color(0xFF8B949E)),
        title: GestureDetector(
          onTap: _openConnectionSheet,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                _assistantName,
                style: const TextStyle(
                  color: Color(0xFFE6EDF3),
                  fontSize: 15,
                  fontWeight: FontWeight.w600,
                ),
              ),
              Text(
                _stateLabel(state),
                style: const TextStyle(
                  color: Color(0xFF8B949E),
                  fontSize: 12,
                ),
              ),
            ],
          ),
        ),
        actions: [
          if (busy)
            TextButton(
              onPressed: widget.client.stopExecution,
              child: const Text(
                'Detener',
                style: TextStyle(
                  color: Color(0xFFF85149),
                  fontWeight: FontWeight.w600,
                ),
              ),
            ),
          IconButton(
            tooltip: 'Configurar nodo Tailscale',
            onPressed: _openConnectionSheet,
            icon: const Icon(Icons.tune, color: Color(0xFF8B949E), size: 20),
          ),
        ],
      ),
      body: SafeArea(
        child: Column(
          children: [
            // Escenario del personaje Rive
            Container(
              width: double.infinity,
              height: 176,
              decoration: const BoxDecoration(
                color: Color(0xFF15181C),
                border: Border(
                  bottom: BorderSide(color: Color(0xFF21262D)),
                ),
              ),
              alignment: Alignment.center,
              child: RiveCompanionAvatar(state: state),
            ),

            // Historial conversacional
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
                      padding: const EdgeInsets.symmetric(
                        horizontal: 16,
                        vertical: 12,
                      ),
                      itemCount: widget.client.messages.length,
                      itemBuilder: (context, index) {
                        final msg = widget.client.messages[index];
                        return _MessageBubble(message: msg);
                      },
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
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _MessageBubble extends StatelessWidget {
  final ChatMessage message;

  const _MessageBubble({required this.message});

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
    );
  }
}
