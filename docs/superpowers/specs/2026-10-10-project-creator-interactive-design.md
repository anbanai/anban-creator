# Project Creator 交互式项目创建设计

## 目标

把项目创建和画像维护从封闭式输入输出 Agent 扩展为 Studio 内的交互式工作区。用户选择 `Project Creator` 后，Studio 自动创建一个短生命周期会话，Agent 通过 DSH 的结构化问题协议逐步收集或修订项目定位、风格、受众、平台、偏好与记忆六个维度，最后由 Server 校验并提交。

`profile-analysis` 继续作为现有零交互托管 Agent，行为和权限不变。首期不引入 `/init` 命令，也不提供“新会话”按钮；创建页面本身就是一次临时会话。

## 设计原则

- Studio 负责用户体验和事件渲染，Server 负责身份、权限、Session lease、草稿校验、幂等和数据库事务。
- DSH Runtime 负责 Agent 推理和 `AskUserQuestion` 语义；Bridge 只做协议适配和生命周期管理。
- MCP 保持原子、无状态。Runtime 只能提交临时草稿，不能创建项目、修改正式 profile 或修改工作区文件。
- 浏览器只能选择 Server 能力注册表中的 Agent，不能提交 profile 路径、插件路径、MCP 配置或用户身份。
- 未提交的 Session 和草稿不持久化。Runtime 退出、页面刷新、关闭、Server 重启或 lease 超时后，数据不可恢复。
- 现有项目上下文由 Server 校验项目归属后生成受限快照；Runtime 不获得原项目路径或文件系统访问权。快照中的文件正文是不可信参考数据，不是 Agent 指令。
- 数据库画像是六维结构化事实源。`AGENTS.md` 和六个 `profile/*.md` 是 Server-owned 的可重建投影；Studio 确认后，Server 先以画像版本 CAS 更新数据库，再通过 memory NAS 原子重投影这些文件。
- 根 `MEMORY.md` 属于项目 memory NAS 的持续维护内容。Project Creator 可以提交受控的追加/合并意图，但不能任意替换文件；Server 使用快照内容指纹或 NAS 版本 CAS 应用变更。并发修改或 NAS 写入失败不能覆盖已有内容，数据库成功后由可重试投影任务补齐。
- Studio 对项目记忆的人工编辑也只能通过 Server 的 NAS 编辑接口完成。普通 Markdown 文件的请求必须带读取时的 `expected_content`，Server 在项目锁内执行原子 CAS 替换；内容发生并发变化时返回冲突，要求刷新后重试。根 `MEMORY.md` 属于持续记忆，不开放整体替换，只接受 Project Creator 或 Server 业务服务提交的受控追加，并使用快照内容指纹 CAS 防止覆盖并发修改。`AGENTS.md`、`CLAUDE.md`、`profile/*` 和 `.claude/*` 不开放人工文件编辑：前两类由 Server/画像事实源生成，后者属于运行时私有记忆。

## UX

项目页的“创建项目”入口跳转到 `/chat?agent=project-creator&intent=create-project`；`/projects/create` 是同一工作区的语义别名。已有项目卡片提供“对话更新画像”入口，跳转到 `/chat?agent=project-creator&intent=update-project-profile&project_id=:id`。通用 `/chat` 初始没有活动 Agent，顶部模式选择器显示 Server 返回的可用 Agent。选择 `Project Creator` 后立即启动临时会话并触发 Runtime `session/start`，首轮欢迎语和问题直接进入消息流。

已有项目模式在页面显示项目名称和“更新现有项目画像”上下文。Agent 收到 Server 生成的只读快照，自主决定保留哪些维度、需要修改哪些内容以及是否需要重新询问；用户确认卡明确执行“更新画像”。新建模式仍使用“确认创建项目”。

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

创建请求接受 `{ "agent_id": "project-creator", "intent": "create-project" }`；更新请求接受 `{ "agent_id": "project-creator", "intent": "update-project-profile", "project_id": "..." }`。`project_id` 只用于选择上下文；Server 校验当前用户归属，并把项目 ID、当前画像版本和只读快照绑定到 Session，不能由 Runtime 或提交请求改变。提交请求仍只接受确认凭据和幂等键；用户身份、Session owner、Agent profile、MCP 权限和提交目标均由 Server 推导。

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

