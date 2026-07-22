# 公众号内容配图设计规范（新 schema + vision 校验）

## Contents

- [核心变化（相对旧版）](#核心变化相对旧版)
- [参考链机制](#参考链机制)
- [反同质化规则](#反同质化规则)
- [image-plan.md 升级 schema](#image-planmd-升级-schema)
  - [旧 schema（已废弃）](#旧-schema已废弃)
- [img_01 章节：{chapter title}](#img01-章节chapter-title)
  - [新 schema（强制）](#新-schema强制)
- [img_01 章节：{chapter title}](#img01-章节chapter-title)
- [字段填写规范](#字段填写规范)
  - [`visual_brief`（最关键）](#visualbrief最关键)
  - [`required_entities`](#requiredentities)
  - [`must_match_excerpts`](#mustmatchexcerpts)
- [完整 image-plan.md 示例](#完整-image-planmd-示例)

## 核心变化（相对旧版）

1. **新增 visual_brief**：1-2 句白话"这张图必须画什么"，替代抽象的 visual_subject
2. **新增 required_entities**：必须出现的具体物体列表（vision 校验依据）
3. **新增 must_match_excerpts**：章节原文锚点，确保实体不脱离章节
4. **强制 vision 校验**：每张图生成后必须用 `analyze_image` 校验，不通过则锐化 prompt 重试

---

## 参考链机制

内容配图的参考链由 `article_image_mode` 决定：

```
cover_and_content → 封面图先生成 → 内容配图使用 ref_image_path="$DIR/cover.png"
content_only → 不生成封面 → 内容配图不传 ref_image_path，或链到首张已生成图
```

**为什么优先用封面**：封面开启时，如果每张图引用上一张，风格漂移会累积放大。封面是风格锚点，确保所有配图保持一致的视觉基准。封面关闭时，严禁把 `ref_image_path` 指向不存在的 `$DIR/cover.png`。

**注意**：`ref_image_path` 只传递"风格语言"。内容贴切度由 prompt 中的 `visual_brief` 和 `required_entities` 决定，并由 vision 校验把关。

## 反同质化约束

内容配图必须服务章节信息，而不是复刻封面主体。`ref_image_path` 只传递"风格语言"，不得复刻封面主体、封面构图或封面隐喻。

- 连续 3 张内容图不得使用同一主体、同一构图或同一色块重心。
- Prompt 必须显式写明：ref_image_path 只传递"风格语言"，不得复刻封面主体。
- 每张图都要写出与章节原文绑定的 `visual_brief`、`required_entities` 和 `anti_generic_constraints`。
- 若 vision 或人工复核认为画面同质化，先改章节实体和构图策略，不要只替换风格形容词。

---

## 反同质化规则

正文配图要像同一套品牌系统，但不能像同一张图的变体。`ref_image_path` 只传递"风格语言"，不得复刻封面主体、构图或核心物件；每张正文图仍由章节自己的 `visual_brief` 和 `required_entities` 决定。

规划 `image-plan.md` 时执行以下检查：

- 同一批正文图不得连续 3 张使用相同主体类型（如全是茶盏/药材/山水/手部特写）。
- 不得连续 3 张使用相同 `composition_type`、相同远近景和相同色调重心，`listicle` 模板的构图统一例外，但主体必须不同。
- 封面主视觉只作风格锚点，不得在正文图中重复作为主要画面；如果封面是莲花，正文图不能连续用莲花或近似荷塘当章节图。
- 中医/养生题材尤其避免"通用养生水墨背景"：每张图必须有能对应章节论点的具体物体、动作或对比关系。

---

## image-plan.md 升级 schema

### 旧 schema（已废弃）

```markdown
## img_01 章节：{chapter title}
- visual_subject: "商务场景"     ← 太抽象，不知道画什么
- source_excerpt: "..."           ← 有，但没约束视觉实体
```

### 新 schema（强制）

```markdown
## img_01 章节：{chapter title}

# 基础字段（沿用旧版）
- chapter_title: {章节标题}
- core_point: {章节核心论点，1 句话}
- composition_type: {8 种构图类型之一}
- source_excerpt: {章节原文摘录}

# 新字段（强制）
- visual_brief: 1-2 句白话描述"这张图必须画什么"
- required_entities: 必须出现的具体物体列表（vision 校验依据）
- must_match_excerpts: 章节中支撑这些实体的原句（防止凭空编造）

# 衍生字段
- prompt_strategy: 把 visual_brief + required_entities + composition_type + 风格语言组合成最终 prompt 的策略
```

---

## 字段填写规范

### `visual_brief`（最关键）

**定义**：1-2 句白话，描述读者看到这张图时应该看到的具体画面。写完问自己："如果让一个陌生人按这句话画图，他能画出来吗？" 答不上来就是不合格。

**好的例子**：
- ✅ "一颗石头路上的裂缝中钻出嫩绿新芽，背景是虚化的晨光。"
- ✅ "一只手握着手摇磨豆器，磨豆器下方接着玻璃瓶，桌面是深色木头。"
- ✅ "空旷的房间里一把空椅子，墙上挂着一幅字，午后阳光从窗户斜射进来。"

**坏的例子**：
- ❌ "自然场景"（太空泛，画什么都行）
- ❌ "商务氛围"（没说画什么）
- ❌ "科技感"（没具体物体）

### `required_entities`

**定义**：列出 vision 模型能在图中识别的具体物体。这些是 vision 校验的"必中清单"——校验时如果任何一个没出现，就判定为不合格。

**好的例子**：
```yaml
required_entities:
  - "stone path with visible crack"
  - "tender green shoots emerging from crack"
  - "soft blurred morning light in background"
```

**坏的例子**：
```yaml
required_entities:
  - "美感"           # 主观，无法识别
  - "氛围"           # 太抽象
  - "高质量摄影"     # 不是物体
```

### `must_match_excerpts`

**定义**：章节中支撑 `required_entities` 的原句。防止 agent 凭空编造视觉元素。

**好的例子**：
```yaml
must_match_excerpts:
  - "他说，'你看这条石板路的缝里，不也长出了新芽？'"
  - "晨光透过窗棂，斜斜地落在那道裂缝上。"
```

**坏的例子**：
```yaml
must_match_excerpts:
  - "本章讨论自然修复"   # 这是论点，不是视觉锚点
```

---

## 完整 image-plan.md 示例

```markdown
# 图片内容规划

## 元信息

- 所选模板: long-form-essay
- 视觉风格: warm natural photography, soft morning light
- 色彩基调: warm earth tones with sage green and gold
- 情绪氛围: serene and meditative
- 计划配图数: 5（hero + 4 section_opener）

---

## img_00 封面（hero slot）

- slot_id: hero
- visual_brief: 一朵晨光中缓缓绽放的莲花，花瓣上有露珠，背景是雾气未散的池塘。
- required_entities:
  - "single lotus flower in mid-bloom"
  - "water droplets on petals"
  - "misty pond background"
  - "soft golden morning light"
- must_match_excerpts:
  - "文章开篇：'真正的力量，像一朵莲花——慢，但不曾停下。'"
- composition_type: 留白主导
- prompt_strategy: 主体居左，右侧大面积留白，建立"静"的基调

---

## img_01 章节：身体的智慧

- slot_id: section_opener
- section_index: 1
- chapter_title: 身体的智慧
- core_point: 身体有自己的节奏，强行加速只会破坏自我修复机制
- composition_type: 三分法
- source_excerpt: "他说，'你看这条石板路的缝里，不也长出了新芽？'"

# 新字段
- visual_brief: 一颗石头路上的裂缝中钻出嫩绿新芽，背景是虚化的晨光。
- required_entities:
  - "stone path with visible crack"
  - "tender green shoots emerging from crack"
  - "soft blurred morning light in background"
- must_match_excerpts:
  - "他说，'你看这条石板路的缝里，不也长出了新芽？'"
  - "晨光透过窗棂，斜斜地落在那道裂缝上。"
- prompt_strategy: 主体在右三分线交点，裂缝横贯画面，新芽向上突破，强化"自我修复"的隐喻
```

---

## 8 种构图类型（沿用）

| # | 构图类型 | 适用场景 | 视觉特征 |
|---|----------|----------|----------|
| 1 | **中心聚焦** | 核心概念、重要观点 | 单一主体居中，视觉冲击力强 |
| 2 | **对角线流动** | 变化、过程、流动感 | 元素沿对角线分布，动态感 |
| 3 | **三分法** | 自然平衡、通用场景 | 主体位于三分线交点，和谐稳定 |
| 4 | **前景/背景** | 层次、上下文、环境 | 前后景分层，纵深感和空间感 |
| 5 | **俯拍** | 结构、细节、展示 | 鸟瞰或平铺视角，秩序感 |
| 6 | **特写** | 质感、情感、细节强调 | 微距聚焦纹理/图案，亲密感 |
| 7 | **留白主导** | 沉思、极简、意境 | 大面积负空间，主体精简，呼吸感 |
| 8 | **重复图案** | 节奏、重复、规律 | 图案重复排列，韵律感和秩序感 |

### 视觉多样性规则

- 3 张以上配图时，必须使用 3 种以上不同构图类型
- **例外**：`listicle` 模板要求所有 section_opener 使用同一种构图（强化"清单"感），不受此规则约束
- 不得出现连续 3 张视觉元素、构图、色调高度相似的配图；命中时回到 image-plan.md 重分配主体、远近景或色彩重心

---

## 内容配图 Prompt 构建

基于 image-plan.md 的字段构建最终 prompt：

```
{VISUAL_STYLE}. {COLOR_PALETTE}.
{VISUAL_BRIEF}.

MUST CONTAIN:
{REQUIRED_ENTITIES 逐行列出}

{COMPOSITION_TYPE_DESCRIPTION}. {MOOD_MATCHING_CHAPTER}.
{IMAGE_SIZE_HINT}, photographic quality.
```

### 示例

基于上面的 img_01：

```
Warm natural photography, soft golden hour light. A tender green shoot emerging from a crack in a weathered stone path, symbolizing natural healing and resilience.

MUST CONTAIN:
- stone path with visible crack
- tender green shoots emerging from crack
- soft blurred morning light in background

Rule of thirds composition, shoots at the right intersection. Warm earth tones with fresh green. 16:9 horizontal, photographic quality.
```

---

## Vision 校验循环

### 步骤 1：生成图片

```
generate_image(
  project_id=$PROJECT_ID,
  prompt=<上面的 prompt>,
  image_type="content",
  output_path="$DIR/img_01.png",
  task_id=$TASK_ID,
  ref_image_path="$DIR/cover.png",
  verify_with_vision=true,
  verification_prompt=<步骤 2 的校验 prompt>
)
```

### 步骤 2：构建校验 prompt

```
这张图用于文章《$ARTICLE_TITLE》的章节《$CHAPTER_TITLE》。
章节核心论点：$CORE_POINT
视觉简报：$VISUAL_BRIEF
必须出现的视觉元素：
$REQUIRED_ENTITIES（逐行列出）

请按 JSON 格式回答，不要包含其他文字：
{
  "all_entities_present": true/false,
  "missing_entities": ["...", "..."],
  "relevance_score": "high" | "medium" | "low",
  "has_forbidden_content": true/false,
  "forbidden_notes": "文字/水印/低俗等问题，如无则空字符串",
  "overall_pass": true/false,
  "sharper_prompt_hint": "如不通过，给出更具体的 prompt 建议"
}
```

### 步骤 3：解析校验结果

`generate_image(verify_with_vision=true)` 的返回值含 `verification` 对象，由服务端解析 vision 模型的 JSON 回答后归一化为以下字段（**agent 必须按这个 schema 读，而不是 LLM 的原始 JSON 字段**）：

```json
{
  "passed": false,
  "score": "medium",
  "missing_entities": ["tender green shoots emerging from crack"],
  "notes": "forbidden_notes: ; sharper_prompt_hint: A single bright green shoot, 3-4 cm tall...",
  "raw": "<vision 模型原始输出，调试用>"
}
```

字段说明：
- `passed`（bool）：是否通过。服务端综合 `overall_pass` 与 `all_entities_present` 判定；若 `has_forbidden_content=true` 强制为 false。
- `score`（"high"/"medium"/"low"/"unknown"）：与章节内容的相关度。
- `missing_entities`（string[]）：缺失的具体实体。
- `notes`（string）：组合了 `forbidden_notes` + `sharper_prompt_hint`，重试时直接用这里的 hint。
- `raw`（string）：vision 模型原始输出，仅供调试。

**判定通过条件**：`passed == true`（或等价地 `score == "high"` 且无 missing_entities）。

### 步骤 4：失败重试

- **第一次失败**：根据 `sharper_prompt_hint` 重写 prompt，加 "MUST CONTAIN" 强调
- **第二次失败**：进一步锐化（加入材质、颜色、方位、数量等具体描述）
- **第三次仍失败**：标记 `quality_status=failed`，记录所有尝试，继续后续 slot

### 步骤 5：锐化 prompt 的具体技巧

| 问题 | 锐化技巧 |
|------|----------|
| 实体缺失 | 在 prompt 开头加 "MUST CONTAIN: " + 实体名，加材质/颜色/数量 |
| 实体错误（出现了不该出现的） | 加 "NO <错误实体>" 排除
| 构图错误 | 加具体方位描述（"主体居左三分线"） |
| 风格漂移 | 加强 ref_image_path 描述（"match the warm earth tone of the reference image"） |
| 主体不突出 | 加 "MAIN SUBJECT: " 前缀，明确唯一焦点 |

---

## 备用方案：单独调用 analyze_image

如果 `generate_image` 的 `verify_with_vision` 参数不可用（旧版 server），agent 可单独调用 `analyze_image`：

```
analyze_image(
  project_id=$PROJECT_ID,
  file_path=$DIR/img_01.png,
  prompt=<步骤 2 的校验 prompt>
)
```

返回的 `analysis` 字段是 vision 模型的文本输出。vision 模型**可能不严格按 JSON 格式回答**（会包裹在 markdown 代码块里、混入解释文字等）；agent 解析时必须容错：

1. 优先抽取 `{"...JSON..."}` 子串（在第一对 `{` 和最后一对 `}` 之间）。
2. 剥离 ```json ... ``` 围栏。
3. 仍解析失败时，按文本关键词降级判定：包含 "all entities present" 或 "全部实体出现" → `passed=true`；包含 "missing" 或 "缺失" → `passed=false`，并把后续名词短语填入 `missing_entities`。

`score` 字段在降级解析时填 `"unknown"`。把降级结果写入 `images.json.verification` 时，`raw` 字段保留 vision 模型原始文本以便事后追溯。

---

## images.json 审计模板

```json
[
  {
    "index": 1,
    "slot_id": "section_opener",
    "section_index": 1,
    "image_type": "content",
    "chapter_title": "身体的智慧",
    "composition_type": "三分法",
    "visual_brief": "一颗石头路上的裂缝中钻出嫩绿新芽，背景是虚化的晨光。",
    "required_entities": [
      "stone path with visible crack",
      "tender green shoots emerging from crack",
      "soft blurred morning light in background"
    ],
    "must_match_excerpts": [
      "他说，'你看这条石板路的缝里，不也长出了新芽？'"
    ],
    "prompt": "Final prompt used",
    "verification": {
      "passed": true,
      "score": "high",
      "missing_entities": [],
      "notes": "",
      "raw": "<vision 模型原始输出>"
    },
    "verification_audit": {
      "attempt_count": 2,
      "sharper_prompt_history": [
        "第一次 prompt（失败）：'Warm natural photography...'",
        "第二次 prompt（通过）：'MUST CONTAIN tender green shoot...'"
      ]
    },
    "ref_image_path": "$DIR/cover.png",
    "file_path": "$DIR/img_01.png",
    "url": "https://cdn.example.com/img_01.png",
    "quality_status": "passed"
  }
]
```

字段说明：
- `verification`：**直接来自服务端 `generate_image(verify_with_vision=true)` 返回值**。不要修改字段名；服务端 schema 是 `{passed, score, missing_entities, notes, raw}`。
- `verification_audit`：**agent 维护**的重试元数据。`attempt_count` ≥ 1，`sharper_prompt_history` 记录每次失败的 prompt（用于事后追溯）。

### 质量状态

- `passed`：`verification.passed == true`（所有 required_entities 出现 + score=high + 无禁止内容）
- `retry_needed`：校验失败但仍在重试中
- `failed`：3 次重试仍不通过（`verification.passed == false`），必须继续后续 slot，并在最终报告中标注
- `skipped`：模板规则下该 slot 不需要图（如 footer 仅 module 无图）

### 审计规则

- 每条必须有 `slot_id` + `section_index`，与 `visual-rhythm-plan.md` 对应
- `verification_audit.attempt_count` 必须 ≥ 1（即至少校验过一次）
- `verification.passed == false` 的图片，最终报告中必须列出
- `ref_image_path` 必须等于 `$DIR/cover.png`
- `prompt_source_excerpt` 已升级为 `must_match_excerpts`（list，允许多条）

---

## 常见失败模式与修复

| 模式 | 现象 | 修复 |
|------|------|------|
| Vision 持续误判实体缺失 | 图片确实有但模型说没有 | 检查实体描述是否具体（"green shoots" vs "3cm tall bright green plant"），用更可识别的描述 |
| Prompt 过载 | 一次塞太多元素 | 砍掉非必要元素，只保留 3-5 个最关键 required_entities |
| 风格被 ref 拉偏 | 所有图都长得像封面 | 加强 prompt 中的主体描述权重，开头说 "MAIN SUBJECT: <具体物体>" |
| 正文图复刻封面主体 | ref_image_path 被误当成内容来源 | 明确写入"ref_image_path 只传递\"风格语言\"，不得复刻封面主体"，并把章节 required_entities 放在 prompt 开头 |
| 连续 3 张同质化 | 主体/构图/色调重心重复 | 回到 image-plan.md 重分配主体类型、composition_type 或远近景 |
| 构图不符合 composition_type | 指定三分法但生成居中 | 加具体方位词："subject placed at the right-third intersection, NOT centered" |
| 重试 3 次仍失败 | 通常 prompt 本身有问题 | 回到 image-plan.md 重新审视 visual_brief 是否合理 |
