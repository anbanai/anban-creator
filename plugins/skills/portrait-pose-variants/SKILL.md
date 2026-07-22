---
name: portrait-pose-variants
description: Use when generating multiple pose/expression variants from a single portrait photo while keeping the person's identity consistent, or when user mentions "人像姿态", "人像一致性", "同一人物不同表情", "封面人物表情包", "人像变体", "portrait consistency", "pose variants", "手势变化", "表情封面". Triggers whenever a user provides one portrait photo and asks for multiple cover-ready variants of the same person — even if they don't explicitly say "一致性". Generates 1-6 vertical 9:16 cover-ready portraits from one reference photo, locked to the same person's face/features/hair/style.
---

# 人像姿态变体——基于一张参考人像生成多张封面

## 案例库

遇到场景分支、产物格式或质量边界不确定时，先读 [references/examples.md](references/examples.md)。

## 图片比例固定规则

本 Skill 只要涉及生成、选择、裁切、校验或引用图片，必须按以下优先级决定画面比例：

1. 用户/任务明确指定的 `image_ratio`、`size` 或平台规格优先。
2. 项目/频道默认比例次之。
3. 业务默认比例只作兜底：微信文章封面/正文图默认 `16:9`；Seednote/XLS/移动信息流默认 `3:4`；电商、广告投放、视频封面按具体平台素材位要求执行。
4. 不得从模型路由、供应商默认 `size` 或模型能力反推业务比例；模型只决定能力和成本，比例属于创作场景约束。


## MCP 工具

| MCP 工具 | 说明 |
|----------|------|
| `analyze_image` (project_id, image_url, file_path, prompt) | 图像视觉分析——传入图像 URL 或服务器文件路径，返回 AI 视觉分析结果。**Read 工具不用于图像视觉分析**，只用来获取 CDN URL。一次只分析一张图；同时传 `file_path` 和 `image_url` 时服务端只用 `file_path` |
| `generate_image` (project_id, prompt, image_type, output_path, ref_image_path, size, task_id) | 生成单张图片，返回 `download_url`（始终为可 HTTP fetch 的存储 URL，不再返回 base64 data URL）和 `file_path`。当前是参考图生成，**不是专用 ID-lock 工具** |
| `download_image` (project_id, url) | 下载在线图片到 MCP 服务器临时路径，返回 `file_path`。用于把 Read 得到的 CDN URL 注册成 `ref_image_path` 可用的服务器端路径 |
| `compress_image` (file_path) | 压缩图片——`analyze_image` 的 `file_path` 方式有 10MB 限制，超出时先压缩 |
| `upload_image` (project_id, file_path) | 上传图片，用于 `compress_image` 仍超 10MB 的兜底场景 |

---

## 当前能力边界（必须向用户透明记录）

- `generate_image` 是**参考图生成**，不是专用 ID-lock 工具（如 InstantID、PhotoMaker）。`ref_image_path` 能提高身份一致性，**不能 100% 锁定人脸**。
- 同一人物生成 6 张姿态变体时，**每张的身份漂移风险独立存在**——某张可能很像原图，另一张可能脸型稍变。这是当前能力边界，无法保证 6 张全部完美一致。
- 在专用 ID-lock 工具接入前，**收敛修正是重新生成**，不是"只改局部表情"。严重身份漂移（脸型变化、性别感变化、年龄感变化）标记 `needs_img2img`，不要反复生成并声称"只改表情"。
- 手势是 AI 生图的高失败率区域——手指数量、关节方向、双手协调都可能出错。若手势畸形，重新生成 1 次仍失败则标记 `needs_manual_edit`。

如用户要求"6 张必须 100% 是同一个人，脸型完全一致"，必须先说明当前能力无法严格保证；只有接入专用 ID-lock 工具后才能承诺。

---

## 核心原则

### 原则 0：身份神圣不可侵犯（最高优先级）

