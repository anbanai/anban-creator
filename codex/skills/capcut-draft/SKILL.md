---
name: capcut-draft
description: 操控剪映/CapCut 草稿文件——创建、读取、修改、删除剪映项目草稿。用户提到"剪映"、"剪映草稿"、"剪映项目"、"CapCut draft"、"JianYing draft"、"导出剪映"、"创建剪映草稿"、"修改剪映草稿"时使用此 skill。当用户需要批量操作剪映项目、自动化视频编辑工作流、或程序化管理剪映草稿时也应触发。
---

# CapCut Draft Manipulation (剪映草稿操控)

This skill enables Claude to directly manipulate CapCut/JianYing (剪映) draft files on the local filesystem using built-in tools (Read, Write, Edit, Bash). No external scripts or servers needed.

## Supported Operations

- **List** all drafts with metadata
- **Create** new drafts with video clips, subtitles, audio, effects, transitions, animations
- **Read** draft structure and content
- **Modify** existing drafts (add/remove/adjust segments, subtitles, effects)
- **Delete** drafts

## Draft Root Discovery (草稿根目录)

CapCut stores all drafts under a root directory. Find it by checking these paths in order:

**macOS:**
```
~/Movies/JianyingPro/User Data/Projects/com.lveditor.draft
```
If not found, try:
```
~/Library/Containers/com.lemon.lvpro/Data/Movies/JianyingPro/User Data/Projects/com.lveditor.draft
```

**Windows:**
```
%LOCALAPPDATA%\JianyingPro\User Data\Projects\com.lveditor.draft
```

To discover programmatically:
```bash
# macOS
ls ~/Movies/JianyingPro/User\ Data/Projects/com.lveditor.draft/root_meta_info.json 2>/dev/null && echo "FOUND"

# Windows (Git Bash)
ls "$LOCALAPPDATA/JianyingPro/User Data/Projects/com.lveditor.draft/root_meta_info.json" 2>/dev/null && echo "FOUND"
```

If the user provides a custom draft root path, use that instead.

## File Structure

```
<DraftRoot>/
├── root_meta_info.json          # Global index of all drafts
├── MyDraft/                     # One folder per draft
│   ├── draft_info.json          # Main content (tracks, materials, segments)
│   ├── draft_meta_info.json     # Metadata
│   └── draft_cover.jpg          # Cover image
└── AnotherDraft/
    └── ...
```

## Time System (时间系统)

CapCut uses **microseconds** for all time values.

- Convert seconds to microseconds: `seconds × 1,000,000`
- Convert microseconds to seconds: `microseconds ÷ 1,000,000`
- Default draft duration: `40,000,000` (40 seconds)
- Animation duration: ~`500,000` (0.5 seconds)
- Transition duration: ~`466,666` (0.47 seconds)

## UUID Rules

All IDs must be **uppercase UUIDs**. Generate with:
```bash
uuidgen | tr '[:lower:]' '[:upper:]'
# Example output: A1B2C3D4-E5F6-7890-ABCD-EF1234567890
```

When using effect/transition presets, always regenerate the `id` field with a new UUID. Only reuse `effect_id` and `resource_id`.

## Canvas Presets (画布预设)

| Name | Width | Height | Ratio |
|------|-------|--------|-------|
| 横屏 Horizontal | 1920 | 1080 | 16:9 |
| 竖屏 Vertical | 1080 | 1920 | 9:16 |
| 全屏 Fullscreen | 1920 | 1440 | 4:3 |
| 正方形 Square | 1920 | 1920 | 1:1 |
| 原始 Original | varies | varies | original |

## Render Index Values

| Track type | render_index |
|------------|-------------|
| video | 0 |
| effect | 11005 |
| text | 14002 |
| audio | 0 |

## Quick Operations

### List all drafts
Read `root_meta_info.json` → iterate `all_draft_store` → show table with draft_name, creation time, duration, draft_id.

### Create a draft
See `references/operations.md` for the full workflow. Summary:
1. Discover root → generate UUID → create folder
2. Build `draft_info.json` with canvas_config, config, materials, tracks
3. Write `draft_meta_info.json`
4. Copy cover image
5. Update `root_meta_info.json` (prepend to all_draft_store, increment draft_ids)

### Modify a draft
Read `draft_info.json` → make changes → write back. Common modifications:
- Add video segment: new Video material + Canvas + Speed + SoundChannelMapping + TrackSegment on video track
- Add subtitle: new Text material + TrackSegment on text track (render_index: 14002, clip.transform.y: -0.75)
- Add effect: clone preset with new UUID + TrackSegment on effect track (render_index: 11005)
- Adjust timing: modify target_timerange on segment

