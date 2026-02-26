# 微信公众号主题风格指南

## 主题概览

### AI 模式主题（推荐）

| 主题 | 命令参数 | 风格描述 | 适合内容 |
|------|----------|----------|----------|
| 🟠 **秋日暖光** | `--theme autumn-warm` | 温暖治愈，橙色调，文艺美学 | 情感故事、生活随笔 |
| 🟢 **春日清新** | `--theme spring-fresh` | 清新自然，绿色调，生机盎然 | 旅行日记、自然主题 |
| 🔵 **深海静谧** | `--theme ocean-calm` | 深邃冷静，蓝色调，理性专业 | 技术文章、商业分析 |
| ⚪ **自定义** | `--theme custom` | 使用自定义提示词 | 特殊需求 |

### 背景类型选择 🆕

除了主题，你还可以自定义背景样式：

| 背景类型 | 命令 | 效果 | 适合场景 |
|----------|------|------|----------|
| `default` | `--background-type default` | 默认背景（纯色或渐变） | 通用内容 |
| `grid` | `--background-type grid` | 网格纹理背景 | 技术文档、笔记类 |
| `none` | `--background-type none` | 无背景（透明） | 嵌入式内容 |

**使用示例：**

```bash
# 使用网格背景
wechatwriter convert article.md --theme elegant-gold --background-type grid

# 使用无背景
wechatwriter convert article.md --theme minimal-blue --background-type none

# 组合使用
wechatwriter convert article.md --theme focus-green --background-type grid --draft --cover cover.jpg
```

**配置文件设置：**

```yaml
# ~/.config/wechatwriter/config.yaml
api:
  background_type: grid  # default/grid/none
```

---

## 主题详情

### autumn-warm（秋日暖光）

#### 整体感觉

温暖治愈、橙色调、文艺美学，适合情感表达和生活方式内容。

#### 配色方案

```css
主背景: #faf9f5 (暖白)
文字色: #4a413d (深褐灰)
主强调色: #d97758 (秋日暖橙)
副强调色: #c06b4d (橙红)
引用背景: #fef4e7 (淡橙)
```

#### 设计特点

- 卡片式布局，米白方格纹理
- 圆角 18px，柔和阴影
- 标题使用 ▶ 符号 + 暖橙文字
- 引用块带内阴影和暖橙左边框

#### 提示词关键词

```
温暖治愈、秋日暖光、橙色调、文艺美学、卡片布局、柔和光效
```

#### 生成指令示例

```
使用秋日暖光主题生成微信公众号 HTML：
- 暖白背景 #faf9f5，深褐灰文字 #4a413d
- 主强调色 #d97758（秋日暖橙）
- 卡片式布局，圆角 18px
- 标题使用 ▶ 符号
- 所有 CSS 必须内联（style 属性）
- 图片使用占位符 <!-- IMG:index -->
```

---

### spring-fresh（春日清新）

#### 整体感觉

清新自然、绿色调、生机盎然，适合户外和自然主题。

#### 配色方案

```css
主背景: #f5f8f5 (淡绿)
文字色: #3d4a3d (深绿灰)
主强调色: #6b9b7a (春日嫩绿)
副强调色: #4a8058 (草地翠绿)
引用背景: #e8f0e8 (淡绿)
```

#### 设计特点

- 清新点状纹理背景
- 圆角 16px，清新阴影
- 标题使用 ❀ 符号 + 绿色文字
- 引用块带清新绿色调

#### 提示词关键词

```
清新自然、春日花园、绿色调、生机盎然、点状纹理、清新阴影
```

#### 生成指令示例

```
使用春日清新主题生成微信公众号 HTML：
- 淡绿背景 #f5f8f5，深绿灰文字 #3d4a3d
- 主强调色 #6b9b7a（春日嫩绿）
- 清新点状纹理背景
- 标题使用 ❀ 符号
- 所有 CSS 必须内联
- 图片使用占位符 <!-- IMG:index -->
```

---

### ocean-calm（深海静谧）

#### 整体感觉

深邃冷静、蓝色调、理性专业，适合技术和商业内容。

#### 配色方案

