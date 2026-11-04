# 验证记录

日期：2026-11-04。工作区：`/Users/medivh/.codex/worktrees/talking-head-quality/anbanwriter`，已隔离的 Git worktree；`harness/` 是独立 Git 子模块。以下区分工程检查与媒体品质验证。

## 工程检查

| 工作目录 | 命令 / 检查 | 实际结果 |
|---|---|---|
| 仓库根目录 | `make agent-pack-generate`、`make agent-pack-check` | 通过；源 Pack、两宿主 Agent、两份 catalog 一致 |
| `server/` | `go test ./mcp -count=1` | 通过，11.569 秒 |
| `server/` | `go test ./agent -run TestJSONOutputContractsDeclareSharedMetadata -count=1` | 通过；首次全量测试发现 Codex 薄适配未显式声明 JSON metadata，补回后复验 |
| `server/` | `go test ./...` | 修正后重新执行，退出码 0；部分未受影响包使用 Go 测试缓存 |
| `server/` | `go build -o /tmp/anban-creator-talking-head-quality .` | 通过，退出码 0 |
| `harness/` | `claude plugin validate .` | 通过；保留现有缺少 icon/privacy-policy 的警告 |
| `harness/` | `python3 /Users/medivh/.codex/skills/.system/skill-creator/scripts/quick_validate.py skills/talking-head-explainer` | 通过 |
| `harness/` | 同上，目标换为 `skills/talking-head-video-generation` | 通过 |
| `harness/` | 逐个用 `python3` 执行下列五个 unittest 文件 | 合计 35 项通过 |
| 根目录和 `harness/` | `git diff --check` | 通过 |
| 报告证据目录 | 逐一重算 manifest 中 20 个证据文件的 SHA-256 | 全部匹配 |

五个媒体/QC 测试文件：

- `skills/talking-head-video-generation/tests/test_validate_generated_video.py`：4 项。
- `skills/talking-head-composition/tests/test_validate_timeline.py`：8 项。
- `skills/video-production-qc/tests/test_render_qc.py`：13 项。
- `skills/video-production-qc/tests/test_generate_audio_content_check.py`：2 项。
- `skills/talking-head-sound-design/tests/test_sound_pass.py`：8 项。

这些测试证明已有生成采用、时间线、音轨、声音和 QC 检查仍满足代码合同，**不证明新指令已能产出参考品质视频**。本轮没有新增仅匹配指令措辞的测试，也没有执行独立模型前向行为测试。

## 未通过的外围检查

- `make dsh-check`：workflow audit 报 `seednote: startup 38416 > 38000`。只读执行 `node scripts/audit-workflows.mjs --baseline` 后，HEAD 基线报告含相同数值与错误；该模式仅输出基线，退出码 0 不是检查通过。本轮未修改 Seednote。
- `make dsh-smoke`：wechat-article 挂载 Skill 列表包含 `redfox-research`，smoke 期望列表缺少它。用 `git show HEAD:packs/wechat-article/agent-pack.yaml` 与 `git show HEAD:dsh/scripts/smoke-profile.mjs` 确认基线已存在此清单不一致；相关 Pack/DSH 源文件没有本次差异。未为本任务修改文章工作流或放宽 smoke。
- 修订后的 `node scripts/audit-workflows.mjs` 仍仅报上述 Seednote 超限；不能将整套 DSH 检查写成全绿。

## 修改范围与证据边界

- 品质修订时 Pack 版本为 1.3.1，插件为 4.2.133；后续按用户要求精简命名为 `ip-explainer`（知识 IP 剪辑 / IP Explainer），Pack 与插件版本分别递增至 1.3.2 / 4.2.134。
- Agent 内容从 canonical Pack 生成，没有只修改生成副本。`viral-ip-video-editor` 的 digest 也变化，因为它引用两个已修改的共享 Skill；其 Agent/Pack 编排没有改动。
- 报告链接、三条输入媒体及证据文件以 manifest 定位。ASR 是机器原稿，未校对，不作为人工听检记录。
- 本轮完成两秒间隔的全时长抽帧覆盖、机器转录和全轨响度测量；未完成正常速度连续视听审片、逐帧动作审核、独立完整媒体审片、领域专家核验、真实目标观众测试或平台留存实验。
- 没有生成新版样片/终片，也没有取得与用户成片哈希对应的原始 take、EDL 和执行链。另一个同题目录的不同 revision 不能补足这些证据。
- 新指令明确要求先验证最难解释单元，再扩展全片；后续达标必须由当前候选媒体和独立审片证明，不能由本次文档改写或单元测试替代。

所有修改保留在当前 worktree，未提交、推送或发布。

## 精简命名后的复验

按用户要求，中文名为“知识 IP 剪辑”，英文名为“IP Explainer”，调用 ID 为 `ip-explainer`。Pack、原生 Agent、运行 Profile、Codex 注册、证据 source 和校验器已同步；现有输入文件名作为输入协议保留。双语 description 明确个人 IP/专家口播/科普知识讲解，两个共享 Skill 分别声明策划验证与演示镜头生成的职责。仅在临时目录运行安装迁移测试，未安装到用户的实际 Codex 配置。

- `make agent-pack-generate` / `make agent-pack-check`、两个 Skill 的 `quick_validate.py`、`claude plugin validate .` 和 `git diff --check` 通过。
- `go test ./...` 重新执行退出码 0；`go test ./mcp -count=1` 通过（11.902 秒）；`go build -o /tmp/anban-creator-ip-explainer .` 通过。
- 新增安装迁移测试验证：旧注册及 TOML 文件退场、新名称可发现、保留其他 Agent 和多行文字中的旧名称、重复安装不改变结果。完整 Agent 测试通过。
- `bun test test/artifact-validator.test.ts`：24 项通过；`PYTHONDONTWRITEBYTECODE=1 python3 skills/video-production-qc/tests/test_render_qc.py`：13 项通过。
- 重新执行 `make dsh-check` 与 `make dsh-smoke`，仍分别因上文 Seednote 字符预算、wechat-article Skill 期望清单问题失败。
- 按 ID 比较 catalog：旧 `talking-head-montage` 被 `ip-explainer` 替换，另有共享 Skill 消费者 `viral-ip-video-editor` 的 digest 更新；大段文本差异主要来自按 ID 排序后位置移动。两宿主生成 Agent 与 canonical Pack 字节一致，Pack/Codex 注册的 description 与别名一致。

这些验证确认命名与安装/证据合同一致，不代表已实测模型在所有自然语言请求下的路由选择。
