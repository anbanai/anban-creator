---
name: flower-content-design
description: Researches flower varieties and generates professional photography-level prompts for flower image generation. Use when creating flower image prompts, researching flower types, or designing floral photography concepts.
---

# 花卉图片设计工具

专业的花卉摄影图片 prompt 工程工具，提供花卉调研指南、摄影级 prompt 模板、风格一致性规则。

---

## A. 花卉调研指南

### 按主题分类的花卉推荐

| 主题 | 推荐花卉 |
|------|----------|
| 春花 | 牡丹、樱花、郁金香、风信子、玉兰、杏花、迎春花 |
| 夏花 | 荷花、绣球花、向日葵、栀子花、紫薇、茉莉、百合 |
| 秋花 | 菊花、桂花、大丽花、波斯菊、秋海棠、金盏花 |
| 冬花 | 梅花、山茶花、水仙、腊梅、君子兰 |
| 婚礼花 | 玫瑰、绣球花、芍药、桔梗、满天星、洋桔梗、白百合 |
| 野花 | 雏菊、矢车菊、薰衣草、波斯菊、虞美人、金鱼草 |
| 热带花 | 天堂鸟、火鹤花、姜荷花、帝王花、热带兰 |
| 小清新 | 勿忘我、铃兰、绣线菊、白晶菊、蓝盆花 |

### 花卉摄影特征

| 花卉 | 花瓣质感 | 颜色特征 | 花蕊结构 | 摄影亮点 |
|------|----------|----------|----------|----------|
| 牡丹 | 丝绸般柔软，层次丰富 | 深红、粉白、黄色渐变 | 金色花蕊簇生 | 花瓣层次的光影变化 |
| 玫瑰 | 天鹅绒般紧实 | 红、粉、白、橙、渐变 | 紧密螺旋状花蕊 | 露珠在花瓣上的折射 |
| 郁金香 | 光滑蜡质 | 纯色或双色条纹 | 黑色花蕊对比 | 杯形轮廓与背光效果 |
| 荷花 | 净洁粉嫩，莲子可见 | 粉白渐变 | 黄色莲蓬 | 水珠在花瓣的滚落 |
| 绣球花 | 纸质轻薄 | 蓝紫渐变、粉色、白色 | 微小花心 | 大量小花的整体体量感 |
| 芍药 | 轻盈柔软，飘逸感强 | 粉红、白、紫红 | 金黄雄蕊 | 开放过程的动态美 |
| 菊花 | 细长管状或扁平舌状 | 黄、白、紫、橙 | 管状花心 | 放射状结构的几何美 |
| 薰衣草 | 细小密集穗状 | 蓝紫色渐变 | 微小无明显花蕊 | 成片紫色的朦胧氛围 |

### 花卉搭配原则

- **颜色对比**：暖色（红、橙、黄）与冷色（紫、蓝）交替出现，避免色彩单调
- **形态互补**：大花（牡丹、玫瑰）与小花（满天星、勿忘我）搭配，丰富画面层次
- **结构多样性**：单瓣花（雏菊）与重瓣花（绣球、牡丹）交替，体现花型多样
- **高度错落**：高茎花（向日葵、百合）与矮花（铃兰、勿忘我）组合，增加空间感

---

## B. Prompt 模板系统

### 核心模板结构

```
A detailed, vertical photograph captures [FLOWER_NAME] in [COMPOSITION_TYPE].
[BLOOM_DESCRIPTION - 盛放花朵形态、颜色层次、花瓣质感].
[BUD_DESCRIPTION - 花蕾/含苞状态].
[FOLIAGE_DESCRIPTION - 叶片茎干质感].
[ATMOSPHERE_DESCRIPTION - 环境氛围：光线质感、空气感、水汽/雨丝/雾气、季节氛围].
[BACKGROUND_DESCRIPTION - 背景虚化、bokeh、纵深].
[COMPOSITION_DESCRIPTION - 花朵空间关系、留白、动感、画面节奏].
The textures are hyper-realistic and vivid. This is a [MOOD] portrait of [FLOWER_NAME].
```

### 各字段填写指南

