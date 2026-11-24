# agent.glb — expected contract

Drop the model at `mobile_app/assets/models/agent.glb`. Until it exists the app
shows the Rive avatar as fallback, and the status pill keeps working.

Recommended pipeline: Ready Player Me avatar (export with ARKit morph targets)
→ Mixamo animations → Blender (merge clips, add mini-laptop prop) → glTF Binary.

## Animation clips (exact names)

| Clip                 | Loop | When                                     |
|----------------------|------|------------------------------------------|
| `Idle`               | yes  | waiting, breathing                       |
| `Thinking`           | yes  | reasoning                                |
| `Coding_Laptop`      | yes  | `CODING_EDIT`, typing on the mini-laptop |
| `Executing_Terminal` | yes  | `EXECUTING`, watching the terminal       |
| `Reading_Code`       | yes  | `READING` / search                       |
| `Success_Celebrate`  | no   | `SUCCESS`, held 2.0 s then `Idle`        |
| `Error_Shock`        | yes  | `ERROR` / disconnected                   |
| `Wave`               | no   | tap while idle                           |

Missing clips are skipped silently. The laptop prop should be parented to a hand
bone and hidden (scale 0) in clips where it is not used.

## Morph targets (ARKit names)

`eyeBlinkLeft`, `eyeBlinkRight` (required: used to find the face mesh),
`mouthSmileLeft`/`mouthSmileRight` (or `mouthSmile`), `browInnerUp`,
`browDownLeft`/`browDownRight`, `eyeWideLeft`/`eyeWideRight`.

## Blender checklist

1. Each action pushed to an NLA track named as above.
2. Export: glTF Binary, Animation → "NLA tracks", Shape Keys on, +Y up.
3. Keep it under ~8 MB; leave Draco compression off.
