import 'dart:convert';
import 'dart:io';

import 'package:http/http.dart' as http;

/// Thrown when the PC answers an HTTP call with an error status.
class PcApiException implements Exception {
  final String message;
  const PcApiException(this.message);

  @override
  String toString() => message;
}

/// Result of `POST /upload`.
typedef UploadedFile = ({String path, String fileName});

/// HTTP endpoints of the PC's direct mobile server, derived from the WebSocket URL.
class PcHttpApi {
  final String wsUrl;
  final String token;

  const PcHttpApi({required this.wsUrl, required this.token});

  Map<String, String> get _auth => {'Authorization': 'Bearer $token'};

  Uri _uri(String path, [Map<String, String>? query]) {
    final ws = Uri.parse(wsUrl);
    return ws.replace(
      scheme: ws.scheme == 'wss' ? 'https' : 'http',
      path: path,
      queryParameters: query,
    );
  }

  Future<Map<String, dynamic>> _getJson(Uri uri, Duration timeout) async {
    final res = await http.get(uri, headers: _auth).timeout(timeout);
    if (res.statusCode != 200) throw PcApiException('HTTP ${res.statusCode}');
    return jsonDecode(res.body) as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> _postFile(
    String path,
    String filePath, {
    Map<String, String> fields = const {},
    Duration timeout = const Duration(seconds: 30),
  }) async {
    final req = http.MultipartRequest('POST', _uri(path))
      ..headers.addAll(_auth)
      ..fields.addAll(fields)
      ..files.add(await http.MultipartFile.fromPath('file', filePath));
    final streamed = await req.send().timeout(timeout);
    final body = await streamed.stream.bytesToString();
    if (streamed.statusCode >= 400)
      throw PcApiException('HTTP ${streamed.statusCode}');
    return jsonDecode(body) as Map<String, dynamic>;
  }

  /// Uploads a file; [target] is `cache`, `cwd` or `downloads`.
  Future<UploadedFile> upload(
    String filePath, {
    String target = 'cache',
  }) async {
    final res = await _postFile(
      '/upload',
      filePath,
      fields: {'target': target},
    );
    return (
      path: res['path'] as String? ?? filePath,
      fileName: res['file_name'] as String? ?? 'archivo',
    );
  }

  /// Raw JSON of `POST /avatar/from-photo`: `variants` list or single `traits`.
  Future<Map<String, dynamic>> avatarFromPhoto(String filePath) => _postFile(
    '/avatar/from-photo',
    filePath,
    timeout: const Duration(seconds: 40),
  );

  /// Obsidian vault listing or note content.
  Future<Map<String, dynamic>> obsidian(String relPath) => _getJson(
    _uri('/obsidian', relPath.isEmpty ? null : {'path': relPath}),
    const Duration(seconds: 6),
  );

  /// `{build, version, apk_exists}` of the APK the PC can serve.
  Future<Map<String, dynamic>> version() =>
      _getJson(_uri('/version'), const Duration(seconds: 4));

  /// Downloads the APK served by the PC into [dest].
  Future<File> downloadApk(File dest) async {
    final res = await http
        .get(_uri('/apk'), headers: _auth)
        .timeout(const Duration(minutes: 5));
    if (res.statusCode != 200) throw PcApiException('HTTP ${res.statusCode}');
    return dest.writeAsBytes(res.bodyBytes, flush: true);
  }
}
