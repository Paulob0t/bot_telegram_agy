import 'package:flutter_secure_storage/flutter_secure_storage.dart';

class HubCredentials {
  final bool enabled;
  final String gistToken;
  final String gistId;
  final String hubSecret;

  const HubCredentials({
    required this.enabled,
    required this.gistToken,
    required this.gistId,
    required this.hubSecret,
  });

  bool get isComplete =>
      gistToken.trim().isNotEmpty &&
      gistId.trim().isNotEmpty &&
      hubSecret.trim().isNotEmpty;
}

class HubCredentialsStore {
  static const _storage = FlutterSecureStorage();
  static const _kEnabled = 'hub_relay_enabled';
  static const _kGistToken = 'github_gist_token';
  static const _kGistId = 'github_gist_id';
  static const _kHubSecret = 'hub_secret';

  static const _kWsUrl = 'direct_ws_url';
  static const _kWsToken = 'direct_ws_token';

  // Build-time defaults injected by scripts/build-apk.sh from the PC's .env,
  // so no secret lives in the source tree. Values saved in Settings win.
  static const _envGistToken = String.fromEnvironment('GITHUB_GIST_TOKEN');
  static const _envGistId = String.fromEnvironment('GITHUB_GIST_ID');
  static const _envHubSecret = String.fromEnvironment('HUB_SECRET');

  static Future<HubCredentials> load() async {
    final enabledRaw = await _storage.read(key: _kEnabled);
    final gistToken = (await _storage.read(key: _kGistToken)) ?? _envGistToken;
    final gistId = (await _storage.read(key: _kGistId)) ?? _envGistId;
    final hubSecret = (await _storage.read(key: _kHubSecret)) ?? _envHubSecret;
    final creds = HubCredentials(
      enabled: true,
      gistToken: gistToken,
      gistId: gistId,
      hubSecret: hubSecret,
    );
    return HubCredentials(
      // Never toggled in Settings: use the hub whenever the build shipped its values.
      enabled: enabledRaw == null ? creds.isComplete : enabledRaw == 'true',
      gistToken: gistToken,
      gistId: gistId,
      hubSecret: hubSecret,
    );
  }

  /// Direct-mode endpoint (`ws://host:8080/ws`) and its `MOBILE_WS_TOKEN`.
  static Future<(String, String)> loadDirect() async {
    return (
      (await _storage.read(key: _kWsUrl)) ?? '',
      (await _storage.read(key: _kWsToken)) ?? '',
    );
  }

  static Future<void> saveDirect(String wsUrl, String token) async {
    await _storage.write(key: _kWsUrl, value: wsUrl.trim());
    await _storage.write(key: _kWsToken, value: token.trim());
  }

  static Future<void> save(HubCredentials creds) async {
    await _storage.write(
      key: _kEnabled,
      value: creds.enabled ? 'true' : 'false',
    );
    await _storage.write(key: _kGistToken, value: creds.gistToken.trim());
    await _storage.write(key: _kGistId, value: creds.gistId.trim());
    await _storage.write(key: _kHubSecret, value: creds.hubSecret.trim());
  }
}
