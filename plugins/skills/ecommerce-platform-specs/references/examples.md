# ecommerce-platform-specs Examples

### Case 1: 微信小店合规扫描

- Input: target_platform=wechat_store，文案含“转发返现”。
- Recommended path: 映射到微信小店规范，标记诱导分享高风险并改写。
- Artifacts: compliance-report.md。
- Quality gate: 命中高风险的图必须重生成后才能归档。

### Case 2: 功效类美妆文案

- Input: 详情页写“7 天根治痘痘”。
- Recommended path: 按广告法和类目规范改成可验证、非治疗承诺表达。
- Artifacts: compliance-report.md。
- Quality gate: 不得保留“根治/永久/100%”等绝对化词。

### Case 3: 疑似误报商标名

- Input: 产品名含“金牌”但为注册品牌词。
- Recommended path: 标中风险人工复核，不自动删除核心商品名。
- Artifacts: compliance-report.md。
- Quality gate: 误报处理要记录依据和人工复核建议。
