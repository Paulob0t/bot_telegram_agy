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

  void _openConnectionSheet() {
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
                'Nodo Tailscale (Go Core)',
                style: TextStyle(
                  color: Color(0xFFE6EDF3),
                  fontSize: 16,
                  fontWeight: FontWeight.w600,
                ),
              ),
              const SizedBox(height: 14),
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
      appBar: AppBar(
        backgroundColor: const Color(0xFF111315),
        elevation: 0,
        titleSpacing: 16,
        title: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text(
              'Paulobot · AGY Core',
              style: TextStyle(
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
                            ? 'Enviar comando o consulta a AGY...'
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
