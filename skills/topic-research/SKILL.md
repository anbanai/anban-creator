---
name: topic-research
description: Use when researching topics, scoring engagement potential, or generating content outlines. Triggers on "选题", "评分", "大纲", "outline", "score", "热点分析", "话题".
user-invocable: false
---

# 微信公众号选题分析工具

## 核心功能

### 0. 查看已有内容（选题前必做）

在开始选题前，先检查已有内容，避免重复主题：

```bash
# 查看草稿箱和已发布文章（统一视图）
anbanwriter account history

# 可指定获取更多条目
anbanwriter account history --count 10
```

列出所有已有标题后，选题应避开这些已有主题。

### 1. 话题评分

评估话题的爆款潜力：

```bash
anbanwriter score -i metrics.json -t "主题" -d [领域]
```

**标志**: `-i/--input`（数据文件）, `-t/--topic`（话题）, `-d/--domain`（领域）

### 2. 内容框架生成

基于话题生成内容框架：

```bash
anbanwriter outline -t "主题" --template [模板] -d [领域] -s [风格] -k [关键词]
```

**标志**: `-t/--topic`, `--template`, `-d/--domain`, `-s/--style`, `-k/--keywords`

**可用模板**: authoritative(权威), comparison(对比), cultural(文化), practical(实用)
**可用领域**: general, tea, tech, lifestyle, culture, business, education

**模板详细说明**: 参见 [`references/outline-templates.md`](references/outline-templates.md)

## 命令帮助

```bash
# 话题评分
anbanwriter score --help

# 内容框架生成
anbanwriter outline --help
```

## 注意事项

- 评分结果仅供参考，需结合实际情况判断
- 建议结合多个话题进行对比分析
