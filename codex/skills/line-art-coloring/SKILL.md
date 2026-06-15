---
name: line-art-coloring
description: Use when coloring line art images, batch coloring multiple images, preserving visual consistency across characters, or when user mentions "线稿上色", "上色", "填色", "coloring", "color consistency", "批量上色", "角色上色", "给线稿上色".
---

# 线稿上色——跨图颜色一致性保障

## MCP 工具

| MCP 工具 | 说明 |
|----------|------|
| `analyze_image` (channel_id, image_url, file_path, prompt) | 图像视觉分析——传入图像 URL 或服务器文件路径，返回 AI 视觉分析结果。analyze_image 一次只分析一张图片；同时传 `file_path` 和 `image_url` 时服务端只会使用 `file_path`。用于实体识别、候选评估、一致性审计、线稿验证 |
| `generate_image` (channel_id, prompt, image_type, output_path, ref_image_path, size, task_id) | 生成单张图片，返回 download_url 和 file_path。当前不是专用的 `colorize_lineart`，也不是严格的 img2img/ControlNet 上色 |
| `upload_image` (channel_id, file_path) | 上传图片 |
| `compress_image` (file_path) | 压缩图片 |
| `download_image` (channel_id, url) | 下载在线图片到 MCP 服务器临时路径或上传到存储，不写入 agent 本地 `$DIR` |

---

## 当前能力边界

当前可用的 MCP 能力以 `generate_image` 为主：它会根据 prompt 和参考图生成一张新图，**不是专用的 `colorize_lineart` 工具，也不是严格的 img2img/ControlNet 上色**。因此本 skill 的输出是“尽力保持线稿”的参考图生成流程，不能承诺 100% 保留原始线稿、构图、比例和所有细节。

必须向用户透明记录这些限制：
- `generate_image` 可能重绘线条、改变人物姿态、增删画面元素或改变宽高比。
- `ref_image_path` 只能提高参考一致性，不能锁定线稿。
- `size` 是宽高比提示，不是像素级裁切或硬约束；从原始线稿推断最接近的支持比例后仍要验证返回结果。
- 在专用 img2img/colorize_lineart 工具可用前，收敛修正和回溯统一都是重新生成，不是“只改颜色不动线条”。严重线稿差异标记为 `needs_img2img`。

如用户要求“原线稿 100% 不变，只填色”，必须先说明当前能力无法严格保证；只有接入专用 img2img/colorize_lineart 能力后才能承诺。

---

## 核心原则

### 原则 0：线稿神圣不可侵犯（最高优先级）

**目标是上色只添加颜色，不修改线稿。** 当前工具只能尽力保持线稿；每根线条、每个笔触、每处构图都必须被审计，但不能承诺 100% 像素级一致。

- 不可修改线条粗细、曲率、位置
- 不可模糊、锐化或重绘线条
- 不可增加或删除线条元素
- 不可改变构图、比例或布局

所有 prompt 必须包含固定语：
```
CRITICAL: PRESERVE the exact line art composition. Every line, stroke, and proportion must remain 100% identical to the original. Do NOT modify, blur, redraw, add, or remove any lines. Only add color.
```

### 原则 1：一致性 > 效率

同一实体在所有图中应尽量颜色一致；但在当前 `generate_image` 非专用上色能力下，不要用大量候选弥补模型能力缺口。默认单候选模式；只有用户明确要求更高质量或预算/耗时允许时才启用 2 候选。

### 原则 2：使用 `analyze_image` 分析图像

**Read 工具不用于图像视觉分析。** 在本环境中 Read 上传图像到 CDN 并返回 URL，不提供视觉内容。所有需要"看"图像的场景必须使用 `analyze_image`。

**analyze_image 一次只分析一张图片。** 调用时二选一传 `image_url` 或 `file_path`；同时传 `file_path` 和 `image_url` 时服务端只会使用 `file_path`，不会做双图对比。线稿保持审计必须先为原始线稿生成线稿指纹，再分析上色图，将上色图审计结果与线稿指纹逐项比对。

---

## 图像视觉分析方法

### 分析流程

1. **获取图像可访问路径**：
   - MCP 服务器端文件路径：直接传 `file_path` 参数
   - 已有 CDN URL：传 `image_url` 参数
   - Read 返回 CDN URL 的场景：先 Read 获取 URL，再用 `image_url` 参数传入
