# writers Examples

### Case 1: 新增专家风格

- Input: 用户要“像资深投资人一样写”。
- Recommended path: 按 README schema 新增 writer YAML，只定义声音、结构和禁忌。
- Artifacts: writers/investor.yaml。
- Quality gate: 不得写视觉风格、封面 prompt 或图片字段。

### Case 2: 排查 writer 未生效

- Input: 文章语气没有变化。
- Recommended path: 检查 lookup order、writer key 和项目配置是否一致。
- Artifacts: writer-diagnostic.md。
- Quality gate: 不要把 author 署名和 writer 风格混淆。

### Case 3: 迁移旧风格文件

- Input: 已有 YAML 含 image_style 字段。
- Recommended path: 删除视觉字段，把视觉要求迁移到项目/task visual_style。
- Artifacts: writers/<name>.yaml、migration-notes.md。
- Quality gate: writer 文件只保留写作声音相关字段。
