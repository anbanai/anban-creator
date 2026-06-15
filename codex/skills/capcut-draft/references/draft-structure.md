# CapCut/JianYing (剪映) Draft File Structure Reference

CapCut/JianYing drafts consist of 3 JSON files. This document describes every field.

## Disk Layout

```
<DraftRoot>/
  root_meta_info.json
  MyDraft/
    draft_info.json
    draft_meta_info.json
    draft_cover.jpg
```

Default draft root paths:

- macOS: `~/Movies/JianyingPro/User Data/Projects/com.lveditor.draft`
- Windows: `%LOCALAPPDATA%\JianyingPro\User Data\Projects\com.lveditor.draft`

## Key Constants

| Constant | Value |
|---|---|
| All IDs | Uppercase UUID (`uuidgen \| tr '[:lower:]' '[:upper:]'`) |
| Time units | Microseconds (seconds x 1,000,000) |
| Default duration | 40,000,000 microseconds |
| version | 360000 |
| new_version | "79.0.0" |
| source | "default" |
| Text font size | 8.0 |
| Text color | "#FFFFFF" |
| Text Y position | -0.75 (bottom of screen) |
| Render indices: video | 0 |
| Render indices: effect | 11005 |
| Render indices: text | 14002 |
| Audio volume | 0.3162277638912201 |

---

## 1. root_meta_info.json

Located at the draft root directory. Contains the list of all drafts.

### Top-Level Structure

```json
{
  "all_draft_store": [Draft, ...],
  "draft_ids": number,
  "root_path": string
}
```

### Draft Entry

```json
{
  "draft_cloud_last_action_download": boolean,
  "draft_cloud_purchase_info": string,
  "draft_cloud_template_id": string,
  "draft_cloud_tutorial_info": string,
  "draft_cloud_videocut_purchase_info": string,
  "draft_cover": string,
  "draft_fold_path": string,
  "draft_id": string,
  "draft_json_file": string,
  "draft_name": string,
  "draft_new_version": string,
  "draft_root_path": string,
  "draft_timeline_materials_size": number,
  "tm_draft_cloud_completed": string,
  "tm_draft_cloud_modified": number,
  "tm_draft_create": number,
  "tm_draft_modified": number,
  "tm_duration": number
}
```

| Field | Type | Description |
|---|---|---|
| `draft_cloud_last_action_download` | boolean | Cloud sync flag |
| `draft_cloud_purchase_info` | string | Cloud purchase info |
| `draft_cloud_template_id` | string | Cloud template ID |
| `draft_cloud_tutorial_info` | string | Cloud tutorial info |
| `draft_cloud_videocut_purchase_info` | string | Cloud video cut purchase info |
| `draft_cover` | string | Path to `draft_cover.jpg` |
| `draft_fold_path` | string | Path to draft folder |
| `draft_id` | string | Uppercase UUID |
| `draft_json_file` | string | Path to `draft_info.json` |
| `draft_name` | string | Display name |
| `draft_new_version` | string | New version string |
| `draft_root_path` | string | Draft root directory path |
| `draft_timeline_materials_size` | number | Timeline materials size in bytes (e.g. 53362227) |
| `tm_draft_cloud_completed` | string | Cloud completed timestamp |
| `tm_draft_cloud_modified` | number | Cloud modified timestamp |
| `tm_draft_create` | number | Creation timestamp (milliseconds) |
| `tm_draft_modified` | number | Last modified timestamp (milliseconds) |
| `tm_duration` | number | Duration in microseconds |

### Management Rules

- `draft_ids` increments when a draft is added, decrements when removed.
- `all_draft_store` is an array; new drafts are prepended (newest first).
- To remove: find by `draft_id`, splice out of the array, decrement `draft_ids`.

---

## 2. draft_info.json

Located inside each draft folder. The core file defining the full timeline.

### Top-Level Structure

```json
{
  "canvas_config": CanvasConfig,
  "color_space": number,
  "config": Config,
  "cover": Cover,
  "create_time": number,
  "duration": number,
  "extra_info": any,
  "fps": number,
  "free_render_index_mode_on": boolean,
  "group_container": any,
  "id": string,
  "keyframe_graph_list": any[],
  "keyframes": Keyframes,
  "last_modified_platform": LastModifiedPlatform,
  "materials": Materials,
  "mutable_config": any,
  "name": string,
  "new_version": string,
  "platform": Platform,
  "relationships": any[],
  "render_index_track_mode_on": boolean,
  "retouch_cover": any,
  "source": string,
  "static_cover_image_path": string,
  "tracks": Track[],
  "update_time": number,
  "version": number
}
```

