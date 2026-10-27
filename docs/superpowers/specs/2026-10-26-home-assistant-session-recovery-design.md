# 首页助手会话可靠性优化设计

日期：2026-10-26
范围：Studio 首页助手的对话、计划暂存、确认创建、刷新恢复和会话重建
状态：设计默认值已确认；本设计只描述方案，不修改产品代码

## 已确认的默认值

- 同一用户、项目和当前 mode 只保留一个可恢复 interaction；多个浏览器标签页连接同一个 interaction，后来的操作按 generation/revision 处理，不创建第二个 runtime。
- 首页 mode 切换继续共享一个 interaction；消息事件按 interaction 排序，结构化 draft 按 mode 分开。
- runtime 失效后由 Server 自动异步替换 binding；Studio 展示 reconnecting/restart_required 状态，不要求用户重新打开页面。
- 结构化 draft 和待确认的创建收据保留 30 天；消息事件和输入收据保留 24 小时，过期后仍保留 draft 摘要和确认所需字段。
- 旧 in-flight session 不做兼容迁移；发布后旧 session 直接进入不可恢复状态，Studio 提供恢复 draft 或重新开始。
- 同一 interaction 的消息、回答和 stage 按 Server 的 command sequence 串行化；generation 变化时，Server 只自动重放幂等且未产生副作用的请求，其他请求返回可执行错误。

## 意图

首页助手的用户目标是“把需求聊清楚，然后在同一个页面确认计划并开始创建”。用户不应该理解 Studio workspace、Server logical session、DSH session、MCP session、runtime token 或 SSE cursor 之间的区别，也不应该因为页面刷新、网络切换、服务重启或等待确认而重新讲一遍需求。

当前首页已经有较多局部恢复逻辑，但会话身份在四个边界之间仍然分裂：浏览器保存 AssistantWorkspace，Server 在内存中保存 InteractiveSession，DSH Bridge 在容器内保存 logical session，MCP 通过 runtime token 决定暂存操作的身份。对话消息走 workspace 路由，Agent 暂存却直接走 MCP session 身份，因此两条路径可能看到不同的会话事实。

本设计把“会话是否仍可继续”和“用户已经确认的结构化计划是否仍可创建”分开。会话可以失效，但计划不能随会话一起消失；计划可以恢复，但必须重新确认，Server 仍是创建任务的唯一权威。

## 用户体验目标

1. 用户发送的每条消息在发送前、发送中、发送失败后都有一个可见且可重试的归宿。
2. session not found 不再作为用户可见的最终解释。Studio 展示“对话连接已失效，已保留当前计划/输入”，并提供明确的“恢复并继续”“重新连接”“重新开始”动作。
3. 用户点击确认暂存或创建时，系统要么返回明确的结果，要么保留可重试的确认材料；不能让用户猜测是否已经创建成功。
4. 页面刷新、后台挂起、短暂断网、Server 重启和 runtime 重启都不会要求用户重复已经确认的内容。
5. 只有用户主动选择“重新开始”时才清空当前对话；普通恢复不清空消息、问题答案或结构化草稿。
6. 所有状态和按钮文案使用用户语言，避免暴露 session、workspace、MCP、token 等实现术语。

## 非目标

- 这次不保留旧首页会话 API 的兼容分支；新项目可以直接收敛到一套新的 Server/Studio 会话合同。
- 这次不让 Agent 直接创建 Task、写 Profile 或发布内容。确认和创建仍由 Studio 调用 Server 完成。
- 这次不把逐字聊天永久存入数据库作为恢复手段；可恢复的事实应是 Server 拥有的结构化草稿、输入收据和事件序列。
- 这次不通过无限延长 TTL 来掩盖会话生命周期错误。TTL 只负责资源回收，不能承担业务可靠性。

## 已确认的现状

### 浏览器端

