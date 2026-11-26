import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';

import '../../../services/ws_client.dart';
import '../../../theme/app_colors.dart';
import '../../../widgets/rive_avatar.dart';
import '../controllers/avatar_prefs_controller.dart';
import '../models/avatar3d_state.dart';
import 'gemini_plush_avatar.dart';

const _accentChoices = [
  AppColors.success,
  AppColors.info,
  AppColors.accent,
  AppColors.violet,
  AppColors.warning,
];

const _boxBackgroundChoices = [
  AppColors.avatarBoxDefault,
  Color(0xFF050608),
  Color(0xFF1B2432),
  Color(0xFF1A2821),
  Color(0xFF2B1F1D),
  Color(0xFF241E2E),
  Color(0xFFE8DFD5),
];

const _hairChoices = [
  ('curly', 'Rizado'),
  ('short', 'Lacio corto'),
  ('long', 'Largo'),
  ('afro', 'Afro / Rizo grande'),
];

const _labelStyle = TextStyle(color: AppColors.textMuted, fontSize: 12);

const _sheetShape16 = RoundedRectangleBorder(
  borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
);

/// Asks the PC to design four characters from [photoPath] and opens the picker.
Future<void> generateAvatarFromPhoto(
  BuildContext context,
  AvatarPrefsController prefs,
  AgyWsClient client,
  String photoPath,
) async {
  if (prefs.isGenerating) return;
  prefs.markGenerating();
  ScaffoldMessenger.of(context).showSnackBar(
    const SnackBar(
      content: Text('Diseñando 4 opciones de personaje desde tu foto…'),
      duration: Duration(seconds: 4),
    ),
  );
  final variants = await client.generateAvatarVariantsFromPhoto(photoPath);
  prefs.setGeneratedVariants(variants);
  if (variants.isNotEmpty && context.mounted) {
    showVariantPicker(context, prefs);
  }
}

void showAvatarCustomizerSheet(
  BuildContext context,
  AvatarPrefsController prefs,
  AgyWsClient client,
) {
  showModalBottomSheet<void>(
    context: context,
    backgroundColor: AppColors.sheetDeep,
    isScrollControlled: true,
    shape: _sheetShape16,
    builder: (sheetCtx) => ListenableBuilder(
      listenable: prefs,
      builder: (_, __) => Padding(
        padding: const EdgeInsets.fromLTRB(20, 20, 20, 24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const Text('Personalizar personaje', style: sheetTitleStyle),
            const SizedBox(height: 12),
            const Text('Estilo', style: _labelStyle),
            const SizedBox(height: 6),
            Wrap(
              spacing: 8,
              children: [
                for (final s in AvatarStyle.values)
                  ChoiceChip(
                    label: Text(s.name),
                    selected: prefs.isBuiltInStyle(s),
                    onSelected: (_) => prefs.selectStyle(s),
                  ),
              ],
            ),
            const SizedBox(height: 12),
            const Text('Accesorio', style: _labelStyle),
            const SizedBox(height: 6),
            Wrap(
              spacing: 8,
              children: [
                for (final a in AvatarAccessory.values)
                  ChoiceChip(
                    label: Text(a.name),
                    selected: prefs.accessory == a,
                    onSelected: (_) => prefs.setAccessory(a),
                  ),
              ],
            ),
            const SizedBox(height: 12),
            const Text('Color de acento', style: _labelStyle),
            const SizedBox(height: 6),
            Row(
              children: [
                for (final c in _accentChoices)
                  _ColorDot(
                    color: c,
                    selected: prefs.accent == c,
                    onTap: () => prefs.setAccent(c),
                  ),
              ],
            ),
            const SizedBox(height: 12),
            const Text('Fondo del cuadro', style: _labelStyle),
            const SizedBox(height: 6),
            _BoxBackgroundRow(prefs: prefs, swatchSize: 28),
            const SizedBox(height: 14),
            FilledButton.tonalIcon(
              onPressed: prefs.isGenerating
                  ? null
                  : () async {
                      final path = await _pickSingle(FileType.image);
                      if (path == null || !sheetCtx.mounted) return;
                      Navigator.of(sheetCtx).pop();
                      await generateAvatarFromPhoto(context, prefs, client, path);
                    },
              icon: const Icon(Icons.auto_awesome_rounded, size: 18),
              label: Text(
                prefs.geminiProfile != null
                    ? 'Subir otra foto para crear 4 opciones'
                    : 'Crear personaje desde tu foto (4 opciones)',
              ),
            ),
            if (prefs.geminiVariants.isNotEmpty) ...[
              const SizedBox(height: 8),
              OutlinedButton.icon(
                onPressed: () {
                  Navigator.of(sheetCtx).pop();
                  showVariantPicker(context, prefs);
                },
                icon: const Icon(Icons.grid_view_rounded, size: 18),
                label: Text('Cambiar entre mis 4 opciones (${prefs.geminiProfile?.vibe ?? "Plush"})'),
              ),
            ],
            const SizedBox(height: 8),
            OutlinedButton.icon(
              onPressed: () async {
                final path = await _pickSingle(FileType.any);
                if (path == null || !path.toLowerCase().endsWith('.riv')) return;
                prefs.setCustomRive(path);
                if (sheetCtx.mounted) Navigator.of(sheetCtx).pop();
              },
              icon: const Icon(Icons.view_in_ar_rounded, size: 18),
              label: Text(
                prefs.customRivePath != null
                    ? 'Rive activo: ${prefs.customRivePath!.split('/').last}'
                    : 'Importar animación (.riv)',
              ),
            ),
          ],
        ),
      ),
    ),
  );
}