| Field | Type | Description |
|---|---|---|
| `canvas_config` | CanvasConfig | Canvas dimensions and ratio |
| `color_space` | number | Optional; -1 for cover drafts |
| `config` | Config | Draft configuration |
| `cover` | Cover | Cover metadata |
| `create_time` | number | Optional creation timestamp |
| `duration` | number | Total duration in microseconds (default 40000000) |
| `extra_info` | any | Extra information |
| `fps` | number | Frames per second |
| `free_render_index_mode_on` | boolean | Render index mode flag |
| `group_container` | any | Group container data |
| `id` | string | Uppercase UUID |
| `keyframe_graph_list` | any[] | Keyframe graph list |
| `keyframes` | Keyframes | Keyframe data |
| `last_modified_platform` | LastModifiedPlatform | Platform last modified on |
| `materials` | Materials | All material definitions |
| `mutable_config` | any | Mutable configuration |
| `name` | string | Draft name |
| `new_version` | string | "79.0.0" |
| `platform` | Platform | Platform info |
| `relationships` | any[] | Relationship data |
| `render_index_track_mode_on` | boolean | Render index track mode flag |
| `retouch_cover` | any | Retouch cover data |
| `source` | string | "default" |
| `static_cover_image_path` | string | Static cover image path |
| `tracks` | Track[] | Timeline tracks |
| `update_time` | number | Update timestamp |
| `version` | number | 360000 |

### CanvasConfig

```json
{
  "height": number,
  "ratio": string,
  "width": number
}
```

### Config

```json
{
  "adjust_max_index": number,
  "attachment_info": any[],
  "combination_max_index": number,
  "export_range": any,
  "extract_audio_last_index": number,
  "lyrics_recognition_id": string,
  "lyrics_sync": boolean,
  "lyrics_taskinfo": any[],
  "maintrack_adsorb": boolean,
  "material_save_mode": number,
  "original_sound_last_index": number,
  "record_audio_last_index": number,
  "sticker_max_index": number,
  "subtitle_recognition_id": string,
  "subtitle_sync": boolean,
  "subtitle_taskinfo": any[],
  "system_font_list": any[],
  "video_mute": boolean,
  "zoom_info_params": {
    "offset_x": number,
    "offset_y": number,
    "zoom_ratio": number
  }
}
```

| Field | Default | Description |
|---|---|---|
| `adjust_max_index` | 1 | Adjust max index |
| `combination_max_index` | 1 | Combination max index |
| `extract_audio_last_index` | 1 | Extract audio last index |
| `lyrics_sync` | true | Lyrics sync flag |
| `maintrack_adsorb` | true | Main track adsorb flag |
| `original_sound_last_index` | 1 | Original sound last index |
| `record_audio_last_index` | 1 | Record audio last index |
| `sticker_max_index` | 1 | Sticker max index |
| `subtitle_sync` | true | Subtitle sync flag |
| `zoom_info_params.zoom_ratio` | 1.0 | Zoom ratio |

### Cover

```json
{
  "cover_draft_id": string,
  "cover_template": any,
  "sub_type": string,
  "type": string
}
```

- `type`: `"image"`
- `sub_type`: `"local"`

---

### Materials

The materials object holds all material definitions referenced by track segments.

```json
{
  "audio_balances": [],
  "audio_effects": [],
  "audio_fades": [],
  "audios": Audio[],
  "beats": [],
  "canvases": Canvas[],
  "chromas": [],
  "color_curves": [],
  "digital_humans": [],
  "drafts": MaterialDraft[],
  "effects": VideoEffect[],
  "green_screens": [],
  "handwrites": [],
  "hsl": [],
  "images": [],
  "log_color_wheels": [],
  "manual_deformations": [],
  "masks": [],
  "material_animations": MaterialAnimation[],
  "material_colors": [],
  "placeholders": [],
  "plugin_effects": [],
  "primary_color_wheels": [],
  "realtime_denoises": [],
  "shapes": [],
  "smart_crops": [],
  "sound_channel_mappings": SoundChannelMapping[],
  "speeds": Speed[],
  "texts": Text[],
  "stickers": [],
  "tail_leaders": [],
  "text_templates": [],
  "transitions": Transition[],
  "video_effects": VideoEffect[],
  "video_trackings": [],
  "videos": Video[]
}
```

