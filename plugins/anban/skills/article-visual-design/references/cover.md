# 公众号封面图设计规范

## Contents

- [角色定位](#角色定位)
- [核心原则：配置优先，账号/内容细化，Writer 无关](#核心原则配置优先账号内容细化writer-无关)
- [三维风格分析](#三维风格分析)
  - [维度 1：账号定位（主要决定因素）](#维度-1账号定位主要决定因素)
  - [维度 2：内容主题（具体场景引导）](#维度-2内容主题具体场景引导)
  - [维度 3：目标受众（色彩和质感偏好）](#维度-3目标受众色彩和质感偏好)
- [账号领域 → 视觉方向参考](#账号领域-视觉方向参考)
- [封面 Prompt 模板](#封面-prompt-模板)
  - [Prompt 构建要点](#prompt-构建要点)
  - [好的封面 prompt 示例](#好的封面-prompt-示例)
- [封面约束](#封面约束)
- [落盘 cover-prompt.md（硬性要求）](#落盘-cover-promptmd硬性要求)

> **生成流程与官方比例规范的权威入口是 `article-cover-design` skill**（硬编码 900×383 / 2.35:1、中心安全区、受控文字策略、公众号封面质量评分卡、迭代闭环）。本文档是该 skill 的**三维风格方向参考**与设计原则补充。

## 角色定位

封面在 `visual-rhythm-plan.md` 中对应 `hero` slot（`image_size=full-bleed`, `2.35:1`），是全篇的视觉锚点。封面定调后，所有内容配图通过 `ref_image_path="$DIR/cover.png"` 继承风格。详见 [rhythm.md](rhythm.md)。

封面也必须通过 vision 校验（`required_entities` 来自文章核心隐喻），不通过则重试一次，仍失败请求用户协助。

## 核心原则：配置优先，账号/内容细化，Writer 无关

公众号视觉是三个**正交**维度之一（图片视觉 `visual_style` / 写作者 `writer` / 排版样式 `theme`），互不推导。**Writer YAML 仅定义文字风格，不携带任何视觉/封面字段**（曾经的 `cover_style`/`cover_prompt` 已移除）。

视觉风格的**权威来源**是任务已解析的 `visual_style` 字段（由 `get_project_profile` 按 `task > project` 两层返回）：
- **有配置值**（`visual_style_source` 为 task/project 之一）→ 以它为视觉锚点，下面的三维分析只做**细化充实**（配色、情绪、构图），**不得偏离或冲突**。
- **无配置值**（两层均为空）→ 完全由账号定位、内容主题、目标受众三维分析确定。

这彻底切断了"writer key 泄漏为图片风格"的诱因链（曾经的 dan-koe → 维多利亚木刻 bug）：即使某项目写作者是 dan-koe，只要视觉维度配置为温暖自然，配图就绝不可生成维多利亚版画。

---

## 三维风格分析

封面视觉风格每次根据账号和内容动态确定，不做固定预设：

### 维度 1：账号定位（主要决定因素）

| 定位类型 | 视觉方向 | 典型色彩 |
|----------|----------|----------|
| 知识/专业型 | 干净、结构感、专业质感 | 冷色调为主：蓝、灰、白，辅以金色点缀 |
| 生活/情感型 | 温暖、自然、氛围感 | 暖色调：米白、暖棕、柔橙、淡金 |
| 文化/艺术型 | 传统美学、雅致质感 | 墨黑、暖棕、青瓷、古铜 |
| 养生/健康型 | 自然元素、有机质感 | 大地色系：暖棕、柔绿、金色、米白 |

### 维度 2：内容主题（具体场景引导）

- 健康/养生 → 自然元素、植物特写、晨光、有机隐喻
- 文化/历史 → 传统意象、笔墨纹理、古建筑局部、器物特写
- 科技/教育 → 抽象几何、光影渐变、简洁概念化
- 生活/情感 → 自然光线场景、日常细节、温暖瞬间
- 哲学/认知 → 沉思场景、留白意境、象征性构图
- 美食/旅行 → 自然光线、质感特写、风景氛围

### 维度 3：目标受众（色彩和质感偏好）

- 成熟/职场人群 → 质感低饱和、自然摄影、克制优雅
- 年轻/休闲人群 → 明度偏高、色彩鲜活、视觉轻快
- 专业/行业人群 → 结构感强、色彩克制、信息密度适中

---

## 账号领域 → 视觉方向参考

以下为方向性参考（非 rigid 映射），实际风格由三维分析综合确定：

| 账号领域 | 视觉方向 | 典型色彩 | 视觉元素示例 |
|----------|----------|----------|-------------|
| 中医/养生/健康 | 自然摄影、有机元素 | 大地色、柔绿、金色 | 荷花、药材特写、晨光、自然肌理 |
| 文化/历史/艺术 | 传统美学、水墨韵味 | 墨黑、暖棕、青瓷 | 水墨意境、陶瓷纹理、书法笔触 |
| 科普/科技/教育 | 干净现代、概念化 | 蓝、青、白 | 抽象几何、柔和渐变、极简构图 |
| 生活/美食/旅行 | 温暖生活方式摄影 | 橙、奶油、橄榄 | 自然光线、食物质感、风景 |
| 育儿/家庭 | 温暖自然、柔和色调 | 柔粉、暖米、鼠尾草 | 自然场景、温和特写、家庭温馨 |
| 金融/商业 | 专业简洁设计 | 藏蓝、金、白 | 建筑线条、抽象隐喻、数据可视化 |

---

## 封面 Prompt 模板

根据三维分析结果，按以下模板从零构建封面 prompt（不使用 writer YAML 的 cover_prompt）：

```
A 2.35:1 horizontal image for a WeChat article cover. {VISUAL_STYLE}.
Cover hook: {COVER_HOOK}. {COLOR_PALETTE}. {CONTENT_SUBJECT} — {VISUAL_METAPHOR_FROM_ARTICLE}.
Thumbnail strategy: {THUMBNAIL_STRATEGY}. Avoid generic visuals: {ANTI_GENERIC_CONSTRAINTS}.
{MOOD_TONE}. {COMPOSITION_GUIDANCE}.
Photographic quality, {TEXT_POLICY_FROM_ARTICLE_COVER_DESIGN}, no watermarks, no logo.
```

### Prompt 构建要点

1. **VISUAL_STYLE**：由三维分析确定的视觉风格（如 "warm natural photography, soft morning light, organic textures"）
2. **COLOR_PALETTE**：与账号定位匹配的色彩（如 "warm earth tones with soft green and gold accents"）
3. **COVER_HOOK**：最终标题和 digest 前半句共同指向的点击钩子
4. **CONTENT_SUBJECT**：文章主题的视觉化表达（如 "a serene lotus flower blooming at dawn"）
5. **VISUAL_METAPHOR_FROM_ARTICLE**：从文章内容中提取的视觉隐喻（使用文章中已有的比喻或意象）
6. **THUMBNAIL_STRATEGY**：缩略图靠什么被看见（主体大小、明暗对比、色块或短文字）
7. **ANTI_GENERIC_CONSTRAINTS**：禁止通用养生水墨背景、无主体山水、与标题无关的摆拍素材
8. **MOOD_TONE**：与内容情绪匹配的氛围（如 "contemplative and peaceful atmosphere"）
9. **COMPOSITION_GUIDANCE**：构图指导（如 "generous negative space, rule of thirds"）
10. **TEXT_POLICY_FROM_ARTICLE_COVER_DESIGN**：按 `article-cover-design` 的受控文字策略决定：需要时写精确 2-8 字短文字，否则写 `NO text`

### 好的封面 prompt 示例

**养生账号，文章关于"慢下来的力量"**：
```
A 2.35:1 horizontal image for a WeChat article cover. Warm natural photography, soft morning light filtering through translucent leaves, organic textures. Cover hook: slow but visible recovery. Warm earth tones with soft sage green and golden accents. A single lotus bud slowly opening at dawn, dewdrops on petals, mist rising from still water. Thumbnail strategy: one large high-contrast lotus bud, visible at 200px. Avoid generic visuals: no empty ink-wash mountains, no tea cup still life. Serene and meditative atmosphere. Generous negative space, the bud placed in the center safe zone. Photographic quality, NO text, no watermarks, no logo.
```

**文化账号，文章关于"文字的温度"**：
```
A 2.35:1 horizontal image for a WeChat article cover. Traditional Chinese aesthetic, subtle ink wash texture blending with warm photography. Cover hook: words can still carry warmth. Ink black, warm brown, and celadon tones. An ancient calligraphy brush resting on handmade paper, a single character partially written, warm golden light from a window. Thumbnail strategy: one large brush tip and glowing paper texture, high contrast against the background. Avoid generic visuals: no empty study room, no random book stack. Contemplative and elegant atmosphere. Shallow depth of field, paper texture in foreground. Photographic quality, NO text, no watermarks, no logo.
```

---

## 封面约束

**必须**：
- 摄影级或绘画级质感，与账号领域匹配
- 与文章内容有视觉隐喻关联（不是通用素材图）
- 与最终标题、digest 前半句强化同一个钩子
- 主体大、对比强，缩成 200px 仍可读
- 温暖/积极的情感基调（适合大多数中文内容账号）
- **2.35:1 横版（900×383px 标准）**：生成用 `size="21:9"`（Volcengine 支持的最近比），服务端按 `platform=article + image_type=cover` 精确中心裁剪到 900×383 并像素断言——微信零裁剪，告别「需要手动裁剪的纯图」
- **主体居中安全区**：主体落在画面中央 ≈1:1 区域，转发卡 1:1 自动裁切后仍完整；避开底部 20%（微信会在底部叠加文章标题）

**禁止**：
- 3D 渲染 / 合成感
- 卡通 / 动漫 / 剪纸风格
- 暗黑 / 恐怖 / 压抑意象（除非账号定位明确要求）
- 违背受控文字策略的文字叠层 / 水印 / logo 占位
- 纯色 / 渐变背景（无内容实体）
- 对称 PPT 式布局
- 通用养生水墨背景、无主体山水、只放茶盏/莲花/药材摆拍导致每篇看起来一样

---

## 落盘 cover-prompt.md（硬性要求）

封面 prompt 构建完成后，**必须原子写入 `$DIR/cover-prompt.md`**（先写 `.cover-prompt.md.tmp` → `fsync` → `rename` 覆盖），完整记录封面生成决策，便于复盘与风格漂移排查。内容必须包含：

- **比例**：公众号 `2.35:1`（900×383px 标准）
- **账号视觉风格来源**：`$VISUAL_STYLE` / `$COLOR_PALETTE` / `$MOOD`，以及三维分析依据（账号定位 / 内容主题 / 目标受众 各自如何决定视觉方向）
- **标题协同字段**：`final_title`、`digest_hook`、`cover_hook`
- **文章核心隐喻**：`visual_metaphor`，即封面要表达的文章最强视觉隐喻
- **缩略图与反同质化**：`thumbnail_strategy`、`anti_generic_constraints`
- **`required_entities`**：封面必须出现的具体物体列表（vision 校验依据）
- **最终 prompt**：实际传给 `generate_image` 的完整 prompt
- **封面质量评分卡**：`visual_quality_scorecard` + 校验 prompt + 结果（passed / score / missing_entities）

封面图**必须 vision 校验通过后**才可作为发布草稿的 `thumb_media_id`；未通过则重试或请求用户协助，不得用未通过 vision 的封面发布。

**注意**：封面仅用于 `thumb_media_id`，**不得复用为正文内容图**。正文每张图的 `wechat_url` 必须各自独立生成上 CDN——服务端 `publish_draft` 会硬拦截"正文 ≥2 图但唯一 URL==1"的草稿。
