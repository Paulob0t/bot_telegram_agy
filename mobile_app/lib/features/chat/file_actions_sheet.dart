import 'package:flutter/material.dart';

import '../../services/ws_client.dart';
import '../../theme/app_colors.dart';

const _imageExtensions = ['.png', '.jpg', '.jpeg', '.webp'];

/// What to do with a file picked from the phone.
void showFileActionsSheet(
  BuildContext context, {
  required AgyWsClient client,
  required String path,
  required String fileName,
  required VoidCallback onAttach,
  required VoidCallback onCreateAvatar,
}) {
  final lower = path.toLowerCase();
  final isImage = _imageExtensions.any(lower.endsWith);

  showModalBottomSheet<void>(
    context: context,
    backgroundColor: AppColors.sheet,
    shape: sheetShape,
    builder: (ctx) {
      Widget action(IconData icon, Color color, String label, VoidCallback run) => ListTile(
            leading: Icon(icon, color: color),
            title: Text(label, style: const TextStyle(color: AppColors.text)),
            onTap: () {
              Navigator.of(ctx).pop();
              run();
            },
          );

      return SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(20, 16, 20, 8),
              child: Text(
                'Archivo: $fileName',
                style: const TextStyle(color: AppColors.text, fontSize: 15, fontWeight: FontWeight.w600),
              ),
            ),
            action(Icons.add_photo_alternate_rounded, AppColors.accent,
                'Adjuntar al mensaje para escribir texto', onAttach),
            action(Icons.auto_awesome, AppColors.accent, 'Enviar directo para analizar con IA',
                () => client.uploadFile(path, target: 'cache', analyzeWithAI: true)),
            if (isImage)
              action(Icons.face_retouching_natural, AppColors.violet,
                  'Crear personaje con Gemini (con laptop)', onCreateAvatar),
            action(Icons.folder_outlined, AppColors.info, 'Guardar en proyecto (cwd)',
                () => client.uploadFile(path, target: 'cwd')),
            action(Icons.download_outlined, AppColors.success, 'Guardar en ~/Downloads',
                () => client.uploadFile(path, target: 'downloads')),
          ],
        ),
      );
    },
  );
}