```css
主背景: #f0f4f8 (淡蓝)
文字色: #3a4150 (深蓝灰)
主强调色: #4a7c9b (深海蔚蓝)
副强调色: #3d6a8a (静谧石蓝)
引用背景: #e8f0f8 (淡蓝)
```

#### 设计特点

- 淡蓝网格纹理背景
- 圆角 14px，深邃阴影
- 标题使用 ◆ 符号 + 蓝色文字
- 引用块带静谧蓝色调

#### 提示词关键词

```
深海静谧、理性专业、蓝色调、网格纹理、深邃蓝调、清晰层次
```

#### 生成指令示例

```
使用深海静谧主题生成微信公众号 HTML：
- 淡蓝背景 #f0f4f8，深蓝灰文字 #3a4150
- 主强调色 #4a7c9b（深海蔚蓝）
- 淡蓝网格纹理背景
- 标题使用 ◆ 符号
- 所有 CSS 必须内联
- 图片使用占位符 <!-- IMG:index -->
```

---

### custom（自定义）

#### 用途

允许用户提供自定义提示词，实现完全个性化的排版风格。

#### 使用方法

```bash
wechatwriter convert article.md --custom-prompt "你的自定义提示词"
```

#### 自定义提示词模板

```
请将以下 Markdown 转换为微信公众号 HTML：

配色要求：
- 主色：#your_color
- 副色：#your_color
- 背景：#your_color

字体要求：
- 字号：16px
- 行高：1.8
- 字体：系统默认无衬线

技术要求：
1. 所有 CSS 必须内联（style 属性）
2. 使用安全的 HTML 标签
3. 图片使用占位符 <!-- IMG:index -->
4. 不使用外部样式表

请转换以下 Markdown内容：
```

---

## 通用技术规范（所有 主题）

### 容器结构（关键）

```html
<!-- 必须在 body 后立即创建主容器 -->
<div style="background-color: #xxx; padding: 40px 10px; letter-spacing: 0.5px;">
  <!-- 所有内容放在这里 -->
</div>
```

### 卡片结构

```html
<section style="max-width: 800px; padding: 25px; background: #fff; border-radius: 16px;">
  <!-- 内容 -->
</section>
```

### 安全 HTML 标签

```html
section, p, span, strong, em, u, a, h1-h6, ul, ol, li,
blockquote, pre, code, table, thead, tbody, tr, th, td,
img, br, hr
```

### 图片占位符格式

```html
<!-- IMG:0 -->
<!-- IMG:1 -->
<!-- IMG:2 -->
```

---

## 主题选择建议

| 内容类型 | 推荐主题 | 理由 |
|----------|----------|------|
| 情感故事、生活随笔 | autumn-warm | 温暖色调营造情感氛围 |
| 旅行日记、自然主题 | spring-fresh | 清新绿色契合自然主题 |
| 技术文章、商业分析 | ocean-calm | 专业蓝色调传达可信感 |
| 品牌定制内容 | custom | 完全自定义风格 |

---

## 切换主题

```bash
wechatwriter convert article.md --theme autumn-warm --preview
```

### 方法 2: 通过 Skill

在 Claude Code 中直接说：

```
请用秋日暖光主题将 article.md 转换为微信公众号格式
```

---

## 主题文件位置

主题配置文件位于 `themes/` 目录：

- `themes/autumn-warm.yaml` - 秋日暖光
- `themes/spring-fresh.yaml` - 春日清新
- `themes/ocean-calm.yaml` - 深海静谧
- `themes/custom.yaml` - 自定义

## 完整主题切换示例

### 秋日暖光（情感故事）

```markdown
# 那些年，我们一起追过的梦

还记得青春年少时...

![怀旧照片](./photos/old-days.jpg)
```

```bash
wechatwriter convert story.md --theme autumn-warm --preview
```

### 春日清新（旅行日记）

```markdown
# 春日游园记

三月的公园，花开正盛...

![公园美景](./photos/spring-park.jpg)
```

```bash
wechatwriter travel.md --theme spring-fresh --preview
```

### 深海静谧（技术文章）

```markdown
# 微服务架构最佳实践

在现代应用开发中...

![架构图](./diagrams/architecture.png)
```

```bash
wechatwriter tech-post.md --theme ocean-calm --preview
```
