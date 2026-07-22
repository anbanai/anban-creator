# 二次优化判定清单

## Contents

- [调用语法](#调用语法)
- [5 项审计 prompt 模板](#5-项审计-prompt-模板)
- [1. 标题清楚度](#1-标题清楚度)
- [2. 构图还原度](#2-构图还原度)
- [3. 人物/主体突出度](#3-人物主体突出度)
- [4. 颜色统一度](#4-颜色统一度)
- [5. 元素精简度](#5-元素精简度)
- [综合评级](#综合评级)
- [评级落地](#评级落地)
- [Audit Target](#audit-target)
- [Decision](#decision)
- [重试判定规则](#重试判定规则)

本文档定义 Phase 4 步骤 6 审计封面生成结果时的 5 项判定标准，以及 `analyze_image` 的审计 prompt 模板。

## 调用语法

```
analyze_image(
  project_id="$PROJECT_ID",
  file_path="$COVER_SERVER_PATH",
  prompt=<下方完整审计模板>
)
```

## 5 项审计 prompt 模板

直接复制粘贴这段作为 `prompt` 参数：

```
审计这张刚生成的短视频封面，从以下 5 个维度逐项打分（PASS / MINOR / FAIL），每项附具体观察依据。最终汇总综合评级。

## 1. 标题清楚度
标准：标题文字（即使可能有 AI 生图的中文渲染瑕疵）整体可辨认，字号层级分明，主标题与副标题差异明显。
- PASS：标题层级清晰，主标题明显大于副标题，文字大致可读
- MINOR：层级在但字号差异不够，或部分字模糊但仍能猜出
- FAIL：标题模糊不可辨认，或字号层级完全混乱，或文字渲染严重失真

观察依据：<具体描述你看到的标题状态>

## 2. 构图还原度
标准：生成图的画面比例、标题位置、主体位置等构图逻辑，与 cover-plan.md 中的迁移决策对齐（不是与参考图对齐——参考只是输入）。
- PASS：构图逻辑完全对齐 cover-plan（比例、主体位置、标题位置）
- MINOR：核心位置对齐但有偏移（如主体本应居中偏左实际居中）
- FAIL：构图完全偏离（如本应 9:16 实际接近 1:1，或主体位置完全错误）

观察依据：<比对 cover-plan.md 的具体差异>

## 3. 人物/主体突出度
标准：人物或核心物体作为视觉重点清晰可辨，与背景有明确区隔，不被其他元素淹没。
- PASS：主体清晰突出，背景衬托到位，视觉重点明确
- MINOR：主体可辨但与背景对比不足，或被装饰元素分散注意力
- FAIL：主体被背景吞噬，或画面有多个竞争焦点，主体不清

观察依据：<具体描述主体与背景的关系>

## 4. 颜色统一度
标准：主色和强调色和谐统一，色彩情绪与账号领域匹配，无突兀杂色。
- PASS：色彩方案与 cover-plan 的 main_color / accent_color 一致，整体和谐
- MINOR：主色调对但有轻微饱和度/明度偏差，或出现 1-2 处不和谐色
- FAIL：色彩与计划完全不符，或出现明显冲突色，或色彩混乱无主次

观察依据：<比对 cover-plan.md 的色彩定义>

## 5. 元素精简度
标准：画面元素数量适度，每个元素都服务于视觉重点；无装饰性堆砌。
- PASS：元素精简，每个元素都有明确作用，视觉重点未被稀释
- MINOR：元素略多但不喧宾夺主，或个别装饰元素可去
- FAIL：元素过多过乱，视觉重点被稀释，画面嘈杂

观察依据：<列举画面中的主要元素并评估必要性>

## 综合评级

- 综合评级：PASS / MINOR / FAIL
- 关键问题（如有）：<列出最需要优化的 1-2 项>
- 重试建议（如 FAIL）：<针对最严重问题的具体 prompt 调整方向>
```

## 评级落地

`analyze_image` 返回的文本直接作为 `$DIR/cover-review.md` 的正文，文件开头加：

```markdown
# Cover Review

## Audit Target

- file: $DIR/cover.png
- analyzed_at: <时间戳>
- cover_server_path: $COVER_SERVER_PATH
- planned_against: $DIR/cover-plan.md

---

<analyze_image 返回的 5 项评级>

## Decision

- 综合评级: PASS / MINOR / FAIL
- 是否重试: <yes/no>
- 重试原因: <若 yes，关键 FAIL 项>
- 最终交付: cover.png / cover_v2.png
```

## 重试判定规则

**必须重试**（生成候选 v2）：
- 标题清楚度 = FAIL（标题是封面的灵魂）
- 人物/主体突出度 = FAIL
- 任 3 项以上 MINOR

**可选重试**：
- 构图还原度 = MINOR（且用户 reference_depth=deep）
- 颜色统一度 = MINOR（且账号领域对色彩敏感，如美妆）

**接受不再重试**：
- 全部 PASS / MINOR 且无 FAIL
- 重试 1 次后仍无改善（接受当前最佳，标注 `needs_manual_edit`）

## 重试 prompt 构建方向

针对不同 FAIL 项的 prompt 调整方向：

| FAIL 项 | 重试 prompt 调整 |
|---------|----------------|
| 标题不清楚 | 加强字体描述："extra-bold sans-serif, high contrast, no thin strokes"；加强反面约束："DO NOT render title with thin or incomplete strokes" |
| 构图偏离 | 显式声明位置："subject MUST be positioned center-left, occupying 50% of frame"；reference_depth=deep 时加 "Reference cover shows the visual logic to follow: ..." |
| 主体不突出 | 加强背景简化："minimal background with no competing elements"；加强主体描述："subject is the dominant focal point, all other elements are supporting only" |
| 颜色不统一 | 重写色彩描述用更具体的实物类比；加强反面约束："DO NOT introduce colors outside the main + accent palette" |
| 元素过多 | 删减 prompt 中的次要元素；加强反面约束："DO NOT include decorative elements that compete with the main subject" |

重试时 `output_path` 改为 `/tmp/anban-creator-short-video-cover/$TASK_ID/cover_v2.png`，重试结果走相同审计；若仍 FAIL 则接受当前最佳并标注 `needs_manual_edit`。

## 能力边界声明

审计报告必须包含能力边界声明（透明告知）：

```
## Capability Boundary

当前 generate_image 是参考图生成，不是 ControlNet 或专用封面排版工具：
- 中文文字渲染可能不精确，FAIL 标题清楚度时建议 PS 二次加工
- 构图位置是 best-effort，无法像素级锁定
- 二次优化是重新生成，不是"只改局部"
```
