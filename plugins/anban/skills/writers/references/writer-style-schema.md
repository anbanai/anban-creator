# 自定义写作风格

## Contents

- [快速开始](#快速开始)
  - [1. 创建风格配置文件](#1-创建风格配置文件)
  - [2. 基础结构](#2-基础结构)
  - [3. 使用新风格](#3-使用新风格)
- [完整配置项说明](#完整配置项说明)
  - [必需字段](#必需字段)
  - [可选字段](#可选字段)
- [writing_prompt 编写指南](#writingprompt-编写指南)
  - [基本结构](#基本结构)
  - [示例：简洁风格](#示例简洁风格)
- [风格文件位置](#风格文件位置)
- [完整示例](#完整示例)

想模仿哪位作家或创作者的风格？可以轻松添加到 anban-creator。

## 快速开始

### 1. 创建风格配置文件

在 `writers/` 目录下创建一个新的 YAML 文件，文件名用英文小写：

```
writers/
├── dan-koe.yaml       # 内置示例
├── my-style.yaml      # 你的自定义风格 ← 新建
└── another-style.yaml # 可以添加多个
```

### 2. 基础结构

```yaml
# writers/my-style.yaml
name: "风格名称"
english_name: "my-style"  # 英文标识，用于命令行
category: "分类"
description: "一句话描述这个风格"
version: "1.0"

# 核心写作 DNA（可选）
core_beliefs:
  - "核心理念1"
  - "核心理念2"

# AI 写作提示词（必需）
writing_prompt: |
  你是[角色描述]...
  请将用户的内容，用[风格名]的风格重新演绎。
```

### 3. 使用新风格

配置文件创建后，通过项目配置和 content-writing Skill 可用：

- 在项目中选择该 writer key，content-writing Skill 会按项目配置读取
- 自然语言："用 my-style 风格写一篇文章"

---

## 完整配置项说明

### 必需字段

| 字段 | 说明 | 示例 |
|------|------|------|
| `name` | 风格中文名称 | `"鲁迅"` |
| `english_name` | 英文标识，用于命令行 | `"luxun"` |
| `writing_prompt` | AI 写作提示词 | 见下方详细说明 |

### 可选字段

| 字段 | 说明 |
|------|------|
| `category` | 分类，如"现代/当代/外国" |
| `description` | 风格描述 |
| `version` | 版本号 |
| `core_beliefs` | 核心信念列表 |
| `writing_style` | 写作风格定义 |
| `title_formulas` | 标题公式库 |
| `quote_templates` | 金句模板 |

> **关于封面/视觉**：writer **不再携带任何视觉身份**（曾经的 `cover_prompt`/`cover_style`/`cover_mood` 字段已移除）。图片视觉是与写作者**正交**的独立维度，由项目/任务各自的 `visual_style`（视觉）字段承载，在任务运行时按 `task > project` 两层解析。这样切断了"writer key 泄漏为图片风格"的 bug（如 dan-koe → 维多利亚木刻）。

---

## writing_prompt 编写指南

这是最重要的字段，它决定了 AI 生成文章的风格。

### 基本结构

```yaml
writing_prompt: |
  你是[角色描述]。

  ## 核心写作 DNA
  1. [核心理念1]
  2. [核心理念2]

  ## 文章结构
  1. 开头：[说明]
  2. 中段：[说明]
  3. 结尾：[说明]

  ## 格式规范
  - **粗体**：[用途]
  - *斜体*：[用途]

  ## 语气要求
  ✅ [要做的]
  ❌ [不要做的]

  请将用户的内容，用[风格名]的风格重新演绎。
```

### 示例：简洁风格

```yaml
writing_prompt: |
  你是海明威，以简洁有力的风格写作。

  核心原则：
  - 只写事实，不要形容词
  - 短句为主，每句不超过15字
  - 用动作和对话表达，不要直接说情绪

  格式：
  - 段落简短
  - 避免修辞
  - 直接陈述

  请将用户的内容，用海明威的冰山理论风格重新演绎。
```

---

## 风格文件位置

Anban Creator 会按以下顺序查找风格文件：

1. `./writers/` - 当前项目目录
2. `~/.config/anban-creator/writers/` - 用户配置目录
3. `~/.anban-creator/writers/` - 用户主目录

---

## 完整示例

参考 `writers/dan-koe.yaml` 查看完整的配置示例。

---

## 常见问题

### Q: 风格不生效？

A: 检查以下几点：

1. 文件名是 `.yaml` 或 `.yml` 后缀
2. `english_name` 字段已填写
3. 文件在正确的目录中

### Q: 如何测试新风格？

A: 在项目配置中选择该 writer key，创建一篇测试文章，检查 `$DIR/03-article.md` 是否体现目标风格。

### Q: 可以分享我的风格吗？

A: 当然！欢迎提交 PR 到项目，让更多人使用你的风格。
