---
name: init
description: Use when user mentions "初始化", "init", "配置", "设置", "setup", "第一次使用", "API Key", "密钥", or when MCP tools fail with auth/connection errors suggesting missing ANBANWRITER_API_KEY.
---

# init — anbanwriter 初始化 (Codex)

## 预检

尝试调用 `list_channels` MCP 工具：

- **成功** → 输出连接状态和可用频道，结束
- **失败**（认证错误、连接失败）→ 进入下方密钥设置流程

## 用户级配置：API Key

向用户说明：

> anbanwriter MCP 服务器需要 API Key 进行认证。请前往 https://creator.anbanai.com 注册账号并获取 API Key。

通过 AskUserQuestion 向用户索取密钥值。

收到后，**不要自动写入 `~/.codex/config.toml`**——直接编辑用户已有的 Codex 配置可能破坏既有 MCP 注册、`[features]`、`[agents]` 等段。改为：

1. **优先引导用户设置环境变量**（推荐做法，对 Codex 与本插件都生效）：
   - macOS / Linux：在 `~/.zshrc` 或 `~/.bashrc` 末尾追加
     ```sh
     export ANBANWRITER_API_KEY="<用户提供的密钥>"
     ```
   - 然后执行 `source ~/.zshrc`（或重启终端）
2. **次选方案**：如果用户希望走 `~/.codex/config.toml`，提示其手动追加以下段（先 Read 现有文件确认无重复，再用 Edit 合并，绝不覆盖）：
   ```toml
   [mcp_servers.anbanwriter]
   url = "${ANBANWRITER_API_URL:-https://api.creator.anbanai.com}/mcp"
   bearer_token_env_var = "ANBANWRITER_API_KEY"
   ```
   并在 shell 中保留 `export ANBANWRITER_API_KEY="..."`（Codex 的 `bearer_token_env_var` 读取的是环境变量，不是 config.toml 内联值）。

## 项目级配置（可选）

API Key 设置完成后，提示用户进行项目级配置。**Codex 没有项目级 settings.local.json 等价物**——以下变量通常放进项目根 `.env` 或 shell 启动文件：

### 服务地址（可选）

如果用户使用的不是默认地址 `https://api.creator.anbanai.com`（如本地服务器），追加：

```sh
export ANBANWRITER_API_URL="<用户的服务地址>"
```

### 默认频道（可选）

如果 `list_channels` 返回多个频道，询问用户是否要设置默认频道：

```sh
export ANBANWRITER_DEFAULT_CHANNEL="<频道 ID>"
```

## 完成

告知用户：

> 配置完成。**请退出并重新启动 Codex**，让 MCP 连接生效。重启后再次运行 `$init` 验证连接。

## 重启后验证

用户重启 Codex 后，`$init` 的预检步骤应自动执行。预期结果：
- `list_channels` 调用成功，返回可用频道列表
- 输出每个频道的 platform、name 和 ID

## 常见问题

**Q: 重启后 `list_channels` 仍然失败？**
A: 检查 `ANBANWRITER_API_KEY` 是否正确导出（在新终端执行 `echo $ANBANWRITER_API_KEY` 验证）。检查 `~/.codex/config.toml` 是否包含 `[mcp_servers.anbanwriter]` 段，且 `bearer_token_env_var` 指向正确的环境变量名。检查网络是否能访问 `https://api.creator.anbanai.com`（如使用自建服务器，检查 `ANBANWRITER_API_URL` 是否正确）。

**Q: 想切换到另一个 API 地址？**
A: 修改 shell 中的 `export ANBANWRITER_API_URL=...`，或修改 `~/.codex/config.toml` 内 `[mcp_servers.anbanwriter] url` 字段，然后重启 Codex。

**Q: 已有 API Key 但忘了存在哪里？**
A: 在终端执行 `echo $ANBANWRITER_API_KEY`；若为空，检查 `~/.zshrc` / `~/.bashrc`，或 `~/.codex/config.toml` 的 `[mcp_servers.anbanwriter]` 段。

**Q: 想同时使用多个 anbanwriter 服务（如生产 + 测试）？**
A: 在 `~/.codex/config.toml` 注册第二个 MCP 服务器（例如 `[mcp_servers.anbanwriter-staging]`），用不同环境变量承载各自的 Bearer Token，再让 subagent 在 `[mcp_servers]` 段中按需引用。