**可变的**：表情、手势、姿态、服装细节、背景、光影。

**不可变的（12 个身份维度）**：

| # | 维度 | 描述要点 |
|---|------|---------|
| 1 | 脸型 | 圆 / 方 / 长 / 瓜子 / 心形，颧骨宽度，下颌线条 |
| 2 | 五官比例 | 三庭五眼比例，五官在脸部的相对位置 |
| 3 | 眼睛形状 | 杏眼 / 桃花眼 / 凤眼，单/双眼皮，眼裂大小 |
| 4 | 鼻子形状 | 鼻梁高度，鼻头形状，鼻翼宽度 |
| 5 | 嘴型 | 嘴唇厚度，嘴角走向，唇峰形状 |
| 6 | 眉毛 | 眉形（柳叶眉 / 剑眉 / 平眉），浓淡，长度 |
| 7 | 发型 | 长度，刘海，卷直，层次 |
| 8 | 发色 | 黑 / 棕 / 栗 / 染色（具体色名） |
| 9 | 肤色 | 白皙 / 偏黄 / 小麦 / 健康古铜，肤质（哑光 / 水光） |
| 10 | 年龄感 | 18-22 / 23-28 / 30+，气质成熟度 |
| 11 | 气质 | 甜美 / 御姐 / 文艺 / 元气 / 高冷 |
| 12 | 神态特征 | 标志性表情倾向（如"嘴角总带一点笑意"） |

12 维度从参考人像提取后写入 `$DIR/identity-lock.md`，每张变体生成后**逐维度比对**。

### 原则 1：逐张生成、逐张审计

每张姿态变体生成后立即 `analyze_image` 验身份。**不要批量生成 6 张后再统一审计**——如果第 1 张就身份漂移，后续 5 张大概率也会漂移；越早发现越省成本。

### 原则 2：参考图链必须指向原始人像

每张变体的 `ref_image_path` 都传入**原始参考人像**的服务器路径（`portrait_server_path`）。**不用前一张变体作下一张的参考**——这会放大错误，导致身份越生成越偏。

### 原则 3：9:16 竖版 + 商业封面质感

默认配置（直接写入每张 prompt）：

```
画幅比例: 9:16 竖版
镜头: 50mm 人像镜头
景别: 半身近景（头肩到胸口）
风格: 真实摄影、商业封面、短视频爆款封面
清晰度: 高清、锐利、面部细节丰富
背景: 纯色或简洁渐变背景
光影: 明亮棚拍灯光，面部受光清晰，背景简洁，轻微景深
```

人物一致性权重最高，表情/手势变化权重中高，背景变化权重低。

---

## 完整工作流

### Phase 0 — 初始化

#### 步骤 1：获取项目和工作目录

- `echo $ANBAN_DEFAULT_PROJECT` → `$PROJECT_ID`
- 如果为空，调用 `list_projects`；只有一个可用项目时自动使用，多个项目且无法从任务上下文判断时停止并提示配置 `ANBAN_DEFAULT_PROJECT`
- 从 `.task-context` 获取 `$TASK_ID`，或使用 CWD 目录名
- 尝试调用 `prepare_workspace(content_type="short-video", task_id=$TASK_ID)` → `$DIR`
  - prepare_workspace 返回的 path 可能是相对路径；相对路径以当前任务工作区 `$CWD` 为根
  - 如果 `prepare_workspace` 调用失败，使用 `$CWD/output/` 作为 `$DIR`
- `mkdir -p "$DIR"`

#### 步骤 2：收集用户输入

需要用户提供以下信息：

| 字段 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| 参考人像本地路径 | ✅ | — | 必须是本地可读的图片文件，最好是清晰的正面或半侧面人像 |
| 目标姿态列表 | ❌ | 全部 6 种 | 可选 6 模板子集（如 `[1, 3, 5]`），或自定义姿态描述 |
| 生成张数 N | ❌ | 6 | 1 ≤ N ≤ 6；超出按 6 处理并提示 |
| 是否逐张确认 | ❌ | false | true 时每张生成后停止等待用户确认；false 时全部生成后统一交付 |