- AssistantWorkspace.tsx 把 workspace、消息、问题、草稿和 SSE cursor 一起写入 sessionStorage。
- 首次发送时，Studio 先 POST /assistant/workspaces，再 POST /assistant/workspaces/:id/messages；请求成功后才把 workspace 放入 React 状态。
- SSE 建立后，Studio 将 workspace.sessions[mode] 作为 X-Assistant-Session-ID，并按 mode 保存 Last-Event-ID。
- 发送、回答、切换模式、checkpoint 和 commit 都有不同的错误分支；过期或 404/410 会清空当前 workspace 并回到 setup，部分分支会保留 composer 文本。
- 过期事件会让 Studio 重置当前对话，但已有的结构化草稿只存在浏览器状态和 runtime session 事件中；没有独立的“可恢复草稿收件箱”。

### Server 端

- assistant.go 将 workspace 持久化到数据库，并在 WorkspaceMessage、WorkspaceAnswer、AssistantWorkspaceSession 等入口发现 logical session 不可复用时尝试重建。
- interactive.go 的 InteractiveSession、runtime token、事件历史、草稿和 pending question 主要保存在当前 Server 进程内存；数据库保存 AssistantSession、lease、workspace 和 checkpoint 等控制面记录。
- workspace 的多个 mode 共享一个 DSH Bridge，但每个 mode 有独立 logical session；Bridge 维护自己的 sessions map，Server 通过 activeSessionID 从共享 runtime token 解析当前 MCP 身份。
- stage_assistant_draft 和 stage_project_creator_draft 经过 MCP 认证后，直接调用 StageDraftForSessionContext(ctx, identity.SessionID, input)。该路径不会经过 AssistantWorkspaceSession 的 workspace 恢复逻辑。
- Server 重启时会把 durable AssistantSession 标成 interrupted；workspace HTTP 入口可以基于 workspace 和 checkpoint 重建 logical session，但旧 runtime/MCP 请求不会自动获得新的 logical session 身份。
- SSE heartbeat 会调用 TouchSession 续租，因此“用户停留十分钟”本身不是充分的过期原因；只有 SSE 未连接、连接已断开、runtime/Server 重启或真实 lease 失效时才会进入过期路径。
- interactiveError 已区分 assistant_workspace_expired、assistant_workspace_not_found、interactive_session_closed、interactive_session_not_found、interactive_event_gap、busy 和 revision conflict，但 Studio 仍把多个生命周期分支归并为“重置 workspace”。

### DSH 与 MCP 端

- DSH Bridge 将 session/start 的 logical session 注册在容器内，并串行执行 prompt/answer；工具错误会通过 tool.result 传回 Studio 可见的 runtime 事件。
- MCP 的交互凭据由 runtime token 解析为 Server 进程内的 session identity。MCP tool 本身是无状态的，但 identity 解析依赖当前进程的内存会话表。
- DSH Bridge 和 MCP 都有自己的会话生命周期：MCP Streamable HTTP session 当前配置为 16 分钟，DSH interactive session 默认 idle 30 分钟、最大 24 小时。这些时间窗口相互独立。

## 根因判断

### 已确认的架构缺口

暂存路径没有使用 workspace 的恢复边界。

普通消息经过 WorkspaceMessage -> AssistantWorkspaceSession，Server 能识别旧 logical session 已关闭或被重启打断，并创建新 session；Agent 暂存经过 MCP identity 直接进入 StageDraftForSessionContext，只查当前进程的 s.sessions[id]。因此同一个 workspace 可能出现“Studio 消息入口可恢复，但 Agent 暂存入口仍指向旧 session”的分裂状态。

这解释了用户看到的现象：计划和上下文仍在对话中，Agent 也已经完成了足够的澄清，但最后一次 stage_*_draft 得到 interactive session not found。重复让 Agent 再试不会改变 MCP identity 或 Server session 表，所以会连续得到同一个错误。

### 高概率触发路径

以下触发条件都会放大上述缺口，当前代码已能证明其存在，但线上发生频率需要日志确认：

