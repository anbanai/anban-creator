# Claude Code Agent 与 Skill 约定

> 更新日期：2026-07-21
>
> 依据：Claude Code 官方 [Subagents](https://code.claude.com/docs/en/sub-agents)、[Skills](https://code.claude.com/docs/en/skills) 与 [Plugins reference](https://code.claude.com/docs/en/plugins-reference)。

## 官方能力边界

- 插件 Agent 支持 `skills` frontmatter；声明的 Skill 正文会在 Agent 启动时注入。
- 插件 Skill 会自动发现，用户仍可直接调用未被 Agent 预载的入口 Skill。
- 插件 Agent 不使用官方未支持的 `permissionMode`、`hooks` 或 `mcpServers` frontmatter 字段。
- Agent 负责端到端业务流程、完成条件与恢复语义；Skill 负责可复用的专业方法；MCP 负责服务端动作和结构化数据边界。

## 当前策略

Agent 只声明实际使用的专业 Skill，不声明与 Agent 正文重复的 umbrella workflow Skill：

| Agent | 启动注入的专业 Skills |
|---|---|
| `designer` | `line-art-coloring` |
| `ecommerce` | `ecommerce-product-analysis`、`ecommerce-copywriting`、`humanizer`、`ecommerce-visual-design`、`ecommerce-platform-specs` |
| `live-slicer` | `live-slice`、`capcut-draft` |
| `moments` | `moments`、`humanizer` |
| `montage` | `montage` |
| `seednote` | `agent-reach`、`seednote-research`、`seednote-viral-analysis`、`seednote-writing`、`seednote-visual-design` |
| `videocreator` | 无；Agent 直接拥有 MCP 视频策划与生成流程 |
| `videoeditor` | `video-use`、四个 overlay Skills、`capcut-draft` |
| `wechatarticle` | 文章专业阶段 Skills 与 `humanizer` |

`article`、`ecommerce` 等 umbrella Skills 保留为用户直接入口，但不随对应 Agent 重复注入。

## 编写规则

1. 不在 Agent 正文重复“加载 Skill”、插件限定名、调用 payload、短回执或隔离执行说明。
2. Skill 输入输出使用文件化产物；Agent 正文只描述阶段目标、业务边界、失败和恢复行为。
3. 删除没有 Agent owner、用户入口或测试证明用途的 Skill；不保留废弃兼容别名。
4. 修改 Agent、Skill、Hook 或运行时文档时，同步升级插件版本并运行契约测试。
5. 评估至少覆盖 should-trigger、should-not-trigger、with-Skill、without-Skill、失败与恢复场景。

## 验证

```bash
go test ./server/agent ./server/mcp -count=1
go test ./... -count=1
claude plugin validate ./claudecode/.claude-plugin/plugin.json --strict
claude plugin validate ./claudecode/.claude-plugin/marketplace.json --strict
```