---

### Track

Each track is a lane on the timeline containing segments of the same type.

```json
{
  "attribute": number,
  "flag": number,
  "id": string,
  "segments": TrackSegment[],
  "type": string
}
```

| Field | Type | Description |
|---|---|---|
| `attribute` | number | Optional track attribute |
| `flag` | number | Segment count |
| `id` | string | Uppercase UUID |
| `segments` | TrackSegment[] | Array of segments in this track |
| `type` | string | `"video"`, `"audio"`, `"text"`, or `"effect"` |

---

### TrackSegment (片段 - the most important structure)

```json
{
  "cartoon": boolean,
  "clip": Clip,
  "common_keyframes": any[],
  "enable_adjust": boolean,
  "enable_color_curves": boolean,
  "enable_color_wheels": boolean,
  "enable_lut": boolean,
  "enable_smart_color_adjust": boolean,
  "extra_material_refs": string[],
  "group_id": string,
  "hdr_settings": HDRSettings,
  "id": string,
  "intensifies_audio": boolean,
  "is_placeholder": boolean,
  "is_tone_modify": boolean,
  "keyframe_refs": any[],
  "last_nonzero_volume": number,
  "material_id": string,
  "render_index": number,
  "reverse": boolean,
  "source_timerange": { "duration": number, "start": number },
  "speed": number,
  "target_timerange": { "duration": number, "start": number },
  "template_id": string,
  "template_scene": string,
  "track_attribute": number,
  "track_render_index": number,
  "uniform_scale": { "on": boolean, "value": number },
  "visible": boolean,
  "volume": number
}
```

| Field | Type | Default | Description |
|---|---|---|---|
| `cartoon` | boolean | false | Cartoon flag |
| `clip` | Clip | -- | Position and transform |
| `common_keyframes` | any[] | [] | Common keyframes |
| `enable_adjust` | boolean | -- | Enable adjust |
| `enable_color_curves` | boolean | true | Enable color curves |
| `enable_color_wheels` | boolean | true | Enable color wheels |
| `enable_lut` | boolean | -- | Enable LUT |
| `enable_smart_color_adjust` | boolean | false | Enable smart color adjust |
| `extra_material_refs` | string[] | -- | References to canvas, speed, animation IDs |
| `group_id` | string | "" | Group ID |
| `hdr_settings` | HDRSettings | -- | Video segments only |
| `id` | string | -- | Uppercase UUID |
| `intensifies_audio` | boolean | false | Intensifies audio flag |
| `is_placeholder` | boolean | false | Placeholder flag |
| `is_tone_modify` | boolean | false | Tone modify flag |
| `keyframe_refs` | any[] | [] | Keyframe references |
| `last_nonzero_volume` | number | 1.0 | Last non-zero volume |
| `material_id` | string | -- | References material in `materials.*[]` |
| `render_index` | number | -- | 0=video, 11005=effect, 14002=text |
| `reverse` | boolean | false | Reverse playback |
| `source_timerange` | object | -- | `{ "duration": number, "start": number }` in microseconds |
| `speed` | number | 1.0 | Playback speed multiplier |
| `target_timerange` | object | -- | `{ "duration": number, "start": number }` in microseconds |
| `template_id` | string | -- | Template ID |
| `template_scene` | string | "default" | Template scene |
| `track_attribute` | number | 0 | Track attribute |
| `track_render_index` | number | 0 | Track render index |
| `uniform_scale` | object | -- | `{ "on": boolean, "value": number }` |
| `visible` | boolean | true | Visibility flag |
| `volume` | number | -- | 1.0 for video/text, 0.316 for audio |

#### Clip (clip - 位置/变换)

```json
{
  "alpha": number,
  "flip": { "horizontal": boolean, "vertical": boolean },
  "rotation": number,
  "scale": { "x": number, "y": number },
  "transform": { "x": number, "y": number }
}
```

