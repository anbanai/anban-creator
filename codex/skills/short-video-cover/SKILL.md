---
name: short-video-cover
description: Use when replicating viral short-video covers, generating a short-video cover from a reference cover image, or when user mentions "短视频封面", "爆款封面", "封面复刻", "复刻封面", "cover replication", "B站封面", "抖音封面", "视频号封面", "小红书视频封面". Triggers whenever a user provides a reference cover image and asks for a new short-video cover based on it — even if they don't explicitly say "复刻". Covers the 9:16 vertical cover replication workflow: analyze reference cover's visual logic → migrate to user's new title → generate cover prompt → quality optimization.
---

# 短视频爆款封面——参考封面复刻工作流

## MCP 工具

| MCP 工具 | 说明 |
|----------|------|
| `analyze_image` (channel_id, image_url, file_path, prompt) | 图像视觉分析——传入图像 URL 或服务器文件路径，返回 AI 视觉分析结果。**Read 工具不用于图像视觉分析**，只用来获取 CDN URL。一次只分析一张图；同时传 `file_path` 和 `image_url` 时服务端只用 `file_path` |
| `generate_image` (channel_id, prompt, image_type, output_path, ref_image_path, size, task_id) | 生成单张图片，返回 `download_url` 和 `file_path`。当前是参考图生成，不是 ControlNet/img2img |
| `download_image` (channel_id, url) | 下载在线图片到 MCP 服务器临时路径或上传到存储，返回 `file_path`。用于把 Read 得到的 CDN URL 注册成 `ref_image_path` 可用的服务器端路径 |
| `compress_image` (file_path) | 压缩图片——`analyze_image` 的 `file_path` 方式有 10MB 限制，超出时先压缩 |
| `upload_image` (channel_id, file_path) | 上传图片，用于 `compress_image` 仍超 10MB 的兜底场景 |

---

## 当前能力边界（必须向用户透明记录）

- `generate_image` 是**参考图生成**，不是 ControlNet 或专用封面排版工具。参考图能提高视觉一致性，**不能锁定构图、字号、文字位置**。
- 中文文字在图片内的渲染**不稳定**——AI 生图模型对中文文字支持差。封面以**视觉冲击为主**，关键文字应作为辅助而非主体；若用户需要精确文字排版，建议生成图后用 PS/Canva 二次加工。
- `size="9:16"` 是宽高比提示，不是像素级硬约束；返回后需用文件尺寸或 `analyze_image` 验证比例。
- 在专用封面排版工具接入前，**二次优化**是重新生成，不是"只改局部"。

如用户要求"标题文字必须精确显示为指定中文"，必须先说明当前能力无法严格保证。

---

## 核心原则

### 原则 0：不抄构图，学视觉逻辑（最高优先级）

参考封面的作用是**拆解其画面结构**——标题位置、主体位置、配色方案、字体气质、视觉重点——而不是像素级复制。**抄构图等于抄表达，会触发原创性风险**；学视觉逻辑等于学方法，可复用到任何新标题。

判断"轻度参考 / 深度参考"的差异：
- **轻度参考**（`reference_depth=light`）：只参考色彩倾向和标题层级思路，构图、主体位置、字体气质全部重做
- **深度参考**（`reference_depth=deep`）：参考整体构图、主体位置、字体气质，但替换具体视觉元素和文字内容

### 原则 1：先用 analyze_image 看图

**Read 工具不用于图像视觉分析**——在本环境中 Read 上传图像到 CDN 并返回 URL，不提供视觉内容。所有需要"看"图像的场景必须使用 `analyze_image`。

### 原则 2：Prompt 8 要素缺一不可

封面 prompt 必须包含 8 个要素（顺序可调）：①画面比例 ②标题排版 ③人物/主体 ④背景 ⑤色彩 ⑥字体气质 ⑦主体元素 ⑧禁止事项。缺要素会直接导致生成结果不稳定。详见 [references/prompt-template.md](references/prompt-template.md)。

### 原则 3：9:16 竖版硬约束

短视频封面默认 **9:16 竖版**，与公众号图文封面 2.35:1、种草笔记封面 3:4 严格区分。除非用户明确指定其他比例，`size` 参数固定传 `"9:16"`。

---

## 完整工作流

### Phase 0 — 初始化

#### 步骤 1：获取频道和工作目录

- `echo $ANBANWRITER_DEFAULT_CHANNEL` → `$CHANNEL_ID`
- 如果为空，调用 `list_channels` 获取频道列表；只有一个可用频道时自动使用，多个频道且无法从任务上下文判断时停止并提示配置 `ANBANWRITER_DEFAULT_CHANNEL`
- 从 `.task-context` 获取 `$TASK_ID`，或使用 CWD 目录名
- 尝试调用 `prepare_workspace(content_type="short-video", task_id=$TASK_ID)` → `$DIR`
  - prepare_workspace 返回的 path 可能是相对路径；相对路径以当前任务工作区 `$CWD` 为根，例如返回 `output` 时使用 `$CWD/output`
  - 如果 `prepare_workspace` 调用失败，使用 `$CWD/output/` 作为 `$DIR`
