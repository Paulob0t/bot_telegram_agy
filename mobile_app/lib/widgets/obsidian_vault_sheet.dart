import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../services/ws_client.dart';

/// Zero-token Obsidian Vault & Prompts browser sheet.
/// Shows root vault folders, project `Prompts` folders, and `.md` files with
/// one-tap copy (so you can paste any prompt straight into the chat).
class ObsidianVaultSheet extends StatefulWidget {
  final AgyWsClient client;
  final ValueChanged<String>? onInsertIntoInput;

  const ObsidianVaultSheet({
    super.key,
    required this.client,
    this.onInsertIntoInput,
  });

  static Future<void> show(
    BuildContext context, {
    required AgyWsClient client,
    ValueChanged<String>? onInsertIntoInput,
  }) {
    return showModalBottomSheet<void>(
      context: context,
      backgroundColor: const Color(0xFF15181C),
      isScrollControlled: true,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (_) => ObsidianVaultSheet(
        client: client,
        onInsertIntoInput: onInsertIntoInput,
      ),
    );
  }

  @override
  State<ObsidianVaultSheet> createState() => _ObsidianVaultSheetState();
}

class _ObsidianVaultSheetState extends State<ObsidianVaultSheet> {
  String _currentRelPath = '';
  bool _loading = true;
  bool _isFile = false;
  String _fileContent = '';
  String _fileName = '';
  List<Map<String, dynamic>> _items = const [];

  @override
  void initState() {
    super.initState();
    _loadPath('');
  }

  Future<void> _loadPath(String relPath) async {
    setState(() => _loading = true);
    final res = await widget.client.fetchObsidian(relPath: relPath);
    if (!mounted) return;
    if (res == null || res['ok'] != true) {
      setState(() => _loading = false);
      return;
    }
    final isFile = res['is_file'] as bool? ?? false;
    setState(() {
      _loading = false;
      _currentRelPath = res['rel_path'] as String? ?? relPath;
      _isFile = isFile;
      if (isFile) {
        _fileName = res['name'] as String? ?? relPath.split('/').last;
        _fileContent = res['content'] as String? ?? '';
      } else {
        _items = (res['items'] as List? ?? const [])
            .whereType<Map<String, dynamic>>()
            .toList();
      }
    });
  }

  void _goBack() {
    if (_currentRelPath.isEmpty) {
      Navigator.of(context).pop();
      return;
    }
    final parts = _currentRelPath.split('/');
    parts.removeLast();
    _loadPath(parts.join('/'));
  }

