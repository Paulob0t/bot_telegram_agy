import 'package:flutter/material.dart';

import '../../services/ws_client.dart';
import '../../theme/app_colors.dart';

/// Resume entry the PC uses for "start a clean chat".
const newChatId = 'NEW';

typedef _PickerItem = ({String label, String sub, String command, bool active});

/// Opens the model, project or resume picker. `/new` starts the chat flow:
/// project, then always the resume list of that project.
Future<void> showOptionPicker(BuildContext context, AgyWsClient client, String cmd) async {
  if (cmd == '/new') cmd = '/project';
  var data = await client.fetchOptionsOverWs(cmd);
  if (!context.mounted) return;
  // The resume list must always appear so a clean chat can be picked even when
  // the PC returns no history or the request fails.
  if (data == null && cmd == '/resume') data = const {};
  if (data == null) return;

  final (title, items) = switch (cmd) {
    '/model' => ('Seleccionar Modelo', _modelItems(data)),
    '/project' => ('Seleccionar Proyecto', _projectItems(data)),
    _ => ('Seleccionar Chat', _resumeItems(data)),
  };

  showModalBottomSheet<void>(
    context: context,
    backgroundColor: AppColors.sheet,
    shape: sheetShape,
    builder: (ctx) => SafeArea(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(20, 16, 20, 10),
            child: Text(title, style: sheetTitleStyle),
          ),
          const Divider(color: AppColors.divider, height: 1),
          Flexible(
            child: ListView.builder(
              shrinkWrap: true,
              itemCount: items.length,
              itemBuilder: (_, idx) => _OptionTile(
                item: items[idx],
                onTap: () {
                  Navigator.of(ctx).pop();
                  _apply(context, client, items[idx]);
                },
              ),
            ),
          ),
        ],
      ),
    ),
  );
}

void _apply(BuildContext context, AgyWsClient client, _PickerItem it) {
  if (!it.command.startsWith('/cd ')) {
    client.sendPrompt(it.command);
    return;
  }
  client.openSession(
    id: '${it.command}_${DateTime.now().millisecondsSinceEpoch}',
    project: it.label,
    title: it.label,
  );
  client.sendPrompt(it.command);
  // The PC handles messages in order, so this list is already filtered to the new project.
  showOptionPicker(context, client, '/resume');
}

List<_PickerItem> _modelItems(Map<String, dynamic> data) {
  final activeMod = data['active_model'] as String? ?? '';
  return [
    for (final m in (data['models'] as List? ?? []))
      (
        label: m['name'] as String? ?? m['id'] as String? ?? '',
        sub: m['id'] as String? ?? '',
        command: '/model ${m['id'] ?? ''}',
        active: m['id'] == activeMod,
      ),
  ];
}

List<_PickerItem> _projectItems(Map<String, dynamic> data) {
  final activeCwd = data['active_cwd'] as String? ?? '';
  return [
    for (final p in (data['projects'] as List? ?? []))
      (
        label: p['name'] as String? ?? '',
        sub: p['path'] as String? ?? '',
        command: '/cd ${p['name'] ?? ''}',
        active: p['path'] == activeCwd,
      ),
  ];
}

List<_PickerItem> _resumeItems(Map<String, dynamic> data) {
  final raw = data['chats'] as List? ?? [];
  // The chat this session is on, so the list matches what the PC will continue.
  final activeChat = data['active_chat'] as String? ?? '';
  return [
    if (!raw.any((c) => c['id'] == newChatId))
      (
        label: '➕ Nuevo chat limpio en este proyecto',
        sub: newChatId,
        command: '/resume $newChatId',
        active: false,
      ),
    for (final c in raw)
      (
        label: c['title'] as String? ?? c['id'] as String? ?? '',
        sub: c['id'] as String? ?? '',
        command: '/resume ${c['id'] ?? ''}',
        active: activeChat.isNotEmpty && c['id'] == activeChat,
      ),
  ];
}

class _OptionTile extends StatelessWidget {
  final _PickerItem item;
  final VoidCallback onTap;

  const _OptionTile({required this.item, required this.onTap});

  @override
  Widget build(BuildContext context) {
    return ListTile(
      leading: Icon(
        item.active ? Icons.check_circle : Icons.circle_outlined,
        color: item.active ? AppColors.success : AppColors.textFaint,
        size: 20,
      ),
      title: Text(
        item.label,
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
        style: const TextStyle(color: AppColors.text, fontSize: 14),
      ),
      subtitle: Text(
        item.sub,
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
        style: const TextStyle(color: AppColors.textFaint, fontSize: 11),
      ),
      onTap: onTap,
    );
  }
}