Default values by segment type:

| Field | Video segments | Text segments |
|---|---|---|
| `alpha` | 1.0 | 1.0 |
| `flip.horizontal` | false | false |
| `flip.vertical` | false | false |
| `rotation` | 0.0 | 0.0 |
| `scale.x` | 2.0 | 1.0 |
| `scale.y` | 2.0 | 1.0 |
| `transform.x` | 0 | 0 |
| `transform.y` | 0 | -0.75 (bottom of screen) |

#### HDRSettings (HDR设置 - video segments only)

```json
{
  "intensity": 1.0,
  "mode": 1,
  "nits": 1000
}
```

---

### Video Material (视频素材)

```json
{
  "check_flag": number,
  "crop": {
    "lower_left_x": number,
    "lower_left_y": 1.0,
    "lower_right_x": 1.0,
    "lower_right_y": 1.0,
    "upper_left_x": number,
    "upper_left_y": number,
    "upper_right_x": 1.0,
    "upper_right_y": number
  },
  "has_audio": boolean,
  "crop_ratio": string,
  "crop_scale": number,
  "duration": number,
  "height": number,
  "width": number,
  "id": string,
  "material_name": string,
  "matting": {
    "interactiveTime": [],
    "strokes": []
  },
  "path": string,
  "picture_from": string,
  "type": string,
  "video_algorithm": {
    "algorithms": []
  }
}
```

| Field | Type | Default | Description |
|---|---|---|---|
| `check_flag` | number | 63487 | Check flag |
| `crop` | object | -- | Crop coordinates (normalized 0-1) |
| `has_audio` | boolean | false | Whether video has audio track |
| `crop_ratio` | string | "free" | Crop ratio |
| `crop_scale` | number | 1.0 | Crop scale |
| `duration` | number | -- | Duration in microseconds |
| `height` | number | -- | Video height in pixels |
| `width` | number | -- | Video width in pixels |
| `id` | string | -- | Uppercase UUID |
| `material_name` | string | -- | Filename |
| `matting` | object | -- | Matting data |
| `path` | string | -- | File path |
| `picture_from` | string | "none" | Picture source |
| `type` | string | -- | `"photo"` or `"video"` |
| `video_algorithm` | object | -- | Video algorithm data |

---

### Audio Material (音频素材)

```json
{
  "app_id": number,
  "category_id": string,
  "category_name": string,
  "check_flag": number,
  "duration": number,
  "effect_id": string,
  "formula_id": string,
  "id": string,
  "intensifies_path": string,
  "local_material_id": string,
  "music_id": string,
  "name": string,
  "path": string,
  "request_id": string,
  "resource_id": string,
  "source_platform": number,
  "team_id": string,
  "text_id": string,
  "tone_category_id": string,
  "tone_category_name": string,
  "tone_effect_id": string,
  "tone_effect_name": string,
  "tone_speaker": string,
  "tone_type": string,
  "type": string,
  "video_id": string
}
```

| Field | Type | Default | Description |
|---|---|---|---|
| `app_id` | number | 0 | App ID |
| `category_id` | string | -- | Category ID |
| `category_name` | string | -- | Category name |
| `check_flag` | number | 1 | Check flag |
| `duration` | number | -- | Duration in microseconds |
| `effect_id` | string | -- | Effect ID |
| `formula_id` | string | -- | Formula ID |
| `id` | string | -- | Uppercase UUID |
| `intensifies_path` | string | -- | Intensifies path |
| `local_material_id` | string | -- | Local material ID |
| `music_id` | string | -- | Music ID |
| `name` | string | -- | Filename |
| `path` | string | -- | File path |
| `request_id` | string | -- | Request ID |
| `resource_id` | string | -- | Resource ID |
| `source_platform` | number | 0 | Source platform |
| `team_id` | string | -- | Team ID |
| `text_id` | string | -- | Text ID |
| `tone_category_id` | string | -- | Tone category ID |
| `tone_category_name` | string | -- | Tone category name |
| `tone_effect_id` | string | -- | Tone effect ID |
| `tone_effect_name` | string | -- | Tone effect name |
| `tone_speaker` | string | -- | Tone speaker |
| `tone_type` | string | -- | Tone type |
| `type` | string | -- | `"extract_music"` |
| `video_id` | string | -- | Video ID |

