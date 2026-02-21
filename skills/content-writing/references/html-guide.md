# 微信公众号 HTML 规范

## 安全标签清单

| 标签 | 属性限制 | 说明 |
|------|----------|------|
| `section` | style | 容器标签，推荐用于整体包裹 |
| `p` | style | 段落 |
| `span` | style | 内联容器 |
| `strong` | style | 加粗 |
| `em` | style | 斜体 |
| `u` | style | 下划线 |
| `a` | style, href | 链接（href 仅支持 https） |
| `h1` - `h6` | style | 标题 |
| `ul`, `ol` | style | 列表 |
| `li` | style | 列表项 |
| `blockquote` | style | 引用块 |
| `pre` | style | 预格式化文本 |
| `code` | style | 代码 |
| `table` | style | 表格 |
| `thead`, `tbody` | style | 表头/表体 |
| `tr`, `th`, `td` | style | 表格行/单元格 |
| `br` | - | 换行 |
| `img` | src, style, alt | 图片（src 必须是微信域名） |
| `hr` | style | 分割线 |

## 禁止使用的标签

script, noscript, iframe, form, input, button, textarea, select, object, embed, video, audio, style, link, meta

## 允许的 CSS 属性

**文字**: color, font-size, font-weight, font-style, font-family, line-height, letter-spacing, text-align, text-decoration, text-indent

**背景**: background-color, background-image（仅限 https 图片）

**边框**: border, border-left/right/top/bottom, border-radius

**间距**: margin, margin-top/bottom/left/right, padding, padding-top/bottom/left/right

**尺寸**: width, max-width, min-width, height, max-height, min-height

**定位**: display（仅 block/inline-block/none）, float（仅 left/right/none）, clear, overflow

**阴影**: box-shadow, text-shadow

## 禁止的 CSS 属性

- position: absolute/fixed/sticky
- flexbox, grid, transform
- transition, animation, @keyframes
- filter, clip-path, backdrop-filter

## 关键注意事项

### 主容器结构（必须）

微信编辑器会剥离 `<body>` 标签的样式，必须在 `<body>` 后创建主容器承载全局样式：

```html
<body>
  <div style="background-color: #faf9f5; padding: 40px 10px; letter-spacing: 0.5px;">
    <section style="max-width: 800px; margin: 0 auto;">
      ...
    </section>
  </div>
</body>
```

### 段落文字颜色（必须）

微信编辑器会强制重置 `<p>` 标签的颜色为黑色，必须为每个 `<p>` 明确指定颜色：

```html
<!-- 错误：颜色会被重置 -->
<p>文字颜色不确定</p>

<!-- 正确 -->
<p style="color: #4a413d;">指定颜色</p>
```

## 内容限制

| 限制项 | 限制值 |
|--------|--------|
| 标题长度 | 64 字符 |
| 摘要长度 | 120 字符 |
| 正文长度 | 建议 2000-10000 字符 |
| 单张图片大小 | < 5MB |
| 图片总数量 | < 100 张 |
| 外链数量 | 建议 < 10 个 |
