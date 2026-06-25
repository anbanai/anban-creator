# AI 接入指南页面设计

## Context

用户在 Studio 后台获取了 API Key 后，不知道如何配置 Claude Code 来使用它。目前接入说明仅存在于 `plugin/README.md` 中，用户需要离开 Studio 才能查看。需要一个 Studio 内的引导页面，以文档向导式布局展示 AI 助手（Claude、未来 OpenClaw 等）的接入配置方法，并与其他页面（如设置页的 API Key 管理）建立交叉导航。

## 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 路由 | `/connect` | 简洁直观 |
| 侧边栏位置 | 底部分隔线区域（与"设置"同级） | 辅助/参考类页面，非核心业务 |
| 布局 | 左右分栏：左侧 Provider 列表 + 右侧文档内容 | 文档向导式，扩展性强 |
| 数据源 | 纯前端静态内容 + API Key 列表查询 | 接入说明是固定文档；API Key 动态展示提升体验 |
| 移动端 | Provider 列表变为顶部水平滚动 | 响应式适配 |

## 页面结构

### 路由注册

在 `studio/src/App.tsx` 添加：
- 路径: `/connect`
- 组件: `ConnectGuidePage` (lazy-loaded)
- 保护: 在 protected route 内（登录用户专属）

### 侧边栏导航

在 `studio/src/components/layout/Sidebar.tsx` 底部分隔线区域，Settings 链接旁边添加：
- 路径: `/connect`
- 标签: `接入指南`
- 图标: `Cable` (lucide-react)

### 组件树

```
ConnectGuidePage
├── PageHeader (title="AI 接入指南", description="配置 AI 助手，开始自动化创作")
├── 响应式容器 (flex, md:flex-row flex-col)
│   ├── ProviderNav (左侧 w-56 / 移动端顶部水平滚动)
│   │   ├── ProviderNavItem "Claude" (默认选中)
│   │   └── ProviderNavItem "OpenClaw" (badge: "即将支持")
│   └── ProviderContent (flex-1)
│       ├── ClaudeGuide
│       │   ├── 概述卡片
│       │   ├── 步骤 1: 安装 Claude Code
│       │   ├── 步骤 2: 安装插件
│       │   ├── 步骤 3: 配置 MCP 连接 (含 API Key 展示 + 跳转设置页)
│       │   ├── 步骤 4: 图片生成配置 (可选)
│       │   └── 步骤 5: 微信公众号配置 (可选)
│       └── OpenClawGuide (占位组件: "即将支持，敬请期待")
```

## Claude Guide 内容

### 步骤 1: 安装 Claude Code

展示安装命令和官方链接：
```bash
npm install -g @anthropic-ai/claude-code
```

### 步骤 2: 安装插件

展示插件安装命令：
```bash
/install-plugin anbanai/anbanwriter
```
提示：安装后通过 `/plugin` 确认版本。

### 步骤 3: 配置 MCP 连接（核心步骤）

**动态内容**：查询用户的 API Key 列表，如果已有 Key 则直接展示（脱敏），否则显示"前往设置页创建 API Key"按钮跳转 `/settings`。

配置命令（代码块 + 复制按钮）：
```bash
# 添加到 ~/.zshrc
export ANBAN_API_KEY="你的API Key"
export ANBAN_API_URL="https://你的域名"  # 本地开发默认 http://localhost:18060
```

### 步骤 4: 图片生成配置（可选）

展示 `~/.anbanwriter/settings.json` 配置模板，包含 provider 对比表格（OpenAI / Gemini / 火山引擎）。

### 步骤 5: 微信公众号配置（可选）

说明通过自然语言或 `/config` skill 配置 AppID 和 AppSecret 的方法。

## 跨页面关联

### ConnectGuidePage → SettingsPage
- 步骤 3 "配置 MCP 连接" 中：
  - 无 API Key 时：显示"前往设置页创建 API Key"按钮 → `<Link to="/settings">`
  - 有 API Key 时：显示脱敏 Key + "管理 API Key" 链接 → `/settings`

### SettingsPage → ConnectGuidePage
- API Key 区域底部添加提示文字："不知道如何使用 API Key？查看接入指南 →" → `<Link to="/connect">`

## 扩展性设计

### 添加新 Provider

添加新 provider 只需：
1. 在 `providers` 数组中添加一个对象 `{ id, name, icon, status }`
2. 创建对应的 `XxxGuide` 组件
3. 在 `ProviderContent` 中添加条件渲染

Provider 数据结构：
```ts
interface Provider {
  id: string
  name: string
  icon: LucideIcon
  status: 'available' | 'coming-soon'
}
```

### 移动端适配

- `md:` 断点以下：Provider 列表变为顶部水平滚动区域
- `md:` 断点以上：左右分栏布局

## 关键文件

| 文件 | 操作 |
|------|------|
| `studio/src/pages/ConnectGuidePage.tsx` | 新建 — 主页面组件 |
| `studio/src/components/connect/ProviderNav.tsx` | 新建 — Provider 导航列表 |
| `studio/src/components/connect/ClaudeGuide.tsx` | 新建 — Claude 接入文档 |
| `studio/src/components/connect/OpenClawGuide.tsx` | 新建 — OpenClaw 占位 |
| `studio/src/components/connect/CodeBlock.tsx` | 新建 — 代码块 + 复制按钮 |
| `studio/src/App.tsx` | 修改 — 添加路由 |
| `studio/src/components/layout/Sidebar.tsx` | 修改 — 添加导航项 |
| `studio/src/pages/SettingsPage.tsx` | 修改 — 添加接入指南链接 |

## 验证方案

1. 访问 `/connect` 确认页面正确渲染
2. 验证侧边栏"接入指南"导航项出现且高亮正确
3. 切换 Provider（Claude / OpenClaw）确认内容切换
4. 点击 API Key 相关链接跳转到 `/settings` 正确
5. 在 SettingsPage 确认"查看接入指南"链接跳转到 `/connect`
6. 移动端视口下确认 Provider 列表变为顶部水平滚动
7. 代码块复制按钮功能正常
