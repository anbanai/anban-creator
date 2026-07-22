# 封面 Prompt 8 要素模板

## Contents

- [8 要素总览](#8-要素总览)
- [完整 prompt 模板](#完整-prompt-模板)
- [Aspect Ratio](#aspect-ratio)
- [Title Layout](#title-layout)
- [Subject / Person](#subject-person)
- [Background](#background)
- [Color Scheme](#color-scheme)
- [Font Vibe](#font-vibe)
- [Visual Hierarchy](#visual-hierarchy)
- [Negative Constraints](#negative-constraints)
- [填充示例](#填充示例)
- [Aspect Ratio](#aspect-ratio)

本文档定义 Phase 3 步骤 5 构建封面 prompt 时的 8 要素模板。

## 8 要素总览

每个封面 prompt 必须覆盖这 8 个要素，缺一不可。顺序可以按效果调整，但内容必须全部到位。

| # | 要素 | 作用 |
|---|------|------|
| 1 | 画面比例 | 锁定竖版 9:16，避免被生成成横图 |
| 2 | 标题排版 | 决定文字层级和视觉重点 |
| 3 | 人物/主体 | 决定画面核心元素和位置 |
| 4 | 背景 | 决定整体氛围和与主体的关系 |
| 5 | 色彩 | 决定情绪基调（用语义色名，禁 hex） |
| 6 | 字体气质 | 决定风格定位（专业 / 活泼 / 高端） |
| 7 | 主体元素 | 决定主次关系和视觉动线 |
| 8 | 禁止事项 | 反向约束比正向描述更有效 |

## 完整 prompt 模板

```
Generate a viral short-video cover image.

## Aspect Ratio
Vertical 9:16 ratio, portrait orientation. The image must be taller than wide.

## Title Layout
The cover features a Chinese title "<NEW_TITLE>" arranged as follows:
- Line 1: "<line_1_text>" — medium size, <position>
- Line 2: "<line_2_text>" — oversized bold, <position>
- Emphasized words: "<word_1>", "<word_2>" rendered larger than other characters

Note: Chinese character rendering may have minor imperfections — prioritize overall visual impact over pixel-perfect text.

## Subject / Person
<if has_person>
Young <role_description> in <clothing>, positioned <position> (e.g. "center-left", "right side of frame"), <pose> (e.g. "looking at camera", "side profile"), <expression>. The subject occupies <percentage>% of the frame.
</if has_person>

<if no_person>
Main subject: <object_or_graphic_description>, positioned <position>, occupying <percentage>% of the frame.
</if no_person>

## Background
<background_type> background with <atmosphere_description>. <relationship_to_subject> (e.g. "subtle gradient that doesn't compete with the title", "blurred实景 that adds depth without distraction").

## Color Scheme
- Main color: <semantic_color_name with physical analogy, e.g. "deep navy blue like midnight sky, NOT pure black">
- Accent color: <semantic_color_name with analogy, e.g. "warm yellow like honey, NOT lemon yellow">
- Contrast: <high/medium/low>
- Saturation: <high/medium/low>

The main color dominates <percentage>% of the canvas; the accent color is reserved for the title and key visual highlights.

## Font Vibe
Chinese title in <font_type> with <weight> weight. Overall style: <vibe_keywords like "commercial poster", "high-end editorial", "playful internet">. <effects> (e.g. "subtle drop shadow for readability against busy backgrounds", "no decorative outlines").

## Visual Hierarchy
First-glance focus: <title text / subject face / main object>. Secondary focus: <supporting element>. Eye flow: <where viewer looks first → second → third>. Everything else is supporting — must not compete with the primary focus.

## Negative Constraints
DO NOT include:
- English text, pinyin, or non-Chinese characters (Chinese only, but accept minor rendering imperfections)
- Watermarks, logos, signatures, brand marks
- Cluttered or busy elements competing with the main subject
- Multiple unrelated subjects (one focal point only)
- Cartoon/anime/illustration style when photorealism is intended (or vice versa)
- Borders or frames around the image
- Heavy filters that obscure facial features
```

## 填充示例

假设 `cover-plan.md` 决定：
- new_title: "3 步学会爆款标题"
- 标题分行：line_1="3 步学会"，line_2="爆款标题"
- emphasized: "爆款标题"
- 有人像：年轻女生，红色卫衣，居中偏左，看镜头
- 背景：深蓝渐变，简洁
- 主色：深夜蓝，强调色：暖黄
- 字体：黑体加粗，商业海报风
- 视觉重点：标题文字主导

填充后的 prompt（节选）：

```
Generate a viral short-video cover image.

## Aspect Ratio
Vertical 9:16 ratio, portrait orientation.

## Title Layout
The cover features a Chinese title "3 步学会爆款标题" arranged as follows:
- Line 1: "3 步学会" — medium size, top center
- Line 2: "爆款标题" — oversized bold, center
- Emphasized words: "爆款标题" rendered approximately 2x larger than "3 步学会"

## Subject / Person
Young female content creator in red hoodie, positioned center-left, looking directly at camera with confident smile. The subject occupies 50% of the frame.

## Background
Deep navy blue gradient background with subtle geometric pattern. The background stays minimal to let the title and subject dominate.

## Color Scheme
- Main color: deep navy blue like midnight sky, NOT pure black
- Accent color: warm yellow like honey, NOT lemon yellow
- Contrast: high
- Saturation: medium-high

The main color dominates 70% of the canvas; the warm yellow accent is reserved for the title text "爆款标题".

## Font Vibe
Chinese title in bold sans-serif (黑体) with extra-bold weight. Overall style: commercial poster, high contrast, viral content creator aesthetic. Subtle drop shadow on title for readability.

## Visual Hierarchy
First-glance focus: the oversized title text "爆款标题". Secondary focus: the female subject's face. Eye flow: title → face → "3 步学会" subheading. Everything else is supporting.

## Negative Constraints
DO NOT include:
- English text or pinyin (Chinese only, accept minor rendering imperfections)
- Watermarks, logos, signatures
- Cluttered elements competing with the title
- Multiple subjects (one focal person only)
- Cartoon/anime style — must be photorealistic commercial photography
- Borders or frames
```

## 参考深度对模板的影响

### `reference_depth=light`

模板不提"参考"。`ref_image_path` 仍传入但仅作为**风格参考**（色彩倾向、字体气质）。`Subject`、`Background`、`Visual Hierarchy` 完全按新标题的账号领域决定。

模板开头**不加**"Reference cover shows..."语句。

### `reference_depth=deep`

模板开头**加**一段参考声明：

```
Reference cover shows the visual logic to follow:
- Title position: <from reference-analysis.md>
- Subject position: <from reference-analysis.md>
- Color scheme: <main + accent from reference-analysis.md>
- Visual hierarchy: <from reference-analysis.md>

Recreate this composition logic with the new title "<NEW_TITLE>" and the new subject described below. Do not copy decorative elements — borrow the structure, not the expression.
```

`Subject`、`Background` 仍按新标题决定，但**位置**和**视觉动线**对齐参考。

## Prompt 长度控制

模板填满大约 300-450 词。若加上复杂反面约束超过 500 词：

1. **保留**：画面比例、标题排版、人物/主体、色彩、禁止事项
2. **删减**：字体气质合并到标题排版、背景简化为一句、视觉动线删减

绝不删减禁止事项——反面约束是 AI 生图模型最容易遗漏的部分。

## 中文 vs 英文 prompt

- **结构指令**用英文（aspect ratio、layout、composition 等）——模型对英文术语响应更精确
- **标题文案**用中文原文（"3 步学会爆款标题"）——必须原样传入
- **颜色描述**用英文语义色名（"deep navy like midnight sky"）——避免 hex 色值
- **风格关键词**中英都可（"commercial poster 风" / "commercial poster style"）