  Future<void> _copyFileFromList(String relPath, String name) async {
    final res = await widget.client.fetchObsidian(relPath: relPath);
    if (!mounted || res == null) return;
    final content = (res['content'] as String? ?? '').trim();
    if (content.isEmpty) return;
    await Clipboard.setData(ClipboardData(text: content));
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text('Copiado: $name'),
        duration: const Duration(seconds: 2),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final title = _currentRelPath.isEmpty
        ? 'Obsidian · Bóveda'
        : _currentRelPath.replaceAll('/', ' / ');

    return SafeArea(
      child: SizedBox(
        height: MediaQuery.of(context).size.height * 0.72,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(12, 12, 12, 8),
              child: Row(
                children: [
                  if (_currentRelPath.isNotEmpty)
                    IconButton(
                      visualDensity: VisualDensity.compact,
                      onPressed: _goBack,
                      icon: const Icon(
                        Icons.arrow_back_rounded,
                        color: Color(0xFFE6EDF3),
                        size: 20,
                      ),
                    )
                  else
                    const Padding(
                      padding: EdgeInsets.symmetric(horizontal: 8),
                      child: Icon(
                        Icons.menu_book_rounded,
                        color: Color(0xFFD97757),
                        size: 20,
                      ),
                    ),
                  Expanded(
                    child: Text(
                      title,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(
                        color: Color(0xFFE6EDF3),
                        fontSize: 15,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                  ),
                  if (_isFile && _fileContent.isNotEmpty) ...[
                    FilledButton.tonalIcon(
                      style: FilledButton.styleFrom(
                        backgroundColor: const Color(0xFFD97757),
                        foregroundColor: const Color(0xFF0D1117),
                        visualDensity: VisualDensity.compact,
                      ),
                      onPressed: () {
                        Clipboard.setData(ClipboardData(text: _fileContent));
                        ScaffoldMessenger.of(context).showSnackBar(
                          SnackBar(
                            content: Text('Prompt copiado ($_fileName)'),
                            duration: const Duration(seconds: 2),
                          ),
                        );
                      },
                      icon: const Icon(Icons.copy_rounded, size: 15),
                      label: const Text(
                        'Copiar',
                        style: TextStyle(fontSize: 12, fontWeight: FontWeight.w600),
                      ),
                    ),
                    if (widget.onInsertIntoInput != null) ...[
                      const SizedBox(width: 6),
                      IconButton(
                        visualDensity: VisualDensity.compact,
                        tooltip: 'Pegar en barra de chat',
                        onPressed: () {
                          widget.onInsertIntoInput!(_fileContent);
                          Navigator.of(context).pop();
                        },
                        icon: const Icon(
                          Icons.input_rounded,
                          color: Color(0xFF58A6FF),
                          size: 19,
                        ),
                      ),
                    ],
                  ],
                ],
              ),
            ),
            const Divider(color: Color(0xFF21262D), height: 1),
            Expanded(
              child: _loading
                  ? const Center(
                      child: CircularProgressIndicator(
                        strokeWidth: 2,
                        color: Color(0xFFD97757),
                      ),
                    )
                  : _isFile
                      ? SingleChildScrollView(
                          padding: const EdgeInsets.all(16),
                          child: SelectableText(
                            _fileContent,
                            style: const TextStyle(
                              color: Color(0xFFE6EDF3),
                              fontSize: 12.5,
                              fontFamily: 'monospace',
                              height: 1.45,
                            ),
                          ),
                        )
                      : _items.isEmpty
                          ? const Center(
                              child: Text(
                                'Carpeta vacía',
                                style: TextStyle(
                                  color: Color(0xFF8B949E),
                                  fontSize: 13,
                                ),
                              ),
                            )
                          : ListView.builder(
                              padding: const EdgeInsets.symmetric(vertical: 6),
                              itemCount: _items.length,
                              itemBuilder: (ctx, idx) {
                                final item = _items[idx];
                                final name = item['name'] as String? ?? '';
                                final rel = item['rel_path'] as String? ?? '';
                                final isDir = item['is_dir'] as bool? ?? false;
                                final isPromptsDir =
                                    isDir && name.toLowerCase() == 'prompts';

                                return ListTile(
                                  leading: Icon(
                                    isDir
                                        ? (isPromptsDir
                                            ? Icons.folder_special_rounded
                                            : Icons.folder_rounded)
                                        : Icons.description_outlined,
                                    color: isPromptsDir
                                        ? const Color(0xFFD97757)
                                        : (isDir
                                            ? const Color(0xFF58A6FF)
                                            : const Color(0xFF8B949E)),
                                    size: 21,
                                  ),
                                  title: Text(
                                    name,
                                    maxLines: 1,
                                    overflow: TextOverflow.ellipsis,
                                    style: TextStyle(
                                      color: const Color(0xFFE6EDF3),
                                      fontSize: 13.5,
                                      fontWeight: isPromptsDir
                                          ? FontWeight.w600
                                          : FontWeight.w400,
                                    ),
                                  ),
                                  trailing: isDir
                                      ? const Icon(
                                          Icons.chevron_right_rounded,
                                          color: Color(0xFF6E7681),
                                          size: 20,
                                        )
                                      : IconButton(
                                          tooltip: 'Copiar contenido',
                                          onPressed: () =>
                                              _copyFileFromList(rel, name),
                                          icon: const Icon(
                                            Icons.copy_rounded,
                                            color: Color(0xFF8B949E),
                                            size: 18,
                                          ),
                                        ),
                                  onTap: () => _loadPath(rel),
                                );
                              },
                            ),
            ),
          ],
        ),
      ),
    );
  }
}