---

### Text Material (文本素材)

```json
{
  "add_type": number,
  "alignment": number,
  "background_alpha": 1.0,
  "background_color": "",
  "background_height": 0.14,
  "background_horizontal_offset": 0.0,
  "background_round_radius": 0.0,
  "background_style": 0,
  "background_vertical_offset": 0.0,
  "background_width": 0.14,
  "bold_width": 0.0,
  "border_color": "",
  "border_width": 0.08,
  "check_flag": 7,
  "combo_info": { "text_templates": [] },
  "content": string,
  "fixed_height": -1.0,
  "fixed_width": -1.0,
  "font_category_id": "",
  "font_category_name": "",
  "font_id": "",
  "font_name": "",
  "font_resource_id": "",
  "font_size": 8.0,
  "font_source_platform": 0,
  "font_team_id": "",
  "font_title": "none",
  "font_url": "",
  "fonts": [],
  "force_apply_line_max_width": false,
  "global_alpha": 1.0,
  "group_id": "",
  "has_shadow": false,
  "id": string,
  "initial_scale": 1.0,
  "is_rich_text": false,
  "italic_degree": 0,
  "ktv_color": "",
  "language": "",
  "layer_weight": 1,
  "letter_spacing": 0.0,
  "line_spacing": 0.02,
  "name": "",
  "preset_category": "",
  "preset_category_id": "",
  "preset_has_set_alignment": false,
  "preset_id": "",
  "preset_index": 0,
  "preset_name": "",
  "recognize_type": 0,
  "relevance_segment": [],
  "shadow_alpha": 0.8,
  "shadow_angle": -45.0,
  "shadow_color": "",
  "shadow_distance": 8.0,
  "shadow_point": { "x": 1.0182337649086284, "y": -1.0182337649086284 },
  "shadow_smoothing": 1.0,
  "shape_clip_x": false,
  "shape_clip_y": false,
  "style_name": "",
  "sub_type": 0,
  "text_alpha": 1.0,
  "text_color": "#FFFFFF",
  "text_preset_resource_id": "",
  "text_size": 30,
  "text_to_audio_ids": [],
  "tts_auto_update": false,
  "type": "text",
  "typesetting": 0,
  "underline": false,
  "underline_offset": 0.22,
  "underline_width": 0.05,
  "use_effect_default_color": true,
  "words": {
    "end_time": [],
    "start_time": [],
    "text": []
  }
}
```