2. **调用 analyze_image**：`analyze_image(channel_id="$CHANNEL_ID", image_url=URL或file_path=路径, prompt=分析提示)`
3. **处理结果**：根据返回的文本描述进行实体匹配、颜色评估等

> **注意**：Read 返回的 CDN URL 约 30 分钟过期。获取后立即使用；需要重新分析时重新 Read 获取新 URL。
>
> **大小限制**：`file_path` 方式分析有 10MB 限制。若 `analyze_image(file_path=...)` 返回图片过大，先调用 `compress_image(file_path=...)` 得到较小文件；仍失败时调用 `upload_image(channel_id, file_path)` 获取 URL，再用 `image_url` 重试。

### 各场景的 analyze_image prompt 模板

**实体识别**（步骤 3）：
```
描述图中所有实体：角色（位置、姿态、朝向、体型比例、发型轮廓、服装类型、配饰、与其他角色空间关系）、物体（位置、大小、材质）、环境元素（整体色调方向）。对每个实体提供足够外观描述用于跨图匹配。
```

**候选颜色评估**（步骤 7）：
```
逐实体逐部位描述颜色。对图中每个角色/物体，列出所有可见部位并描述每个部位的颜色。格式：
- [实体名]: [部位1]=[颜色描述], [部位2]=[颜色描述], ...
```

**一致性审计**（步骤 8）：
```
逐实体逐部位描述颜色，与以下 Color Bible 规格比对并标注 PASS/MINOR/FAIL：
[Color Bible 内容]

对每个实体的每个部位：
- PASS: 颜色与定义一致
- MINOR: 色调正确但有轻微饱和度/明度偏差
- FAIL: 色调错误
```

**线稿验证**（每张上色图生成后）：

先为原始线稿生成线稿指纹：
```
只描述这张原始线稿的可验证线稿指纹，不评论颜色。包括：画面宽高方向、主体数量、主体位置、姿态、轮廓关键线、服装/道具/背景线条、构图边界、容易被重绘或丢失的小线条。
```

再审计上色图：
```
只描述这张上色图的线条和构图状态，不评论颜色。按原始线稿指纹逐项检查：画面宽高方向、主体数量、主体位置、姿态、轮廓关键线、服装/道具/背景线条、构图边界、小线条是否存在。输出 PASS/MINOR/FAIL，并列出任何线条重绘、模糊、构图偏移、比例变化或元素增删。
```

将上色图审计结果与线稿指纹逐项比对；不能确认时标记 `needs_img2img`。

---

## 核心机制

### 1. 渐进式 Color Bible

Color Bible 不在开始时一次性建完，而是逐图渐进构建：
- 处理第 1 张图时建立初始 Color Bible
- 处理第 N 张图时：匹配已有实体（复用颜色）+ 发现新实体（定义新颜色加入）
- 这避免了全局规划的遗漏问题

颜色定义方法论详见 [references/color-bible.md](references/color-bible.md)。

### 2. 语义颜色锚定

**不用 hex 色值**——AI 模型经常忽略 "#FF5733" 这种写法。

改用三层颜色描述：
- **语义色名**："bright cherry red, like a fire truck"
- **实物类比**："hair like dark chocolate, not milk chocolate"
- **反面约束**："must NOT be blonde or light brown, it must be very dark brown, almost black"

### 3. 单候选优先 + 可选最优选

默认每张线稿生成 **1 个候选**，减少成本和长任务失败率。以下情况才生成 2 个候选：
- 用户明确要求质量优先
- 第一候选颜色明显失败但线稿保持尚可
- 跨图关键实体需要更稳定的颜色版本

2 候选流程：
- 候选 A 和 B 使用不同的 prompt 措辞（描述同一颜色但换说法）
- 用 `analyze_image` 逐实体逐部位比对 Color Bible
- 选匹配度最高的作为正式结果
- 如果两个都 < 70% → 生成候选 C

注意：在 `generate_image` 不是专用线稿上色的前提下，多候选只能提高整体风格和颜色命中率，不能保证线稿一致。

### 4. Per-Entity Best Reference 追踪

