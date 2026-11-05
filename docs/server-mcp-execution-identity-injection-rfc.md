# Server/MCP Execution Identity Injection RFC

状态：提案；本次只定义契约，不修改 Server 或 MCP 行为。

## 问题

托管 Agent 已经在执行范围内拥有 task、project 和 execution 身份，但业务工具仍要求每次显式重复传参。缺少托管上下文时，Agent 容易自行降级为本地运行；显式参数和执行范围不一致时，也缺少统一的拒绝规则。

## 目标

- Server 在 bootstrap 时向一次性 Agent 容器注入执行范围。
- MCP 在同一执行范围内自动补全省略的 task/project 参数。
- 省略参数、显式同值和冲突值都有确定行为。
- 环境变量只提供上下文便利，不替代执行 token 授权。
- API key 或非执行上下文仍必须显式提供身份。

## Bootstrap 契约

Server 为每次执行注入以下只读环境变量：

```text
ANBAN_TASK_ID
ANBAN_PROJECT_ID
ANBAN_EXECUTION_ID
ANBAN_WORKSPACE_ROOT
ANBAN_EXECUTION_CONTEXT_FILE
```

`ANBAN_EXECUTION_CONTEXT_FILE` 指向任务私有、只读、版本化 JSON，建议包含 `schema_version`、`task_id`、`project_id`、`execution_id`、`workspace_root`、`issued_at`、`expires_at` 和允许的 MCP scope。文件内容不包含 API key 或执行 token。

Server 必须在 Agent 启动前验证变量与上下文文件一致；缺失、格式错误、过期或跨任务挂载都在 bootstrap 阶段失败。

## MCP 中间件行为

中间件从 execution scope 读取默认值：

| 请求 | 行为 |
| --- | --- |
| 省略 task/project | 自动补全 execution scope 中的值 |
| 显式提供且同值 | 接受并记录为 explicit_same_scope |
| 显式提供且冲突 | 立即返回 `execution_identity_mismatch`，不调用业务能力 |
| 非执行上下文 | 要求显式身份和授权 |
| 跨项目访问 | 返回 `execution_identity_mismatch`，不尝试降级 |

中间件只做认证、上下文补全和参数一致性校验；它不组合工作流步骤，不决定最终发布状态。

## 授权与安全

执行 token 仍是授权来源。环境变量和只读上下文文件不能单独获得访问权限，也不能被下游业务当成 API key。日志只记录 execution id 和脱敏的 scope，不记录 token、API key 或完整上下文文件。

显式参数优先用于展示和审计，但不能覆盖 execution scope。需要跨项目操作时必须由 Server 创建新的 execution scope，而不是让 Agent 改写环境变量。

## 兼容与迁移

旧客户端继续显式传 task/project 时，在存在 execution scope 的情况下按“显式同值”处理；不提供 scope 的 API key 调用保持现有显式身份要求。Server 可在过渡期把省略参数的旧请求标记为 `legacy_explicit_identity_required`，但不能猜测 task/project。

## 测试矩阵

Server/MCP 实现时必须覆盖：

1. 省略 task/project 自动补全；
2. 显式同值通过；
3. task 冲突拒绝；
4. project 冲突拒绝；
5. 跨项目访问拒绝；
6. 缺失/过期上下文文件拒绝；
7. 旧客户端显式身份兼容；
8. 环境变量存在但 execution token 无效时仍拒绝。

## 与 Agent/Skill 的边界

Agent 只读取运行时提供的上下文，不从目录名、日志或旧产物猜身份。`runtime_mode=local_test` 是本地直接调用的显式声明；它不能伪装成 managed，也不能自动获得 Server/MCP 权限。Server/MCP 的实现、持久化和最终业务状态仍归 Server 所有。