void showVariantPicker(BuildContext context, AvatarPrefsController prefs) {
  showModalBottomSheet<void>(
    context: context,
    backgroundColor: AppColors.sheetDeep,
    isScrollControlled: true,
    shape: _sheetShape16,
    builder: (sheetCtx) => SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(18, 18, 18, 22),
        child: ListenableBuilder(
          listenable: prefs,
          builder: (_, __) {
            final variants = prefs.geminiVariants;
            final active = prefs.geminiProfile ?? variants.first;
            return Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                const Text('Elige tu versión del personaje', style: sheetTitleStyle),
                const SizedBox(height: 4),
                const Text('4 estilos creados con los rasgos y paleta de tu foto', style: _labelStyle),
                const SizedBox(height: 10),
                _ChipRow(label: 'Rasgos:', children: [
                  ChoiceChip(
                    label: const Text('Masculino'),
                    selected: !active.isFeminine,
                    onSelected: (_) => prefs.editVariants(
                      (e) => e.copyWith(genderStyle: 'masculine', eyelashes: false),
                    ),
                  ),
                  ChoiceChip(
                    label: const Text('Femenino'),
                    selected: active.isFeminine,
                    onSelected: (_) => prefs.editVariants(
                      (e) => e.copyWith(genderStyle: 'feminine', eyelashes: true),
                    ),
                  ),
                ]),
                const SizedBox(height: 6),
                _ChipRow(label: 'Cabello:', children: [
                  for (final h in _hairChoices)
                    ChoiceChip(
                      label: Text(h.$2),
                      selected: active.hairStyle == h.$1,
                      onSelected: (_) => prefs.editVariants((e) => e.copyWith(hairStyle: h.$1)),
                    ),
                ]),
                const SizedBox(height: 8),
                Row(
                  children: [
                    const Text('Fondo cuadro:', style: _labelStyle),
                    const SizedBox(width: 8),
                    Expanded(child: _BoxBackgroundRow(prefs: prefs, swatchSize: 24)),
                  ],
                ),
                const SizedBox(height: 12),
                GridView.builder(
                  shrinkWrap: true,
                  physics: const NeverScrollableScrollPhysics(),
                  itemCount: variants.length,
                  gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
                    crossAxisCount: 2,
                    mainAxisSpacing: 12,
                    crossAxisSpacing: 12,
                    childAspectRatio: 1.05,
                  ),
                  itemBuilder: (_, idx) {
                    final v = variants[idx];
                    return _VariantTile(
                      variant: v,
                      selected: prefs.geminiProfile?.variant == v.variant,
                      boxBackground: prefs.boxBackground,
                      onTap: () {
                        prefs.selectVariant(v);
                        Navigator.of(sheetCtx).pop();
                      },
                    );
                  },
                ),
              ],
            );
          },
        ),
      ),
    ),
  );
}

