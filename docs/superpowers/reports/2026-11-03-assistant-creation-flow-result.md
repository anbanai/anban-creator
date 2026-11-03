# 助手创作流程优化验收记录

日期：2026-11-03。工作分支：`codex/assistant-creation-flow`。本次修改保存在独立 worktree，未推送、合并或部署。

## 实施结果

- 创作计划仅保留一张当前卡片和一处固定操作区（修改需求 / 开始创作）；正常待确认状态不再重复显示顶部警告。补充信息错误会阻止继续创建，修改后必须确认新版本。
- 总结卡改为紧凑的分组行：要求、限制和本次临时调整保持可见，人设背景与默认设置折叠。已回答问题显示简短决策记录，原始问答可展开查看。
- 新用户消息分别保留原文与执行文本，界面不再拼入内部创作类型。历史文本没有可靠的原文标记时保留原样，避免误删用户内容；Server 仍只持久化交互元数据，不新增用户正文持久化。
- 任务右侧改为人设摘要和少量本次创作参数。实时人设明确标记为“当前人设”，任务参数使用冻结配置；空素材、未设置风格、手动创建等低价值占位不再堆叠。
- 按用户最后的简化要求，仅在 `completed` 时显示一行“总消耗 X 积分”，取后端 `billing_total_credits ?? billing_price_credits`，保留零值。创作中不显示积分，不增加估算或额外账单入口。
- Server 保留严格计划校验，返回安全的字段/类型诊断。助手启动恢复在开放监听前进行有界重试，初始化成功后停止清理；取消和重试耗尽均保持安全状态。
- Studio 同时识别旧的环境不可用提示和新的初始化未完成提示：即使已有人设或选中作品，也不自动准备会话或启用发送；切换模式不重复展示相同错误。启动命令在超时后返回普通进程错误时，Server 仍按实际上下文记录超时，不输出原始命令错误。
- 修复创作与复盘模式的 DSH 外层注册组同名覆盖问题，并保持规范源、生成文件和分发适配一致。Planner 优先本次明确要求，信息足够即暂存，仅一句话交接；按字段错误最多修正一次，最终创建仍由 Studio 确认。
- Harness 版本 `4.2.128`，两份原生清单同步更新；子模块提交 `02ee07d0`，父仓库集成提交 `d8eb993d`。
- 最后两处可靠性修复提交为 `69c04437`，仅涉及启动恢复、助手页面及各自的行为回归测试。

## 验证

| 范围 | 结果 |
| --- | --- |
| Studio | 178 个测试文件、1,666 项测试通过；`bun run build` 通过 |
| Go | 全量 38 个有测试的包通过；Server 构建通过；MCP 字段诊断/边界与生成合同通过 |
| Harness | 13 个测试文件、485 项通过，1 项既有跳过；类型检查、构建、source integrity、profile smoke 通过 |
| 实际 DSH 启动 | project-creator、创作单模式、复盘单模式、共享创作、共享复盘共 5 个场景通过；真实运行时配合确定性模型/MCP 测试端点 |
| 浏览器 | 实际 Studio + 本地测试 API：选择 AI 内容创作先锋、提交需求、回答问题、计划审阅、失败提示、修改需求、新版本确认、一次任务创建与跳转均通过 |
| 响应式界面 | 桌面及 390×844 窄屏检查通过；固定按钮保持成组；运行中无积分、完成后仅最终总额 |
| 初始化失败界面 | 新提示仅出现一次，输入和发送禁用；切换模式后无重复错误、无 `/assistant/` POST 请求 |
| 指令走查 | 独立模拟检查了当前意图优先、直接暂存、合法字段、单次确认、简短交接及有界修正；按钮文案与“修改需求”一致 |

浏览器记录的任务创建计数为 1。重复点击、旧版本失效和重连行为由组件/服务回归测试覆盖。本次新代码未用线上模型复测，不把确定性测试或指令模拟称为线上端到端验收。原先线上不可用已通过同版本重启恢复；先前实际创建的[作品已完成](https://creator.anbanai.com/tasks/bb3e9d77-2890-44a5-bb75-11caf1576618)。

主要验证命令（在对应目录执行）：

```sh
# studio/
bun run test
bun run build

# server/（最终修复后的全量验证；代理设置仅对命令生效）
GOPROXY=https://proxy.golang.org,direct go test -p 1 ./...
GOPROXY=https://proxy.golang.org,direct go build -p 1 -o /tmp/anban-creator-server-final-fix .

# 仓库根目录
make agent-pack-generate
make agent-pack-check
npm_config_verify_deps_before_run=false make dsh-smoke

# harness/
node node_modules/vitest/vitest.mjs run --maxWorkers=1 --no-file-parallelism
node dsh/scripts/package-integrity.mjs source

# runtime-dsh/
npm run test:startup
```

最终回归覆盖两条环境提示 × 三种上下文（创作已有角色、复盘已选作品、创作无人设），以及模式切换后不重复显示错误。Go 使用标准 `testing/synctest` 推进真实 attempt context 的期限，验证清理/恢复两阶段返回普通进程错误时仍诊断为超时、三次重试及虚拟总时长 100 秒，并继续覆盖父取消优先级；这些回归均先复现失败再修复通过。

“修改需求”文字对齐后重新生成并检查了生成合同及 Server 构建；最终可靠性修复后再次完成全量 Studio/Go 测试及构建，未重跑未改动的 Harness/DSH 套件。Studio 依赖已使用冻结锁文件恢复；本地测试 API/Vite 已停止，临时浏览器尺寸已重置。

## 验证限制与范围取舍

- `package-integrity.mjs pack` 的离线安装阶段缺少 registry 元数据，报 `ERR_PNPM_NO_OFFLINE_META`；source integrity、profile smoke 和实际启动均通过，但不能据此声称离线安装已验收。
- 本机 Claude CLI 不接受清单既有的 `displayName`（marketplace 另有既有根 `description`）字段；这次没有改动这些字段。原生安装兼容性仍需在发布环境验证。
- 保留原有清单字段与依赖范围，避免把本次 DSH/创作流程修复扩大成清单或依赖改造。此取舍的代价是打包安装/原生宿主兼容性仍未完整验证，发布前必须复验。
- 既有 Vite 配置/体积提示和 macOS 本地库链接目标版本警告仍存在；未进行无关工具链改动。

## 审查

各任务的需求与代码审查均通过。整体审查发现初始化提示未被 Studio 正确识别（P2）及进程错误的超时诊断不准确（P3），已在 `69c04437` 修复。独立的最终定向复审确认两项均已解决，切换模式的重复提示也已消除；未发现修复引入的新问题，结论为 PASS。复审核对了回归用例和完整验证日志，没有重复运行已通过的套件。

## 界面证据

本地测试数据截图目录：`/Users/bbfisher/.codex/visualizations/2026/11/02/01a19d0e-63a1-75d3-9372-4c45a91e88db/assistant-flow-implementation/`。

- `compact-plan-desktop.png` / `compact-plan-mobile.png`：紧凑总结和单一确认区。
- `missing-input-blocked.png`：缺信息时准确提示并禁用创建。
- `sidebar-running-desktop.png`：人设与创作要点，无积分展示。
- `sidebar-completed-desktop.png` / `sidebar-completed-mobile.png`：完成后的一行总消耗（示例 1,100 积分）。
- `sidebar-completed-detail.png`：完成状态右侧栏局部预览，1,100 积分为测试数据。
- `initialization-unavailable.png`：初始化未完成时的一条提示和禁用状态。