维护映射表 `$DIR/best-refs.md`：
```
## Entity: Girl with red hood
- best_ref: colored_00.png
- quality: hair=perfect, skin=perfect, hood=perfect
- images: colored_00, colored_02, colored_05

## Entity: Big bad wolf
- best_ref: colored_02.png
- quality: fur=perfect, eyes=perfect
- images: colored_00, colored_02
```

每完成一张上色图就更新：如果新图中某实体颜色比当前 best_ref 更好，更新 best_ref。

### 5. 收敛修正循环

全部上完后审计 → 修正 → 再审计 → 再修正，最多 3 轮，直到全部 PASS。

当前能力边界下，收敛修正是重新生成整张图。如果问题是线稿改变、构图偏移或仅需局部换色，则标记 `needs_img2img`；不要反复生成并声称“只修颜色”。

### 6. 回溯统一

如果修正后某实体的 best_ref 变了（后面的图颜色更好），回头重新上色前面的图。

当前能力边界下，回溯统一同样可能改变线稿。仅在颜色一致性收益大于线稿风险时执行；否则记录 `needs_img2img`。

---

## 完整工作流

### Phase 0 — 初始化

#### 步骤 1：获取频道和工作目录

- `echo $ANBANWRITER_DEFAULT_CHANNEL` → `$CHANNEL_ID`
- 如果为空，调用 `list_channels` 获取频道列表并选择；只有一个可用频道时自动使用，多个频道且无法从任务上下文判断时停止并提示配置 `ANBANWRITER_DEFAULT_CHANNEL`
- 从 `.task-context` 获取 `$TASK_ID`，或使用 CWD 目录名
- 尝试调用 `prepare_workspace(content_type="design", task_id=$TASK_ID)` → `$DIR`
  - prepare_workspace 返回的 path 可能是相对路径；相对路径以当前任务工作区 `$CWD` 为根，例如返回 `output` 时使用 `$CWD/output`
  - 如果 `prepare_workspace` 调用失败，使用 `$CWD/output/` 作为 `$DIR`
- `mkdir -p "$DIR"`

#### 步骤 2：确认输入线稿

- 收集用户提供的线稿图路径列表
- 处理 TIF、TIFF、BMP 等非标准格式：先转换成 PNG，再进入分析/生成流程。macOS 可用 `sips -s format png "$IN" --out "$OUT.png"`；若安装了 ImageMagick，可用 `magick "$IN" "$OUT.png"`。保留原始文件路径和转换后 PNG 路径到 manifest。
- Read 每张 PNG/JPG/WebP/GIF 图验证存在且可读取。Read 对 TIF 常会失败，不要把 TIF 直接传给 Read。
- 如果用户未指定顺序：
  - 对每张线稿调用 `analyze_image`，参数 `prompt="识别图中所有角色/实体的数量、类型（人物/动物/物体）、位置、构图复杂度。列出每个实体的简要描述。"`
  - 按角色数量 × 构图简洁度降序排列
- 写入 `$DIR/input-manifest.md`：

```markdown
# Input Manifest

## Processing Order

| # | File | Reason |
|---|------|--------|
| 0 | /path/to/lineart_01.png | 3 characters, simple composition → anchor |
| 1 | /path/to/lineart_03.png | 2 characters, shares Girl with #0 |
| 2 | /path/to/lineart_02.png | 1 character, complex pose |
```

---

### Phase 1 — 渐进式上色循环

对 `input-manifest.md` 中的每张线稿（按顺序）执行步骤 3-7：

#### 步骤 3：读取线稿，识别实体

Read 当前线稿图获取 CDN URL → 调用 `analyze_image(channel_id="$CHANNEL_ID", image_url=CDN_URL, prompt=实体识别prompt)` → 识别所有实体：

- **角色类**：人物、动物、拟人角色
  - 描述：位置、姿态、朝向、大小、服装特征、配饰、与其他角色的空间关系
  - 识别依据：外观特征（发型、服装轮廓、体型比例）、上下文线索
- **物体类**：关键道具、标志性物品
  - 描述：位置、大小、材质暗示
- **环境类**：场景背景、氛围元素
  - 描述：整体色调方向（温暖/冷调/中性）

关键原则：**识别的目的是匹配**——描述要足够详细，以便与后续图中的同一实体匹配。

