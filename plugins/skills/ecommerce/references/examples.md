# ecommerce Examples

### Case 1: 茶叶全套素材

- Input: 上传干茶、茶汤、叶底、包装 4 张图，选择主图+详情页。
- Recommended path: 先建 product-bible，再做 FABE 文案，最后按部位选参考图生成各模块。
- Artifacts: product-bible.md、copywriting.md、asset-plan.md、manifest.json。
- Quality gate: 每张电商图都带相关产品 ref，不允许纯文生图臆造商品。

### Case 2: 只生成分享图

- Input: 任务 selected_modules 只勾选 share。
- Recommended path: 跳过主图/详情/SKU，仍先做产品分析和合规检查。
- Artifacts: share_01.png、manifest.json。
- Quality gate: 未勾选模块不得生成，manifest 只列 share。

### Case 3: Seedream 单参考风险

- Input: 复杂包装文字但项目模型是单参考。
- Recommended path: 按最相关部位传单图，prompt 点名保真，并在报告披露一致性风险。
- Artifacts: best-refs.md、risk-notes.md。
- Quality gate: 不得自动切模型；只能建议用户下次选择多参考模型。