DSH SDK 扩展 `session/start`、`session/prompt`、`session/answer`、`shutdown`。`session/start` 可携带受长度限制的 Server 生成 `initialContext`，DSH 将其作为 `runtime-context` 来源的注入快照加入 root Agent 上下文，再启动首轮；它不是用户消息，不伪造用户输入。`session/answer` 校验 live root Agent、`callId`、一次性回答和结构化问题 Schema，并复用 DSH `userQuestions.answer()`。

## 权限边界

Agent Pack manifest 以 `interaction.user_questions: true` 显式声明交互能力。现有 `profile-analysis`、article、seednote 等托管 Agent 保持禁止 `AskUserQuestion`；`project-creator` 通过独立 DSH runtime 启用；子 Agent 不得直接向用户提问。Server 注册表是最终准入点，`runtime-claude/src/runner.ts` 的托管执行禁止逻辑不直接放开。

Project Creator 只获得白名单 capability：`stage_project_creator_draft` 及其所需读取能力。该原子能力校验项目字段和六维画像，替换当前 Session 临时草稿、递增 revision 并发布事件；不创建项目、不修改正式 profile、不写入 `AGENTS.md`、`CLAUDE.md`、项目 memory 或活动生成上下文。快照将现有文件内容作为引用数据，Agent 必须把文件正文视为不可信数据而非对 Agent 的新指令。

更新模式的快照由 Server 按需构造：项目元数据、当前数据库画像渲染出的 `AGENTS.md` 与六个 `profile/*.md`，以及项目 memory 根目录中的 `MEMORY.md`（若存在且通过大小限制）。不传入 `.claude/agent-memory` 等运行时私有目录，不递归暴露其他文件。快照明确记录来源画像版本和截断状态。

## 提交事务

用户确认后，Studio 提交确认 token 和幂等键。新建模式在一个事务中创建项目、创建 `ProjectProfileRevision`、写入 profile（状态为 `draft`）、写入幂等记录并返回项目。更新模式在一个事务中锁定原项目，确认归属和画像版本仍与 Session 快照一致，更新六维画像并递增 revision、记录 `ProjectProfileRevision`、保留原有 profile/初始化生命周期状态、写入幂等记录并返回原项目；版本冲突时不写入任何内容，要求用户重新同步。两种模式成功后由 Server 通过 memory NAS 原子重投影 `AGENTS.md` 和六个 `profile/*.md`，并按快照指纹/CAS 应用根 `MEMORY.md` 的受控变更；NAS 投影失败不回滚已提交的数据库事务，写入任务必须可重试且不能覆盖并发修改。重复使用相同幂等键和确认凭据返回同一结果；相同幂等键提交不同内容返回冲突。提交成功后关闭 Session 并跳转相应项目详情或画像页；失败保留确认卡以便重试。

## 失败和生命周期

Session 创建失败时保留 Agent 选择器和表单兜底入口。问题回答失败时保留问题卡并允许重试。Runtime 启动失败、SSE 断开、lease 超时和 Server 重启均产生明确状态；重新开始直接创建新 Session。提交成功前数据库中不存在项目，临时草稿只在 Session 内存中存在，lease 到期会清理 Runtime、token、草稿和待回答问题。

## 验收

- 项目页进入后无需点击“新会话”即可看到 Project Creator 首轮对话。
- 通用聊天入口能通过 DSH 风格模式选择器选择可用 Project Creator。
- 页面和协议不出现 `/init`。
- Agent 能连续提出结构化问题，用户能用选项或文本回答。
- 六维进度随草稿更新，不强制固定顺序。
- 新建模式确认前数据库不存在项目；重复提交只创建一个项目。
- 更新模式只接受用户拥有的项目，快照绑定画像 revision，过期 revision 的提交冲突且不覆盖新画像。
- 更新模式的项目确认和画像 revision 写入原子完成；Server 通过 memory NAS 重投影六个画像文件和 `AGENTS.md`，并以 CAS 应用受控的根 `MEMORY.md` 变更。
- 更新模式只向 Agent 暴露白名单 Markdown 快照，不暴露任意路径、私有 Agent memory 或写文件能力。
- Runtime 消失后 Session 和草稿不可恢复。
- 现有托管 Agent 仍禁止 `AskUserQuestion`，Project Creator 仅使用白名单 capability。
- DSH SDK、Bridge、Go Session/API/MCP、Agent Pack 和 Studio 测试覆盖协议、权限、幂等、错误和响应式交互。
