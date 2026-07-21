# anban-setup Examples

### Case 1: 首次安装后认证预检

- Input: 用户说“第一次使用，帮我初始化”。
- Recommended path: 用 list_projects 验证 MCP 认证和项目可见性。
- Artifacts: setup-check.md。
- Quality gate: 只报告 ANBAN_API_KEY 是否存在，绝不打印密钥值。

### Case 2: MCP 认证失败诊断

- Input: 任一创作 skill 调 MCP 返回 401/403。
- Recommended path: 切换到 anban-setup，检查密钥存在性、API URL 和默认项目配置。
- Artifacts: diagnostic-report.md。
- Quality gate: 不得绕过 MCP 写自定义 HTTP 客户端。

### Case 3: 自定义 API 地址不可达

- Input: 用户配置了自定义 API URL，但 MCP 连接超时。
- Recommended path: 检查 URL 格式、TLS 和网络可达性，再重新调用 list_projects。
- Artifacts: connectivity-report.md。
- Quality gate: 不修改用户配置，不回显认证信息。
