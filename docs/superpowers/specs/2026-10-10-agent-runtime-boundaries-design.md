# Agent Runtime 边界设计

## 目标

将当前同时承载 Claude 托管任务和 DSH 交互 Bridge 的 `agent-ts/` 拆分为两个职责清晰的 TypeScript runtime：

- `runtime-claude/`：一次性、无交互的 Claude Agent SDK 托管执行。
- `runtime-dsh/`：短生命周期、可回答结构化问题的 DSH 交互执行。

Project Creator 的业务 Agent id、Agent Pack、Server Session API 和 Studio UX 不改变；本次只调整执行承载边界、依赖、构建和发布契约。

## 边界

`runtime-claude` 保留现有 `main.ts`、Claude runner、bootstrap、artifact、reporter、workspace、Hypit 和托管任务测试。它不依赖 DSH SDK，也不包含交互 Bridge。

`runtime-dsh` 只包含 `dsh-bridge.ts` 及其协议测试，依赖 DSH SDK 和 Node 类型工具链。它不包含 Claude Agent SDK、托管任务 artifact、billing、resume 或 completion 逻辑。

Server 负责在启用交互功能时从 `/app/runtime-dsh` 启动 Bridge；托管 Agent 镜像从 `/app/runtime-claude` 构建并运行 `runtime-claude`。业务 Agent Pack 与 runtime 实现解耦，`project-creator` 只是 DSH runtime 上的一个能力注册 Agent。

## 构建与发布

- Agent Dockerfiles 复制 `runtime-claude/`。
- Server Dockerfile 只构建和复制 `runtime-dsh/`。
- CI 分别测试两个 runtime；Release 分别产出 `anban-runtime-claude-<version>.tar.gz` 和 `anban-runtime-dsh-<version>.tar.gz`。
- `ANBAN_DSH_BRIDGE_DIR` 默认指向 `./runtime-dsh`，Compose 镜像内指向 `/app/runtime-dsh`。
- `ANBAN_AGENT_IMAGE_*`、`runtime_profile` 和 Agent Pack id 保持不变，因为它们描述业务 Agent 镜像身份，不是源码目录。

## 迁移约束

- 不改变 DSH wire protocol：`session/start`、`session/prompt`、`session/answer`、`shutdown` 保持不变。
- 不改变托管 Agent 的 bootstrap、artifact、completion 和运行时身份协议。
- 不保留一个同时包含 Claude 与 DSH 依赖的兼容 package。
- 历史设计文档可保留原路径作为历史记录；当前源码、部署、CI、契约测试和运行文档必须使用新目录名。
- 默认交互开关继续关闭，只有配置 Bridge、DSH patch、MCP URL 后才启用。

## 验收

- `runtime-claude` 可独立安装、typecheck、测试和构建。
- `runtime-dsh` 可独立安装、typecheck、测试和构建。
- Agent 镜像契约不包含 DSH runtime 目录或 DSH 依赖。
- Server 镜像契约不包含 Claude runtime 目录或 Claude Agent SDK。
- Server 交互服务能从 `runtime-dsh/dist/dsh-bridge.js` 启动。
- CI、Release、Makefile、文档和 Docker contract tests 不再把 `agent-ts` 当作当前 runtime 名称。
