# CapCut Draft CRUD Operations Reference / 剪映草稿 CRUD 操作参考

Step-by-step guides for CRUD operations on CapCut/JianYing (剪映) draft files.

## Key Conventions / 关键约定

| Convention | Detail |
|---|---|
| **IDs** | Uppercase UUIDs: `uuidgen \| tr '[:lower:]' '[:upper:]'` |
| **Time values** | All microseconds: `seconds x 1,000,000` |
| **Default duration** | 40,000,000 us (40 seconds) |
| **Version** | `version: 360000`, `new_version: "79.0.0"`, `source: "default"` |
| **Timestamps** | Millisecond epoch integers (e.g., `1717400000000`) |

> **Working reference**: `templates/example_minimal.json` contains a complete, valid draft_info.json with 1 video + 1 text + 1 audio segment. All IDs are correctly linked. Use it as your primary reference for how everything connects.

### Standard paths

- macOS: `~/Movies/JianyingPro/User Data/Projects/com.lveditor.draft`
- macOS sandboxed: `~/Library/Containers/com.lemon.lvpro/Data/Movies/JianyingPro/User Data/Projects/com.lveditor.draft`
- Windows: `%LOCALAPPDATA%\JianyingPro\User Data\Projects\com.lveditor.draft`

Look for `root_meta_info.json` in the candidate directory to confirm the correct path.

---

## Operation 1: List All Drafts (列出所有草稿)

Retrieve and display all drafts known to CapCut/JianYing.

1. **Find draft root** using standard paths above.
2. **Read `root_meta_info.json`**. If missing, report "No drafts found -- root_meta_info.json does not exist" and stop.
3. **Parse JSON** and iterate `all_draft_store`.
4. **Present as a table**: `draft_name`, creation date (convert `tm_draft_create` ms -> `date -r $((ms / 1000)) "+%Y-%m-%d %H:%M:%S"`), duration (`tm_duration` us / 1,000,000 -> seconds), `draft_id`.

---

## Operation 2: Create Draft (创建草稿)

Create a brand new CapCut/JianYing draft project from scratch.

### Input Needed

- **Draft name**, **canvas preset** (or custom width/height/ratio)
- **Media files** with timing: file path, start_seconds, end_seconds
- **Optional**: subtitle text with timing, audio file, cover image

### Steps

#### Step 1 -- Discover draft root path (see standard paths above)

#### Step 2 -- Read or init `root_meta_info.json`

- If exists: read and parse.
- If missing: create `{ "all_draft_store": [], "draft_ids": 0, "root_path": "<draftRootPath>" }`

#### Step 3 -- Generate `draft_id` via `uuidgen | tr '[:lower:]' '[:upper:]'`

#### Step 4 -- Create draft folder under draft root

Handle name collisions by appending `(1)`, `(2)`, etc. Use `mkdir -p`.

#### Step 5 -- Generate draft_info.json

##### 5a. canvas_config

Based on the chosen preset:

| Preset | width | height | ratio |
|--------|-------|--------|-------|
| 横屏 Horizontal | 1920 | 1080 | "16:9" |
| 竖屏 Vertical | 1080 | 1920 | "9:16" |
| 全屏 Fullscreen | 1920 | 1440 | "4:3" |
| 正方形 Square | 1920 | 1920 | "1:1" |
| 原始 Original | 0 | 0 | "original" |

```json
{ "width": <width>, "height": <height>, "ratio": "<ratio>" }
```

##### 5b. config object

Use the `config` section from `templates/full_draft_info.json` directly -- it contains all required fields.

##### 5c. Initialize empty materials arrays

`videos`, `audios`, `texts`, `effects`, `transitions`, `canvases`, `speeds`, `sound_channel_mappings`, `material_animations`, `drafts` -- all start as `[]`.

##### 5d. For each video/image segment

Generate UUIDs: `videoId`, `canvasId`, `speedId`, `soundChannelMappingId`, `segmentId`.

