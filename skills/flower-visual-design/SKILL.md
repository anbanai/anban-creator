---
name: flower-visual-design
description: Generates flower arrangement image series with sequential reference chain for visual consistency. Use when creating multi-image flower series with style coherence.
---

# 花卉图片视觉一致性生成

## 核心 Gotcha

花卉图片系列中每张花的 prompt 不同（不同花种），因此**不能用 `--count` 批量模式**（该模式适用于同一 prompt 的多张变体）。必须逐张生成，但通过 `--ref` 参考链保持风格一致。

---

## 参考链流程

```
第1张（首图）：不使用 --ref，用完整 $STYLE 描述确立基准
    → 生成 flower_01_[name].png

第2张起：使用第1张作为 --ref
    → anbanwriter image generate "PROMPT" --size 9:16 --style "$STYLE" --ref ./flower_01_[name].png

第3张及以后：继续使用第1张（基准图）作为 --ref
    → 不要用上一张，始终用第1张保持风格基准稳定
```

**为什么始终用第1张**：用上一张会导致风格漂移（每张图的微小差异累积放大），第1张是风格锚点。

---

## CLI 命令

```bash
# 第1张（首图，无参考图）
anbanwriter image generate "PROMPT_1" --size 9:16 --style "STYLE_DESC" -o ./flower_01_peony.png

# 第2张起（以首图为参考）
anbanwriter image generate "PROMPT_2" --size 9:16 --style "STYLE_DESC" --ref ./flower_01_peony.png -o ./flower_02_rose.png

# 用户提供参考图时（所有图片统一使用）
anbanwriter image generate "PROMPT_1" --size 9:16 --style "STYLE_DESC" --ref ./user_ref.png -o ./flower_01_peony.png
```

**参数**：
- `--size 9:16`：竖版，适合花卉摄影
- `--style "STYLE_DESC"`：全局风格（色调、光线、氛围），每张保持一致
- `--ref`：参考图（始终用第1张）
- 命名规范：`flower_序号_花名.png`（如 `flower_01_peony.png`）

---

## 风格描述设计

`$STYLE` 需涵盖三个维度：

```
[氛围关键词], [光线描述], [构图原则], 9:16 portrait format
```

示例：
- 雨后清新：`post-rain atmosphere, water films on petals, soft diffused light, varied composition, 9:16`
- 晨雾空灵：`morning mist, low fog layers, pollen particles in air, varied negative space, 9:16`
- 黄金时刻：`golden hour backlight, rim-lit petal edges, warm shadows, dynamic composition, 9:16`

见 [flower-content-design](../flower-content-design/SKILL.md) 中的完整 prompt 模板和花卉调研指南。