1. Server 重启、发布或实例切换后，数据库 workspace 仍在，旧 DSH runtime/token 仍在发送 MCP 请求，新的 Server 进程没有对应的内存 session。
2. logical session 在 runtime error、lease expiry 或关闭流程中被清理，Agent 随后才发出延迟到达的暂存调用。
3. workspace mode 切换或恢复创建了新的 logical session，但共享 runtime/MCP 请求仍带着旧 mode 的调用上下文。
4. MCP Streamable HTTP session 超时后，DSH client 重新建立传输会话；重新认证和 interactive identity 恢复之间没有一个以 workspace 为中心的原子握手。

### 当前修复为何仍会失败

- Studio 已能在 SSE 收到 session.expired 后保留 composer，但这只恢复浏览器输入，不能让 runtime 的 MCP token 绑定到新 session。
- Server 已能在 workspace HTTP 请求中重建 session，但 MCP stage tool 不会触发该重建。
- sessionStorage 保存的是旧的 session_id、sessions 和 cursor；恢复时如果只重用这个快照，仍会把用户带回已失效的 runtime identity。
- 统一返回“重新打开会话”把修复责任推给用户，也没有告诉用户当前计划是否仍然保留。

### 仍需运行时证据确认的部分

线上日志需要在不记录 token、正文和敏感素材的前提下关联以下字段：workspace_id、logical_session_id、mode_id、runtime_id、server_instance_id、MCP request id、stage attempt id、session status、durable session status 和 error code。只有这样才能区分“Server 重启”“runtime 关闭”“MCP transport 过期”和“竞态关闭”四种触发来源。

## 设计决策

### 1. Workspace 成为唯一的用户会话边界

浏览器、Server、DSH Bridge 和 MCP 都围绕一个 Server-owned assistant_interaction 记录通信。logical runtime session 变成可替换的执行句柄，不再是用户可恢复事实的主键。

建议的新状态模型：

assistant_interaction
  id
  user_id
  mode_id
  project_id / content_id
  lifecycle: active | reconnecting | needs_confirmation | completed | abandoned
  generation
  expires_at
  last_activity_at

assistant_interaction_runtime
  interaction_id
  generation
  server_instance_id
  runtime_id
  logical_session_id
  runtime_token_hash
  status: starting | running | closed | failed

assistant_interaction_draft
  interaction_id
  mode_id
  revision
  payload
  payload_digest
  ready
  confirmation_state: pending | confirmed | consumed | superseded
  source_generation

assistant_interaction_event
  interaction_id
  sequence
  type
  data
  created_at

结构化 draft 和用户输入收据属于 interaction，而不是 runtime session。runtime 重建只增加 generation，并继续使用同一个 interaction id。

### 2. 暂存必须是 Server 的 workspace 原子能力

保留 MCP 的 stateless transport 原则，但让 MCP stage_assistant_draft 只提交一次带有 interaction_id、runtime_generation 和幂等键的能力调用。Server 做以下原子校验：

1. 验证 runtime token 属于当前 interaction，而不是只验证内存 session id。
2. 如果 generation 已过期，Server 在同一 interaction 内创建或返回新的 runtime binding，并把 stage 请求转为 needs_confirmation 或可安全重放的结果。
3. 将 draft 写入 durable assistant_interaction_draft，返回稳定的 draft_id、revision 和 confirmation_required。
4. 如果同一 stage attempt 已成功，重复调用返回原结果；如果 payload digest 不同，返回明确的 draft_revision_conflict。

Agent 不再直接持有“必须长期有效的 session id”；它只需要在当前 turn 使用 Server 注入的 interaction capability。

### 3. 恢复是显式状态机，而不是清空并重试

建议的用户可见状态和动作：

| 状态 | 用户看到的说明 | 主动作 | 保留内容 |
|---|---|---|---|
| connecting | 正在连接助手 | 无 | 当前输入 |
| reconnecting | 连接刚刚中断，正在恢复 | 取消恢复 | 消息、问题、草稿 |
| needs_confirmation | 连接已恢复，请确认最新计划 | 确认创建 / 继续修改 | durable draft、消息摘要 |
| input_retryable | 刚才这条消息未送出 | 重试发送 | composer 输入 |
| draft_retryable | 计划已保留，暂存需要重试 | 重试暂存 | draft 全量 |
| restart_required | 当前运行环境已结束，计划仍已保留 | 恢复计划 / 重新开始 | draft、上下文摘要 |
| completed | 已创建任务 | 查看任务 | receipt、draft 摘要 |

