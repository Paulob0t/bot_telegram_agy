/// Contract between the Go `agent_status` WebSocket event and the 3D avatar.
///
/// Clip names and blendshape keys must match `assets/models/agent.glb`
/// (see `assets/models/AVATAR_SPEC.md`).
enum Avatar3DState {
  idle('Idle', true, {'mouthSmile': 0.25}),
  starting('Thinking', true, {'browInnerUp': 0.35, 'eyeWide': 0.2}),
  thinking('Thinking', true, {'browInnerUp': 0.6}),
  codingEdit('Coding_Laptop', true, {'browDown': 0.35}),
  executing('Executing_Terminal', true, {'eyeWide': 0.4}),
  reading('Reading_Code', true, {'browDown': 0.15}),
  wrappingUp('Coding_Laptop', true, {'mouthSmile': 0.45}),
  success('Success_Celebrate', false, {'mouthSmile': 0.95}),
  error('Error_Shock', true, {'eyeWide': 0.7, 'browInnerUp': 0.5});

  final String clip;
  final bool loop;

  /// Logical blendshape weights. A key such as `mouthSmile` also drives
  /// `mouthSmileLeft` / `mouthSmileRight` when the model splits them.
  final Map<String, double> blendshapes;

  const Avatar3DState(this.clip, this.loop, this.blendshapes);

  bool get isUsingLaptop =>
      this == Avatar3DState.codingEdit ||
      this == Avatar3DState.executing ||
      this == Avatar3DState.reading ||
      this == Avatar3DState.wrappingUp;

  bool get isWorking =>
      this != Avatar3DState.idle &&
      this != Avatar3DState.success &&
      this != Avatar3DState.error;

  static Avatar3DState fromWire(String? raw) {
    switch (raw?.toUpperCase().trim()) {
      case 'STARTING':
        return Avatar3DState.starting;
      case 'THINKING':
        return Avatar3DState.thinking;
      case 'CODING_EDIT':
      case 'CODING_CREATE':
        return Avatar3DState.codingEdit;
      case 'EXECUTING':
        return Avatar3DState.executing;
      case 'READING':
      case 'SEARCHING':
        return Avatar3DState.reading;
      case 'WRAPPING_UP':
        return Avatar3DState.wrappingUp;
      case 'SUCCESS':
        return Avatar3DState.success;
      case 'ERROR':
      case 'DISCONNECTED':
        return Avatar3DState.error;
      default:
        return Avatar3DState.idle;
    }
  }
}

/// One-shot reaction clip played on tap while idle.
const String kAvatarWaveClip = 'Wave';

/// Blendshapes the procedural blink loop drives.
const List<String> kBlinkShapes = ['eyeBlinkLeft', 'eyeBlinkRight'];

const Duration kAvatarCrossFade = Duration(milliseconds: 250);
const Duration kAvatarSuccessHold = Duration(milliseconds: 3500);

/// Payload of `{"type":"agent_status", ...}`.
class AgentActivity {
  final Avatar3DState state;
  final String label;
  final String detail;
  final String engine;

  const AgentActivity({
    required this.state,
    this.label = '',
    this.detail = '',
    this.engine = '',
  });

  static const idle = AgentActivity(state: Avatar3DState.idle);

  factory AgentActivity.fromJson(Map<String, dynamic> json) => AgentActivity(
        state: Avatar3DState.fromWire(json['state'] as String?),
        label: json['label'] as String? ?? '',
        detail: json['detail'] as String? ?? '',
        engine: json['engine'] as String? ?? '',
      );
}

/// Visual traits extracted by Gemini Vision (`/avatar/from-photo`) to construct
/// a custom stylized character from a user photo.
class GeminiAvatarProfile {
  final String variant; // plush_hood, vinyl_you, cyber_pod, spirit_plush
  final String genderStyle; // feminine, masculine, neutral
  final bool eyelashes;
  final int skinColor;
  final String hairStyle; // short, curly, spiky, long, wavy_long, ponytail, afro, none
  final int hairColor;
  final String glasses; // none, round, square, visor
  final String facialHair; // none, beard, goatee, mustache
  final String headwear; // none, cap, beanie, headphones, plush_hood
  final int hoodieColor;
  final int accentColor;
  final String vibe;

