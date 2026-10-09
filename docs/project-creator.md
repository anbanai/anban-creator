# Project Creator 交互式项目创建

## 方案结论

新增交互 Agent：

- 内部 id：`project-creator`
- 展示名：`Project Creator`
- `profile-analysis` 保留为现有零交互托管 Agent，不改行为。
- 不使用 `/init`。
- 不显示“新会话”按钮，也不要求用户确认一次“开始新会话”。
- 用户选择 Agent 后，Studio 自动创建临时会话并启动 Agent。
- 临时会话只存在于当前创建页面；Runtime 退出、页面刷新、关闭或超时后，会话和未提交草稿全部丢失。

参考 DSH 的 UX 结构和交互协议，但不直接嵌入完整 DSH Web Shell。Anban Studio 使用自己的 React 组件适配 DSH 的模式选择器、聊天、问题卡和消息状态。

## UX

### 入口与路由

- 新增通用聊天工作区，例如 `/chat`。
- 项目页的“创建项目”入口跳转到：
  `/chat?agent=project-creator&intent=create-project`
- 通用聊天入口默认没有活动 Agent；用户在顶部模式选择器中选择 `Project Creator` 后立即启动临时会话。
- `/projects/create` 可作为面向项目创建的语义别名，最终渲染同一聊天工作区。
- 现有表单保留为次级入口：“使用表单创建”，用于故障兜底和已有用户习惯。

### 页面结构

桌面端：

- Studio 原有导航保持不变。
- 聊天区域顶部显示：
  - 当前工作区或项目上下文；
  - DSH 风格的 Agent 模式选择器；
  - 当前 Agent 名称、图标和运行状态。
- 中间为消息流，限制最大阅读宽度。
- 底部为固定输入框。
- 右侧显示六维画像收集进度。

移动端：

- Agent 选择器保持在顶部；
- 六维进度改为可横向滚动的紧凑状态条；
- 输入框固定在底部；
- 问题卡和确认卡使用全宽布局。

### Agent 模式选择器

模式选择器行为：

1. 初始没有活动 Agent 时，显示可用 Agent 列表。
2. 选择 `Project Creator` 后，自动创建临时 Runtime Session。
3. 不出现“新会话”按钮。
4. 会话开始后，当前 Agent 名称保留在顶部。
5. 会话已经产生用户输入后，不允许静默切换 Agent；切换需要明确提示会丢弃当前临时草稿。
6. Agent 列表来自 Server 返回的能力注册表，浏览器不能提交任意 profile、插件路径或 MCP 配置。

### 首次启动

选择 `Project Creator` 后：

1. Studio 调用创建临时会话接口。
2. Server 启动一个专属 TypeScript Bridge 子进程。
3. Bridge 启动 DSH Runtime 并绑定 `project-creator` preset。
4. Runtime 通过受控的 `session/start` 事件触发第一轮 Agent 行为。
5. Agent 自动发出欢迎语和第一组结构化问题。

`session/start` 是 Runtime 控制事件，不是伪造的用户消息，不显示 `/init`，也不会把隐藏提示词写入聊天记录。

### 对话交互

聊天区支持：

- 普通文本消息；
- Agent Markdown 回复；
- DSH 风格 `AskUserQuestion` 问题卡；
- 单选、多选、自定义文本；
- Agent 思考、等待回答、错误和完成状态。

问题卡规则：

- 一次显示一个待回答的问题批次；
- 问题卡未提交前，普通输入框禁用，避免并行回答造成顺序冲突；
- 用户提交后，卡片变为已回答状态；
- Agent 可根据回答动态追加问题，不使用固定四步表单；
- 用户可以通过“继续修改”回到普通对话，让 Agent重新调整已收集内容。

### 六维进度

进度栏展示以下六个维度：

- 定位 `identity`
- 风格 `style`
- 受众 `audience`
- 平台 `platforms`
- 偏好与红线 `preferences`
- 记忆 `memory`

每个维度只有状态，不对应固定步骤：

- 未开始；
- 已收集；
- 待确认；
- 已确认。

进度由 Agent 提交的结构化草稿驱动，Server 只负责校验，不决定 Agent 的提问顺序。

### 最终确认卡

Agent 收集足够信息后提交临时草稿，Studio显示确认卡：

- 项目名称；
- 项目定位与平台；
- 六维画像摘要；
- 缺失字段和推断字段；
- “确认创建项目”；
- “继续修改”。

未通过 Server Schema 校验时，不显示可用的创建按钮。

用户点击确认后：

1. Studio 提交确认凭据和幂等键；
2. Server 在事务中创建项目；
3. 写入六维画像草稿和 profile revision；
4. profile 状态保持 `draft`，不能因为创建项目而自动变成 `confirmed`；
5. 会话关闭；
6. 跳转项目详情页或画像确认页。

### 离开、刷新和失败

- 离开页面且项目未提交时，显示：
  “当前项目草稿尚未保存，离开后将丢失。”
- 浏览器 `beforeunload` 只做尽力提醒，不承诺恢复。
- 刷新后创建新临时会话，不恢复旧对话。
- Runtime 断开、超时或 Server 重启后，显示明确错误状态。
- 用户可点击“重新开始创建”，该操作直接创建新临时会话，不展示“新会话”术语。
- 创建提交失败时保留确认卡，允许重试。
- Server 成功提交后，重复请求必须返回同一项目结果，不能重复创建。

## 技术架构

### Server

新增交互会话服务，负责：

- 用户鉴权；
- Session 所有权；
- Runtime lease 和超时；
- Agent 能力校验；
- SSE 事件转发；
- 问题回答转发；
- 临时草稿校验；
- 最终项目创建和幂等提交。

建议接口：

