# Miniapp ↔ Studio 功能对齐 & 前卫化重设计

**Date:** 2026-06-27
**Goal:** 确保 `miniapp/`（uni-app，目标 mp-weixin）包含 `studio/`（React 19）的**完整功能且全部可用**，并采用前卫的 UX 设计与技术方案。

## 现状判定（探索结论）

miniapp 已具备 ~27 页面、覆盖 12+ 功能域，结构成熟，**不是从零构建**，而是“功能滞后 + 关键链路断裂”。两个根本问题：

1. **🔴 致命：miniapp 未跟随 2026-06-25 的 `channel→project` 改名。**
   - 服务端 `server/router/router.go` 现在只注册 `/projects/*`；task/plan handler 强制 `project_id`（`handler/task.go:121 "project_id is required"`）。
   - miniapp 仍打 `/channels/*`、`/channels/:id/topics`，task/plan 仍发 `channel_id`。
   - 后果：**账号 tab、选题池、创建任务、创建计划、按项目筛选** 全部 404。这是“不能用”的主因。
   - `miniapp/scripts/parity-test.mjs` 是陈旧契约（只断言文件存在 + 关键词），通过 ≠ 能用。

2. **🟡 功能广度差距：核心创建表单被大幅简化。**
   studio 的任务/计划创建表单含 ~20 字段（image_model_key、style、writing_style、theme、作者/人设块、goal_mode/goal、模板选择、电商字段…），miniapp 只保留 ~7 字段。

## 平台约束（决定“前卫”的边界）

- 目标平台：**mp-weixin 为主，H5 为开发/兜底**。mp-weixin 无 DOM、不支持 GSAP/任意 JS 动画库、SSE 不原生支持。
- “最前卫”在 mp-weixin 上的合理形态：WebSocket 实时、骨架屏、CSS 过渡/微交互、uCharts 图表、统一现代设计系统、深色模式、流畅下拉刷新/无限滚动。**不追求** studio 的桌面级特性（⌘K 命令面板、`g d` 键盘快捷键——移动端不适用）。

## 决策（默认值，可被纠正）

| 维度 | 默认决策 | 理由 |
|---|---|---|
| 平台 | mp-weixin 主，H5 次 | “小程序”语义；uni-app 多端，H5 便于本机调试 |
| 改名范围 | 全量：API 路径 + 参数 + 类型 + UI 标签（账号→项目）+ 文件/目录 | 与 studio 对齐，避免长期双轨 |
| 实时 | 任务详情升级 WebSocket（`uni.connectSocket`→`/ws`），轮询兜底 | 服务端已有 `/ws`；比轮询更“前卫”且更省电 |
| 设计深度 | 先达到功能对齐 → 再统一现代化设计系统（不另起炉灶重写 IA） | 先保证“能用”，再“好看”；规避“悄悄删功能”红线 |
| 表单对齐 | 把 studio 创建表单的**业务字段**补齐到 miniapp（按平台条件显隐） | 这是功能完整性的核心 |

## 分阶段计划

### Phase 0 — 致命修复：channel→project 全量迁移（使 app 可用）
**这是最高优先级、无歧义的 bug 修复。**
- `api/channels.ts` → `projects.ts`：所有 `/channels` → `/projects`；`fetchProfile(platform,url,app_id,secret)` 签名对齐。
- `api/topic-pool.ts`：`/channels/:id/topics` → `/projects/:project_id/topics`。
- `api/tasks.ts`：create/list 参数 `channel_id` → `project_id`；`list({ project_id })`。
- `api/plans.ts`：`channel_id`/`channelId` → `project_id`。
- `types/channel.ts` → `project.ts`（类型 `Channel`→`Project`），更新 `types/index.ts`。
- 组件：`ChannelSelector`→`ProjectSelector`、`ChannelCard`→`ProjectCard`。
- 页面目录 `pages/channels/` → `pages/projects/`；`pages.json` 路径 + tabBar 文案 账号→项目。
- 全局替换 `channel_id`/`channelId`/`Channel` 引用；`api/index.ts` 导出更名。
- 更新 `parity-test.mjs` 断言为 `project.ts` + `/projects`。
- **验收**：`bun run type-check`（vue-tsc）通过；H5 `dev:h5` 起服务后账号/任务/计划 CRUD 真实跑通。

### Phase 1 — 功能对齐（补齐 studio 字段/能力）
- **新建 `api/image-models.ts`**（`GET /image-models`）+ `ProjectImageModelSelector` 组件。
- **任务创建**补字段：image_model_key、style、writing_style、theme、author/author_style_intro/author_avatar_url（人设块）、goal_mode/goal、template_id（模板选择）、seednote has_content_image/has_tail_image、电商（product_photos 多图、selected_modules、target_platform、selling_points、language）。按 channel 平台条件显隐。
- **计划创建**补字段：image_model_key、style、writing_style、theme、人设块、goal、模板、seednote 图开关。
- **项目详情**补：avatar 上传、人设块、`analyzeImage`（参考图→风格）、模板导入。
- **任务列表**补：搜索、项目筛选、多选+批量 ZIP 下载、列表内“标记发布”、6 态 tab。
- **任务详情**补：删除动作、日志复制、日志跟随开关。
- **积分页**补：完整 pricing/会员对比/模型能力（新建 `pages/credits/index.vue` 或并入 me）。
- **用量页**：cache 拆 read/creation。
- **模板页**：补 category/scope 筛选。
- **首页**：uCharts 图表、邀请链接复制。
- 新建 `api/feedback.ts`（统一 feedback 调用）。

### Phase 2 — 实时升级
- 任务详情：`uni.connectSocket` 连 `/ws`，订阅 task progress 事件；断线重连；轮询兜底。替换纯轮询。

### Phase 3 — 前卫化设计系统
- 重构 `components/common/Ab*`：现代视觉语言（玻璃质感/精致间距/微交互/统一动效曲线）。
- 全局骨架屏、空态、错误态规范化。
- 深色模式（CSS 变量 + `prefers-color-scheme`/系统主题）。
- 页面过渡、卡片入场动画、下拉刷新动效。
- 图表统一用 uCharts。

## 风险与红线
- **CLAUDE.md 红线**：不得以“净化/简化”为名删除已发布功能；改名时逐项核对表单字段/按钮/交互。重构前先列受影响项 → diff 后逐项核对。
- 改名涉及面广，须 `vue-tsc --noEmit` + parity-test + H5 手测三重验收。
- 移动端不强行移植桌面特性（命令面板/键盘快捷键），属合理平台适配而非“删功能”，会在 commit 中说明。

## 验收标准
1. miniapp 所有页面调用的 API 路径/参数与服务端 `router.go` 完全一致（无 `/channels`、无 `channel_id`）。
2. studio 每个用户可见功能（表单字段/按钮/交互）在 miniapp 都有可用对应（平台不适配项除外，且已说明）。
3. `vue-tsc --noEmit` 通过；`parity-test.mjs` 通过（断言已更新为 project）。
4. H5 端真实跑通：登录→建项目→建任务（含补齐字段）→任务详情实时进度→建计划→时间线→积分→用量。
5. 设计系统统一、骨架屏/空态/错误态规范、深色模式可用。