写入 `$DIR/input-manifest.md`：

```markdown
# Input Manifest

## User Inputs

- reference_portrait: /Users/.../portrait.png
- target_poses: [1, 2, 3, 4, 5, 6]  # 或 "all" 或自定义列表
- variant_count: 6
- confirm_per_image: false

## Workspace

- $PROJECT_ID: <项目 ID>
- $DIR: <工作目录>
- $TASK_ID: <任务 ID>
```

---

### Phase 1 — 锁定身份

#### 步骤 3：注册参考人像 + 提取身份锁

**3a. 注册参考人像到 MCP 服务器**：

```
1. Read 参考人像本地路径 → 得到 CDN_URL（约 30 分钟过期，立即使用）
2. download_image(project_id="$PROJECT_ID", url=CDN_URL) → 返回 PORTRAIT_SERVER_PATH
```

把 `PORTRAIT_SERVER_PATH` 记录到 `$DIR/server-paths.md`。

**3b. 提取身份锁**：

调用 `analyze_image` 提取 12 个身份维度，prompt 模板见 [references/identity-lock-template.md](references/identity-lock-template.md)：

```
analyze_image(
  project_id="$PROJECT_ID",
  file_path="$PORTRAIT_SERVER_PATH",
  prompt=<参考 references/identity-lock-template.md 的 12 维度提取模板>
)
```

如果因 10MB 限制失败：先 `compress_image(file_path=PORTRAIT_SERVER_PATH)`；仍失败则 `upload_image` 后用 `image_url` 重试。

**3c. 写入身份锁**：

把 12 维度结构化描述写入 `$DIR/identity-lock.md`。这份文件是后续每张变体 prompt 的**身份部分**直接抄写来源，也是 Phase 4 审计的比对基准。

---

### Phase 2 — 选择姿态

#### 步骤 4：确定要生成的姿态列表

根据 `input-manifest.md` 的 `target_poses`，从 6 个标准模板（参考 [references/pose-templates.md](references/pose-templates.md)）中选择：

| # | 模板名 | 适用封面类型 |
|---|--------|-------------|
| 1 | 震惊瞪眼 + 双手捂脸 | "震惊内幕"、"太离谱了" |
| 2 | 自信微笑 + 单手指向镜头 | "干货分享"、"重要提醒"、"别再踩坑" |
| 3 | 疑惑皱眉 + 单手托下巴 | "为什么会这样"、"很多人都想错了" |
| 4 | 开心大笑 + 双手点赞 | "好消息"、"太值了"、"强烈推荐" |
| 5 | 严肃警告 + 单手停止手势 | "别再这样做"、"危险提醒"、"千万注意" |
| 6 | 惊喜兴奋 + 双手张开 | "终于发现了"、"原来这么简单"、"太惊喜了" |

如果是自定义姿态（不在 6 模板内），用户须提供：表情描述 + 手势描述 + 整体情绪 + 适用封面类型。

把选定的姿态列表写入 `$DIR/selected-poses.md`，包含每个姿态的：编号、表情、手势、情绪基调、prompt 段落。

---

### Phase 3 — 逐张生成

#### 步骤 5：循环生成 N 张变体

对每个姿态 i（i = 1..N）执行 5a-5e：

**5a. 构建 prompt**：

按以下顺序拼接（详细模板见 [references/pose-templates.md](references/pose-templates.md)）：

```
[身份锁段落 — 从 identity-lock.md 抄写]
[当前姿态段落 — 从 selected-poses.md 抄写]
[通用风格段落 — 9:16, 半身, 商业封面, 棚拍, etc.]
[通用负面约束段落 — 见下方"通用负面提示词"]
```