**[COMPOSITION_TYPE]** 构图类型（替代原排列方式）：

新增灵动构图：
- `a dreamy scattered petal arrangement with soft focus` — 花瓣散落梦幻构图
- `a graceful multi-stem dance, flowers at varying heights and angles` — 多茎错落灵动构图
- `a partial close-up showing petals flowing beyond the frame edge` — 局部特写溢出画面
- `a natural wildflower meadow scene with depth layers` — 野花草地纵深构图
- `an overhead bird's-eye view of loosely arranged blooms` — 俯视松散花朵
- `a single elegant stem swaying with gentle motion blur` — 单茎动感微摇
- `a cascading arrangement tumbling down from upper frame` — 从上方倾泻而下
- `floating blooms on still water surface with reflections` — 花朵漂浮水面倒影

经典构图（仍可使用）：
- `a natural cluster arrangement` — 自然簇生排列
- `an elegant single-stem composition` — 单茎优雅构图
- `a dense bouquet formation` — 密集花束形态
- `a wild field arrangement` — 野地自然排列

**[BLOOM_DESCRIPTION]** 盛放描述示例：
- 牡丹：`The fully bloomed peony displays hundreds of layered petals in deep crimson transitioning to blush pink at the edges, each petal's surface showing a delicate silk-like texture with subtle veining`
- 玫瑰：`The roses show velvety red petals arranged in perfect spiral formations, the outer petals curving gracefully outward while the inner petals remain tightly furled`
- 郁金香：`The tulips display smooth, waxy petals in pure coral-orange, their cup-shaped forms perfectly symmetrical with a subtle translucency when backlit`

**[BUD_DESCRIPTION]** 花蕾描述示例：
- `Nearby buds are tightly closed, their green sepals still clasping the future bloom`
- `Several half-opened buds show a glimpse of the color within, petals just beginning to unfurl`
- `Small round buds in various stages of opening add depth and anticipation to the composition`

**[FOLIAGE_DESCRIPTION]** 叶片茎干描述示例：
- `The deep green leaves have a slightly glossy surface with visible veining, and the thick stems add structural support`
- `Slender emerald stems arch gracefully, their surface texture catching the light`
- `Broad leaves with fine serrated edges frame the blooms, their undersides lighter in tone`

**[ATMOSPHERE_DESCRIPTION]** 环境氛围（核心升级字段）：

光线质感：
- `Soft morning light filters through petals from behind, creating a translucent glow that reveals the delicate vein structure within each petal`
- `Golden hour side-backlight grazes the petal edges, drawing luminous rim light and long soft shadows that accentuate three-dimensional form`
- `Overcast diffused light wraps evenly around each bloom, eliminating harsh shadows and rendering subtle color gradients with full fidelity`
- `Dappled light through overhead leaves casts shifting patches of brightness and shadow across the arrangement`

空气感与微粒：
- `Sunlit air carries visible dust motes and floating pollen particles, creating a luminous atmospheric haze around the blooms`
- `A soft haze of airborne moisture softens distant elements, lending the scene a painterly, dreamlike quality`

水汽与雨丝：
- `Fine rain threads diagonally across the frame, each droplet streak catching backlight, while fresh water films coat every petal surface and refract tiny rainbow spectra`
- `After recent rain, every petal surface holds a thin water film that reflects and refracts surrounding colors; larger drops gather at petal tips and tremble at the edge of falling`
- `Morning mist settles low around the stems, blurring the lower third of the frame into soft white while upper blooms remain sharp and vivid`

露珠效果：
- `Tiny water droplets cling to the petals and leaves, each droplet catching and refracting the morning light into miniature rainbows`
- `Delicate dew beads rest on the velvety petal surfaces, some sliding slowly down the curved edges`
- `Crystal-clear water droplets are scattered across the petals, creating sparkling light points`

季节与温度感：
- `A warm spring breeze sets the petals trembling with faint motion blur at their tips, suggesting life and movement`
- `Summer heat creates a faint shimmer in the air above the blooms, the atmosphere thick with warmth and fragrance`
- `Low autumn sunlight casts long golden shadows, the cool clear air giving the scene crystalline sharpness`
- `Winter cold renders the air perfectly transparent and still; the scene has an icy, jewel-like clarity`

