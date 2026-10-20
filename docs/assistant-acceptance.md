# AI 助手验收记录

本次整合画像、创作计划和作品复盘入口。三个模式由 Server 决定准入，
首页及旧新手引导入口均不会自动启动 Runtime。

| 需求 | 实现与验收入口 |
| --- | --- |
| 无画像／有画像无作品／有完成作品的准入 | `server/service/assistant_test.go`、`studio/src/pages/AssistantHome.test.tsx` |
| 模式名称、说明、禁用原因、画像与作品选择 | `AssistantHome`，桌面及 390px 手机浏览器检查 |
| 首页零 Runtime，显式开始 | Bootstrap 与 Session API 分离，页面及 handler 测试 |
| 三个隔离 Preset，共用 Runtime 镜像 | Bridge allowlist、容器／Kubernetes 测试、三个真实 DSH 启动 smoke |
| 用户／模式／画像／作品隔离 | Server 身份与 MCP capability gate、浏览器缓存 scope、租约唯一约束 |
| 结构化恢复，不重放聊天 | Server-owned checkpoint、所有权／版本校验、重启与前端恢复测试 |
| 保存成功后再关闭及切换 | ChatPage 保存失败保留会话，旧会话关闭后回到目标模式上下文选择 |
| 确认的草稿就是提交的内容 | 显示 payload 与 Server 草稿比对、payload digest、提交冻结与冲突测试 |
| 画像确认与创建作品任务 | Profile CAS；冻结计划交给现有一次性 Task 执行，DSH 不负责生产 |
| 创建后崩溃不重复创建／计费 | Task checkpoint 唯一键、冻结输入比对、已有任务恢复保护与回执重试 |
| 复盘不立即更新权威画像 | candidate 与回执同事务；用户确认后仍需独立后续 analytics revision |
| 单控制面与升级隔离 | `replicas: 1`、`Recreate`，运行说明见 `server/ASSISTANT-RUNTIME.md` |

模式切换先保存并释放旧会话，再回到目标模式的画像／作品选择。用户点击
“开始对话”后才创建新会话；这让复盘有机会选定作品，也避免仅浏览模式就分配 Pod。

本地验收命令：

```sh
make agent-pack-check
(cd server && go test ./... && go build -o /tmp/anban-assistant-server .)
(cd studio && bun run test -- --maxWorkers=2 && bun run build)
(cd runtime-dsh && bun run test && bun run typecheck && bun run test:startup)
(cd harness && bun run build && bun run typecheck && bun run test)
node harness/dsh/scripts/package-integrity.mjs pack
```

正式部署后再采集启动延迟、会话成本和 candidate 验证率；本地测试不代表
已验证真实集群、外部模型质量或线上性能。首期不启用预热池。