Prompt 控制在 500 词以内；超过时优先保留身份锁 + 当前姿态 + 关键负面约束。

**5b. 调用 generate_image**：

```
result_i = generate_image(
  project_id="$PROJECT_ID",
  prompt=<5a 构建的 prompt>,
  image_type="cover",
  output_path="/tmp/anban-creator-portrait-pose/$TASK_ID/variant_0i.png",
  size="9:16",
  ref_image_path="$PORTRAIT_SERVER_PATH"  # 始终用原始人像，不用前一张变体
)
DOWNLOAD_URL_i = result_i.download_url
VARIANT_SERVER_PATH_i = result_i.file_path
```

**5c. 本地归档 + prompt 备份**：

- 下载 `DOWNLOAD_URL_i` 到 `$DIR/variant_0i.png`
- 把实际 prompt、image_type、size、output_path、ref_image_path、provider、model、response_type、revised_prompt、output_mime 追加到 `$DIR/image-prompts.md`
- 把 `VARIANT_SERVER_PATH_i` 追加到 `$DIR/server-paths.md`

**5d. 逐张身份审计**：

立即（不等其他变体生成）调用 `analyze_image` 审计：

```
analyze_image(
  project_id="$PROJECT_ID",
  file_path="$VARIANT_SERVER_PATH_i",
  prompt=<参考 references/consistency-audit.md 的 12 维度比对模板，基准是 identity-lock.md>
)
```

**5e. 必要时立即重试**：

关键维度（脸型 / 五官比例 / 发型发色）任一 FAIL：
- 用更严格 prompt 重试 1 次（加强身份锁描述、加强反面约束）
- `output_path` 改为 `/tmp/anban-creator-portrait-pose/$TASK_ID/variant_0i_v2.png`
- 重试结果走 5d 审计；仍 FAIL 则接受当前最佳并标记 `needs_img2img`
- **不要无限重试**——最多 1 次

**5f. 逐张确认模式**：

如果 `confirm_per_image=true`：每张完成后**停止并提示用户**：

```
[3/6] variant_03.png 已生成（姿态：疑惑皱眉 + 单手托下巴）

身份审计结果：脸型 PASS / 五官比例 PASS / 发型 PASS / 表情夸张度 MINOR

是否接受这张？接受请回复"继续"，重新生成请回复"重做"。
```

得到用户确认后再生成下一张。

---

### Phase 4 — 全量一致性审计

#### 步骤 6：生成汇总报告

步骤 5 的逐张审计结果汇总到 `$DIR/consistency-report.md`：

```markdown
# Consistency Report

## Identity Lock Source

- file: $DIR/input-manifest.md 中的 reference_portrait
- analyzed_at: <时间戳>
- portrait_server_path: $PORTRAIT_SERVER_PATH
- 12 维度身份锁: $DIR/identity-lock.md

## Per-Variant Audit

| # | Pose | 脸型 | 五官比例 | 眼睛 | 鼻子 | 嘴型 | 眉毛 | 发型 | 发色 | 肤色 | 年龄感 | 气质 | 神态 | Overall |
|---|------|------|---------|------|------|------|------|------|------|------|--------|------|------|---------|
| 1 | 震惊捂脸 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | PASS |
| 2 | 自信指向 | ✅ | ✅ | ✅ | ✅ | ⚠️ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | MINOR |
| 3 | 疑惑托下巴 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ✅ | ✅ | FAIL（已重试）|
| ... |

## Summary

- PASS: 4 张
- MINOR: 1 张（关键维度 PASS，可接受）
- FAIL: 1 张（重试后仍漂移，标记 needs_img2img）

## Retry History

- variant_03.png: 发型 FAIL（卷发变直发）→ 重试 prompt 加强"hair must be wavy curls, NOT straight"→ 仍 FAIL → 标记 needs_img2img

## Capability Boundary

当前 generate_image 是参考图生成，不是专用 ID-lock 工具。MINOR/FAIL 是能力边界，非流程缺陷。
```