**[BACKGROUND_DESCRIPTION]** 背景描述示例：
- `The background dissolves into soft green and warm gold bokeh circles, creating a dreamy, out-of-focus garden atmosphere`
- `Creamy white bokeh merges seamlessly with the light background, emphasizing the flower's delicate colors`
- `Deep emerald bokeh creates a lush, garden-fresh backdrop that makes the flowers pop`
- `Layered depth transitions from sharp foreground stems through mid-ground bokeh blooms to completely dissolved background tones`

**[COMPOSITION_DESCRIPTION]** 构图描述示例：
- `The main bloom occupies the lower-left third of the frame, leaving generous negative space in the upper-right that breathes air into the composition`
- `Flowers cascade from the upper frame downward in a diagonal rhythm, creating visual flow and movement`
- `A foreground bloom is sharply rendered while mid-ground and background flowers dissolve progressively, building depth through multiple focal planes`
- `Stems lean at opposing angles, creating dynamic tension; petals brush the right frame edge, implying continuation beyond the image boundary`
- `The arrangement follows a loose S-curve through the vertical frame, guiding the eye from top to bottom in a natural reading rhythm`

### 完整 Prompt 示例

**示例 1：雨后散落花瓣（环境氛围 + 散落构图）**：
```
A detailed, vertical photograph captures a field of poppies in a dreamy scattered petal arrangement with soft focus. The fully opened poppies display tissue-thin petals in vivid scarlet with ink-black basal markings, their crinkled surfaces catching light from multiple angles. Nearby buds lean on slender arching stems, their hairy green sepals splitting to reveal the crimson within. Wiry dark stems support the blooms at varying heights, their fine surface hairs visible in the sharp foreground. After recent rain, every petal surface holds a thin water film that reflects surrounding colors; larger drops gather at petal edges and tremble at the edge of falling, while fine rain threads still cross the background, each streak catching backlight. The background dissolves into warm amber and soft olive bokeh circles, the rainy atmosphere adding atmospheric depth. The main blooms scatter across the frame with generous negative space between each flower; petals brush the left frame edge, implying a wider field beyond; the composition breathes with open air between stems. The textures are hyper-realistic and vivid. This is a wild, windswept portrait of the scarlet poppy.
```

**示例 2：晨雾中的多茎错落（雾气 + 多茎构图）**：
```
A detailed, vertical photograph captures irises in a graceful multi-stem dance, flowers at varying heights and angles. The upper blooms display royal purple falls with intricate veining and golden beards, their ruffled edges catching the diffused morning light; lower blooms show earlier stages of opening, petals still curved inward. Half-opened buds sheathed in papery green spathes promise further blooms above the opened flowers. Sword-shaped blue-green leaves fan outward in geometric arcs, their surface carrying a waxy sheen. Morning mist settles low around the stems, blurring the lower third of the frame into soft white while upper blooms remain sharp and vivid; sunlit air carries visible pollen particles that create a luminous haze between stems. The background transitions from sharp middle-ground foliage into complete dissolution of color and form, reinforcing the misty atmosphere. Stems lean at varied angles with flowers at three distinct heights, creating a layered vertical rhythm; the upper bloom tilts toward the upper-left corner while lower blooms anchor the base, forming a dynamic diagonal spine through the composition. The textures are hyper-realistic and vivid. This is an ethereal, misty portrait of the bearded iris.
```

**示例 3：逆光局部特写（光线穿透 + 溢出画面构图）**：
```
A detailed, vertical photograph captures a magnolia in a partial close-up showing petals flowing beyond the frame edge. The central bloom fills most of the frame, its large ivory tepals backlit to translucency, delicate veining illuminated from within like stained glass; subtle cream-to-blush gradients play across the surface. A single half-opened bud in the upper corner shows tightly clasped tepals, their outer surfaces flushed with deeper rose pink. Short thick stems carry a slightly rough brownish-grey bark texture with visible lenticels. Golden hour side-backlight grazes every tepal edge, drawing luminous rim light and rendering the entire flower as a glowing, luminous form; the warm low sun creates long feathered shadows on inner tepal surfaces. The background is almost entirely dissolved into warm cream and pale gold bokeh, with the suggestion of further blossoms completely out of focus. The main bloom is cropped by all four frame edges, creating radical intimacy; the composition centers on the luminous interior while outer petals flow off-frame, suggesting infinite scale; generous negative space in the upper-right breathes air against the dense bloom. The textures are hyper-realistic and vivid. This is an intimate, radiant portrait of the magnolia blossom.
```

