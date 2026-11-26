import 'dart:convert';
import 'package:http/http.dart' as http;

/// Descubre dinámicamente la URL activa del Hub consultando un GitHub Gist privado.
class GistDiscoveryService {
  static const String _gistFileName = 'hub_config.json';

  final http.Client _client;

  GistDiscoveryService({http.Client? client}) : _client = client ?? http.Client();

  /// Consulta `GET https://api.github.com/gists/<gistId>` y extrae `hub_url` de `hub_config.json`.
  Future<String> fetchActiveHubUrl({
    required String gistId,
    required String gistToken,
  }) async {
    final cleanId = gistId.trim();
    final cleanToken = gistToken.trim();
    if (cleanId.isEmpty || cleanToken.isEmpty) {
      throw Exception('GITHUB_GIST_ID y GITHUB_GIST_TOKEN son requeridos');
    }

    final uri = Uri.parse('https://api.github.com/gists/$cleanId');
    final resp = await _client.get(
      uri,
      headers: {
        'Authorization': 'Bearer $cleanToken',
        'Accept': 'application/vnd.github+json',
      },
    ).timeout(const Duration(seconds: 10));

    if (resp.statusCode < 200 || resp.statusCode >= 300) {
      throw Exception('GitHub Gist HTTP ${resp.statusCode}');
    }

    final body = jsonDecode(resp.body) as Map<String, dynamic>;
    final files = body['files'] as Map<String, dynamic>?;
    final fileObj = files?[_gistFileName] as Map<String, dynamic>?;
    final content = fileObj?['content']?.toString().trim() ?? '';
    if (content.isEmpty) {
      throw Exception('$_gistFileName no encontrado en el Gist');
    }

    final cfg = jsonDecode(content) as Map<String, dynamic>;
    final hubUrl = cfg['hub_url']?.toString().trim() ?? '';
    if (hubUrl.isEmpty) {
      throw Exception('hub_url vacío en $_gistFileName');
    }
    return hubUrl;
  }

  /// Convierte `https://...` a `wss://.../ws/client` (o `http://` -> `ws://`).
  static String toClientWsUrl(String hubUrl) {
    final parsed = Uri.parse(hubUrl.trim());
    final scheme = switch (parsed.scheme) {
      'https' => 'wss',
      'http' => 'ws',
      'wss' || 'ws' => parsed.scheme,
      _ => throw Exception('Esquema no soportado: ${parsed.scheme}'),
    };
    final basePath = parsed.path.endsWith('/')
        ? parsed.path.substring(0, parsed.path.length - 1)
        : parsed.path;
    return parsed.replace(scheme: scheme, path: '$basePath/ws/client').toString();
  }
}
