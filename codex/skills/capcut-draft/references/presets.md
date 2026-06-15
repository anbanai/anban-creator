# CapCut Draft Presets Reference (剪映预设参考)

---

## Canvas Presets (画布预设)

| Name | 中文名 | Width | Height | Ratio |
|------|--------|-------|--------|-------|
| Horizontal | 横屏 | 1920 | 1080 | 16:9 |
| Vertical | 竖屏 | 1080 | 1920 | 9:16 |
| Fullscreen | 全屏 | 1920 | 1440 | 4:3 |
| Square | 正方形 | 1920 | 1920 | 1:1 |
| Original | 原始 | 0 | 0 | original |

> **Note:** When using "Original" (原始), set `width` and `height` to the actual media dimensions. A value of `0` means "use the source material size".

---

## Entry Animations (入场动画)

### Common Properties

All entry animations share these fields:

```json
{
  "category_id": "入场",
  "category_name": "in",
  "material_type": "video",
  "panel": "video",
  "platform": "all",
  "start": 0,
  "type": "in",
  "duration": 500000
}
```

### Available Entry Animations

| Name | 中文名 | id | resource_id |
|------|--------|----|-------------|
| Slide Up | 向上滑动 | 624739 | 6798333487523828238 |
| Slide Down | 向下滑动 | 624735 | 6798333705401143816 |
| Slide Left | 向左滑动 | 624747 | 6798332871267324423 |
| Slide Right | 向右滑动 | 624743 | 6798333076469453320 |

### Full JSON Example: Slide Up (向上滑动)

```json
{
  "category_id": "入场",
  "category_name": "in",
  "duration": 500000,
  "id": "624739",
  "material_type": "video",
  "name": "向上滑动",
  "panel": "video",
  "platform": "all",
  "resource_id": "6798333487523828238",
  "start": 0,
  "type": "in"
}
```

---

## Exit Animations (出场动画)

### Common Properties

All exit animations share these fields:

```json
{
  "category_id": "出场",
  "category_name": "out",
  "material_type": "video",
  "panel": "video",
  "platform": "all",
  "start": 0,
  "type": "out"
}
```

### Available Exit Animations

| Name | 中文名 | id | resource_id | duration |
|------|--------|----|-------------|----------|
| Fade Out | 渐隐 | 624707 | 6798320902548230669 | 466666 |
| Slide Down | 向下滑动 | 624735 | 6798333705401143816 | 500000 |
| Slide Left | 向左滑动 | 624747 | 6798332871267324423 | 500000 |
| Slide Right | 向右滑动 | 624743 | 6798333076469453320 | 500000 |
| Shrink | 缩小 | 624755 | 6798332584276267527 | 500000 |
| Enlarge | 放大 | 624751 | 6798332733694153230 | 500000 |
| Fade In | 渐显 | 624705 | 6798320778182922760 | 500000 |

> **Note:** "Fade Out" (渐隐) has `duration: 466666`. All other exit animations have `duration: 500000`.

---

## Video Effects (视频特效)

### Common Properties

All video effects share these fields:

```json
{
  "apply_target_type": 2,
  "common_keyframes": [],
  "formula_id": "",
  "platform": "all",
  "render_index": 0,
  "request_id": "20230813075814C18E8A692D95AF98747C",
  "source_platform": 0,
  "track_render_index": 0,
  "type": "video_effect",
  "value": 1.0,
  "version": ""
}
```

> **IMPORTANT:** The `id` field in presets is a fixed UUID. When creating a new effect instance, you **MUST** generate a new uppercase UUID for `id`. Only reuse `effect_id`, `resource_id`, and `adjust_params` from the presets below.

### 1. Monochrome Fill / 单色填充

| Field | Value |
|-------|-------|
| effect_id | `"3956309"` |
| resource_id | `"7128329164314120717"` |
| category | 复古 |
| category_id | `7731` |

**adjust_params:**

| Key | Value |
|-----|-------|
| effects_adjust_range | 0.76 |
| effects_adjust_color | 0.36 |
| effects_adjust_filter | 0.5 |
| effects_adjust_intensity | 1.0 |

### 2. Pulse Beat / 脉搏跳动

| Field | Value |
|-------|-------|
| effect_id | `"1522814"` |
| resource_id | `"7052226294972420621"` |
| category | 动感 |
| category_id | `7730` |

