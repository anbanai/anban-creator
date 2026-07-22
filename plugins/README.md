# Anban 智能创作助手插件

> Claude Code 与 Codex 共用一份 Skill、模板、写作风格和运行资产；宿主差异只保留在原生 manifest、Agent、MCP/Hook 或安装适配层。

Codex 安装说明见 [docs/codex-installation.md](docs/codex-installation.md)。

## 接入流程

按下面顺序操作，第一次接入最省事：

1. 打开 [Anban Studio / Web 管理端](https://creator.anbanai.com) 注册或登录
2. 在设置页创建 API Key
3. 在 Claude Code 里安装插件
4. 按 Claude Code 提示填写插件配置
5. 运行 `/anban:anban-setup`
6. 完全退出并重新启动 Claude Code
7. 重启后再次运行 `/anban:anban-setup` 验证连接
8. 开始用自然语言或指定 Agent 创作

---

## 1. 注册账号

访问 [https://creator.anbanai.com](https://creator.anbanai.com)，先注册或登录你的 Anban 账号。

如果你还没有 API Key，后面的插件无法连接平台服务。

## 2. 创建 API Key

登录后前往 [设置页](https://creator.anbanai.com/settings) 创建一个新的 API Key。

- 建议给 Key 起一个容易识别的名字，例如 `My MacBook`、`Office Claude`
- Key 只会在创建成功时完整展示一次
- 如果当时没有复制完整 Key，需要回到设置页重新创建一个新的

## 3. 安装插件

从本仓库根目录注册并安装：

```bash
claude plugin marketplace add ./plugins
claude plugin install --scope user anban@anbanai
```

`plugin@marketplace` 中的 `anban` 是插件 ID，`anbanai` 是发布方/市场源。插件内的 MCP server key 固定为 `creator`；Agent 与 Skill 文档只使用裸 MCP 工具名（例如 `generate_image`、`prepare_workspace`），具体工具名前缀由 Claude Code 运行时处理。Agent 命名空间使用 `anban:<agent>`。

安装完成后，可以用 `/plugin` 或插件列表确认插件已经启用。

## 4. 设置 API Key

插件声明了 Claude Code 官方 `userConfig`：

- `api_key`：必填，敏感字段，用于连接 Anban Creator MCP 服务
- `api_url`：可选，默认 `https://api.creator.anbanai.com`

安装或启用插件时，Claude Code 会提示填写这些配置，并将敏感值保存到安全存储中。通常不需要手动编辑 `~/.claude/settings.json`。

如果你接的是自建或本地服务，在插件配置里把 `api_url` 改成你自己的服务地址。

## 5. 运行 `/anban:anban-setup`

安装并写好 Key 后，在 Claude Code 中运行：

```bash
/anban:anban-setup
```

`/anban:anban-setup` 会帮你检查：

- 本地视频工具是否已自动准备好
- API Key 是否生效
- MCP 服务是否连通
- 当前账号下有哪些可用项目

## 6. 重启 Claude Code

`/anban:anban-setup` 完成后，请**完全退出并重新启动 Claude Code**。

这是为了让新的环境变量和 MCP 连接真正生效。只刷新当前会话通常不够。

重启以后，再运行一次：

```bash
/anban:anban-setup
```

如果能看到项目列表或连接成功提示，就说明接入已经完成。

## 7. 开始使用

### 方式一：直接说需求

你可以直接输入自然语言，让插件自动识别内容类型：

```text
帮我写一篇关于 AI Agent 的公众号文章
种草笔记，主题是降噪耳机
把 ./live.mp4 做成直播切片
```

### 方式二：指定 Agent

如果你想明确指定流程，也可以直接运行：

```bash
claude --verbose --agent anban:wechatarticle AI Agent 入门指南
claude --verbose --agent anban:seednote 降噪耳机种草笔记
claude --verbose --agent anban:live-slicer ./live.mp4
```

如果你调整 Claude Code 权限模式，请只在受信任工作区中放宽权限，并确认本地媒体文件与输出目录都可被 agent 访问。

## 常用命令

- `/anban:anban-setup`
  初始化配置并验证连接
- `/plugin`
  查看插件是否已安装成功
- `/anban:article`
  公众号图文创作
- `/anban:live-slicer`
  直播视频切片，需要本机可用 `ffmpeg` 和 `ffprobe`

## 遇到问题时先检查

1. 是否已经在 [设置页](https://creator.anbanai.com/settings) 创建并复制了完整 API Key
2. Claude Code 插件配置里是否已经填写 `api_key`
3. 是否已经完全退出并重启过 Claude Code
4. 重启后是否重新执行过 `/anban:anban-setup`

## 支持的创作类型

| 类型 | 触发示例 | 创作流程 |
|------|---------|---------|
| 微信公众号图文 | "帮我写一篇关于 AI Agent 的文章" | 选题研究 → AI 写作 → 去痕优化 → SEO 优化 → 封面配图 → HTML 转换 → 草稿发布 |
| 种草笔记 | "种草笔记，主题是降噪耳机" | 选题研究 → 爆款拆解（复刻模式）→ 内容创作 → 图片规划 → 封面 + 内容配图 → 合规检查 → 交付 |
| 直播切片 | "把 live.mp4 剪成短视频切片" | ffmpeg 准备音频/封面 → 听悟转写 → 无效句过滤 → 智能切片规划 → 批量裁剪 → 报告 |

## 项目结构

```
├── .claude-plugin/ # Claude Code 插件清单与 marketplace 元数据
├── .codex-plugin/  # Codex 插件清单
├── .mcp.json       # Claude Code MCP 配置（使用 userConfig 注入）
├── agents/         # Claude Markdown Agents + Codex TOML subagents
├── skills/         # 两个宿主共用的唯一 Skill 树
├── hooks/          # Claude Code 原生质量检查 Hook
├── install/        # Codex MCP、subagent 安装与注册脚本
├── templates/       # 文章结构模板
├── writers/         # 写作风格 (YAML)
└── docs/            # 宿主安装与插件开发说明
```

## Skill 上游来源与批量更新索引

这张表记录哪些 skill 直接内置、改编或结构参考了开源 skill，方便后续让 AI 批量对齐上游。未在“上游/参考”中标为 copy 的内容，默认按 Anban 原创业务 workflow 维护。

维护本表和所有 `SKILL.md` 时，先按本仓库 `docs/claude/` 中的 Claude Skills 指南检查：frontmatter 触发描述、`SKILL.md` 体量、渐进式披露、一层 `references/`、无 skill 内辅助 README、MCP 边界、错误处理和测试/验证记录。

| 本地 skill | 上游/参考 | 本地改造 | 批量更新提示 |
|------------|-----------|----------|--------------|
| `agent-reach` | 边界封装 [Panniantong/agent-reach](https://github.com/Panniantong/agent-reach) CLI 与安装文档 | 只记录 Seednote/Xiaohongshu 外部真实数据采集的 doctor、active backend、证据归档和失败停机规则，不复制上游代码 | 更新 Agent-Reach CLI/安装流程时更新唯一 Skill；保留“不伪造数据、不绕过 Agent-Reach”的边界 |
| `humanizer` | [blader/humanizer](https://github.com/blader/humanizer) v2.8.2（MIT），固定来源为根仓库 `third_party/Humanizer` | 不做 Anban 改编；共享 `SKILL.md` 必须与固定上游逐字一致，Seednote/Article/电商约束归各业务 workflow | 运行 `make humanizer-update` 拉取上游并同步唯一副本；审阅上游 diff、跑契约测试并升级两个 manifest 版本 |
| `moments` | 内容拆解方法参考 [Caihui0127/caihui-moments-skill](https://github.com/Caihui0127/caihui-moments-skill) 的公开框架 | 产出 Anban 朋友圈素材包；不默认使用“彩卉”人设，不复制私有素材，不把参考 repo 作为运行时依赖 | 更新时只同步公开方法层；保持 `material-analysis.md`、`content.md`、`quality-review.md` 三件套和不伪造案例/数据红线 |
| `article*`、`seednote*`、`ecommerce*`、`live-slice`、`capcut-draft`、`line-art-coloring`、`portrait-pose-variants`、`anban-setup`、`config`、`writers`、`topic-research`、`seo-optimization`、`content-writing`、`short-video-cover` | Anban 原创业务 workflow；结构模式参考 [anthropics/skills](https://github.com/anthropics/skills)、[anthropics/claude-plugins-official](https://github.com/anthropics/claude-plugins-official)、[obra/superpowers](https://github.com/obra/superpowers) 和 [affaan-m/ecc](https://github.com/affaan-m/ecc) | 使用标准 `SKILL.md` + 一层 `references/`；案例是 Anban 场景原创，不复制外部措辞或业务流程 | 从 Anban 产品、MCP、server contract 更新；宿主差异放进薄适配器，不复制 Skill |

批量更新规则：

1. 先判断是“直接内置/改编上游”还是“结构参考”。直接内置必须保留许可证、署名和来源版本；结构参考不得把第三方提示词或业务流程原样复制进来。
2. `skills/` 是唯一内容源；宿主差异只能进入 manifest、MCP、Hook、Agent 或安装适配器。
3. 修改运行资产时同步 bump `.claude-plugin/plugin.json` 与 `.codex-plugin/plugin.json`，并记录 changelog。
4. 变更后至少跑 `go test ./server/agent ./server/mcp -count=1`、Claude plugin strict validation 与 Codex plugin validation。

## 开发与安全

- 版本变更见 [CHANGELOG.md](CHANGELOG.md)
- 贡献指南见 [CONTRIBUTING.md](CONTRIBUTING.md)
- 漏洞报告和密钥处理规则见 [SECURITY.md](SECURITY.md)

不要把 API Key、Bearer token、草稿私密链接或 MCP Authorization header 写进 issue、日志、截图、测试 fixture 或生成产物。

## 其他版本

- [Web 管理端](https://creator.anbanai.com) — 在线管理后台，支持任务管理、积分充值等

## 加入社群

扫码加入 **Anban 智能创作助手讨论群**，获取使用技巧、功能更新和问题解答：

<img src="community-qr.jpg" alt="Anban 智能创作助手讨论群" width="200">

## License

MIT