#### 步骤 4：实体匹配与 Color Bible 更新

将识别到的实体与 `$DIR/color-bible.md` 中已有实体逐一匹配：

**匹配方法**（详见 [references/color-bible.md](references/color-bible.md)）：
- 基于外观特征描述：体型、发型轮廓、服装类型、配饰
- 基于上下文线索：角色在场景中的位置、与其他角色的关系
- 基于语义线索：故事中的角色功能（主角、对手、配角）

**已知实体**：
- 从 Color Bible 读取颜色规格
- 读取 `$DIR/best-refs.md` 确定该实体的最佳参考图路径

**新实体**：
- 为该实体定义颜色规格（语义色名 + 实物类比 + 反面约束）
- 颜色选择原则：
  - 角色性格匹配（活泼角色用暖色、沉稳角色用冷色）
  - 场景氛围匹配（户外场景用自然色、室内场景用柔和色）
  - 跨实体区分（不同角色的颜色应有足够区分度，避免混淆）
  - 与已有实体颜色的关系（互补/和谐/对比）
- 追加到 `$DIR/color-bible.md`

写入/更新 `$DIR/color-bible.md`。

#### 步骤 5：构建上色 Prompt

构建包含以下要素的 prompt（颜色描述使用英文，因为 image generation 模型对英文颜色术语响应更精确；其余指令可用中文）。Prompt 控制在 500 词以内，避免长 prompt 触发 504 Gateway Timeout；超过时删减到关键实体、关键颜色和 1-2 个最重要反面约束。

> **提醒**：颜色描述使用语义色名 + 实物类比 + 反面约束，**绝对不用 hex 色值**。详见下方"语义色名参考"表。

```
Color this line art illustration.

CRITICAL: PRESERVE the exact line art composition. Every line, stroke, and
proportion must remain 100% identical to the original. Do NOT modify, blur,
redraw, add, or remove any lines. Only add color.

COLOR SPECIFICATIONS (must match exactly):

[Known entities — match the reference image]:
- [Entity A]: [element] is [语义色名, e.g. "deep dark chocolate brown, NOT light brown"],
  wearing [garment] in [语义色名, e.g. "bright cherry red, like a fire truck"],
  [element] in [语义色名]
  CONSTRAINT: [Entity A]'s [element] must NOT be [常见错误色]

[New entities — use these colors]:
- [Entity B]: [element] [语义色名], wearing [garment] in [语义色名]

COLOR RELATIONSHIPS:
- [Entity A]'s [element] is the same color as [Entity B]'s [element]
```

**Prompt 要点**：
- 已知实体：强调与参考图一致 + 反面约束
- 新实体：完整定义颜色 + 实物类比
- 跨实体颜色关系明确写出
- **线稿保持固定语**必须包含
- 不使用 hex 色值
- 当前文生图/参考图生成模型更适合更简单直接的颜色指令，例如 "blue jacket"、"red scarf"；复杂语义色名只用于关键实体，不要堆叠多层反面约束。
- 记录实际 prompt 到 `$DIR/image-prompts.md`，便于复盘 504、色偏和线稿偏移。

#### 步骤 6：生成候选

**参考图像路径解析**：

`generate_image` 的 `ref_image_path` 参数需要服务器可访问的路径。路径解析规则：

1. **之前由 generate_image 生成的图像**：使用返回的 `file_path`（服务器端路径）作 ref_image_path
   ```
   # 第一次生成
   result_a = generate_image(..., output_path="/tmp/anbanwriter-line-art/$TASK_ID/colored_00_a.png")
   server_path_a = result_a.file_path  # 服务器端路径

   # 后续使用该图作参考时
   generate_image(..., ref_image_path=server_path_a)
   ```
2. **用户提供的本地线稿图**（仅用于需要以线稿本身作为参考图的场景）：先 Read 获取 CDN URL，再调用 `download_image(channel_id, CDN_URL)` 让服务器下载注册，返回的 `file_path` 作为 `ref_image_path`
3. **无参考图**（纯 prompt 驱动）：不传 ref_image_path 参数

确定参考图：
- 有已知实体 → `ref_image_path = 包含当前图实体最多且 best_ref 最好的那张`（使用其服务器端 file_path）
- 全部新实体 → 无参考图（纯 prompt）
- 多个已知实体但各自 best_ref 不同 → 选包含实体最多的那张