- `mkdir -p "$DIR"`

#### 步骤 2：收集用户输入

需要用户提供以下信息（缺失时停止并询问，不要凭空发挥）：

| 字段 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| 参考封面本地路径 | ✅ | — | 必须是本地可读的图片文件 |
| 新封面标题 | ✅ | — | 用户的新标题文案 |
| 账号领域 | ✅ | — | 知识干货/娱乐/美妆/科技/教育/...，决定视觉调性 |
| 画面比例 | ❌ | `9:16` | 一般不需调整 |
| 是否有人像 | ❌ | 自动判断 | 若参考封面有人像，新封面也建议保留人像位置逻辑 |
| 参考深度 | ❌ | `light` | `light`（只学色彩和层级）或 `deep`（参考构图和主体位置）|

写入 `$DIR/input-manifest.md`：

```markdown
# Input Manifest

## User Inputs

- reference_cover: /Users/.../ref.png
- new_title: 3 步学会爆款标题
- account_domain: 知识干货
- aspect_ratio: 9:16
- has_person: true
- reference_depth: light

## Workspace

- $CHANNEL_ID: <频道 ID>
- $DIR: <工作目录>
- $TASK_ID: <任务 ID>
```

---

### Phase 1 — 拆解参考封面

#### 步骤 3：注册参考图 + 视觉分析

**3a. 注册参考图到 MCP 服务器**：

`ref_image_path` 需要服务器可访问的路径。用户提供的本地路径不能直接传，必须先注册：

```
1. Read 参考封面本地路径 → 得到 CDN_URL（约 30 分钟过期，立即使用）
2. download_image(channel_id="$CHANNEL_ID", url=CDN_URL) → 返回 REF_SERVER_PATH
```

把 `REF_SERVER_PATH` 记录到 `$DIR/server-paths.md`。

**3b. 视觉分析**：

调用 `analyze_image` 提取参考封面的 8 个维度，prompt 模板见 [references/analysis-template.md](references/analysis-template.md)：

```
analyze_image(
  channel_id="$CHANNEL_ID",
  file_path="$REF_SERVER_PATH",
  prompt=<参考 references/analysis-template.md 的 8 维度分析模板>
)
```

如果因 10MB 限制失败：先 `compress_image(file_path=REF_SERVER_PATH)`；仍失败则 `upload_image` 后用 `image_url` 重试。

**3c. 写入分析结果**：

把 analyze_image 返回的 8 维度结构化描述写入 `$DIR/reference-analysis.md`，包含：画面比例、标题位置与层级、人物/主体位置、背景氛围、主色与强调色、字体气质、视觉重点、构图技巧。

---

### Phase 2 — 迁移到新内容

#### 步骤 4：构建 cover-plan.md

根据新标题、账号领域、参考分析（特别是 `reference_depth`），判断迁移策略。**这一步是"思路"到"行动"的关键过渡**——必须把抽象的视觉逻辑翻译成针对当前新标题的具体决策。

判断维度：

1. **新标题分行**：根据标题字数和语义节奏，决定分几行、哪些词放大
   - 短标题（≤6 字）：1 行，整体放大
   - 中标题（7-12 字）：2 行，关键词放大
   - 长标题（13+ 字）：2-3 行，主关键词明显放大，辅助词缩小
2. **放大词识别**：从新标题中挑出 1-2 个最具情绪张力或信息密度的词
3. **主体位置**：参考封面的人像/主体放在哪里？新封面是否保留这个位置逻辑？
   - `reference_depth=deep`：保留参考的主体位置逻辑
   - `reference_depth=light`：根据新标题重新决定
4. **背景调整**：参考背景氛围是否适合新标题的账号领域？不适合则替换
5. **素材保留/替换**：参考封面中的装饰元素（图标、徽章、几何形）是否保留？
   - 装饰性元素：可保留以保持视觉密度
   - 语义性元素（具体产品/Logo）：必须替换

写入 `$DIR/cover-plan.md`：

```markdown
# Cover Plan

## Migration Strategy

- reference_depth: light
- title_breakdown:
  - line_1: "3 步学会"（中等大小）
  - line_2: "爆款标题"（放大，强视觉冲击）
- emphasized_words: ["爆款标题"]
- subject_position: 居中偏上（保留参考逻辑）
- background: 由参考的纯黑改为深蓝渐变（更符合知识干货调性）
- decorative_elements: 保留右下角箭头图标，替换左上角徽章

## Style Anchors

- main_color: 深蓝（参考的黑色调整为更亲和的深蓝）
- accent_color: 暖黄（与主色对比，提升标题可读性）
- font_vibe: 黑体加粗（参考是无衬线粗体，沿用）
- visual_focus: 标题文字（参考的视觉重点是人脸，新封面改为文字主导）
```