Future<String?> _pickSingle(FileType type) async {
  final picked = await FilePicker.pickFiles(type: type);
  if (picked.isEmpty) return null;
  return picked.first.path;
}

class _ChipRow extends StatelessWidget {
  final String label;
  final List<Widget> children;

  const _ChipRow({required this.label, required this.children});

  @override
  Widget build(BuildContext context) {
    return SingleChildScrollView(
      scrollDirection: Axis.horizontal,
      child: Row(
        children: [
          Text(label, style: _labelStyle),
          const SizedBox(width: 8),
          for (final c in children)
            Padding(padding: const EdgeInsets.only(right: 6), child: c),
        ],
      ),
    );
  }
}

class _ColorDot extends StatelessWidget {
  final Color color;
  final bool selected;
  final VoidCallback onTap;

  const _ColorDot({required this.color, required this.selected, required this.onTap});

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      onTap: onTap,
      child: Container(
        margin: const EdgeInsets.only(right: 10),
        width: 28,
        height: 28,
        decoration: BoxDecoration(
          color: color,
          shape: BoxShape.circle,
          border: Border.all(color: selected ? Colors.white : Colors.transparent, width: 2),
        ),
      ),
    );
  }
}

class _BoxBackgroundRow extends StatelessWidget {
  final AvatarPrefsController prefs;
  final double swatchSize;

  const _BoxBackgroundRow({required this.prefs, required this.swatchSize});

  @override
  Widget build(BuildContext context) {
    final current = prefs.boxBackground ?? AppColors.avatarBoxDefault;
    return SingleChildScrollView(
      scrollDirection: Axis.horizontal,
      child: Row(
        children: [
          for (final bg in _boxBackgroundChoices)
            GestureDetector(
              onTap: () => prefs.setBoxBackground(bg),
              child: Container(
                margin: const EdgeInsets.only(right: 10),
                width: swatchSize,
                height: swatchSize,
                decoration: BoxDecoration(
                  color: bg,
                  borderRadius: BorderRadius.circular(swatchSize / 4),
                  border: Border.all(
                    color: current == bg ? AppColors.accent : AppColors.border,
                    width: current == bg ? 2 : 1,
                  ),
                ),
              ),
            ),
        ],
      ),
    );
  }
}

class _VariantTile extends StatelessWidget {
  final GeminiAvatarProfile variant;
  final bool selected;
  final Color? boxBackground;
  final VoidCallback onTap;

  const _VariantTile({
    required this.variant,
    required this.selected,
    required this.boxBackground,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      onTap: onTap,
      child: Container(
        padding: const EdgeInsets.all(10),
        decoration: BoxDecoration(
          color: const Color(0xFF0E1013),
          borderRadius: BorderRadius.circular(12),
          border: Border.all(
            color: selected ? AppColors.accent : const Color(0xFF262B32),
            width: selected ? 2 : 1,
          ),
        ),
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Expanded(
              child: Center(
                child: GeminiCharacterPreview(
                  profile: variant,
                  size: 86,
                  boxBackgroundColor: boxBackground,
                ),
              ),
            ),
            const SizedBox(height: 6),
            Text(
              variant.vibe,
              style: TextStyle(
                color: selected ? AppColors.accent : AppColors.text,
                fontSize: 12.5,
                fontWeight: FontWeight.w600,
              ),
            ),
          ],
        ),
      ),
    );
  }
}