---

## C. 风格一致性规则

### 统一风格元素清单

为保持一组花卉图片的视觉一致性，以下元素应在所有图片中保持一致：

| 元素 | 统一策略 |
|------|----------|
| **主环境氛围** | 统一同一批次的主氛围（如全部"雨后"、全部"晨雾"、全部"黄金时刻逆光"），每张细节表现可不同 |
| **光线色温** | 统一冷暖倾向（如"暖色调，3200-4000K"） |
| **背景色调** | 统一 bokeh 颜色范围（如"暖绿色和金色背景"） |
| **景深程度** | 统一背景虚化程度（浅景深或中景深） |
| **构图比例** | 花朵占据画面 30-60%，留白 40-70%，留白增加空灵感 |
| **构图多样性** | 同一批次中，每张分配不同构图类型，避免重复 |

### 构图类型分配策略

根据批次数量自动分配构图类型，避免重复：

- **1-3 张**：全景/散落 + 中景/多茎错落 + 特写/局部溢出
- **4-6 张**：在上述基础上增加：俯视鸟瞰、动感微摇、倾泻而下等变体
- **7+ 张**：每种主要构图类型至少出现一次，循环复用时优先选择与前一张视觉差异最大的

### $STYLE 风格描述模板

```
hyper-realistic vertical flower photography, atmospheric haze, light particles,
soft directional light with rim highlights, varied composition with negative space,
shallow to medium depth of field, rich color gradients, 9:16 portrait format
```

根据用户需求调整的变体：
- **雨后清新**：`post-rain atmosphere, water films on petals, rain streak bokeh, fresh cool light`
- **晨雾空灵**：`morning mist, low fog layers, diffused soft light, pollen particles in air`
- **黄金时刻**：`golden hour backlight, rim-lit petal edges, long warm shadows, luminous glow`
- **清新小清新**：`pastel tones, soft diffused light, light pink and white bokeh, delicate watercolor-like quality`
- **复古胶片**：`film grain, warm amber tones, vintage color grading, slightly desaturated`
- **暗调奢华**：`dark moody background, dramatic chiaroscuro lighting, deep jewel tones, velvety shadows`
- **明亮通透**：`bright airy atmosphere, pure white background, high key lighting, clean and fresh`

### 参考图一致性流程

```
第1张（首图）: 不使用参考图，使用完整 $STYLE 描述确立基准风格
    ↓ 调用 generate_image 生成结果: flower_01_[name].png
第2张起: 使用第1张作为参考图（ref），传递风格基准
    ↓ 调用 generate_image，传入 ref 参数
后续图片: 继续使用第1张（基准图）作为参考图
```

**注意**：每张花卉图的 prompt 不同（不同花卉种类），因此不能使用 `generate_batch_images` 批量模式（该模式适用于同一 prompt 的多张变体）。每张独立调用 `generate_image`，通过 ref 参数保持风格一致。

---

## D. 工具参考

通过 MCP 工具逐张调用 `generate_image`：

1. **第1张（首图，无参考图）**：在 prompt 中使用完整 $STYLE 描述确立基准风格
2. **第2张起（以首图为参考）**：在 prompt 中包含风格描述，传入第1张图片路径作为 ref
3. **使用用户提供的参考图（所有图片）**：传入用户提供的参考图路径作为 ref

**参数说明**：
- 在 prompt 中指定 `9:16 portrait format` 竖版 9:16 比例
- 在 prompt 中包含全局风格描述（色调、光线、氛围）
- ref 参数传入参考图路径，用于保持多张图片的视觉风格一致性
- 输出文件命名规范：`flower_01_peony.png`、`flower_02_rose.png`（序号_花名）