  const GeminiAvatarProfile({
    this.variant = 'plush_hood',
    this.genderStyle = 'neutral',
    this.eyelashes = false,
    this.skinColor = 0xFFF3D1BC,
    this.hairStyle = 'curly',
    this.hairColor = 0xFF231C18,
    this.glasses = 'none',
    this.facialHair = 'none',
    this.headwear = 'plush_hood',
    this.hoodieColor = 0xFFEFE6DC,
    this.accentColor = 0xFFD97757,
    this.vibe = 'Cozy Plush',
  });

  bool get isFeminine => genderStyle == 'feminine' || eyelashes;

  GeminiAvatarProfile copyWith({
    String? variant,
    String? genderStyle,
    bool? eyelashes,
    String? hairStyle,
  }) {
    return GeminiAvatarProfile(
      variant: variant ?? this.variant,
      genderStyle: genderStyle ?? this.genderStyle,
      eyelashes: eyelashes ?? this.eyelashes,
      skinColor: skinColor,
      hairStyle: hairStyle ?? this.hairStyle,
      hairColor: hairColor,
      glasses: glasses,
      facialHair: (eyelashes ?? this.eyelashes) ? 'none' : facialHair,
      headwear: headwear,
      hoodieColor: hoodieColor,
      accentColor: accentColor,
      vibe: vibe,
    );
  }

  static int _parseHex(String? hex, int fallback) {
    if (hex == null || hex.isEmpty) return fallback;
    final clean = hex.replaceAll('#', '').trim();
    if (clean.length == 6) {
      return int.tryParse('FF$clean', radix: 16) ?? fallback;
    }
    if (clean.length == 8) {
      return int.tryParse(clean, radix: 16) ?? fallback;
    }
    return fallback;
  }

  factory GeminiAvatarProfile.fromJson(Map<String, dynamic> json) {
    final gStyle = (json['genderStyle'] as String? ?? 'neutral').toLowerCase();
    final lashes = (json['eyelashes'] as bool?) ?? (gStyle == 'feminine');
    return GeminiAvatarProfile(
      variant: (json['variant'] as String? ?? 'plush_hood').toLowerCase(),
      genderStyle: gStyle,
      eyelashes: lashes,
      skinColor: _parseHex(json['skinColor'] as String?, 0xFFF3D1BC),
      hairStyle: (json['hairStyle'] as String? ?? 'curly').toLowerCase(),
      hairColor: _parseHex(json['hairColor'] as String?, 0xFF231C18),
      glasses: (json['glasses'] as String? ?? 'none').toLowerCase(),
      facialHair: (json['facialHair'] as String? ?? 'none').toLowerCase(),
      headwear: (json['headwear'] as String? ?? 'plush_hood').toLowerCase(),
      hoodieColor: _parseHex(json['hoodieColor'] as String?, 0xFFEFE6DC),
      accentColor: _parseHex(json['accentColor'] as String?, 0xFFD97757),
      vibe: json['vibe'] as String? ?? 'Cozy Plush',
    );
  }

  Map<String, dynamic> toJson() => {
        'variant': variant,
        'genderStyle': genderStyle,
        'eyelashes': eyelashes,
        'skinColor': '#${(skinColor & 0xFFFFFF).toRadixString(16).padLeft(6, '0')}',
        'hairStyle': hairStyle,
        'hairColor': '#${(hairColor & 0xFFFFFF).toRadixString(16).padLeft(6, '0')}',
        'glasses': glasses,
        'facialHair': facialHair,
        'headwear': headwear,
        'hoodieColor': '#${(hoodieColor & 0xFFFFFF).toRadixString(16).padLeft(6, '0')}',
        'accentColor': '#${(accentColor & 0xFFFFFF).toRadixString(16).padLeft(6, '0')}',
        'vibe': vibe,
      };
}
