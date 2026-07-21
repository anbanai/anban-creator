# seednote-viral-analysis Examples

### Case 1: 结构迁移判断

- Input: 源笔记是“3 个省钱技巧”清单。
- Recommended path: 拆标题钩子、正文节奏、视觉页主题，给 recommended_clone_depth。
- Artifacts: source-analysis.md、template.json。
- Quality gate: tight 只用于低风险通用结构。

### Case 2: 高风险视觉复刻

- Input: 源封面有人像姿势和品牌包装。
- Recommended path: 标出 do_not_copy：姿势、图标组合、文字框位置。
- Artifacts: risk-report.md。
- Quality gate: 只能迁移信息层级和互动机制。

### Case 3: 爆款模板输出

- Input: 源笔记互动强但主题要换成另一产品。
- Recommended path: 产出可填充模板：标题公式、开头方式、内容页结构。
- Artifacts: template.md。
- Quality gate: 模板不包含源文案原句。
