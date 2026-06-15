# 公众号封面图设计规范

## 核心原则：账号驱动，非 Writer 驱动

**Writer YAML 定义文字风格（语气、结构、修辞），不定义图片风格。** 封面图的视觉风格由账号定位、内容主题和目标受众三个维度独立决定。

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
{COLOR_PALETTE}. {CONTENT_SUBJECT} — {VISUAL_METAPHOR_FROM_ARTICLE}.
{MOOD_TONE}. {COMPOSITION_GUIDANCE}.
Photographic quality, no text overlays, no watermarks, no logo.
```

### Prompt 构建要点

1. **VISUAL_STYLE**：由三维分析确定的视觉风格（如 "warm natural photography, soft morning light, organic textures"）
2. **COLOR_PALETTE**：与账号定位匹配的色彩（如 "warm earth tones with soft green and gold accents"）
3. **CONTENT_SUBJECT**：文章主题的视觉化表达（如 "a serene lotus flower blooming at dawn"）
4. **VISUAL_METAPHOR_FROM_ARTICLE**：从文章内容中提取的视觉隐喻（使用文章中已有的比喻或意象）
5. **MOOD_TONE**：与内容情绪匹配的氛围（如 "contemplative and peaceful atmosphere"）
6. **COMPOSITION_GUIDANCE**：构图指导（如 "generous negative space, rule of thirds"）

### 好的封面 prompt 示例

**养生账号，文章关于"慢下来的力量"**：
```
A 2.35:1 horizontal image for a WeChat article cover. Warm natural photography, soft morning light filtering through translucent leaves, organic textures. Warm earth tones with soft sage green and golden accents. A single lotus bud slowly opening at dawn, dewdrops on petals, mist rising from still water. Serene and meditative atmosphere. Generous negative space, the bud placed at the left third. Photographic quality, no text overlays, no watermarks, no logo.
```

**文化账号，文章关于"文字的温度"**：
```
A 2.35:1 horizontal image for a WeChat article cover. Traditional Chinese aesthetic, subtle ink wash texture blending with warm photography. Ink black, warm brown, and celadon tones. An ancient calligraphy brush resting on handmade paper, a single character partially written, warm golden light from a window. Contemplative and elegant atmosphere. Shallow depth of field, paper texture in foreground. Photographic quality, no text overlays, no watermarks, no logo.
```

---

## 封面约束

**必须**：
- 摄影级或绘画级质感，与账号领域匹配
- 与文章内容有视觉隐喻关联（不是通用素材图）
- 温暖/积极的情感基调（适合大多数中文内容账号）
- 2.35:1 横版比例（900×383px 标准）

**禁止**：
- 3D 渲染 / 合成感
- 卡通 / 动漫 / 剪纸风格
- 暗黑 / 恐怖 / 压抑意象（除非账号定位明确要求）
- 文字叠层 / 水印 / logo 占位
- 纯色 / 渐变背景（无内容实体）
- 对称 PPT 式布局