1. Copy `templates/material_video.json` and replace:
   - `__VIDEO_ID__` -> new UUID
   - `__FILE_PATH__` -> absolute image/video file path
   - `__FILE_NAME__` -> filename
   - `__IMAGE_HEIGHT__` / `__IMAGE_WIDTH__` -> media dimensions
   - `__SEGMENT_DURATION_US__` -> duration in microseconds
   - Use `type: "photo"` for images, `type: "video"` for video files.
   - Insert into `materials.videos[]`.

2. Copy `templates/aux_canvas.json` -> replace `__CANVAS_ID__` with new UUID. Insert into `materials.canvases[]`.

3. Copy `templates/aux_speed.json` -> replace `__SPEED_ID__` with new UUID. Insert into `materials.speeds[]`.
   - Only `{ "id": "<speedId>", "speed": 1.0, "type": "speed" }` -- no `mode` or `name` fields.

4. Copy `templates/aux_sound_channel.json` -> replace `__SOUND_CHANNEL_ID__` with new UUID. Insert into `materials.sound_channel_mappings[]`.
   - Only `{ "id": "<soundChannelMappingId>", "type": "none" }` -- no `audio_channel_mapping` field.
   - Note: sound_channel_mapping is NOT included in `extra_material_refs`.

5. Copy `templates/segment_video.json` -> replace all `__*__` placeholders with generated IDs and timing values. Add to video track's `segments[]`.
   - `render_index`: fixed `0`. For photos: `source_timerange.start` = 0, duration = target duration.
   - `clip.scale`: `{ "x": 2.0, "y": 2.0 }` for video segments. `volume`: `1.0`.

6. `extra_material_refs` = `[audioId, canvasId, speedId]` (omit audioId if no audio; append animationId if animation applied). Do NOT include soundChannelMappingId.

7. **Optional animation**: generate `animationId`, add to `materials.material_animations[]`, append to `extra_material_refs`. See `references/presets.md` for animation presets.

##### 5e. For each subtitle

1. Generate `textId`, `segmentId`.

2. Copy `templates/material_text.json` and replace:
   - `__TEXT_ID__` -> new UUID
   - `__TEXT_CONTENT__` -> the subtitle text
   - Insert into `materials.texts[]`.

3. Copy `templates/segment_text.json` and replace:
   - `__SEGMENT_ID__` -> new UUID
   - `__TEXT_ID__` -> same UUID as above
   - `__TEXT_DURATION_US__` -> subtitle duration in microseconds
   - `__TEXT_START_US__` -> subtitle start time in microseconds
   - `render_index`: fixed `14002`.
   - Add to text track's `segments[]`.

##### 5f. For audio (if provided)

1. Generate `audioId`.

2. Copy `templates/material_audio.json` and replace:
   - `__AUDIO_ID__` -> new UUID
   - `__AUDIO_DURATION_US__` -> audio duration in microseconds
   - `__AUDIO_FILE_NAME__` -> filename
   - `__AUDIO_FILE_PATH__` -> absolute audio file path
   - Insert into `materials.audios[]`.

3. Copy `templates/segment_audio.json` and replace:
   - `__SEGMENT_ID__` -> new UUID
   - `__AUDIO_ID__` -> same UUID as above
   - `__AUDIO_DURATION_US__` -> audio duration in microseconds
   - `render_index`: fixed `0`. Default `volume`: `0.3162277638912201` (= `1/sqrt(10)`).
   - Add to audio track's `segments[]`.

##### 5g. Build tracks

Create 4 tracks: `video` (with video segments), `effect` (empty initially), `audio` (with audio segments), `text` (with text segments).

Each track: `{ "attribute": 0, "flag": <segmentCount>, "id": "<trackId>", "segments": [...], "type": "<type>" }`. `flag` = number of segments.

##### 5h. Cover draft

See `templates/example_minimal.json` -> `materials.drafts[0]` for exact structure. Generate UUIDs for `coverDraftId` and `coverVideoId`, set canvas_config to match main draft, point video path to `<draft_folder>/draft_cover.jpg`.