`output_path` 是 MCP 服务器端路径，不是客户端当前目录路径。为避免权限错误，生成候选时固定使用 `/tmp/anbanwriter-line-art/$TASK_ID/colored_NN_a.png` 这类路径，并把返回的服务器端 `file_path` 写入 `$DIR/server-paths.md`。不要把 `workspace/colored_NN.png` 当作服务器端可写路径。

`size` 是宽高比提示。从原始线稿推断最接近的支持比例（如 7:5 接近 `3:2` 或 `4:3`），传入 `size="3:2"` / `"4:3"`；返回后用文件尺寸或 `analyze_image` 检查是否被裁切、变形或转为竖图。

生成候选 A：
```
result_a = generate_image(
  channel_id="$CHANNEL_ID",
  prompt="[主 prompt]",
  image_type="content",
  output_path="/tmp/anbanwriter-line-art/$TASK_ID/colored_NN_a.png",
  size="[从原始线稿推断的比例，如 3:2]"
)
SERVER_PATH_A = result_a.file_path
```

如需高质量模式，生成候选 B（微调 prompt 措辞）：
- 换一种方式描述同一颜色（如 "crimson red" → "deep blood red"）
- 或换一种实体描述顺序
- 或在 prompt 开头增加更强的一致性声明
- 使用与候选 A 相同的参考来源独立生成，不把候选 A 当作候选 B 的参考，避免放大 A 的错误

```
result_b = generate_image(
  channel_id="$CHANNEL_ID",
  prompt="[微调后 prompt]",
  image_type="content",
  output_path="/tmp/anbanwriter-line-art/$TASK_ID/colored_NN_b.png",
  size="[同候选 A]",
  ref_image_path=BEST_REF_SERVER_PATH  # 可为空；与候选 A 使用同一参考来源
)
SERVER_PATH_B = result_b.file_path
```

#### 步骤 7：候选评估 + 最优选

1. 调用 `analyze_image(channel_id="$CHANNEL_ID", file_path=SERVER_PATH_A, prompt=候选颜色评估prompt)` → 获取候选 A 的颜色描述。如果因 10MB 限制失败，先 `compress_image`；仍失败则 `upload_image` 后用 `image_url` 分析。
2. 高质量模式下，同样分析 SERVER_PATH_B。
3. 对每个候选，逐实体逐部位比对 Color Bible → 计算匹配分，同时记录线稿/构图差异。
4. 选择颜色得分最高且线稿风险最低的候选。
5. 将选中候选的服务器端 `file_path` 写入 `$DIR/server-paths.md`。不能把 `download_image` 当作写入 `$DIR/colored_NN.png` 的本地归档步骤；`download_image` 只返回服务器端临时 `file_path` 或上传 URL。如需要本地归档，用 shell 下载 `download_url` 到 `$DIR/colored_NN.png`（或在 `download_url` 是 data URL 时解码写入该文件）。
6. 如果两个候选都 < 70% 匹配：
   - 生成候选 C（换参考图或加强 prompt 约束）
   - 选三者中最好的
7. 调用 `analyze_image` 验证线稿完整性：先为原始线稿生成线稿指纹，再审计上色图，并将上色图审计结果与线稿指纹逐项比对。
8. 更新 `$DIR/best-refs.md`：如果新图中某实体颜色比当前 best_ref 更好，更新
9. 删除未选中的候选文件

**产出**：`$DIR/colored_NN.png`

---

### Phase 2 — 全量一致性审计

验证与修正方法论详见 [references/verification.md](references/verification.md)。

#### 步骤 8：全面审计

对每张 `$DIR/colored_NN.png`：调用 `analyze_image(channel_id="$CHANNEL_ID", file_path=服务器端路径, prompt=一致性审计prompt)` → 对 Color Bible 中每个跨图实体逐部位比对。

生成 `$DIR/consistency-report.md`：

