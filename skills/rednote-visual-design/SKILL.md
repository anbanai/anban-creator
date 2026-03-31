---
name: rednote-visual-design
description: Generates cover and content images for Xiaohongshu (小红书) posts with 3:4 ratio design norms. Use when creating rednote visual content including covers, content pages, and tail pages.
---

# 小红书图片生成

## 平台 Gotcha

小红书图片是 **3:4 竖版**，强视觉驱动。封面决定点击率，内容图决定完读率，尾图决定互动率。三类图片目标不同，prompt 构建方式也不同。

---

## 视觉风格设计原则

风格无固定预设，每次根据账号定位和内容动态设计：

**三个维度定调**：
1. **账号定位** — 知识干货型（专业简洁，结构感强）/ 生活美学型（温暖氛围，情绪感强）/ 娱乐趣味型（活泼鲜艳，夸张对比）
2. **内容主题** — 美食/旅行/家居/时尚各有视觉惯例，参考主题的典型配色和构图
3. **目标受众** — 年龄层、消费力影响配色（年轻用户偏饱和鲜艳；成熟用户偏质感低饱和）

**封面与内容图的一致性**：
- 无配置参考图时：先生成封面确立基准风格 → 以封面 `--ref` 批量生成内容图
- 有配置参考图时：所有图片统一使用配置参考图

---

## 封面设计规范

见 [references/cover.md](references/cover.md)

---

## 内容图设计规范

见 [references/content.md](references/content.md)

---

## 尾图设计规范

见 [references/tail.md](references/tail.md)

---

## CLI 命令

```bash
# 生成封面（单张）
anbanwriter image generate "{prompt}" --mode xhs --cover -o ./cover.png

# 批量生成内容图（N-2 张，不含尾图）
anbanwriter image generate "{paged_prompt}" --mode xhs --count N-2 -o ./

# 单独生成尾图
anbanwriter image generate "{tail_prompt}" --mode xhs --ref ./cover.png -o ./tail.png

# 带参考图（保持风格一致）
anbanwriter image generate "{prompt}" --mode xhs --cover --ref ./cover.png -o ./cover.png
anbanwriter image generate "{paged_prompt}" --mode xhs --count N-2 --ref ./cover.png -o ./

# 带风格描述
anbanwriter image generate "{prompt}" --mode xhs --cover --style "手绘感，暖色调，小清新" -o ./cover.png
```

**关键规则**：内容图必须用 `--count` 批量生成，尾图单独生成（`tail.png`），封面单独生成（`cover.png`）。`--mode xhs` 自动使用 3:4:1K 规格。
