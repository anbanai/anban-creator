# live-slice Examples

### Case 1: 带货直播切片

- Input: 2 小时直播视频，目标提取 8 条 30-60 秒片段。
- Recommended path: 先转写并按商品/卖点分段，再用评分筛选高密度片段。
- Artifacts: transcript.json、clip-manifest.json、clip_results.json。
- Quality gate: 每条 clip 有 transcript span 和 ffmpeg 导出文件。

### Case 2: 知识直播金句切片

- Input: 讲座类视频，没有商品但有观点。
- Recommended path: 按问题-观点-证据闭环找片段，保留上下文，不截断关键句。
- Artifacts: segments.md、clip-manifest.json。
- Quality gate: 片段开头能独立成立，字幕与音频对齐。

### Case 3: ASR 不完整降级

- Input: 部分音频噪声导致词级时间戳缺失。
- Recommended path: 用句级时间戳粗切，报告风险，并避免过短卡点。
- Artifacts: asr-warnings.md、clip_results.json。
- Quality gate: 不伪造词级时间戳。