```markdown
# Consistency Report

## Entity: Girl with red hood

| Image | Hair | Skin | Hood | Dress | Overall |
|-------|------|------|------|-------|---------|
| colored_00 | ✅ dark chocolate | ✅ warm beige | ✅ cherry red | ✅ navy blue | PASS |
| colored_02 | ✅ dark chocolate | ✅ warm beige | ⚠️ slightly darker red | ✅ navy blue | MINOR |
| colored_05 | ✅ dark chocolate | ✅ warm beige | ❌ appears orange | ✅ navy blue | FAIL |

## Summary
- PASS: 5 entities across 12 appearances
- MINOR: 2 entities across 3 appearances
- FAIL: 1 entity across 1 appearance
```

---

### Phase 3 — 收敛修正循环（最多 3 轮）

#### 步骤 9：修正轮次

专用 img2img/colorize_lineart 工具可用前，收敛修正和回溯统一只能 best-effort 执行。遇到“颜色只需局部微调，但重新生成会破坏线稿”的情况，直接在 `consistency-report.md` 标记为 `needs_img2img`，不要继续消耗候选。

**每轮修正**：

**9a. FAIL 级修正**（用最佳参考图重新生成）：

对每个 FAIL 实体，构建修正 prompt：
```
CORRECTION PASS for color inconsistency.
The reference image shows the CORRECT color scheme for [Entity].

CRITICAL LINE PRESERVATION: Every line, stroke, and proportion must remain
100% identical to the original line art. Do NOT modify, blur, redraw, add,
or remove any lines. Only change the COLOR of [Entity], nothing else.

SPECIFIC ISSUES TO FIX:
- [Entity]'s [element] should be [语义色名] (currently appears [错误色描述])
- [Entity]'s [element] should be [语义色名] (currently appears [错误色描述])

Use the reference image's colors EXACTLY. The result must be visually
indistinguishable from the reference in terms of [Entity]'s colors.
```

- 用户明确要求质量优先时生成 2 个候选选最优；默认生成 1 个候选
- `ref_image_path = 该实体当前 best_ref 的服务器端路径`
- 更新 best-refs.md

**9b. MINOR 级修正**（增加反面约束）：

在原 prompt 基础上增加反面约束：
```
IMPORTANT COLOR CORRECTION:
- [Entity]'s [element] must be [语义色名], NOT [当前错误方向]
- The reference shows the correct shade — match it exactly

CRITICAL: PRESERVE the exact line art composition. Every line must remain
100% identical. Only change the color.
```

生成 1 个候选即可。

**9c. 重新审计**：
- 更新 consistency-report.md
- 判断：
  - 全部 PASS → 跳出循环，进入 Phase 4
  - FAIL 数减少 → 继续下一轮
  - 无改善 → 停止循环，剩余 FAIL 标记 `needs_manual_review`

---

### Phase 4 — 回溯统一

#### 步骤 10：回溯检查

检查 Phase 3 中是否有实体的 best_ref 发生了变化：

- 如果某实体原来的 best_ref 是 `colored_00.png`，修正后变成了 `colored_05.png`
- 那么包含该实体的其他图（如 `colored_00.png`、`colored_02.png`）中，该实体的颜色可能不再与新的 best_ref 一致
- 需要：用新的 best_ref 作参考，重新上色这些图

回溯修正同样遵循单候选优先；只有质量优先模式才 2 候选选最优。若回溯会明显改变线稿，标记 `needs_img2img`。

回溯修正后重新审计，确认一致性。

---

### Phase 5 — 归档报告

#### 步骤 11：最终报告

向用户交付结果：

```
线稿上色完成

总图数: 8
修正轮次: 2
最终一致性: PASS / MINOR / needs_img2img 汇总

Color Bible 实体数: 5（3 角色 + 2 物体）
一致性报告: $DIR/consistency-report.md
能力边界: 当前使用 generate_image best-effort 参考图生成，未使用专用 img2img/colorize_lineart

成果文件:
- colored_00.png ~ colored_07.png

人工复核: 无
```

如果有人工复核项：
```
需要人工复核:
- colored_05.png: [Entity] 的 [element] 经 3 轮修正仍偏差
  建议: 手动指定该部位颜色后重新运行修正步骤
```

---

## Prompt 构建技巧

### 语义色名参考

不用 hex，用 AI 模型能准确理解的色名：

