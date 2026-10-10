# Project Creator 交互式项目创建设计

## 目标

把项目创建从封闭式输入输出 Agent 扩展为 Studio 内的交互式工作区。用户选择 `Project Creator` 后，Studio 自动创建一个短生命周期会话，Agent 通过 DSH 的结构化问题协议逐步收集项目定位、风格、受众、平台、偏好与记忆六个维度，最后由 Server 校验并一次性创建项目。

`profile-analysis` 继续作为现有零交互托管 Agent，行为和权限不变。首期不引入 `/init` 命令，也不提供“新会话”按钮；创建页面本身就是一次临时会话。

## 设计原则

- Studio 负责用户体验和事件渲染，Server 负责身份、权限、Session lease、草稿校验、幂等和数据库事务。
- DSH Runtime 负责 Agent 推理和 `AskUserQuestion` 语义；Bridge 只做协议适配和生命周期管理。
- MCP 保持原子、无状态。Runtime 只能提交临时草稿，不能创建项目、修改正式 profile 或修改工作区文件。
- 浏览器只能选择 Server 能力注册表中的 Agent，不能提交 profile 路径、插件路径、MCP 配置或用户身份。
- 未提交的 Session 和草稿不持久化。Runtime 退出、页面刷新、关闭、Server 重启或 lease 超时后，数据不可恢复。

## UX

项目页的“创建项目”入口跳转到 `/chat?agent=project-creator&intent=create-project`；`/projects/create` 是同一工作区的语义别名。通用 `/chat` 初始没有活动 Agent，顶部模式选择器显示 Server 返回的可用 Agent。选择 `Project Creator` 后立即启动临时会话并触发 Runtime `session/start`，首轮欢迎语和问题直接进入消息流。

页面顶部保留 Studio 导航、上下文、Agent 模式选择器和运行状态。中间是限制阅读宽度的消息流，底部是固定输入框；桌面右侧是六维进度栏，移动端改为可横向滚动的紧凑状态条。问题卡支持单选、多选和补充文本；问题卡提交前禁用普通输入框，提交后保留已回答状态。Agent 可以动态追加问题，不采用固定步骤表单；“继续修改”会回到普通对话。

页面不显示“新会话”按钮。用户已经产生输入或草稿后切换 Agent，必须明确确认丢弃临时草稿。离开页面时尽力提示“当前项目草稿尚未保存，离开后将丢失”；刷新不会恢复旧会话。Runtime 断开、过期、错误和关闭都保留已有消息与本地确认信息，展示错误状态和“重新开始创建”操作。

六维进度由 Agent 提交的 `draft.updated` / `draft.ready` 驱动，Server 只校验结构，不决定提问顺序。每个维度状态为“未开始、已收集、待确认、已确认”。最终确认卡展示项目名称、定位与平台、六维摘要、缺失字段和推断字段。只有通过 Server Schema 校验并带有效确认凭据时才启用“确认创建项目”。

## 事件和会话协议

公开 API 为：

```text
GET    /api/v1/interactive/agents
POST   /api/v1/interactive/sessions
GET    /api/v1/interactive/sessions/:id/events
POST   /api/v1/interactive/sessions/:id/messages
POST   /api/v1/interactive/sessions/:id/questions/:callId/answers
POST   /api/v1/interactive/sessions/:id/commit
DELETE /api/v1/interactive/sessions/:id
```

创建请求只接受 `{ "agent_id": "project-creator", "intent": "create-project" }`。提交请求接受确认凭据和幂等键；用户身份、Session owner、Agent profile 和 MCP 权限均由 Server 推导。

Bridge 将 DSH 原始事件转换成稳定事件：

```text
session.ready
session.status
assistant.message
question.opened
question.answered
draft.updated
draft.ready
session.expired
session.error
session.closed
```

SSE 使用递增事件 ID；Studio 断线时以 `Last-Event-ID` 有限次退避重连。终态事件停止重连。Server 多副本首期要求同一 Session 的请求和 SSE 粘到同一实例；Server 重启会使临时会话失效，不做迁移和恢复。

DSH SDK 扩展 `session/start`、`session/prompt`、`session/answer`、`shutdown`。`session/start` 是 Runtime 控制事件，不伪造用户消息，不写入隐藏提示词；`session/answer` 校验 live root Agent、`callId`、一次性回答和结构化问题 Schema，并复用 DSH `userQuestions.answer()`。

## 权限边界

Agent Pack manifest 以 `interaction.user_questions: true` 显式声明交互能力。现有 `profile-analysis`、article、seednote 等托管 Agent 保持禁止 `AskUserQuestion`；`project-creator` 通过独立 DSH runtime 启用；子 Agent 不得直接向用户提问。Server 注册表是最终准入点，`runtime-claude/src/runner.ts` 的托管执行禁止逻辑不直接放开。

Project Creator 只获得白名单 capability：`stage_project_creator_draft` 及其所需读取能力。该原子能力校验完整项目字段和六维画像，替换当前 Session 临时草稿、递增 revision 并发布事件；不创建数据库项目、不修改正式 profile、不写入 `AGENTS.md`、`CLAUDE.md`、项目 memory 或活动生成上下文。

## 提交事务

用户确认后，Studio 提交确认 token 和幂等键。Server 在一个事务中创建项目、创建 `ProjectProfileRevision`、写入项目 profile、保持 profile 状态为 `draft`、写入幂等记录并返回项目。重复使用相同幂等键和内容返回同一结果；相同幂等键提交不同内容返回冲突。提交成功后关闭 Session 并跳转项目详情或画像确认页；失败保留确认卡以便重试。

## 失败和生命周期

Session 创建失败时保留 Agent 选择器和表单兜底入口。问题回答失败时保留问题卡并允许重试。Runtime 启动失败、SSE 断开、lease 超时和 Server 重启均产生明确状态；重新开始直接创建新 Session。提交成功前数据库中不存在项目，临时草稿只在 Session 内存中存在，lease 到期会清理 Runtime、token、草稿和待回答问题。

## 验收

- 项目页进入后无需点击“新会话”即可看到 Project Creator 首轮对话。
- 通用聊天入口能通过 DSH 风格模式选择器选择可用 Project Creator。
- 页面和协议不出现 `/init`。
- Agent 能连续提出结构化问题，用户能用选项或文本回答。
- 六维进度随草稿更新，不强制固定顺序。
- 确认前数据库不存在项目；重复提交只创建一个项目。
- Runtime 消失后 Session 和草稿不可恢复。
- 现有托管 Agent 仍禁止 `AskUserQuestion`，Project Creator 仅使用白名单 capability。
- DSH SDK、Bridge、Go Session/API/MCP、Agent Pack 和 Studio 测试覆盖协议、权限、幂等、错误和响应式交互。