Set top-level `cover`: `{ "cover_draft_id": "<coverDraftId>", "sub_type": "local", "type": "image" }`.

##### 5i. Assemble and write draft_info.json

See `templates/example_minimal.json` for the complete top-level structure. Key fields: `canvas_config`, `config`, `cover`, `duration`, `fps: 30.0`, `id`, `materials`, `new_version: "79.0.0"`, `source: "default"`, `tracks`, `version: 360000`. **No `platform` or `name` field** -- source code doesn't set them.

#### Step 6 -- Generate draft_meta_info.json

Copy `templates/full_draft_meta_info.json` and replace the 5 unique placeholders: `__DRAFT_FOLDER__` (3 occurrences), `__DRAFT_ID__`, `__DRAFT_NAME__`, `__TIMESTAMP_MS__` (2 occurrences), `__TOTAL_DURATION_US__`.

Important: `draft_root_path` points to the draft **folder**, not the root directory.

Write to the draft folder.

#### Step 7 -- Copy cover image (if provided)

```bash
cp "<coverImagePath>" "<draftFolderPath>/draft_cover.jpg"
```

#### Step 8 -- Update root_meta_info.json

Copy `templates/root_meta_entry.json`, replace: `__DRAFT_FOLDER__`, `__DRAFT_ID__`, `__DRAFT_NAME__`, `__DRAFT_ROOT__`, `__TIMESTAMP_MS__`. Prepend to `all_draft_store` (newest first), increment `draft_ids`, write back.

#### Step 9 -- Validate

Read back `root_meta_info.json` and confirm new draft appears in `all_draft_store`.

---

## Operation 3: Read Draft Details (读取草稿详情)

Display the full contents and structure of a specific draft.

1. **List all drafts** (Operation 1), let user pick one by name or index.
2. **Read `draft_info.json`** from `draft_fold_path`.
3. **Parse and present**:
   - Canvas config: width x height, ratio
   - Duration: microseconds and seconds
   - FPS
   - Track breakdown with per-segment details:

   **Video segments**: file path, source/target timeranges, speed, canvas, animations
   **Text segments**: content (strip formatting), target timerange, position (transform.x/y), font size/color
   **Audio segments**: file path, duration, volume
   **Effects**: type/name, target timerange
   **Transitions**: type, referenced segments

---

## Operation 4: Modify Draft (修改草稿)

All modifications follow the same pattern: **read** `draft_info.json` -> **modify** in memory -> **write back** -> **update `tm_draft_modified`** in both `draft_info.json` and `draft_meta_info.json`.

### 4a. Add Video Segment (添加视频片段)

Append a new video or image segment to the video track.

1. **Read `draft_info.json`**.
2. **Calculate video track end time**: max of all segments' `target_timerange.start + target_timerange.duration` (0 if empty).
3. **Generate UUIDs**: `videoId`, `canvasId`, `speedId`, `soundChannelMappingId`, `segmentId`.
4. Use templates: `material_video.json`, `aux_canvas.json`, `aux_speed.json`, `aux_sound_channel.json`, `segment_video.json` -- replace all `__*__` placeholders with generated IDs and timing values. Set `target_timerange.start` to calculated end time. `render_index: 0`, `clip.scale: { "x": 2.0, "y": 2.0 }`. Include `audioId` in `extra_material_refs` if applicable.
5. **Append** segment to video track's `segments[]`, increment `track.flag`.
6. **Update duration** if segment extends beyond current: `max(currentDuration, segment end time)`.
7. **Write back** and **update timestamps**.

---

### 4b. Add Subtitle (添加字幕)

Add a text/subtitle segment to the text track.

