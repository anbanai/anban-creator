# article-publishing Examples

### Case 1: 带封面的正式草稿

- Input: 已有 article.html、cover.png、author 字段和 digest。
- Recommended path: 上传/绑定封面，创建 WeChat 草稿，author 只用 profile 顶层 author。
- Artifacts: draft.json、publish-report.md。
- Quality gate: 草稿 thumb_media_id 存在，正文 HTML 无 script/iframe/style/link 等 unsafe 标签。

### Case 2: text_only 草稿

- Input: 文章流水线关闭所有图片。
- Recommended path: 创建不带 thumb_media_id 的图文草稿，报告提醒后台展示限制。
- Artifacts: draft.json、final-review.md。
- Quality gate: 不得把缺封面判失败；digest 和正文必须完整。

### Case 3: 发布前返工

- Input: 草稿创建前发现正文多张图共用同一 URL。
- Recommended path: 停止发布，回到 article-visual-design 重新生成/上传正文图。
- Artifacts: publish-blocker.md、images.json。
- Quality gate: 正文图片 URL 去重后数量必须等于正文图片数量。
