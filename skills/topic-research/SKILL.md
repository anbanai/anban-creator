---
name: topic-research
description: 微信公众号选题分析工具，提供热点评估和数据驱动的选题建议。Use when user says "选题"、"评分"、"大纲"、"outline"、"score"、"热点分析"。
user-invocable: false
metadata:
  author: rick
  version: 2.3.0
---

# 微信公众号选题分析工具

## 核心功能

### 1. 话题评分

评估话题的爆款潜力：

```bash
wechatwriter score -i metrics.json -t "主题" -d [领域]
```

**标志**: `-i/--input`（数据文件）, `-t/--topic`（话题）, `-d/--domain`（领域）, `-o/--output`（输出格式: json/text）

### 2. 内容框架生成

基于话题生成内容框架：

```bash
wechatwriter outline -t "主题" --template [模板] -d [领域] -s [风格] -k [关键词]
```

**标志**: `-t/--topic`, `--template`, `-d/--domain`, `-s/--style`, `-k/--keywords`, `-o/--output`（输出格式: json/text）

**可用模板**: authoritative(权威), comparison(对比), cultural(文化), practical(实用)
**可用领域**: general, tea, tech, lifestyle, culture, business, education

## 命令帮助

```bash
# 话题评分
wechatwriter score --help

# 内容框架生成
wechatwriter outline --help
```

## 注意事项

- 评分结果仅供参考，需结合实际情况判断
- 建议结合多个话题进行对比分析
