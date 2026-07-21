---
name: seo-optimization
description: Use when optimizing WeChat article titles, keywords, digests, CTR variants, or search-facing metadata.
---

# 微信公众号 SEO 优化


## 案例库

遇到场景分支、产物格式或质量边界不确定时，先读 [references/examples.md](references/examples.md)。

## 图片比例固定规则

本 Skill 只要涉及生成、选择、裁切、校验或引用图片，必须按以下优先级决定画面比例：

1. 用户/任务明确指定的 `image_ratio`、`size` 或平台规格优先。
2. 项目/频道默认比例次之。
3. 业务默认比例只作兜底：微信文章封面/正文图默认 `16:9`；Seednote/XLS/移动信息流默认 `3:4`；电商、广告投放、视频封面按具体平台素材位要求执行。
4. 不得从模型路由、供应商默认 `size` 或模型能力反推业务比例；模型只决定能力和成本，比例属于创作场景约束。

## Intent Routing

Use this Skill after the final article draft exists. SEO judgment, title variants, keyword layout, digest writing, and CTR scoring happen inside the Skill. MCP is not used for SEO generation.

## Discovery First

Read these local artifacts first:

1. `$DIR/04-article-final.md` if present; otherwise `$DIR/03-article.md`.
2. `$DIR/context-brief.md` and `$DIR/01-research.md` for user intent, positioning, chosen topic, and historical differentiation.
3. `$DIR/02-outline.md` for section logic and SEO seed keywords.
4. `get_project_profile(project_id, scope="article", task_id?)` only when project keywords, audience, or writer context is missing from local files.

## Configuration Boundaries

- SEO changes must not alter factual claims, promised value, compliance boundaries, or article positioning.
- Keywords are placed naturally; no keyword stuffing, fake recency, or unsupported authority claims.
- Title/digest must stay consistent with the article body and content-quality report.
- CTR optimization is subordinate to compliance. Any variant with exaggerated, absolute, deceptive, or welfare-bound wording is rejected.

## Output Contract

Write `$DIR/seo-result.md` with:

```markdown
## SEO 优化方案

### 关键词
- 核心关键词：...
- 长尾关键词：...
- 语义关键词：...

### 标题优化
- 原标题：...
- 最终标题：...
- 优化理由：...

### 摘要
50-120 字 digest，前半句给利益/悬念/反差，后半句落核心价值与关键词。

### CTR 变体评分
| 标题 | 公式 | 好奇心 | 情绪 | 数字/具体性 | 合规 | 核心词前置 | 字数 | 总分 | 结论 |
|---|---|---:|---:|---:|---:|---:|---:|---:|---|

### 内容建议
- 关键词布局：...
- 需要回写正文的建议：无 / 列表
```

## Optimization Protocol

1. Extract 1-2 core keywords, 3-6 long-tail keywords, and related semantic words.
2. Check whether the current title, opening, H2s, and ending naturally carry the core topic.
3. Generate at least 3 title variants with different formulas: number, pain point, contrast, curiosity, benefit, or empathy.
4. Score each variant from 0-2 on curiosity gap, emotional strength, specificity, compliance, core keyword front-loading, and 22-25 character fit.
5. Select the highest compliant title. If no compliant variant wins, keep the safest title and explain why.
6. Write the digest and keyword plan into `$DIR/seo-result.md`.

## Failure Handling

- Missing final article: stop and request the content-writing output.
- Title conflicts with content-quality report: revise the title/digest, not the report.
- All CTR variants are risky: keep a conservative keyword title and record rejected risks.
- Article body needs keyword adjustment: list recommended edits; do not silently rewrite the article body unless the caller explicitly routes back to content-writing.

## 参考文档

- 平台标题规范：[title-guidelines.md](references/title-guidelines.md)
