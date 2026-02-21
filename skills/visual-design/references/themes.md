# 微信公众号主题风格指南

## 可用主题

| 主题 | 命令参数 | 配色 | 适合内容 |
|------|----------|------|----------|
| 秋日暖光 | `--theme autumn-warm` | 暖白 #faf9f5 / 暖橙 #d97758 | 情感故事、生活随笔 |
| 春日清新 | `--theme spring-fresh` | 淡绿 #f5f8f5 / 嫩绿 #6b9b7a | 旅行日记、自然主题 |
| 深海静谧 | `--theme ocean-calm` | 淡蓝 #f0f4f8 / 蔚蓝 #4a7c9b | 技术文章、商业分析 |
| 自定义 | `--theme custom` | 用户自定义 | 特殊需求 |

## 主题配色详情

### autumn-warm（秋日暖光）

- 主背景: #faf9f5（暖白）/ 文字: #4a413d（深褐灰）
- 强调色: #d97758（暖橙）/ 副强调: #c06b4d（橙红）
- 卡片式布局，圆角 18px，标题使用 ▶ 符号

### spring-fresh（春日清新）

- 主背景: #f5f8f5（淡绿）/ 文字: #3d4a3d（深绿灰）
- 强调色: #6b9b7a（嫩绿）/ 副强调: #4a8058（翠绿）
- 点状纹理背景，圆角 16px，标题使用 ❀ 符号

### ocean-calm（深海静谧）

- 主背景: #f0f4f8（淡蓝）/ 文字: #3a4150（深蓝灰）
- 强调色: #4a7c9b（蔚蓝）/ 副强调: #3d6a8a（石蓝）
- 网格纹理背景，圆角 14px，标题使用 ◆ 符号

### custom（自定义）

```bash
wechatwriter convert article.md --custom-prompt "你的自定义提示词"
```

## 主题选择建议

| 内容类型 | 推荐主题 | 理由 |
|----------|----------|------|
| 情感故事、生活随笔 | autumn-warm | 温暖色调营造情感氛围 |
| 旅行日记、自然主题 | spring-fresh | 清新绿色契合自然主题 |
| 技术文章、商业分析 | ocean-calm | 专业蓝色调传达可信感 |
| 品牌定制内容 | custom | 完全自定义风格 |

## 切换主题

```bash
# 通过命令行参数指定主题
wechatwriter convert article.md --theme autumn-warm --preview

# 或在 Claude Code 中直接说
# "请用秋日暖光主题将 article.md 转换为微信公众号格式"
```

## 通用技术规范

- 所有 CSS 必须内联（style 属性），不使用外部样式表
- 安全标签：section, p, span, strong, em, h1-h6, ul, ol, li, blockquote, pre, code, table, img, br, hr
- 图片使用占位符 `<!-- IMG:index -->`

## 主题文件位置

主题配置文件位于 `themes/` 目录：

- `themes/autumn-warm.yaml` - 秋日暖光
- `themes/spring-fresh.yaml` - 春日清新
- `themes/ocean-calm.yaml` - 深海静谧
- `themes/custom.yaml` - 自定义
