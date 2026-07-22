---
name: anban-setup
description: Use when user mentions "初始化", "anban-setup", "第一次使用", "API Key", "密钥", or when MCP tools fail with auth/connection errors suggesting missing ANBAN_API_KEY.
---

# Anban Creator 初始化

## 案例库

遇到场景分支、产物格式或质量边界不确定时，先读 [references/examples.md](references/examples.md)。

## 图片比例固定规则

本 Skill 只要涉及生成、选择、裁切、校验或引用图片，必须按以下优先级决定画面比例：

1. 用户/任务明确指定的 `image_ratio`、`size` 或平台规格优先。
2. 项目/频道默认比例次之。
3. 业务默认比例只作兜底：微信文章封面/正文图默认 `16:9`；Seednote/XLS/移动信息流默认 `3:4`；电商、广告投放、视频封面按具体平台素材位要求执行。
4. 不得从模型路由、供应商默认 `size` 或模型能力反推业务比例；模型只决定能力和成本，比例属于创作场景约束。


## 预检

尝试调用 `list_projects` MCP 工具：

- **成功** → 输出连接状态和可用项目，结束
- **失败**（认证错误、连接失败）→ 进入下方密钥设置流程

## 用户级配置：API Key

向用户说明：

> Anban Creator MCP 服务器需要 API Key 进行认证。请前往 https://creator.anbanai.com 注册账号并获取 API Key。

先识别当前宿主，再使用对应的原生配置方式。不要猜测，也不要同时修改两套配置。

### Claude Code

`.claude-plugin/plugin.json` 已声明官方 `userConfig`：

- `api_key`：必填、敏感字段，映射到 MCP Authorization header。
- `api_url`：可选，默认 `https://api.creator.anbanai.com`。

认证失败时，提示用户打开插件配置填写或更新 `api_key`。不要把 API Key 写入项目文件，也不要把密钥打印到日志或最终回答。

### Codex

Codex 使用环境变量与安装器写入的 `[mcp_servers.creator]` 配置：

- `ANBAN_API_KEY`：必填 Bearer token。
- `ANBAN_API_URL`：可选，默认 `https://api.creator.anbanai.com`。

优先引导用户在 shell 启动文件中设置 `ANBAN_API_KEY`，再重启 Codex。不要自动覆盖 `~/.codex/config.toml`；用户明确要求手动注册 MCP 时，使用 `bearer_token_env_var = "ANBAN_API_KEY"` 合并现有配置。

## 项目级配置

API Key 设置完成后，可根据需要提示用户补充项目级配置。

### 服务地址（可选）

Claude Code 在插件配置中修改 `api_url`；Codex 设置 `ANBAN_API_URL` 环境变量。

### 默认项目（可选）

如果 `list_projects` 返回多个项目，询问用户是否要设置 `ANBAN_DEFAULT_PROJECT`。Claude Code 可合并写入项目本地 `.claude/settings.local.json`：

```json
{
  "env": {
    "ANBAN_DEFAULT_PROJECT": "<项目 ID>"
  }
}
```

Codex 在 shell 启动文件或项目环境中设置同名变量。任何已有配置都必须先读取并合并，不得覆盖。

## 完成

**写作去 AI Skill 可用性校验**：在当前插件根目录检查 `skills/humanizer/SKILL.md`。该 Skill 随插件安装，无需联网或 `git clone`；缺失时提示用户重新安装插件。

**Agent-Reach 小红书数据预检**：Seednote 外部小红书研究完全走 Agent-Reach。用 Bash 执行 `agent-reach doctor --json` 检查可用性；如果命令不存在或 `xiaohongshu` 无可用 backend，提示用户按 Agent-Reach 官方流程安装/配置：

```text
帮我安装 Agent Reach：https://raw.githubusercontent.com/Panniantong/agent-reach/main/docs/install.md
```

不要把 OpenCLI、xiaohongshu-mcp 或 xhs-cli 写成 Anban 自己的安装方案；它们只是 Agent-Reach doctor 选择和提示的 backend。

告知用户：

> 配置完成。请完全退出并重新启动当前宿主，让 MCP 连接生效；重启后再次运行本 Skill 验证连接。

## 重启后验证

用户重启 Claude Code 或 Codex 后，再次运行本 Skill。预期结果：
- `list_projects` 调用成功，返回可用项目列表
- 输出每个项目的 platform、name 和 ID

## 常见问题

**Q: 重启后 `list_projects` 仍然失败？**
A: Claude Code 检查插件 `api_key`/`api_url`；Codex 检查 `ANBAN_API_KEY`/`ANBAN_API_URL`。同时确认网络可访问服务地址。

**Q: 想切换到另一个 API 地址？**
A: Claude Code 修改插件 `api_url`；Codex 修改 `ANBAN_API_URL`。然后重启当前宿主。

**Q: 已有 API Key 但忘了存在哪里？**
A: Claude Code 打开插件配置重新设置；Codex 检查 shell 启动文件中的 `ANBAN_API_KEY`。不要在对话、日志或项目文件中输出密钥值。