```text
GET    /api/v1/interactive/agents
POST   /api/v1/interactive/sessions
GET    /api/v1/interactive/sessions/:id/events
POST   /api/v1/interactive/sessions/:id/messages
POST   /api/v1/interactive/sessions/:id/questions/:callId/answers
POST   /api/v1/interactive/sessions/:id/commit
DELETE /api/v1/interactive/sessions/:id
```

创建会话请求至少包含：

```json
{
  "agent_id": "project-creator",
  "intent": "create-project"
}
```

提交请求至少包含：

```json
{
  "confirmation_token": "...",
  "idempotency_key": "...",
  "project": {},
  "profile": {}
}
```

`user_id`、Session owner、Agent profile 和 MCP 权限全部由 Server 侧推导，不能信任浏览器字段。

### TypeScript Bridge

Bridge 使用 DSH SDK，并按会话启动一个临时子进程：

- Go Server 管理生命周期；
- Bridge 使用 DSH SDK 启动 DSH Runtime；
- 一个 Project Creator Session 对应一个独立 Runtime；
- Bridge 退出后，不保留 Agent 或对话状态；
- 不新增持久化 Agent 服务；
- Bridge 将 DSH 的原始 Session Event 转换成 Anban 稳定事件。

建议事件类型：

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

多副本部署首期要求同一临时 Session 的请求和 SSE 连接落到同一 Server 实例；Server 重启会使所有临时会话失效，不做恢复迁移。

### DSH SDK 扩展

当前 DSH SDK 只有 `initialize`、`session/prompt`、`shutdown`，无法从外部回答 `AskUserQuestion`。需要在 `deepseek-harness` 增加：

```text
session/start
session/prompt
session/answer
shutdown
```

`session/answer` 必须：

- 校验 Session 所属的 live root Agent；
- 校验 `callId` 属于当前未回答问题；
- 校验每个问题恰好回答一次；
- 拒绝子 Agent、过期问题和重复回答；
- 复用 DSH `userQuestions.answer()` 的现有语义；
- 保留 DSH 的结构化问题 Schema。

### AskUserQuestion 权限

禁止策略按 Agent 能力区分：

- 现有 `profile-analysis`、article、seednote 等托管 Agent：继续禁止 `AskUserQuestion`；
- `project-creator`：显式声明允许 `AskUserQuestion`；
- 未来交互 Agent：必须在 Agent Pack manifest 中声明交互能力后才能启用；
- 子 Agent：禁止直接向用户提问；
- Server 只允许注册表中的 Agent 使用交互能力；
- `agent-ts/src/runner.ts` 的现有全局禁止逻辑不直接放开，Project Creator 使用独立 interactive runner 配置。

### 临时草稿与 MCP 边界

新增 Project Creator 专用的原子能力：

```text
stage_project_creator_draft
```

它只做：

- 校验完整的项目字段和六维画像；
- 替换当前 Session 的临时草稿；
- 递增临时 draft revision；
- 发布 `draft.updated` 或 `draft.ready` 事件。

它不创建数据库项目，也不修改正式 profile。

最终提交由 Server 业务服务完成，可命名为：

```text
commit_project_creator_draft
```

提交事务负责：

- 创建项目；
- 创建 `ProjectProfileRevision`；
- 写入项目 profile；
- 设置 profile 为 `draft`；
- 写入幂等记录；
- 返回已创建项目。

Runtime、Agent 和日志都不能修改 `AGENTS.md`、`CLAUDE.md`、正式项目 profile、项目 memory 或活动生成上下文。

## Agent Pack

新增 `harness/packs/project-creator/`：

- `agent-pack.yaml`
- `agent.claude.md`
- `agent.codex.toml`
- 必要的结构化草稿 Schema
- `interaction.user_questions: true`
- 只允许 Project Creator 所需 MCP capability
- 禁止发布、写入正式项目 profile 和调用其他工作流

同时清理所有旧的 `profile-analysis-interactive` 引用。若该 id 尚不存在，则直接创建 `project-creator`，不要把现有批处理 `profile-analysis` 改名。

任何 `harness/` Agent、Pack 或 manifest 变更，都要：

- 运行 `make agent-pack-generate`；
- 运行 `make agent-pack-check`；
- 同步递增 `harness/.claude-plugin/plugin.json` 和 `harness/.codex-plugin/plugin.json`；
- 保证 Pack、Claude Agent、Codex Agent 和生成 catalog 一致。

## 实施顺序

1. 先写入并评审设计文档和本实现计划。
2. 在 DSH 中增加 `session/start`、`session/answer` 协议及测试。
3. 创建 Project Creator Agent Pack 和能力注册表。
4. 实现 Go 交互会话服务、SSE、lease、草稿校验和最终事务提交。
5. 实现 TypeScript Bridge 和 DSH Runtime 生命周期管理。
6. 在 Studio 增加通用聊天工作区、Agent 选择器、问题卡、六维进度和确认卡。
7. 保留现有项目表单作为次级入口。
8. 完成单元测试、协议测试、API 测试和 Studio 交互测试。
9. 最后再启用配置开关和灰度入口。

## 验收标准

- 用户从项目页点击创建后，不需要点击“新会话”即可看到 Project Creator 对话。
- 通用聊天入口可以通过 DSH 风格模式选择器选择 Project Creator。
- 不出现 `/init`。
- Agent 可以连续提出结构化问题，用户可以用选项或文本回答。
- 六维进度随草稿更新，不强制固定步骤。
- 未确认前数据库中不存在项目。
- 确认后只创建一个项目，重复请求返回同一结果。
- Runtime 消失后临时会话和草稿不可恢复。
- 现有托管 Agent 仍禁止 `AskUserQuestion`。
- Project Creator 只能使用白名单 capability。
- Studio、Go、Agent Runner、DSH SDK 和 Agent Pack 测试全部通过。