1. **Read `draft_info.json`**. Generate `textId`, `segmentId`.
2. Use templates: `material_text.json` (replace `__TEXT_ID__`, `__TEXT_CONTENT__`), `segment_text.json` (replace `__SEGMENT_ID__`, `__TEXT_ID__`, `__TEXT_DURATION_US__`, `__TEXT_START_US__`). Default `content`: `"<size=8.0>[Your Text Here]</size>"`, `text_color`: `"#FFFFFF"`. `render_index: 14002`, `clip.transform.y: -0.75`.
3. **Find or create** text track. If track with `type: "text"` exists, append and increment `track.flag`; otherwise create new text track (see Step 5g for structure).
4. **Write back** and **update timestamps**.

---

### 4c. Add Effect (添加特效)

Add a visual effect to the effect track.

1. **Read `draft_info.json`**. Choose effect preset from `references/presets.md`.
2. Generate UUID. Use templates: `material_effect.json`, `segment_effect.json` -- replace `__*__` placeholders. `render_index: 11005`.
3. Append to `materials.effects[]`. Find or create effect track, append segment, increment `track.flag`.
4. **Write back** and **update timestamps**.

---

### 4d. Add Transition (添加转场)

Add a transition between two video segments.

1. **Read `draft_info.json`**. Choose transition preset from `references/presets.md`.
2. Generate UUID. Use template: `material_transition.json` -- replace `__*__` placeholder.
3. Add to `materials.transitions[]`. Add transition `id` to appropriate video segment's `extra_material_refs`. Transitions have no track segment -- they are referenced through `extra_material_refs`.
4. **Write back** and **update timestamps**.

---

### 4e. Remove Segment (删除片段)

Remove a segment and all its associated materials.

1. **Read `draft_info.json`**.
2. Find target track and segment (by `material_id` or `target_timerange`).
3. Remove segment from `track.segments[]`, decrement `track.flag`.
4. Remove corresponding material (by `id == material_id`) and all `extra_material_refs` materials from their respective arrays (canvases, speeds, sound_channel_mappings, material_animations, etc.).
5. **Write back** and **update timestamps**.

---

### 4f. Adjust Timing (调整时间)

Change the start time or duration of a segment.

1. **Read `draft_info.json`**. Find target segment.
2. Modify `target_timerange` (and `source_timerange` if applicable).
3. If ripple edit: shift subsequent segments' `target_timerange.start` by the duration delta to prevent gaps/overlaps.
4. Update draft `duration`: `max(all segments' target_timerange.start + target_timerange.duration)`.
5. **Write back** and **update timestamps**.

---

## Operation 5: Delete Draft (删除草稿)

Permanently remove a draft and all its files.

1. **List drafts** (Operation 1), show draft name and ID, get explicit user confirmation.
2. **Read `root_meta_info.json`**, remove entry from `all_draft_store` by `draft_id` or `draft_name`, decrement `draft_ids`, write back.
3. Remove draft folder: `rm -rf "<draft_fold_path>"`.
4. Verify: `test -d "<draft_fold_path>" && echo "ERROR" || echo "OK"`.

---

## Validation Rules (验证规则)

Apply after any write operation:

### 1. Valid JSON
```bash
jq empty "<filePath>" && echo "Valid JSON"
```

### 2. Material References Exist
Every segment `material_id` must resolve to an entry in the corresponding `materials.*[]` array.

### 3. Extra Material References Resolve
All IDs in `extra_material_refs` must exist in one of the materials arrays.

### 4. No Overlapping Timings Within a Track
Within each track, `target_timerange` ranges must not overlap.

### 5. Non-negative Time Values
All time values (start, duration) must be >= 0, in microseconds.

### 6. Uppercase UUIDs
All `id` and `material_id` fields must match `XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX`.

### 7. Required Fields Present
- Every material: `id`, `type`
- Every segment: `id`, `material_id`
- Every timerange: `start`, `duration`
- Top-level: `canvas_config`, `duration`, `materials`, `tracks`, `version`

### 8. Fixed render_index Values
- Video segments: `0`
- Effect segments: `11005`
- Text segments: `14002`
- Audio segments: `0`
