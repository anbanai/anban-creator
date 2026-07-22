# short-video-cover Examples

### Case 1: 轻度参考封面

- Input: 用户给参考封面和新标题“3 个剪辑误区”。
- Recommended path: 只学色彩/字体层级，构图和主体重新设计。
- Artifacts: reference-analysis.md、cover-prompt.md、cover.png。
- Quality gate: reference_depth=light 时 prompt 不要求参考构图。

### Case 2: 深度参考但换语义

- Input: 参考封面是人物居中+大字，新主题是职场表达。
- Recommended path: 保留主体位置逻辑，替换人物、背景和装饰语义。
- Artifacts: migration-plan.md、cover.png。
- Quality gate: 不能像素级复制参考封面。

### Case 3: 生成后优化

- Input: 首版文字可读但主体遮挡标题。
- Recommended path: 根据 optimization-checklist 调整标题区安全边距和主体位置。
- Artifacts: cover-review.md、cover_v2.png。
- Quality gate: 必须记录为什么重生成。
