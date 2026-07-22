# Cover Effectiveness Reference

## Purpose

Use this reference before generating a WeChat article cover. The cover is not only a style anchor; it is the article's thumbnail promise. Style can be controlled by prompt, but content relevance and click motivation must be proven before image generation.

## Method Sources

- **NN/g information scent**: the title, digest, and cover must give the reader a clear expectation of what they will get after clicking.
- **Google ABCD**: translate Attention / Brand / Connection / Direction into WeChat cover checks: stop the eye, fit the account, connect to the reader's pain, and point to the article promise.
- **YouTube thumbnail/title guidance**: be accurate but attractive（准确但有吸引力）; the thumbnail and title should reinforce each other without clickbait.
- **CMI content marketing**: start from 明确受众, useful relevance, and an action-driving promise.

## Build `cover_strategy`

Read `context-brief.md`, `seo-result.md`, `04-article-final.md`, and digest. Write this before any image generation:

```json
{
  "target_reader": "具体读者画像，不写泛泛的公众号读者",
  "reader_pain_or_job": "读者正在试图解决的问题或要完成的任务",
  "article_promise": "本文承诺帮读者看清/避免/获得什么",
  "content_proof_points": ["正文原句或明确场景证据 1", "证据 2"],
  "click_trigger": "让目标读者愿意点开的冲突、反差、利益或恐惧",
  "cover_concept_candidates": [
    {
      "concept": "候选概念 A",
      "title_hook": "它如何回应标题",
      "digest_promise": "它如何回应摘要",
      "body_evidence": "它来自哪句正文/哪个场景",
      "reader_click_reason": "目标读者为什么会停下",
      "visual_entities": ["必须能画出来的实体"],
      "mislead_risk": "可能误导之处",
      "swap_risk": "是否能换到其他文章仍成立"
    }
  ],
  "selected_cover_concept": "三项硬测试全过且评分最高的概念"
}
```

## Three Concept Gate

Generate at least 3 `cover_concept_candidates` before building the image prompt. Do not draw first. Score each candidate against the title, digest, body evidence, reader motivation, visual specificity, and truthfulness. Select one only after the tests below pass.

## Hard Tests

- `generic_swap_test`: fail if the cover could be moved to any generic methodology article and still make sense. A vague boat, mountain, empty road, fog, abstract doorway, or calm landscape usually fails unless the article itself is specifically about that object.
- `promise_proof_test`: pass only when the cover's subject, conflict, or short text can be traced to `digest_hook`, `article_promise`, or exact `content_proof_points`.
- `audience_motivation_test`: pass only when the `target_reader` can understand within 0.5 seconds, "this is about my problem."

All three hard tests must pass. If any one fails, rewrite `cover_strategy` and choose a new concept.

## `cover_effectiveness_scorecard`

Use high / medium / low for soft dimensions and booleans for hard tests:

```json
{
  "information_scent_alignment": "high|medium|low",
  "audience_motivation": "high|medium|low",
  "content_specificity": "high|medium|low",
  "thumbnail_attention": "high|medium|low",
  "truthfulness_not_clickbait": "high|medium|low",
  "brand_style_fit": "high|medium|low",
  "visual_distinctiveness": "high|medium|low",
  "safe_zone_text_policy": "high|medium|low",
  "generic_swap_test": true,
  "promise_proof_test": true,
  "audience_motivation_test": true,
  "overall_pass": true
}
```

`overall_pass=true` only when all three hard tests are true and all soft dimensions are at least medium. **仅有旧的 6 维 vision 全 high 不得通过**; that proves style/shape quality, not click relevance.

## Failure Handling

- Missing `cover_strategy`, `cover_concept_candidates`, or `cover_effectiveness_scorecard` blocks publishing.
- `cover_effectiveness_scorecard.overall_pass=false` blocks publishing even if `visual_quality_scorecard.overall_pass=true`.
- Vision JSON type mismatch, verification timeout, or missing effectiveness fields means the image is not approved. Do not manually `upload_image` and continue; re-run verification or rebuild the concept.
- 缺 `viral-audit.md` 不得发布. `viral-audit.md` must read `cover-prompt.md` and judge visual stay by relevance and click promise, not by style consistency alone.

## Current Failed Example

Article: `企业做 AI 内容，别从买工具开始`

- Bad concept: `静水孤舟` / 远山方向. It fails `generic_swap_test` because it could fit almost any "先辨方向" essay, weakly signals the business reader's pain, and does not carry the title's AI tooling conflict.
- Better concept territory: `散落 AI 工具年卡`, `空白选题库`, `SOP 流程线`, `阅读对折`, or a desk scene where bought tools are visible but the workflow/selection system is missing.
- Even in a Chinese ink-wash style, the cover should draw the business conflict: tools bought first, process absent, reader outcomes falling. The style follows the strategy; it does not replace it.