| Field | Type | Default | Description |
|---|---|---|---|
| `add_type` | number | 2 | Add type |
| `alignment` | number | 1 | Text alignment (1=center) |
| `background_alpha` | number | 1.0 | Background alpha |
| `background_color` | string | "" | Background color |
| `background_height` | number | 0.14 | Background height |
| `background_horizontal_offset` | number | 0.0 | Background horizontal offset |
| `background_round_radius` | number | 0.0 | Background corner radius |
| `background_style` | number | 0 | Background style |
| `background_vertical_offset` | number | 0.0 | Background vertical offset |
| `background_width` | number | 0.14 | Background width |
| `bold_width` | number | 0.0 | Bold width |
| `border_color` | string | "" | Border color |
| `border_width` | number | 0.08 | Border width |
| `check_flag` | number | 7 | Check flag |
| `combo_info` | object | -- | `{ "text_templates": [] }` |
| `content` | string | -- | Formatted text content, e.g. `"<size=8.0>[text]</size>"` |
| `fixed_height` | number | -1.0 | Fixed height |
| `fixed_width` | number | -1.0 | Fixed width |
| `font_category_id` | string | "" | Font category ID |
| `font_category_name` | string | "" | Font category name |
| `font_id` | string | "" | Font ID |
| `font_name` | string | "" | Font name |
| `font_resource_id` | string | "" | Font resource ID |
| `font_size` | number | 8.0 | Font size |
| `font_source_platform` | number | 0 | Font source platform |
| `font_team_id` | string | "" | Font team ID |
| `font_title` | string | "none" | Font title |
| `font_url` | string | "" | Font URL |
| `fonts` | array | [] | Fonts list |
| `force_apply_line_max_width` | boolean | false | Force line max width |
| `global_alpha` | number | 1.0 | Global alpha |
| `group_id` | string | "" | Group ID |
| `has_shadow` | boolean | false | Shadow enabled |
| `id` | string | -- | Uppercase UUID |
| `initial_scale` | number | 1.0 | Initial scale |
| `is_rich_text` | boolean | false | Rich text flag |
| `italic_degree` | number | 0 | Italic degree |
| `ktv_color` | string | "" | KTV color |
| `language` | string | "" | Language |
| `layer_weight` | number | 1 | Layer weight |
| `letter_spacing` | number | 0.0 | Letter spacing |
| `line_spacing` | number | 0.02 | Line spacing |
| `name` | string | "" | Name |
| `preset_category` | string | "" | Preset category |
| `preset_category_id` | string | "" | Preset category ID |
| `preset_has_set_alignment` | boolean | false | Preset has set alignment |
| `preset_id` | string | "" | Preset ID |
| `preset_index` | number | 0 | Preset index |
| `preset_name` | string | "" | Preset name |
| `recognize_type` | number | 0 | Recognize type |
| `relevance_segment` | array | [] | Relevance segments |
| `shadow_alpha` | number | 0.8 | Shadow alpha |
| `shadow_angle` | number | -45.0 | Shadow angle |
| `shadow_color` | string | "" | Shadow color |
| `shadow_distance` | number | 8.0 | Shadow distance |
| `shadow_point` | object | -- | `{ "x": 1.018..., "y": -1.018... }` |
| `shadow_smoothing` | number | 1.0 | Shadow smoothing |
| `shape_clip_x` | boolean | false | Shape clip X |
| `shape_clip_y` | boolean | false | Shape clip Y |
| `style_name` | string | "" | Style name |
| `sub_type` | number | 0 | Sub type |
| `text_alpha` | number | 1.0 | Text alpha |
| `text_color` | string | "#FFFFFF" | Text color |
| `text_preset_resource_id` | string | "" | Text preset resource ID |
| `text_size` | number | 30 | Text size |
| `text_to_audio_ids` | array | [] | Text to audio IDs |
| `tts_auto_update` | boolean | false | TTS auto update |
| `type` | string | "text" | Type |
| `typesetting` | number | 0 | Typesetting mode |
| `underline` | boolean | false | Underline enabled |
| `underline_offset` | number | 0.22 | Underline offset |
| `underline_width` | number | 0.05 | Underline width |
| `use_effect_default_color` | boolean | true | Use effect default color |
| `words` | object | -- | Word-level timing: `{ "end_time": [], "start_time": [], "text": [] }` |

---

### VideoEffect (视频特效 - effects and video_effects)

```json
{
  "adjust_params": [{ "default_value": number, "name": string, "value": number }],
  "apply_target_type": number,
  "category_id": string,
  "category_name": string,
  "common_keyframes": [],
  "effect_id": string,
  "formula_id": "",
  "id": string,
  "name": string,
  "platform": "all",
  "render_index": 0,
  "request_id": string,
  "resource_id": string,
  "source_platform": 0,
  "track_render_index": 0,
  "type": "video_effect",
  "value": 1.0,
  "version": ""
}
```

| Field | Type | Default | Description |
|---|---|---|---|
| `adjust_params` | array | [] | Adjustable parameters |
| `apply_target_type` | number | 2 | Apply target type |
| `category_id` | string | -- | Category ID |
| `category_name` | string | -- | Category name |
| `common_keyframes` | array | [] | Common keyframes |
| `effect_id` | string | -- | Effect ID |
| `formula_id` | string | "" | Formula ID |
| `id` | string | -- | **MUST regenerate UUID when using presets** |
| `name` | string | -- | Effect name |
| `platform` | string | "all" | Platform |
| `render_index` | number | 0 | Render index |
| `request_id` | string | -- | Request ID |
| `resource_id` | string | -- | Resource ID |
| `source_platform` | number | 0 | Source platform |
| `track_render_index` | number | 0 | Track render index |
| `type` | string | "video_effect" | Type |
| `value` | number | 1.0 | Effect value |
| `version` | string | "" | Version |

---

### Transition (转场)

```json
{
  "category_id": string,
  "category_name": string,
  "duration": number,
  "effect_id": string,
  "id": string,
  "is_overlap": boolean,
  "name": string,
  "platform": "all",
  "request_id": string,
  "resource_id": string,
  "type": "transition"
}
```

