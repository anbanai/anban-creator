# article Examples

### Case 1: 企业创始人观点稿

- Input: 输入是“AI 销售团队怎么落地”，项目已有科技类公众号定位。
- Recommended path: 按 topic-research 找争议点，content-writing 写观点主线，article-viral-strategy 强化开头冲突，视觉模式用 cover_and_content。
- Artifacts: 01-research.md、02-outline.md、04-article-final.md、visual-rhythm-plan.md、draft.json。
- Quality gate: 不得直接回答“怎么落地”；必须完成草稿链路，封面和正文图按 article_image_mode 判定。

### Case 2: 纯文字深度长文

- Input: 输入指定 text_only，主题是“消费降级下的品牌信任”。
- Recommended path: 跳过封面和正文配图生成，只保留视觉节奏说明与排版渲染，发布草稿不带 thumb_media_id。
- Artifacts: 04-article-final.md、05-seo.md、article.html、draft.json、final-review.md。
- Quality gate: 缺 cover.png、images.json 不算失败；final-review 说明后台可能需手动设置封面。

### Case 3: 内容图开启但封面关闭

- Input: 输入指定 content_only，用户已有外部封面但要正文插图。
- Recommended path: 不得生成 cover.png；正文图逐张独立生成并上传，禁止 ref 指向不存在的 cover.png。
- Artifacts: image-plan.md、images.json、article-with-images.html、draft.json。
- Quality gate: 每个正文 img 使用独立 wechat_url，不能复用封面 URL 或多图共用 URL。