“重新开始”是破坏当前 interaction 的动作，必须单独确认；普通错误恢复不能调用它。

### 4. 采用短期 command receipt，避免等待确认时依赖 runtime

Agent 生成 ready draft 时，Server 立即落 durable draft receipt。用户过一段时间再点击“创建”时，Studio 直接向 Server 提交 draft_id + revision + idempotency_key，不要求原 DSH runtime 或原 MCP session 仍然活着。若 draft 已被替换，Server 返回最新 revision，Studio 保留用户未发送文本并让用户重新确认。

这会把“对话会话失效”和“成稿计划丢失”彻底解耦，直接消除本次用户最痛的重复操作。

### 5. Studio 只从 Server 恢复权威状态

sessionStorage 只保存未发送 composer、附件引用和最近一次展示快照；进入页面或收到 stale 错误后，Studio 必须调用 GET /assistant/interactions/:id 获取 interaction、draft、generation 和可用动作。不能把旧 sessions 或 Last-Event-ID 当作恢复依据。

SSE 断开时，客户端先保留 UI 状态并进入 reconnecting；重连请求携带 interaction id 和 generation。若 Server 返回 generation changed，客户端丢弃旧 cursor，加载 durable event/draft snapshot，再继续订阅。

### 6. 运行时重启由 Server 调度，不由用户重新打开页面

Server 发现 runtime binding 失效时，使用同一个 interaction 创建新 runtime，并将新的 generation 注入 Bridge。旧 runtime 的迟到事件必须被 generation 丢弃，不能覆盖当前 draft 或状态。若 runtime 无法恢复，Server 将 interaction 标为 restart_required，同时保留 draft receipt。

## API 合同（新项目可直接采用）

### 创建或恢复 interaction

POST /assistant/interactions
Idempotency-Key: <client-attempt>

{
  \"mode_id\": \"creation\",
  \"project_id\": \"...\",
  \"content_id\": \"...\",
  \"resume_interaction_id\": \"...\"
}

返回 interaction_id、generation、lifecycle、available_actions 和当前 draft_summary。旧 session id 不出现在 Studio API 中。

### 发送消息与回答问题

POST /assistant/interactions/:id/messages

{
  \"generation\": 7,
  \"client_message_id\": \"...\",
  \"text\": \"...\"
}

Server 用 client_message_id 去重；generation 过期时返回 assistant_interaction_generation_changed，并附当前交互快照。

### 暂存草稿

MCP 和 Server 内部都归一到同一个能力：

{
  \"interaction_id\": \"...\",
  \"generation\": 7,
  \"attempt_id\": \"...\",
  \"payload\": { \"plan\": {} },
  \"payload_digest\": \"...\",
  \"ready\": true
}

返回 draft_id、revision、confirmation_required、available_actions。所有重试使用同一个 attempt_id。

### 确认创建

POST /assistant/interactions/:id/commit

{
  \"draft_id\": \"...\",
  \"revision\": 3,
  \"idempotency_key\": \"...\"
}

Server 先验证 draft revision，再执行现有任务创建和结算流程；结果必须是明确的 created、already_created、needs_reconciliation 或可修复错误。

## 错误合同

错误码必须描述可执行的恢复动作，而不是底层组件：

- assistant_interaction_generation_changed：加载最新快照后继续。
- assistant_interaction_reconnecting：等待 Server 恢复，禁止重复发送。
- assistant_interaction_restart_required：恢复已有 draft 或重新开始。
- assistant_message_not_accepted：保留 composer，允许用同一 client message id 重试。
- assistant_draft_saved_retryable：draft 已持久化，重试暂存不会丢失。
- assistant_draft_revision_conflict：加载最新 draft，要求重新确认。
- assistant_commit_already_applied：展示已创建结果，不重复创建。
- assistant_commit_reconciliation_required：展示核对入口，禁止盲目重试。