---

### Phase 3 — 生成封面

#### 步骤 5：构建 prompt + 调用 generate_image

**5a. 构建 prompt**：

按 [references/prompt-template.md](references/prompt-template.md) 的 8 要素模板组装 prompt。Prompt 控制在 500 词以内，避免长 prompt 触发 504；超过时删减到关键要素 + 1-2 个最重要反面约束。

构建要点：
- 已确定要素直接填入：`画面比例 9:16`、`标题分行`、`主体位置`、`背景描述`、`色彩主+强调`、`字体气质`、`主体元素`、`禁止事项`
- `reference_depth=deep` 时，prompt 中显式声明"参考封面的构图逻辑"
- `reference_depth=light` 时，prompt 中只提色彩和字体气质参考，构图完全自主
- 中文文字描述要具体（"标题'爆款标题'用大号黑体加粗"），但要在能力边界说明中告知用户渲染可能不精确

**5b. 调用 generate_image**：

```
result = generate_image(
  channel_id="$CHANNEL_ID",
  prompt=<5a 构建的 prompt>,
  image_type="cover",
  output_path="/tmp/anbanwriter-short-video-cover/$TASK_ID/cover.png",
  size="9:16",
  ref_image_path="$REF_SERVER_PATH"
)
DOWNLOAD_URL = result.download_url
COVER_SERVER_PATH = result.file_path
```

**5c. 本地归档 + prompt 备份**：

- 下载 `DOWNLOAD_URL` 到 `$DIR/cover.png`（若 `DOWNLOAD_URL` 是 data URL，base64 解码后写入；否则用 curl/wget 下载）
- 把实际 prompt、image_type、size、output_path、ref_image_path、provider、model、response_type、revised_prompt、output_mime 全部追加到 `$DIR/cover-prompts.md`，便于复盘 504、构图偏移和颜色问题
- 把 `COVER_SERVER_PATH` 记录到 `$DIR/server-paths.md`

---

### Phase 4 — 二次优化

#### 步骤 6：审计 + 必要重试

**6a. 审计生成结果**：

```
analyze_image(
  channel_id="$CHANNEL_ID",
  file_path="$COVER_SERVER_PATH",
  prompt=<参考 references/optimization-checklist.md 的 5 项审计模板>
)
```

审计 5 项（详见 [references/optimization-checklist.md](references/optimization-checklist.md)）：

1. 标题是否清楚（文字可辨认、层级分明）
2. 构图是否接近参考的逻辑（不是像素级，是结构逻辑）
3. 人物/主体是否突出（视觉重点是否对焦在主体）
4. 颜色是否统一（主色和强调色和谐，无杂色）
5. 元素是否过多（画面是否杂乱，视觉重点是否被稀释）

写入 `$DIR/cover-review.md`，每项打分 PASS/MINOR/FAIL 并附分析依据。

**6b. 必要时重试**：

任一关键项（标题清楚、主体突出）FAIL，或 3 项以上 MINOR：
- 用更具体的 prompt 重试 1 次（明确指出问题项，加强反面约束）
- `output_path` 改为 `/tmp/anbanwriter-short-video-cover/$TASK_ID/cover_v2.png`
- 重试结果同样走 6a 审计；若仍 FAIL 则接受当前最佳并在 cover-review.md 标注 `needs_manual_edit`（建议用户用 PS/Canva 二次加工具体文字）

**不要无限重试**——最多 1 次重试，避免消耗。

---

### Phase 5 — 归档报告

#### 步骤 7：最终报告

向用户交付：

```
短视频封面复刻完成

参考封面: $DIR/input-manifest.md 记录的路径
新标题: <用户的新标题>
参考深度: light / deep

视觉迁移摘要:
- 参考的视觉逻辑: <从 reference-analysis.md 提炼的 2-3 句话>
- 实际迁移决策: <从 cover-plan.md 提炼的关键改动>

成果文件:
- $DIR/cover.png （主交付）
- $DIR/cover_v2.png （若重试过，作为备选）

审计结论:
- 5 项审计结果: <PASS/MINOR/FAIL 汇总>
- 能力边界: 当前 generate_image 不保证中文文字精确渲染；若封面文字不清晰建议 PS 二次加工

复盘材料:
- $DIR/reference-analysis.md （参考封面拆解）
- $DIR/cover-plan.md （迁移决策）
- $DIR/cover-prompts.md （prompt 备份）
- $DIR/cover-review.md （质量审计）
```

---

## Prompt 构建技巧

