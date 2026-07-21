# 内容写作参考

content-writing Skill 直接完成正文创作，不再把正文生成委托给服务端生成工具。它以 writer resource、`context-brief.md` 和 `02-outline.md` 为输入，产出文件化 Markdown。

## 输入来源

- `$DIR/context-brief.md`: 用户需求、项目定位、历史避重、选题理由、章节锚点。
- `$DIR/02-outline.md`: 标题、钩子、章节结构、每节论点和素材方向。
- `get_project_profile`: writer key、主题、关键词、受众与任务覆盖。
- `list_resources/get_resource(category="writers")`: writer metadata、raw YAML、语气、结构和禁用表达。

## 写作模式

| 场景 | 做法 |
|---|---|
| 从零写文章 | 按大纲逐节扩写，每节绑定 context anchor |
| 改写片段 | 保留信息点和意图，补足结构、钩子和例证 |
| 根据标题写作 | 先把标题拆成承诺、读者、冲突和证据，再生成正文 |
| 根据大纲填充 | 保持章节顺序，补具体素材和过渡 |

## Writer 使用原则

- writer 只控制文字风格，不控制封面、配图、theme 或模板。
- 缺少 writer resource 时，优先使用 project profile 中的默认 writer；仍缺失则停止并说明。
- 不把 writer 名称写进正文，不伪造作者经历。
- 语气可以模仿，事实不能杜撰。

## 文件产物

- `$DIR/03-article.md`: 初稿正文。
- `$DIR/04-article-final.md`: humanizer 与合规预检后的最终正文。
- `$DIR/content-quality-report.md`: 公众号文章预检结果。

## 质量检查

正文必须具备：具体场景、可验证判断、移动端可读段落、自然互动问题、与标题摘要一致的承诺。发现导流风险、内容不完整、标题摘要不一致或 AI 套话时，直接改写并重新检查。