---

### Phase 5 — 归档报告

#### 步骤 7：最终交付

向用户交付：

```
人像姿态变体生成完成

参考人像: $DIR/input-manifest.md 记录的路径
生成张数: N
姿态列表: <从 selected-poses.md 提炼>

成果文件:
- $DIR/variant_01.png ~ variant_0N.png（主交付）
- $DIR/variant_0N_v2.png（若重试过，作为备选）

身份一致性:
- 12 维度审计结果: <PASS/MINOR/FAIL 汇总>
- 关键维度（脸型/五官比例/发型发色）: <汇总>
- 能力边界: 当前使用 generate_image best-effort 参考图生成，未使用专用 ID-lock

复盘材料:
- $DIR/identity-lock.md （身份锁）
- $DIR/selected-poses.md （姿态选择）
- $DIR/image-prompts.md （prompt 备份）
- $DIR/consistency-report.md （一致性审计）

人工复核:
- variant_0X.png: <描述仍存在的问题>，建议手动指定该维度后重新生成或使用专用 ID-lock 工具
```

---

## 通用负面提示词

每张变体的 prompt 末尾**必须**包含以下反面约束（来自参考文档原文，不要删减）：

```
DO NOT change the person's identity. DO NOT swap face. DO NOT turn into a different person.
DO NOT change age, gender, or ethnic features. DO NOT change face shape or facial proportions.

DO NOT include multiple people. DO NOT include extra fingers or deformed hands.
DO NOT distort facial features. DO NOT make expression stiff. DO NOT use low resolution.
DO NOT blur. DO NOT use plastic skin or over-smoothing. DO NOT make cartoon or anime style.
DO NOT over-distort to the point of unreality.

DO NOT include text, watermark, logo, border, or cluttered background.
DO NOT obscure the face. DO NOT use heavy face shadows.
```

---

## Prompt 构建技巧

### 身份锁段落写法

从 `identity-lock.md` 抄写时，**转换成英文 prompt 友好的格式**：

```
Identity lock (MUST remain identical across all variants):
- Face shape: <oval with defined jawline, NOT round>
- Facial proportions: <three-tenths proportions, balanced features>
- Eyes: <almond-shaped, double eyelids, medium eye opening>
- Nose: <medium-height bridge, rounded tip>
- Mouth: <medium-thick lips, slight upward corners>
- Eyebrows: <arched, medium thickness, well-defined>
- Hair: <shoulder-length wavy curls with side-swept bangs, NOT straight>
- Hair color: <dark chocolate brown, NOT black or light brown>
- Skin: <fair with subtle warmth, semi-matte finish>
- Age vibe: <mid-20s, youthful but professional>
- Aura: <confident, slightly playful, approachable>
- Signature demeanor: <eyes carry a hint of smile even when mouth is neutral>

CONSTRAINT: The variant MUST be recognizably the SAME person as the reference. Identity dimensions 1-12 are non-negotiable. Only expression, gesture, pose, clothing details, background, and lighting may change.
```

### 姿态段落写法（参考 pose-templates.md）

每个姿态段落包含 4 部分：

1. **核心动作**（一句话描述姿态）
2. **表情细节**（眉、眼、嘴的具体状态）
3. **手势细节**（手的位置、形状、与身体的关系）
4. **情绪基调**（这个姿态传达什么情绪，适合什么封面类型）

详见 [references/pose-templates.md](references/pose-templates.md) 的 6 个完整模板。

### 跨变体一致性技巧

每张变体的 prompt 中**显式声明**这是同一人物的不同姿态：

```
This is variant #3 of 6 variants of the SAME person shown in the reference image.
All variants share the same identity (see identity lock above).
Only this variant's expression, gesture, and pose change.
Background and clothing may vary slightly but the person MUST be identical.
```

