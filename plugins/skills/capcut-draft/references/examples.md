# capcut-draft Examples

### Case 1: 单视频加字幕草稿

- Input: 用户给一段口播视频和字幕文本。
- Recommended path: 创建最小 draft_info/draft_meta_info，导入 video/audio/text segment。
- Artifacts: draft_content.json、draft_meta_info.json。
- Quality gate: 时间轴素材 ID 可追踪，文本轨不覆盖视频轨。

### Case 2: 直播切片批量草稿

- Input: live-slice 已输出 6 个 clip 文件。
- Recommended path: 为每个 clip 生成独立草稿目录，套用统一字幕和片头预设。
- Artifacts: drafts/<clip>/draft_content.json、manifest.json。
- Quality gate: 每个草稿引用本地存在的视频文件，时长与 clip manifest 对齐。

### Case 3: 替换 BGM 与转场

- Input: 已有草稿需批量换音乐和淡入转场。
- Recommended path: 读取现有草稿，保留主视频 segment，只替换 audio/material_transition。
- Artifacts: patched-draft/、operation-log.md。
- Quality gate: 修改前后素材引用完整，不能破坏原主轨时间线。
