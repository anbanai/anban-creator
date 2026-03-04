# 视觉风格预设参考

小绿书图片帖视觉风格预设系统，确保同一组图片的设计风格、配色方案、边框样式、背景类型完全统一。

## 如何使用预设

```bash
# 查看所有可用预设
wechatwriter style list

# 查看预设详情（配色、边框、背景等）
wechatwriter style show morandi-flat

# 直接将预设名称作为 --style 参数
wechatwriter image generate "封面标题" --post --style morandi-flat
wechatwriter image generate "内容图" --post --style morandi-flat
```

预设名称（kebab-case 英文）直接传入 `--style`，系统自动解析为对应的完整提示词。未命中预设名称时，视为自由文本（向后兼容）。

## 内置预设一览

| 英文名 | 中文名 | 分类 | 适用场景 |
|--------|--------|------|---------|
| `morandi-flat` | 莫兰迪扁平 | 知识图卡 | 知识分享、教程、方法论 |
| `tech-dark` | 科技暗色 | 科技现代 | 科技、编程、AI、产品 |
| `warm-paper` | 暖色纸质 | 文艺清新 | 生活、文化、读书、感悟 |
| `minimal-white` | 极简白色 | 商务简约 | 职场、效率、商业、管理 |
| `nature-watercolor` | 自然水彩 | 自然手绘 | 旅行、美食、自然、治愈 |

## 各预设视觉特征

### morandi-flat — 莫兰迪扁平

**适用**：知识类小绿书、方法论拆解、步骤教程

**视觉特征**：

- 配色：低饱和蓝灰 + 暖棕 + 陶土橙，背景米白
- 边框：16px 圆角卡片
- 背景：浅米色到暖白色垂直渐变
- 字体：粗体无衬线标题

---

### tech-dark — 科技暗色

**适用**：科技资讯、编程技巧、AI 产品介绍

**视觉特征**：

- 配色：深夜蓝底 + 电蓝 + 青绿 + 霓虹紫，冷白文字
- 边框：12px 圆角 + 细边框发光效果
- 背景：深夜蓝纯色 + 顶部蓝色光晕
- 字体：粗体等宽/无衬线标题

---

### warm-paper — 暖色纸质

**适用**：生活感悟、读书笔记、文化探讨、个人随笔

**视觉特征**：

- 配色：深棕 + 暖赤 + 金黄，米黄纸色背景
- 边框：8px 圆角，不规则手绘风
- 背景：米黄纸张底色 + 细微纸纹理 + 轻微做旧
- 字体：粗体衬线标题

---

### minimal-white — 极简白色

**适用**：职场干货、效率工具、商业分析、管理方法

**视觉特征**：

- 配色：深炭灰 + 中灰，纯白背景
- 边框：4px 小圆角，极细线框或无边框
- 背景：纯白，大量留白
- 字体：特粗无衬线标题 + 细体正文强对比

---

### nature-watercolor — 自然水彩

**适用**：旅行打卡、美食探店、自然风光、治愈系内容

**视觉特征**：

- 配色：草绿 + 天蓝 + 珊瑚粉，薄荷白背景
- 边框：24px 大圆角，水彩晕染边
- 背景：薄荷白到淡绿柔和渐变 + 水彩纸纹理
- 字体：圆润粗体无衬线标题

## 如何创建自定义预设

在 `styles/` 目录新建 YAML 文件：

```yaml
# styles/my-preset.yaml
name: "我的预设"           # 必填：中文展示名
english_name: "my-preset"  # 必填：英文标识（用于 --style 参数）
description: "简短描述"
category: "自定义"

design:
  style: "设计风格"
  mood: "氛围基调"

colors:
  primary: "主色 #XXXXXX"
  secondary: "辅色 #XXXXXX"
  accent: "强调色 #XXXXXX"
  background: "背景色 #XXXXXX"
  text: "文字色 #XXXXXX"

border:
  style: "边框样式"
  radius: "圆角值"

background:
  type: "背景类型"
  description: "背景描述"

typography:
  heading: "标题字体"
  body: "正文字体"

prompt: |            # 必填：发送给图片模型的完整提示词
  具体的视觉风格描述，包含配色、边框、背景、字体等关键维度...
```

**注意**：`prompt` 字段是发送给图片模型（Gemini/DALL-E）的实际文本，应包含所有视觉维度的具体描述。其他结构化字段用于人类阅读和 CLI 展示。
