# 种草笔记任务长时间运行：排查结论与修改方案

日期：2026-10-28（Asia/Shanghai）。本次仅排查并提出方案，未修改业务代码、运行配置或线上任务。

本文保留排查时的证据与代码版本。用户随后授权在新分支实施修复，实施及验证记录见 [修复计划](../plans/2026-10-28-seednote-failure-finalization.md)。

## 结论

本次现象由两层问题叠加：图像理解调用收到 HTML 而非 JSON，触发质量审核失败；运行时停止钩子又把合法的可恢复失败当成必须继续修复的条件，推动 Agent 在报告失败后继续等待、诊断和重试。

停止契约冲突已在当前源码上最小复现。线上日志的先失败、再继续重试顺序与其吻合，但尚未获取该 execution 的原始 Hook transcript 和部署镜像版本，不能声称已经直接观测到线上那一次 Stop 返回值。HTML 的来源仍需线上请求证据定位，不能仅凭 Agent 的描述断言供应商宕机。

## 现场证据

- 任务：[企业选大模型API，先过这5关](https://creator.anbanai.com/tasks/27eacfb1-b680-4df4-9bc2-7d71f290100c)。
- Task ID：`27eacfb1-b680-4df4-9bc2-7d71f290100c`。
- Execution ID：`6e2c9a53-a763-4a0f-af1c-69e99afe2ed1`；运行配置 `effective`。
- 线上概览：开始于 09:29:51，完成时间为空；约 10:14 核查时仍为运行中。
- 平台阶段：图像阶段 09:48 完成，交付校验阶段 09:50 完成，显示 5/5。
- 完整日志先报告附件理解连续 3 次失败，随后报告对三张生成图片的审核全部失败。
- Agent 已输出最终报告，明确写出“可恢复失败 — 图片审核不可用，整体质量闸门未放行”，并声明保存 `output/failure-state.json`，错误码 `image_review_unavailable`。
- 此后又出现“将重试三张输出图”、更换输入方式、缩略图测试、已重试八次、持续失败约 20 分钟及延长等待等消息。

日志引用的底层错误为：

```text
expected destination type of 'string' or '[]byte' for responses with
content-type 'text/html; charset=utf-8' that is not 'application/json'
```

网页展示的是 Agent 输出及其引用的错误，不是完整 HTTP trace。日志里的“2.2MB”“62KB”“哈希未变”等是 Agent 陈述，本次未读取运行容器中的对应文件验证。

## 原因分析

### 1. 已复现：失败停止与成功验收混用

调用顺序：

```text
Agent 写 failure-state.json 并尝试结束
  → runner.ts:createSeednoteProgressStopHook
  → artifact-validator.ts:validateFinalArtifacts
  → 只要 failure-state.json 存在，就返回 failure_state
  → blockedStopOutput 要求 Resolve the recorded failure before stopping
  → Agent 继续尝试解决外部审核故障
```

对应源码：

- `runtime-claude/src/artifact-validator.ts:23`：失败文件存在即拒绝最终产物验收，不区分内容是否合法。
- `runtime-claude/src/runner.ts:553`：先执行上述验收，失败直接返回，尚未运行 Seednote 专用质量闸门。
- `runtime-claude/src/runner.ts:575`：明确要求解决已记录的失败后才能停止。
- `runtime-claude/src/main.ts:372`、`:555`：Agent 返回以后才读取失败记录、覆盖成功结果，再进入产物上传和完成上报。

本仓库固定的 harness revision 为 `330396cb7c8fa1240bcf1fc560aee50afbe8237f`。其 [seednote-quality-gate.sh](https://github.com/anbanai/creator-harness/blob/330396cb7c8fa1240bcf1fc560aee50afbe8237f/hooks/seednote-quality-gate.sh#L96) 已将字段完整的可恢复失败定义为允许结束的结果，但被 Runtime 外层拦截。

这不是“关闭质量审核”才能解决的问题。失败应允许结束执行，同时继续禁止被判定为成功交付。`stop_hook_active` 有防递归分支，因此准确描述是错误的继续执行指令诱发长时间自救，不能仅靠静态代码断言 Hook 本身构成无穷递归。

### 2. 已定位到协议层：图像理解收到非 JSON 响应，具体来源未确定

调用链为 `analyze_image` → `TaskImageOperationsService.Analyze` → `CompleteWithImageResult` → OpenAI-compatible Chat Completions。

`server/service/task_image_operations.go:645` 附近显示，`file_path` 与 `image_url` 最终都由 Server 读取为图片字节，再编码成 data URL 发给同一个理解模型。切换这两个参数不会切换供应商路由，不能当成两条独立上游通道。

核对 `server/go.mod` 指定的 OpenAI Go SDK v3.74.0 [响应处理实现](https://github.com/openai/openai-go/blob/v3.74.0/internal/requestconfig/requestconfig.go#L743)：HTTP 状态 >=400 会先进入另一条错误处理分支；本次引用的错误位于后面的 Content-Type 检查。因此，在实际部署 SDK 与本地一致的前提下，它提示最终拿到的是状态 <400 的 HTML 响应，不足以证明是普通 502/503 宕机。

优先排查：实际 `model_routes.image_understanding` 对应 provider、base URL 与 `/chat/completions` 路径；重定向后的地址；代理是否落到登录页、站点首页、SPA fallback 或其他 HTML 页面。也可能是上游错误页被包装为成功状态。需要 HTTP status、Content-Type、最终 host/path 和请求 ID 才能区分。

示例配置中的 provider/model 不等于线上实际配置，本次不据此判断线上供应商或模型名错误。

### 3. 已确认：5/5 是阶段汇报，不是服务端成功结果

`runtime-claude/src/runner.ts:475` 的 Hook 将 Agent 的 `TaskUpdate completed` 上报为阶段完成。`server/service/task_lifecycle.go:91` 保存阶段进度；不会因此将整个任务判定成功。当前设计把产物验收放在 Stop 时执行，现有 `runner.test.ts` 也明确测试了这一行为。

因此，Agent 提前完成所有阶段，再被 Stop 拦截，能够产生截图中的“5/5 + 运行中”。审核 unavailable 时把“交付校验”标完成，本身也是进度语义错误。

### 4. 重连与超时的解释

- “连接断开，正在重试 (1/3)”来自 `studio/src/pages/TaskDetailPage.tsx:476` 的 SSE 重连。它不表示图片请求重试次数，也不能证明执行容器已退出。
- `runtime-claude/src/main.ts:623` 的心跳每 30 秒独立发送。Agent 等待或重复调用时仍可能正常心跳，因此心跳超时无法判断业务无进展。
- `server/agent/runtime_reconciler.go:128` 已有执行总时限与心跳兜底；示例配置的总时限为 3600 秒，不能直接视作线上配置。核查时约运行 45 分钟，不足以证明现有总超时机制失效。
- 另有独立的超时边界隐患：`model_client.go:117` 在上层已有 deadline 时跳过 route timeout，即上层更长 deadline 可能覆盖更短的模型超时；未证明它触发了本次 HTML 错误，不列为主因。

## 最小修改方案（尚未实施）

### P0：修正失败退出路径

1. 区分“允许结束执行”和“成功交付验收”。合法可恢复失败允许结束；缺产物且没有合法失败记录的成功尝试继续阻断。
2. 在 Runtime 中复用同一套严格失败文件读取/验证规则：普通文件、受限大小、版本、有限状态、必要字段、路径与符号链接限制。不要简单按文件存在就放行，也不要将失败文件当成平台级可信故障证据。
3. 初始范围限定 Seednote 的失败停止路径，保留其他 Agent 的现有行为；确需共享读取器时保持现有分类语义。
4. 退出后沿用现有流程：读取失败态 → 上传已完成产物及失败诊断 → 上报失败 → Server 持久化终态。失败停止不运行只适用于成功交付的后置工作，不删除失败记录，不把审核不可用改成通过。

验证：同一合法 `image_review_unavailable` 文件，Stop 不再要求继续修复；即便 SDK 返回 success，也最终上报失败；部分图片和正文仍能上传。非法/缺字段/符号链接失败文件不能绕过成功验收。补充覆盖真实 Seednote Hook 组合的测试，避免只测分离函数。

### P1：定位并修复图像理解路由，明确故障处理

1. 从上述 execution 获取部署 Server/Agent 镜像版本、理解路由配置和一条完整失败请求的脱敏响应证据。当前机器没有可用 Kubernetes context，本次未取得这些生产信息。
2. 先依据真实 host/path、status、Content-Type 和重定向结果修正地址或代理规则；如果证据确认供应商不可用，再处理上游恢复或经配置授权的替代路由。
3. Server 模型客户端把非 JSON 响应归类为稳定的协议错误；记录脱敏 endpoint、provider/model、status、Content-Type、request ID 和耗时。MCP 仅传递单次能力结果，不承担跨调用工作流编排。
4. Seednote 的 Agent/Skill 明确审核故障边界：持续同类协议错误不得通过改文件名、输入方式、缩略图或长时间 sleep 绕开重试限制。完成计划中必要的逐图记录后，以 `image_review_unavailable` 结束本次执行；恢复使用已有图片，只有发现实际质量问题才重生成。

验证：`httptest` 模拟 200 HTML、重定向到 HTML、401/403、429、5xx、正常 JSON；确认错误可区分且不暴露密钥、base64 图片或完整 HTML。故障场景能在约定预算内结束，恢复后只补未完成审核。

### P1：修正阶段汇报时机

1. Seednote 只有质量闸门通过后，才将“交付校验”汇报 complete；失败保留当前阶段，由 Server 现有终态收敛逻辑标记失败。
2. 保持作品状态由 Server 决定，不用“所有阶段完成”推导业务成功。
3. 本轮优先使用已有 lifecycle 与失败诊断，不新增数据库状态、百分比或另一套执行状态机。若仍需区分 Agent 阶段完成与服务端收尾，再针对 UI 单独提出最小展示调整。

验证：审核不可用场景不再产生“交付已完成”的全绿进度；成功、失败、恢复和 SSE 断连重连后展示与持久化状态一致。

### 后续独立加固

对模型调用总是叠加 route timeout，让有效 deadline 为上层和 route 中较早者；验证“上层 deadline 更长”和“更短”两种情况。不要通过扩大总执行时限缓解本次停止契约冲突。

实施涉及跨文件/跨仓库修改，应先进入隔离 worktree。修改 Pack/Agent/Skill 前阅读项目要求的两份规范；通过 Pack 源生成分发文件，按要求同步两个插件 manifest 版本。

## 已执行验证与限制

- 只读打开线上任务，核对完整日志、时间、当前状态和 execution ID。
- 检查当前主仓库 `384a828a` 以及其固定 harness revision；本地 harness 子模块未初始化，使用 GitHub API 读取固定版本，未初始化或改变子模块。
- 在临时目录创建合法失败样本，直接抽取并执行未修改的 Runtime 验证/Stop/收尾函数，同时执行固定版本插件质量闸门：外层 Stop 返回 block，专用质量闸门调用次数 0；插件单独允许退出；退出后的分类为 `success=false`、`workflow_error`、`image_review_unavailable`。临时文件已删除。
- 上述是隔离的最小函数复现，未连接真实 Claude SDK 或真实供应商，也没有重跑付费任务。当前环境无 Bun 和 runtime node_modules，未运行完整 Runtime 测试套件。
- 没有改业务代码、生产配置、积分或任务状态；只新增本排查文档。原有未跟踪文件 `docs/system-architecture.md` 保留。