### Delete a draft
Remove entry from `root_meta_info.json` → delete draft folder with `rm -rf`

## Key Defaults

- Text font size: `8.0`
- Text color: `#FFFFFF`
- Text Y position: `-0.75` (bottom of screen)
- Text content format: `<size=8.0>[text content]</size>`
- Audio volume: `0.3162277638912201`
- Video clip scale: `{x: 2, y: 2}`
- Video HDR: `{intensity: 1.0, mode: 1, nits: 1000}`
- version: `360000`
- new_version: `"79.0.0"`
- source: `"default"`

## Important Notes

- **Close CapCut before modifying drafts** — if CapCut is running, it may overwrite your changes or cause corruption
- **Always validate JSON** after writing — invalid JSON will crash CapCut
- **material_id references must be consistent** — every segment's material_id must exist in the corresponding materials array
- **extra_material_refs must resolve** — video segments reference audio, canvas, speed, and animation IDs; all must exist
- **No time overlap** within the same track — segments on the same track must not have overlapping target_timerange

## JSON Templates (JSON 模板)

All templates use `__PLACEHOLDER__` format. Copy a template, replace placeholders with actual values.

**Start here:**
- `templates/example_minimal.json` — Complete working example (1 video + 1 text + 1 audio). All IDs correctly linked. Use as primary reference.

**Complete draft files:**
- `templates/full_draft_info.json` — Full draft_info.json with all sections (copy → fill per-segment arrays)
- `templates/full_draft_meta_info.json` — draft_meta_info.json (5 unique placeholders)
- `templates/root_meta_entry.json` — Entry to prepend into root_meta_info.json's all_draft_store (5 unique placeholders)

**Material templates** (add to materials.*[] arrays):
- `templates/material_video.json` — Video/photo material (6 placeholders: VIDEO_ID, FILE_PATH, FILE_NAME, IMAGE_WIDTH, IMAGE_HEIGHT, SEGMENT_DURATION_US)
- `templates/material_audio.json` — Audio material (4 placeholders: AUDIO_ID, AUDIO_DURATION_US, AUDIO_FILE_NAME, AUDIO_FILE_PATH)
- `templates/material_text.json` — Text/subtitle material (2 placeholders: TEXT_ID, TEXT_CONTENT)
- `templates/material_effect.json` — Video effect (clone from preset, replace instance ID + preset IDs)
- `templates/material_transition.json` — Transition (clone from preset, replace instance ID + preset IDs)
- `templates/material_animation.json` — Animation container (fill from presets)

**Track segment templates** (add to track.segments[] arrays):
- `templates/segment_video.json` — Video segment (references VIDEO_ID, AUDIO_ID, CANVAS_ID, SPEED_ID, timing)
- `templates/segment_audio.json` — Audio segment (references AUDIO_ID, duration)
- `templates/segment_text.json` — Text segment (references TEXT_ID, timing; render_index=14002, y=-0.75)
- `templates/segment_effect.json` — Effect segment (references EFFECT_ID, timing; render_index=11005)

**Auxiliary material templates** (one per video segment):
- `templates/aux_canvas.json` — Canvas material (1 placeholder: CANVAS_ID)
- `templates/aux_speed.json` — Speed material (1 placeholder: SPEED_ID)
- `templates/aux_sound_channel.json` — Sound channel mapping (1 placeholder: SOUND_CHANNEL_ID)

### Workflow: Create a new draft using templates

1. Generate UUIDs: `for i in $(seq 1 20); do uuidgen | tr '[:lower:]' '[:upper:]'; done`
2. Copy `full_draft_info.json` → replace top-level placeholders (CANVAS, FPS, DRAFT_ID, DURATION)
3. For each video segment: copy `material_video.json` + `segment_video.json` + `aux_canvas.json` + `aux_speed.json` + `aux_sound_channel.json`, fill IDs/timing, insert into the arrays
4. For each subtitle: copy `material_text.json` + `segment_text.json`, fill TEXT_ID/timing
5. For audio: copy `material_audio.json` + `segment_audio.json`, fill AUDIO_ID/duration
6. Copy `full_draft_meta_info.json` → replace placeholders
7. Copy `root_meta_entry.json` → prepend to root_meta_info.json's all_draft_store

## Reference Files

For detailed specifications, read these files:

- `references/draft-structure.md` — Complete JSON schema for all three draft files
- `references/presets.md` — All animation, effect, and transition presets with exact IDs and parameters
- `references/operations.md` — Detailed step-by-step guides for each CRUD operation