### 8 要素写法要点

| 要素 | 好的写法 | 差的写法 |
|------|---------|---------|
| 画面比例 | "vertical 9:16 ratio, portrait orientation" | "竖图" |
| 标题排版 | "title '爆款标题' in 2 lines, line 1 small, line 2 oversized bold" | "有大标题" |
| 人物/主体 | "young woman in red hoodie, half-body, positioned center-left, looking at camera" | "有个女生" |
| 背景 | "deep navy gradient background with subtle geometric pattern, clean and uncluttered" | "深色背景" |
| 色彩 | "main color deep navy like midnight sky, accent color warm yellow like honey" | "蓝黄色调" |
| 字体气质 | "bold sans-serif Chinese title, high contrast, commercial poster vibe" | "黑体" |
| 主体元素 | "central focus on the oversized title text, woman as supporting element behind text" | "标题为主" |
| 禁止事项 | "no English text, no watermark, no logo, no border, no cluttered elements" | "干净" |

### 参考深度对 prompt 的影响

- `reference_depth=light`：prompt 不提"参考构图"，只描述新封面的目标视觉；ref_image_path 仍传入但仅作为风格参考
- `reference_depth=deep`：prompt 开头加 "Reference cover shows the visual logic to follow: title in [position], subject in [position], color scheme [main + accent]. Recreate this composition logic with the new title and subject."

### 反面约束的力量

AI 生图模型容易跑偏，**显式禁止比正向描述更有效**：

```
DO NOT include:
- English text or pinyin (Chinese only, with possible character rendering imperfections)
- Watermarks, logos, signatures
- Cluttered or busy elements competing with the main subject
- Multiple unrelated subjects
- Cartoon or anime style (must be photorealistic/commercial photography style)
```

---

## 常见失败与修复

| 问题 | 原因 | 修复 |
|------|------|------|
| 标题文字渲染模糊/错字 | AI 模型对中文文字支持差 | 在 prompt 中明确"Chinese characters may have rendering imperfections, prioritize overall visual impact over text precision"；建议用户 PS 二次加工 |
| 构图完全偏离参考 | reference_depth=deep 但 prompt 未显式声明参考逻辑 | prompt 开头加"Reference cover shows the visual logic to follow: ..." |
| 人物位置错误 | 主体描述不够具体 | 明确位置："positioned center-left"、"right side of frame" |
| 颜色与参考差距大 | 色彩描述过于抽象 | 用实物类比："deep navy like midnight sky, NOT pure black" |
| 画面元素过多过乱 | 缺少反面约束 | prompt 末尾加"DO NOT include cluttered elements" |
| 太像参考图（构图照搬） | reference_depth=deep 但未替换语义元素 | 把参考的"装饰元素保留、语义元素替换"原则写进 prompt |
| 太不像参考图（视觉断裂） | reference_depth=light 但色彩和字体气质也未对齐 | 即使 light 模式，主色和字体气质也应参考；只重做构图 |
| CDN URL 过期 | Read 返回的 CDN URL 约 30 分钟后过期 | 获取后立即使用；需要重新分析时重新 Read 获取新 URL |
| analyze_image 文件过大 | `file_path` 方式分析有 10MB 限制 | 先 `compress_image`，再失败则 `upload_image` 后用 `image_url` |
| output_path 权限错误 | `output_path` 是 MCP 服务器端路径 | 使用 `/tmp/anbanwriter-short-video-cover/$TASK_ID/...` |
| 长 prompt 504 Gateway Timeout | prompt 过长或约束过多 | Prompt 控制在 500 词以内，优先 8 要素和最关键反面约束 |
| ref_image_path 无法访问 | 远程 MCP Server 无法访问本地文件路径 | 通过 Read + download_image 注册到服务器端 |

---

## 验证清单

### 单张封面完成后

- [ ] `$DIR/input-manifest.md` 已生成，包含全部 6 个用户输入字段
- [ ] `$DIR/server-paths.md` 已记录 REF_SERVER_PATH（和 COVER_SERVER_PATH）
- [ ] `$DIR/reference-analysis.md` 已生成，覆盖 8 个分析维度
- [ ] `$DIR/cover-plan.md` 已生成，包含迁移决策和 style anchors
- [ ] `$DIR/cover.png` 实际下载到本地
- [ ] `$DIR/cover-prompts.md` 已备份完整 prompt + provider 元数据
- [ ] `$DIR/cover-review.md` 已生成，5 项审计 PASS/MINOR/FAIL 评级

### 全部完成后

- [ ] 任一关键项（标题清楚、主体突出）FAIL 时已重试 1 次
- [ ] 重试后仍 FAIL 已标记 `needs_manual_edit` 并向用户说明
- [ ] 最终报告已交付，包含路径、迁移摘要、审计结论、能力边界声明