| Field | Type | Default | Description |
|---|---|---|---|
| `category_id` | string | -- | Category ID |
| `category_name` | string | -- | Category name |
| `duration` | number | 466666 | Duration in microseconds |
| `effect_id` | string | -- | Effect ID |
| `id` | string | -- | **MUST regenerate UUID when using presets** |
| `is_overlap` | boolean | -- | Whether overlap transition |
| `name` | string | -- | Transition name |
| `platform` | string | "all" | Platform |
| `request_id` | string | -- | Request ID |
| `resource_id` | string | -- | Resource ID |
| `type` | string | "transition" | Type |

---

### Animation (动画 - inside MaterialAnimation)

```json
{
  "category_id": string,
  "category_name": string,
  "duration": number,
  "id": string,
  "material_type": "video",
  "name": string,
  "panel": "video",
  "platform": "all",
  "resource_id": string,
  "start": 0,
  "type": string
}
```

| Field | Type | Default | Description |
|---|---|---|---|
| `category_id` | string | -- | Category ID (`"入场"` or `"出场"`) |
| `category_name` | string | -- | Category name (`"in"` or `"out"`) |
| `duration` | number | 500000 or 466666 | Duration in microseconds |
| `id` | string | -- | Animation ID |
| `material_type` | string | "video" | Material type |
| `name` | string | -- | Animation name |
| `panel` | string | "video" | Panel |
| `platform` | string | "all" | Platform |
| `resource_id` | string | -- | Resource ID |
| `start` | number | 0 | Start time |
| `type` | string | -- | `"in"` or `"out"` |

### MaterialAnimation (素材动画)

```json
{
  "animations": Animation[],
  "id": string,
  "type": "sticker_animation"
}
```

| Field | Type | Description |
|---|---|---|
| `animations` | Animation[] | Array of Animation objects |
| `id` | string | Uppercase UUID |
| `type` | string | "sticker_animation" |

---

### Canvas (画布 - per video segment)

```json
{
  "id": string,
  "type": "canvas_color"
}
```

### Speed (速度 - per video segment)

```json
{
  "id": string,
  "speed": 1.0,
  "type": "speed"
}
```

### SoundChannelMapping (声道映射 - per video segment)

```json
{
  "id": string,
  "type": "none"
}
```

### MaterialDraft (封面草稿 - for cover)

```json
{
  "id": string,
  "draft": {
    "canvas_config": CanvasConfig,
    "color_space": -1,
    "materials": {
      "videos": [
        {
          "id": string,
          "path": string
        }
      ]
    }
  },
  "type": "composition"
}
```

The `path` in `materials.videos` points to `draft_cover.jpg`.

---

## 3. draft_meta_info.json

Located inside each draft folder. Contains metadata for the draft.

```json
{
  "draft_cloud_materials": [],
  "draft_cover": string,
  "draft_fold_path": string,
  "draft_id": string,
  "draft_deeplink_url": "false",
  "draft_materials": [],
  "draft_materials_copied_info": [],
  "draft_name": string,
  "draft_root_path": string,
  "draft_segment_extra_info": [],
  "draft_timeline_materials_size_": number,
  "tm_draft_create": number,
  "tm_draft_modified": number,
  "tm_duration": number
}
```

| Field | Type | Description |
|---|---|---|
| `draft_cloud_materials` | array | Cloud materials |
| `draft_cover` | string | Path to `draft_cover.jpg` |
| `draft_fold_path` | string | Path to draft folder |
| `draft_id` | string | Uppercase UUID |
| `draft_deeplink_url` | string | Deep link URL (string "false") |
| `draft_materials` | array | Draft materials (can be array of DraftMaterial) |
| `draft_materials_copied_info` | array | Copied materials info |
| `draft_name` | string | Display name |
| `draft_root_path` | string | Draft folder path (NOT the root path) |
| `draft_segment_extra_info` | array | Segment extra info |
| `draft_timeline_materials_size_` | number | Timeline materials size (e.g. 53362227) |
| `tm_draft_create` | number | Creation timestamp (milliseconds) |
| `tm_draft_modified` | number | Last modified timestamp (milliseconds) |
| `tm_duration` | number | Duration in microseconds |