这段声明帮助模型理解"我要画同一个人的不同照片"，而不是"画 6 个不同的人"。

---

## 常见失败与修复

| 问题 | 原因 | 修复 |
|------|------|------|
| 身份漂移（脸型变化） | 模型对参考图身份锁定不严 | 加强 identity-lock 段落；增加反面约束"face shape MUST be [具体], NOT [常见错误]"；严重时标记 `needs_img2img` |
| 发色变化 | 模型对深浅色偏好不同 | 用实物类比"dark chocolate brown, NOT milk chocolate, NOT black"；加强反面约束 |
| 表情不够夸张 | 模型倾向中性表情 | 姿态段落用更具体的描述"eyes WIDE OPEN, eyebrows raised HIGH, mouth forming an O"；加强反面约束"DO NOT use neutral expression" |
| 手势畸形（多指、扭曲） | AI 生图模型对手部处理能力差 | 手势描述更具体"five fingers visible, palm facing camera, thumb tucked"；加强反面约束"DO NOT add extra fingers or distort joints"；重试 1 次仍失败标记 `needs_manual_edit` |
| 背景污染主体 | 模型无法分离前景/背景 | 加强背景简化"solid color background, NO patterns, NO textures"；加强反面约束"background must NOT compete with subject" |
| 肤色变化 | 模型对肤色一致性处理弱 | 身份锁中肤色维度加实物类比"fair skin with peach warmth, NOT pale white, NOT tanned" |
| 画风偏卡通 | 参考 prompt 中"商业摄影"描述不够强 | 加强风格描述"photorealistic commercial photography, hyper-detailed skin texture, NOT illustration, NOT cartoon" |
| 多张变体之间身份不一致 | 每张变体身份漂移方向不同 | 确保所有变体 ref_image_path 都指向同一原始人像；不要用变体作下一张参考 |
| CDN URL 过期 | Read 返回的 CDN URL 约 30 分钟后过期 | 获取后立即使用；需要重新分析时重新 Read 获取新 URL |
| analyze_image 文件过大 | `file_path` 方式分析有 10MB 限制 | 先 `compress_image`，再失败则 `upload_image` 后用 `image_url` |
| output_path 权限错误 | `output_path` 是 MCP 服务器端路径 | 使用 `/tmp/anban-creator-portrait-pose/$TASK_ID/...` |
| 长 prompt 504 Gateway Timeout | prompt 过长（12 维度身份锁 + 6 姿态模板容易超长） | Prompt 控制在 500 词以内；身份锁可压缩到 8-10 行核心维度；姿态段落保留核心动作和情绪 |
| ref_image_path 无法访问 | 远程 MCP Server 无法访问本地文件路径 | 通过 Read + download_image 注册到服务器端 |

---

## 验证清单

### 单张变体完成后

- [ ] `$DIR/variant_0N.png` 实际下载到本地
- [ ] `$DIR/image-prompts.md` 已追加该张的完整 prompt + provider 元数据
- [ ] `$DIR/server-paths.md` 已记录 VARIANT_SERVER_PATH_N
- [ ] 逐张身份审计已通过（关键维度 PASS 或重试后接受）
- [ ] `confirm_per_image=true` 时已得到用户确认

### 全部完成后

- [ ] N 张变体全部生成
- [ ] `$DIR/identity-lock.md` 覆盖 12 个身份维度
- [ ] `$DIR/selected-poses.md` 列出所有选定姿态
- [ ] `$DIR/consistency-report.md` 汇总所有变体的 12 维度审计
- [ ] 关键维度 FAIL 已重试 1 次；重试后仍 FAIL 已标记 `needs_img2img`
- [ ] 手势畸形的变体已重试 1 次；重试后仍畸形已标记 `needs_manual_edit`
- [ ] 最终报告已交付，包含 N 张路径、身份锁摘要、审计汇总、能力边界声明