**adjust_params:**

| Key | Value |
|-----|-------|
| effects_adjust_luminance | 0.8 |
| effects_adjust_intensity | 0.9 |
| effects_adjust_filter | 1.0 |
| effects_adjust_range | 0.7 |

### 3. Bounce Swing / 回弹摇摆

| Field | Value |
|-------|-------|
| effect_id | `"4720539"` |
| resource_id | `"7146090225855369742"` |
| category | 扭曲 |
| category_id | `39539` |

**adjust_params:**

| Key | Value |
|-----|-------|
| effects_adjust_speed | 0.3 |
| effects_adjust_size | 0.5 |
| effects_adjust_intensity | 0.0 |

### 4. Fluorescent Scan / 荧光扫描

| Field | Value |
|-------|-------|
| effect_id | `"1482382"` |
| resource_id | `"7041474808986472967"` |
| category | 潮酷 |
| category_id | `38510` |

**adjust_params:**

| Key | Value |
|-----|-------|
| effects_adjust_color | 0.5 |
| effects_adjust_speed | 0.33 |
| effects_adjust_filter | 0.0 |
| effects_adjust_intensity | 1.0 |

### 5. Gradual Zoom / 渐渐放大

| Field | Value |
|-------|-------|
| effect_id | `"634067"` |
| resource_id | `"6730912024596845063"` |
| category | 综艺 |
| category_id | `27966` |

**adjust_params:**

| Key | Value |
|-----|-------|
| effects_adjust_speed | 0.33 |
| effects_adjust_range | 0.66 |
| effects_adjust_horizontal_shift | 0.5 |
| effects_adjust_vertical_shift | 0.5 |

### 6. Ripple Chromatic / 波纹色差

| Field | Value |
|-------|-------|
| effect_id | `"634285"` |
| resource_id | `"6709347834690277892"` |
| category | 动感 |
| category_id | `7730` |

**adjust_params:**

| Key | Value |
|-----|-------|
| effects_adjust_speed | 0.33 |
| effects_adjust_intensity | 1.0 |
| effects_adjust_horizontal_chromatic | 0.6 |

### 7. Falling Leaves / 落叶

| Field | Value |
|-------|-------|
| effect_id | `"635043"` |
| resource_id | `"6740863535674298888"` |
| category | 自然 |
| category_id | `7734` |

**adjust_params:**

| Key | Value |
|-----|-------|
| effects_adjust_speed | 0.33 |
| effects_adjust_background_animation | 1.0 |

### 8. Fireflies / 萤火

| Field | Value |
|-------|-------|
| effect_id | `"1357502"` |
| resource_id | `"7006265184050221576"` |
| category | 氛围 |
| category_id | `7729` |

**adjust_params:**

| Key | Value |
|-----|-------|
| effects_adjust_speed | 0.33 |
| effects_adjust_background_animation | 1.0 |

---

## Transitions (转场)

### Common Properties

All transitions share these fields:

```json
{
  "duration": 466666,
  "platform": "all",
  "request_id": "20230813073044A16DAD0CF7A7CBFD8ADA",
  "type": "transition"
}
```

> **IMPORTANT:** The `id` field in presets is a fixed UUID. When creating a new transition instance, you **MUST** generate a new uppercase UUID for `id`. Only reuse `effect_id`, `resource_id`, and `is_overlap` from the presets below.

### Available Transitions

| Name | 中文名 | effect_id | resource_id | category | category_id | is_overlap |
|------|--------|-----------|-------------|----------|-------------|------------|
| Flash Black | 闪黑 | 321493 | 6724239388189921806 | 叠化 | 40427 | false |
| Flash White | 闪白 | 322575 | 6724845376098013708 | 叠化 | 40427 | false |
| Overlay | 叠加 | 1003369 | 6914112332205396488 | 叠化 | 40427 | true |
| Slide Left | 向左 | 359529 | 6724227717195108867 | 运镜 | 40428 | false |
| Slide Right | 向右 | 359527 | 6724227599616184836 | 运镜 | 40428 | false |
| Pull Away | 拉远 | 359365 | 6724226338418332167 | 运镜 | 40428 | false |
| Blur | 模糊 | 4212596 | 6911569618171597320 | 模糊 | 40429 | true |
