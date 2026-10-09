# Project Creator 交互方案

## 结论

采用临时 DSH Runtime + TypeScript Bridge + Go Server 控制面 + Studio 原生聊天 UI。

DSH 的“创造模式”应作为交互语义参考：用户从“新建 Agent 会话”入口选择 Agent，选择会话模式后进入空白聊天，不使用 `/init`。Studio 复用 DSH 的模式选择、问题卡片、确认卡片和状态反馈语义，但不直接嵌入完整 DSH Web Host、Cordis 或 DSH UI Runtime。

`profile-analysis-interactive` 不作为最终名称。新增 Agent 使用：

- 内部 id：`project-creator`
- 展示名称：`Project Creator`
- 现有 `profile-analysis` 保持零交互，不做行为改变

## 核心架构

- **Studio**
  - 在创建项目入口提供“新建 Agent 会话”。
  - Agent 选择器提供 Project Creator。
  - 原生实现聊天记录、AskUserQuestion 问题卡、自由文本输入、确认卡和运行状态。
  - 使用 SSE 接收事件，使用 REST 提交消息和答案。

- **Go Server**
  - 创建临时交互 Session，绑定当前用户和权限。
  - 保存内存中的 Session ownership、lease、状态和 Bridge 连接，不保存可恢复会话。
  - 提供 Project Creator 专用 MCP capability。
  - 在用户确认后，以一个事务同时创建项目、画像草稿和画像 revision。
  - 不复用绑定 `task_id` 的 `submit_profile_result`。

- **TypeScript DSH Bridge**
  - 启动并监管单个 Project Creator DSH Runtime。
  - 将 Anban 默认模型路由映射到 DSH provider/model adapter。
  - 转发 prompt、消息、AskUserQuestion answer 和关闭命令。
  - 将 DSH 原始事件转换为语义化的 `message`、`question.open`、`question.settled` 和 `status` 事件。

- **DSH Runtime**
  - 执行 Project Creator Agent。
  - 使用 AskUserQuestion 动态收集信息。
  - 仅使用受限的 Project Creator MCP capability。
  - 不直接访问数据库、项目画像文件或 Server 私有状态。

## Protocol 与生命周期

DSH SDK 增加面向交互 Agent 的通用 answer 通道：

```text
session/answer
{
  sessionId,
  callId,
  answer
}
```

同时向 Bridge 输出语义化问题事件；Studio 不解析 DSH 原始 Session Event。现有 `initialize`、`session/prompt` 和进程级 `shutdown` 保持兼容。Project Creator 的 runtime profile 由 Server/Bridge 的受信任 allowlist 选择，不接受浏览器传入的任意 runtime 或插件路径。

Studio 对 Server 暴露：

```text
POST   /api/v1/interactive/sessions
GET    /api/v1/interactive/sessions/:id/events
POST   /api/v1/interactive/sessions/:id/messages
POST   /api/v1/interactive/sessions/:id/questions/:callId/answers
DELETE /api/v1/interactive/sessions/:id
```

Session 状态至少包括：

```text
starting
ready
running
waiting_user
committing
completed
failed
expired
closed
```

Session 只存在于当前 Server 和 Runtime 内存中。页面刷新、关闭、Runtime 崩溃或 lease 超时后不支持恢复，也不产生新的项目。已经成功提交的项目创建事务不受 Runtime 后续退出影响。

## Project Creator 流程

1. 用户进入项目创建入口。
2. 点击“新建 Agent 会话”，选择 Project Creator。
3. Server 创建 ephemeral Session，尚未创建项目。
4. Bridge 启动 Project Creator Runtime。
5. Agent 使用 AskUserQuestion 动态收集项目元数据和六个画像维度。
6. Agent 生成项目摘要和六维画像草稿。
7. Agent 使用问题卡请求最终确认。
8. 用户确认后，Agent 调用受限的 `commit_project_creator_draft` capability。
9. Server 校验：
   - Session 属于当前用户；
   - Agent profile 为 `project-creator`；
   - 确认问题 callId 已被用户明确确认；
   - 项目字段和六维画像符合现有模型 Schema；
   - payload 大小、平台和权限合法。
10. Server 在单个事务中：
    - 创建项目；
    - 写入六维 profile draft；
    - 创建 profile revision；
    - 设置 profile initialization 为 ready；
    - 保持 profile status 为 draft。
11. Studio 跳转项目详情或画像确认页。
12. 后续 confirmed 状态仍由现有用户确认流程负责。

Runtime 不创建临时项目，也不直接修改 `AGENTS.md`、画像文件、项目 profile 或生成上下文。Server 完成数据库持久化后再按现有机制更新投影文件。

## AskUserQuestion 策略

AskUserQuestion 按 Agent capability 区分，禁止全局开启：

- `profile-analysis` 和现有托管 Agent：继续禁止。
- `project-creator`：允许 AskUserQuestion，允许回答桥接。
- 未来交互 Agent：必须在 Agent Pack/profile manifest 中显式声明 `interactive: true` 和允许的交互 capability。
- 子 Agent 不允许直接向用户提问；问题只能由 live root Agent 发起。
- Server 只接受当前 Session、当前 root Agent 和当前 callId 的回答。

这种策略保留现有零交互托管任务的自动化边界，同时允许交互 Agent 使用同一套通用协议。

## Review 结论与约束

已修正以下风险：

- 不再使用 `/init` 作为入口。
- 不把 DSH 的 Creator mode 误解为命令；它是新会话模式选择器。
- 不创建临时项目来满足旧的 `project_id` 要求。
- 不让 Project Creator 复用任务级 `submit_profile_result`。
- 不把 DSH Web UI 组件直接复制到 Studio。
- 不把 Go 胶水层当作完整实现；AskUserQuestion 的 live Agent answer 必须由 DSH Bridge 处理。
- 不把 transient conversation 当作持久化证据。
- 不全局解除 `AskUserQuestion` 禁止。
- 不让浏览器选择任意 DSH profile、插件或 MCP capability。
- 不假设 DSH 只能使用 Claude-compatible provider；模型路由必须经过 Server 配置的 adapter 映射。

## 验证方案

- DSH protocol：验证 answer callId、完整问题集合、重复回答、非法回答和 Runtime 关闭。
- Bridge：验证问题事件转换、用户答案转发、Runtime 崩溃和超时清理。
- Go Server：验证 ownership、lease、权限、确认 token、事务回滚、重复 commit 幂等性。
- Studio：验证 Agent 选择器、聊天流、问题卡、确认卡、断开状态和无恢复行为。
- 集成测试：
  - 未确认即关闭：不创建项目。
  - 确认成功：只创建一个项目，并保存六维 draft revision。
  - commit 中途失败：项目和 profile 不产生半成品。
  - `profile-analysis` 仍拒绝 AskUserQuestion。
  - Project Creator Runtime 消失后 Session 终止，但已完成的事务保持 durable。

设计文档应写入：

```text
docs/superpowers/specs/2026-10-09-project-creator-interactive-design.md
```

实现计划随后写入：

```text
docs/superpowers/plans/2026-10-09-project-creator-interactive.md
```

当前仍处于 Plan Mode，按照当前执行约束本轮不能修改仓库文件；上述内容是已完成 review 的决策完备方案，下一步执行阶段首先落盘设计文档。
