# Claude Profile 显式环境配置设计

## 背景

当前 `claude.execution_profile_env_defaults` 会先把共享 Claude Runtime 环境变量复制到每个执行 Profile，再由 Profile 的 `envs` 覆盖。它减少了 YAML 重复，但引入了隐式继承；审计单个 Profile 时无法直接看到完整运行环境。

## 决策

删除共享默认层。每个 `effective`、`balanced`、`quality` Profile 的 `envs` 必须完整声明自身所需的 Claude Runtime 环境变量。

- 从 Go 配置模型删除 `ExecutionProfileEnvDefaults`。
- 从 Claude YAML 严格字段集合删除 `execution_profile_env_defaults`；旧字段直接导致启动失败。
- 删除默认值校验及合并逻辑，不保留兼容解析器。
- `server/config.example.yaml` 在三个 Profile 中显式写入原先共享的环境变量。
- 保留各 Profile 当前覆盖值：`effective` 使用 medium effort 和 `393216` output，`quality` 启用 always-enable-effort。
- Provider、认证、模型字段继续由各 Profile 独立声明。
- `model_usage_aliases` 规则不变：缺失映射由 Server 派生恒等映射，配置只保留 `"kimi-k3[1m]": "kimi-k3"`。

## 行为合同

配置切换前后，三个 Profile 解析后的 `envs`、冻结快照、指纹输入、Agent Bootstrap 环境和生成行为必须等价。唯一有意变化是配置语法：新 Server 不再接受 `execution_profile_env_defaults`。

历史 Task/Execution 已固化的 Profile Snapshot 不迁移、不重写；续跑仍从历史快照恢复非密钥环境，并只注入当前认证密钥。

## 测试

- 配置测试验证 `execution_profile_env_defaults` 被作为未知 Claude 字段拒绝。
- `config.example.yaml` 完整加载测试验证三个 Profile 都包含完整公共环境字段及各自差异值。
- Agent Profile/Bootstrap 现有测试继续验证快照、指纹、密钥脱敏、alias 派生和历史续跑。
- 完整验证执行 `go test ./...`、Server/Agent build、Studio test/build；Studio 无行为变更，但作为仓库合并门禁保留。

## 部署

仅 Server 配置和 Go Server 配置解析发生变化。实现提交本身只要求重建 Server 镜像，并将生产 ConfigMap 与新镜像原子更新；Agent、Studio 和 wcflink 镜像不因本次后续调整而重建。

部署前应将生产 ConfigMap 中的共享字段展开到三个 Profile，并确认不再包含 `execution_profile_env_defaults`。旧 ConfigMap 与新 Server 不兼容，这是预期的前向切换。