底层 interactive_session_not_found 只进入结构化日志和内部诊断，不再直接作为 Agent 或用户文案。

## 观测与诊断

每次用户操作生成不可猜测但不含敏感内容的 interaction_attempt_id。日志和指标至少关联：

- interaction id、draft id、attempt id、generation、mode id；
- server instance、runtime id、logical session id 的 hash 或内部关联键；
- operation（message、answer、stage、events、commit）；
- durable lifecycle、runtime status、MCP transport status；
- latency、重试次数、最终 error code。

禁止记录 runtime token、MCP bearer、用户正文、完整草稿和素材 URL。需要排查时通过 attempt id 查询事件链。

关键指标：

- assistant_interaction_recovery_total{cause, outcome}；
- assistant_draft_stage_total{outcome}；
- assistant_draft_stage_retry_after_session_loss_total；
- assistant_commit_duplicate_prevented_total；
- assistant_runtime_generation_replaced_total；
- 从用户发送到 draft durable 的 p50/p95。

## 验证策略

### Server

- interaction generation 在消息、SSE、MCP stage 和 commit 间不一致时，所有入口返回同一结构化错误并附最新快照。
- Server 重启后，workspace/interactions 可以重建 runtime；旧 generation 的迟到 stage 不得覆盖新 draft。
- runtime 关闭与 stage 并发时，stage 要么 durable 成功，要么返回 assistant_draft_saved_retryable；不得返回裸 session not found。
- 相同 attempt id 的 stage/commit 重放必须幂等；不同 payload digest 必须冲突。
- durable draft 在 runtime 终止后仍可确认创建，创建结果可重放。

### Studio

- 404/410/断流不清空对话和 draft；进入 reconnecting 或 restart_required 状态，并显示可执行按钮。
- 刷新页面只从 interaction snapshot 恢复，不向旧 logical session 发送消息。
- 暂存失败保留计划摘要和确认入口；确认失败保留 draft revision，不能要求重新讲完整需求。
- 重复点击确认只产生一个任务，并展示已创建结果。

### DSH/MCP

- MCP transport session 过期后，Bridge 能以 interaction capability 重新初始化，而不把 transport session 当成业务 session。
- 多 mode 共用一个 runtime 时，旧 generation 的 tool result 被丢弃，当前 mode 的 stage 仍能正确落在 interaction 上。
- 运行时错误必须产生可关联的 attempt/event 记录，不能只在 stdout 中出现。

## 分阶段实施顺序

1. 先增加 attempt/generation/structured error 的观测和合同测试，证明现有故障来自哪一个边界。
2. 把 draft receipt 从 runtime session 内存迁移到 Server durable interaction draft，并让 commit 只依赖 receipt。
3. 将 MCP stage 改为 interaction-scoped 原子能力，加入重放和 generation 校验。
4. 将 workspace HTTP 和 SSE 收敛到 interaction API，Studio 改为只恢复 Server snapshot。
5. 最后移除旧 session-id 暴露和“重新打开会话”兜底文案，运行全量 Go、Studio、runtime 检查。

每一步都应有可独立验证的用户路径；在第 2 步完成前，不应通过增加 TTL 或静默重试来宣称问题已解决。

## 设计取舍

- 选择 durable draft receipt，而不是延长 runtime TTL：用户真正需要的是计划可继续确认，runtime 只是生成工具；延长 runtime 会增加资源占用，仍不能解决 Server 重启或 MCP transport 失效。
- 选择 interaction generation，而不是继续暴露 session id：session id 属于执行实现，generation 能明确拒绝迟到事件和旧请求，且允许同一交互内安全替换 runtime。
- 选择显式恢复状态，而不是自动清空重建：自动清空虽然简单，但会让用户误以为计划丢失，并造成重复输入；状态机能让恢复动作可见且可验证。
- 选择新合同而不是兼容旧 API：用户明确说明这是新项目；删除双轨 session/workspace 语义可以减少以后再次出现身份分裂的机会。