| 色系 | 好的描述 | 差的描述 |
|------|---------|---------|
| 红色 | bright cherry red, like a fire truck | #FF0000 |
| 深红 | deep crimson, like dried blood | dark red |
| 蓝色 | bright sky blue on a clear day | #0000FF |
| 深蓝 | dark navy blue, like a midnight suit | #000080 |
| 绿色 | fresh grass green, like spring lawn | #00FF00 |
| 棕色 | dark chocolate brown, not milk chocolate | brown |
| 金色 | warm golden, like honey in sunlight | #FFD700 |
| 粉色 | soft rose pink, like cherry blossoms | pink |
| 紫色 | rich plum purple, like ripe grapes | #800080 |
| 黑色 | deep jet black, like polished obsidian | black |
| 白色 | pure clean white, like fresh snow | white |
| 灰色 | cool slate gray, like overcast sky | gray |

关键：给主要实体附带实物类比——"like a fire truck"、"like dark chocolate"——这给模型一个具体的视觉锚点。次要实体使用更简单直接的颜色指令即可。

### 反面约束模板

```
[Entity]'s [element] must NOT be:
- [常见错误色 1] (too light / too dark / wrong hue)
- [常见错误色 2] (common AI generation mistake)
It must be [正确色名], [实物类比]
```

### 跨实体颜色关系

如果两个实体共享某种颜色，明确写出：
```
COLOR RELATIONSHIPS:
- Girl's hood is the SAME bright cherry red as the picnic blanket
- Wolf's eyes are the SAME amber gold as the sunset
```

这帮助模型理解颜色必须跨实体一致。

---

## 常见失败与修复

| 问题 | 原因 | 修复 |
|------|------|------|
| 颜色不跟随参考图 | 模型随机性过大 | 简化 prompt；必要时 2 候选选最优；仍失败则标记人工复核 |
| 角色色偏（如头发偏亮） | 模型对浅色有偏好 | 增加反面约束："must NOT be blonde" |
| 背景色渗入角色 | 模型无法分离前景/背景 | prompt 明确分离："character colors must NOT be influenced by background" |
| 多角色图中某角色颜色错误 | 多实体增加复杂度 | 单独指定每个实体的反面约束 |
| 实体匹配错误 | 不同角色外观相似 | 增加 more specific 描述（位置、配饰、体型差异） |
| 新实体颜色与已有实体冲突 | 颜色区分度不够 | 选色时确保跨实体区分度 |
| 线条被修改或重绘 | 当前 generate_image 不是严格上色工具 | 强化 prompt 后仍失败则标记 `needs_img2img`，不要承诺 100% 保留 |
| CDN URL 过期 | Read 返回的 CDN URL 约 30 分钟后过期 | 获取后立即使用；需要重新分析时重新 Read 获取新 URL |
| analyze_image 文件过大 | `file_path` 方式分析有 10MB 限制 | 先 `compress_image`，再失败则 `upload_image` 后用 `image_url` |
| output_path 权限错误 | `output_path` 是 MCP 服务器端路径 | 使用 `/tmp/anbanwriter-line-art/$TASK_ID/...` |
| 本地归档缺失 | 把 `download_image` 误当成本地保存工具 | 下载 `download_url` 到 `$DIR/colored_NN.png`，`download_image` 仅用于服务器端注册/中转 |
| 长 prompt 504 Gateway Timeout | prompt 过长或约束过多 | Prompt 控制在 500 词以内，优先关键实体和关键颜色 |
| ref_image_path 无法访问 | 远程 MCP Server 无法访问本地文件路径 | 使用 generate_image 返回的 file_path（服务器端路径），或通过 download_image 中转 |

---

## 验证清单（每张图完成后）

- [ ] 所有已识别实体都有颜色规格
- [ ] Color Bible 已更新
- [ ] 候选评估完成，选中匹配度最高的
- [ ] best-refs.md 已更新
- [ ] 正式上色图 `$DIR/colored_NN.png` 存在
- [ ] 线稿完整性已通过 analyze_image 审计；失败时已标记 `needs_img2img`

## 验证清单（全部完成后）

- [ ] 所有上色图存在
- [ ] consistency-report.md 已生成
- [ ] 收敛修正完成（全部 PASS 或达最大轮次）
- [ ] 回溯统一完成（如需要）
- [ ] 无 FAIL 项，或已标记人工复核 / `needs_img2img`
- [ ] 线稿完整性在所有图中已审计
- [ ] 最终报告已交付
