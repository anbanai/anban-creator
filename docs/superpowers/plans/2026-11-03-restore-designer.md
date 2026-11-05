# Restore Designer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restore the independent Studio Designer image workbench and its Server functionality against current main.

**Architecture:** Reuse the last pre-removal implementation at `31e94c16db3eb0f1e15838a5e7968765b185077d`, selectively adapting restored files to the current Server image package, capability catalog, uploads, billing and Studio components. Server owns durable execution, file ownership, charging and failure settlement. Studio exposes the old workbench through current navigation and lazy routes.

**Tech Stack:** Go/Fiber/GORM, existing fixed-SKU wallet and image providers; React/TypeScript/Vite/Vitest/Bun.

**Spec:** `docs/superpowers/specs/2026-11-03-restore-designer-design.md` (approved).

## Global Constraints

- Work only in `/Users/bbfisher/.codex/worktrees/restore-designer/anban-creator`, branch `codex/restore-designer`; preserve unrelated work.
- Scope: Studio and Server only. Do not restore Miniapp, Designer Agent Pack, Skills or Tauri.
- `/designer` page, navigation and all Designer APIs require administrator access. Admin status does not bypass capability tier, billing or ownership.
- Use current `generation_features`, existing `image.generate` SKUs and `n = 1`. Do not reintroduce `designer_features` configuration or change catalog prices.
- Use the current upload finalization pipeline and durable storage identities. Persist generation output before reporting completion.
- Same operation ID and fingerprint must not charge or execute twice; conflicting replay must fail.
- Failure state and reversal intent must be durable and idempotent; started but expired executions must terminate without automatic provider retries.
- Never run Designer table deletion on startup. Migration must preserve existing rows across repeated runs.
- Tests precede behavior changes; retain useful historical behavior tests, adapt obsolete fixtures and add focused regressions.
- No unrelated refactors or dependency additions. Do not push, merge or deploy as part of this task.
- Bun is available through `npm exec --yes --package=bun -- bun`; dependencies and recursive harness submodules are installed.

## Task 1: Restore the Server Designer workflow

**Files:**
- Restore: `server/model/designer.go`, `server/repository/designer_reference.go`, `server/repository/image_generation.go`, `server/service/designer.go`, `server/handler/designer.go` and their relevant historical tests.
- Modify: `server/model/model.go`, `server/repository/repository.go`, repository construction/transaction wiring, `server/service/direct_upload.go`, `server/main.go`, `server/router/router.go`, affected tests.
- Remove: `server/service/designer_removal_migrate.go`, `server/service/designer_removal_migrate_test.go` and obsolete startup removal assertions.
- Create: `server/migrations/20261103_restore_designer.sql`, behavior tests for migration, API authorization, execution/recovery and current upload finalization.
- If needed, split newly restored execution/reference responsibilities into `server/service/designer_execution.go` and `server/service/designer_reference.go`; no broad unrelated changes.

**Interfaces:**
- Consumes current `config.ImageGenerationRouteConfig.GenerationFeatures`, `Config.ImageAPIForCapability`, `ImageCapabilityResolver`, `BillingCatalogService`, `BillingWalletService`, `ProviderCostService`, upload-session finalization and storage APIs.
- Produces historical public `/api/v1/designer` contracts, adapted internally: POST `/quote`, `/generate`, `/register-reference`, `/upload-reference`, `/upload-reference-from-url`; GET `/history`, `/generations/:id`.
- Quote request: `{operation_id, project_id, prompt, capability_key, quality, size, n, output_format, output_compression?, background?, reference_file_ids?, mask_file_id?, watermark?}`.
- Quote response includes `quote_id`, `operation_id`, `request_fingerprint`, `price_credits`, `expires_at`; generate adds quote ID/fingerprint and returns `{generation_id,status,price_credits}`.
- Reference registration uses `{upload_id,key}` and returns `{file_id,filename,size}`. URL registration uses `{url}` with the same result. Upload purpose is `designer_reference`.
- History accepts optional `project_id,page,page_size`, returns `{items,total,page,page_size}`. Generation returns historical public fields (`id`, `project_id`, prompt, capability key/name, selected parameters, status/error, billing fields, timestamps and results). Each result contains durable accessible `image_url`, dimensions, index and `file_id`; do not disclose local server paths or provider credentials.
- Generated records use states `generating`, `completed`, `failed`. Internal queued/claimed distinction may use start timestamps and an execution deadline; public API remains these three states.
- Keep `NewDesignerService(db, cfg, store, logger)` and billing setters where compatible with existing patterns; expose a service recovery function wired to server lifecycle. Handler delegates generation admission/dispatch to service, not workflow composition.

- [x] **Step 1: Write regression tests and verify failure before restoration.**

Start with an actual database migration regression that can compile before the models return:

```go
func TestAutoMigrateCreatesDesignerTables(t *testing.T) {
    db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
    if err != nil { t.Fatal(err) }
    if err := AutoMigrate(db); err != nil { t.Fatal(err) }
    for _, table := range []string{"designer_references", "image_generations", "image_generation_results"} {
        if !db.Migrator().HasTable(table) { t.Errorf("missing %s", table) }
    }
}
```

Run `cd server && go test ./model -run TestAutoMigrateCreatesDesignerTables -count=1` and record missing-table failure.
Restore historical behavioral tests first, adapt import paths from `anban-creator/app/` to `anban-creator/server/app/` and fixtures from `DesignerFeatures` to `GenerationFeatures`.
Add table-driven tests for non-admin API rejection, foreign project/reference/mask rejection, disabled capability, invalid parameters and `n != 1`.
Add generation replay assertions against real database charge/record counts and a fake image provider call count.
Use `httptest` for provider output and a memory/local test storage provider; do not make paid calls.

- [x] **Step 2: Restore persistence and remove destructive startup behavior.**

Use explicit historical files, never revert the entire deletion commit:

```sh
git show 31e94c16:server/model/designer.go > server/model/designer.go
git show 31e94c16:server/repository/designer_reference.go > server/repository/designer_reference.go
git show 31e94c16:server/repository/image_generation.go > server/repository/image_generation.go
```

Wire the restored model structs into `AutoMigrate`, repository interfaces and transaction implementations. Add atomic claim/conditional terminal-state operations as needed by execution.
Remove `MigrateDesignerRemoval` call and implementation. Replace removal-specific tests with preservation tests that insert a generation, run migration again and verify the same record/results remain.
Add idempotent MySQL `CREATE TABLE IF NOT EXISTS` statements matching restored model columns and indexes; exercise structural equivalence using repository migration test conventions.

- [x] **Step 3: Restore service/handler with current contracts and durable lifecycle.**

Read the complete historical files before selectively restoring:

```sh
git show 31e94c16:server/service/designer.go > server/service/designer.go
git show 31e94c16:server/handler/designer.go > server/handler/designer.go
```

Replace old app imports and capability type/field names with current equivalents. Resolve only configured current capability keys; remove restored obsolete provider fallback paths.
Use the existing fixed-SKU quote, standalone charge transaction and settlement outbox. Validate ownership at quote/admission; replays must not bypass user/fingerprint checks.
Move admission plus dispatch into the service. Before upstream invocation, atomically claim an unstarted `generating` record; repeated dispatch must become a no-op. Apply the configured route timeout, persist its deadline and condition terminal writes so recovery and late output cannot both win.
Recover queued work safely and expire abandoned started work through Server lifecycle polling without repeating an uncertain upstream request. Persist failure and one reversal intent in a transaction. Use current provider cost evidence APIs.
Only a durably stored result can mark generation completed; transactionally persist result rows and completion. Failed storage must result in failure settlement, not a completed record.
Add `designer_reference` to current upload-purpose policy. Finalize uploads through current session/Asset functions before registering a user-owned reference; verify actual ownership using DB records/storage identities for URL reuse.
Enforce login/admin access for every Designer endpoint using the current user repository; preserve per-user result/history filtering even for admins.
Wire service, recovery, handler and routes in current `main.go`/router without changing unrelated application initialization.

- [x] **Step 4: Verify the Server deliverable.**

Run focused restored tests and the new tests, then:

```sh
cd server
go test ./model ./repository ./handler ./router ./migrations
go test ./service -run 'Designer' -count=1
go build -o /tmp/anban-designer-server .
```

Cover success, reference generation, mask editing, repeated/concurrent admission/execution, insufficient funds, expired/mismatched quote, foreign ownership, invalid output/storage failure, provider timeout and restarted execution recovery. If useful historical tests rely on obsolete contracts, update fixtures and assertions to current behavior; do not keep stale configuration aliases for tests.

- [x] **Step 5: Self-review and commit only Task 1 files.**

Run `git diff --check`, stage exact backend files and commit `feat(designer): restore server image workbench workflow`.
Write a report with changed interfaces, test commands/results, failure-before-fix evidence, and any remaining limitations. Do not touch Studio files or modify the approved spec's scope.

## Task 2: Restore Studio Designer and navigation

**Files:**
- Restore: `studio/src/pages/DesignerPage.tsx`, `studio/src/components/designer/` components and useful tests, `studio/src/lib/api/designer.ts`, `studio/src/lib/designer-session.ts`, `studio/src/types/designer.ts`, page behavioral tests.
- Modify: `studio/src/App.tsx`, `studio/src/lib/navigation.ts`, `studio/src/components/layout/AppLayout.tsx`, `studio/src/lib/direct-upload.ts`, relevant navigation/sidebar/command palette/layout tests.
- Use current image capability types in `studio/src/types`; only modify shared types if genuinely required.

**Interfaces:**
- Consumes Task 1's API contracts above via the current HTTP client. The existing `/image-capabilities` response exposes `generation_features`, never `designer_features`.
- Retain single-image quantity. Use current preset strings such as `1:1:2K`; do not reintroduce a fixed-pixel-only configuration assumption.
- Produces lazy `/designer` page guarded by `AdminRoute`, admin-only sidebar and command-palette entry, responsive workbench and current Web downloads.
- Preserve Server ownership and current billing flows; client cancellation means stopping local waiting, not canceling/refunding an accepted server operation.

- [x] **Step 1: Write visible behavior tests and observe failure.**

Update an existing admin sidebar test to expect the restored link; keep ordinary-user absence:

```tsx
expect(screen.getByRole('link', { name: '设计师' })).toHaveAttribute('href', '/designer')
```

Run `cd studio && npm exec --yes --package=bun -- bun run test -- src/components/layout/Sidebar.test.tsx` and record the missing-link failure.
Restore historical page/component/API tests before implementation, adapting their fixtures to `generation_features`. Favor rendered interaction and request-payload assertions; remove source-text tests tied to the retired layout rather than duplicating private implementation.

- [x] **Step 2: Restore the old workbench and adapt it to current Studio.**

Restore individual historical files after the failing tests, for example:

```sh
git show 31e94c16:studio/src/pages/DesignerPage.tsx > studio/src/pages/DesignerPage.tsx
git show 31e94c16:studio/src/lib/api/designer.ts > studio/src/lib/api/designer.ts
git show 31e94c16:studio/src/lib/designer-session.ts > studio/src/lib/designer-session.ts
git show 31e94c16:studio/src/types/designer.ts > studio/src/types/designer.ts
```

Restore DesignerToolbar, DesignerGenerationToolbar, DesignerCanvas, GeneratingAnimation, HistoryDrawer, ImagePreview and InlineMaskEditor from the same commit.
Adapt capability normalization, current AgentPromptInput/ProjectContextControl props, direct uploads and Web-only download helpers. Remove all Tauri imports and old host-specific branches.
Use current `lazyWithRecovery`, `AdminRoute` and navigation visibility rules. Register the page in workspace layout with a height-constrained canvas and mobile-safe prompt area.
Use current backend preset strings; initialize defaults from capabilities and keep all unsupported settings hidden/disabled. Display one image consistently.
Maintain selected-generation state, history reload, input references and mask export. Handle refresh by polling a persisted accepted generation ID. Avoid the old 6-minute polling cap: poll accepted jobs until terminal status, with bounded consecutive-error handling and an explicit stop-waiting control.
Key any persisted active-generation state by current user or clear it on identity change so accounts do not share active image state.
Preserve one operation ID per attempted logical submission through quote/generate retries. Show unavailable price/capability, insufficient balance and upload/terminal errors without exposing internal provider details.

- [x] **Step 3: Verify interactive behavior, API and access boundaries.**

Tests must cover capability selection, normalized single quantity, submitting current fields, drag/drop reference registration, unsupported references, valid mask submission, history regeneration, preview/download, cancellation during quote/upload and state refresh after completion.
Add a regression ensuring stopping wait after acceptance does not imply provider cancellation/refund and refresh can still recover the accepted operation.
Verify admin entry visibility in sidebar and command palette, with ordinary users excluded. Ensure restoring Designer does not change home/Assistant navigation.

Run:

```sh
cd studio
npm exec --yes --package=bun -- bun run test
npm exec --yes --package=bun -- bun run build
```

- [x] **Step 4: Self-review and commit only Task 2 files.**

Run `git diff --check`, stage exact Studio files and commit `feat(designer): restore Studio image workbench`.
Write the report with test counts, red/green evidence, changed API assumptions and UI checks. Do not push.

## Final integration and verification

Controller runs complete `go test ./...` plus Go build and confirms Studio full tests/build from the current reviewed code, rerunning only when changes invalidate prior evidence. Inspect the actual rendered page at desktop/mobile sizes using a local mock API if live credentials are unavailable; exercise generation/history/edit behavior against that mock, clearly distinguishing it from a paid provider integration.
Dispatch an independent whole-branch review against the approved spec. Fix actionable findings through the implementer and scoped re-review. Keep plan progress and rulings in the plan-owned SDD ledger. Report the final branch, tests, UI evidence and any real integration limitations.


## 完成记录（2026-11-03）

实现与独立复核完成，最后功能提交为 `bf00ccb2`。保留分支 `codex/restore-designer` 与原隔离工作区；未推送、合并或部署。

| 阶段 | 提交 | 结果 |
| --- | --- | --- |
| Server 恢复及审查修复 | `eb931e86`、`7fd634a6`、`f83d1f8a` | 持久化、权限、单张生成、报价扣费、幂等执行、失败冲正、上传和恢复；任务审查通过。 |
| Studio 恢复及交互修复 | `27b6b575`、`849d431d` | 管理员入口、工作台、参考图、蒙版、历史、预览下载；修复橡皮擦导出、空格输入、小屏工具栏；任务审查通过。 |
| 前后端集成修复 | `0256233d` | 独立生成使用空项目值；内部媒体鉴权；精确授权当前用户已完成的存储结果；提交响应丢失时保留恢复标识。 |
| 历史项目归属修复 | `bf00ccb2` | 从全部历史重新生成时恢复原项目，空项目记录保持独立；最终定向复核 Spec / Quality 均通过，无新增问题。 |

### 最终自动验证

所有命令在此工作区执行，Studio 使用 Bun。Go 在 `server/` 执行。

| 命令 | 对应代码 | 结果 |
| --- | --- | --- |
| `go test -json ./...` | `0256233d`；后续无 Go 修改 | 退出码 0，38 个包通过，6 个无测试包，0 个失败事件。 |
| `go build -o /tmp/anban-designer-final-server .` | 同上 | 退出码 0。 |
| `npm exec --yes --package=bun -- bun run --cwd studio test -- src/pages/DesignerPage.reference-drop.test.tsx` | `bf00ccb2` | 32 个测试通过；修改前项目归属回归实际失败。 |
| `npm exec --yes --package=bun -- bun run --cwd studio test` | `bf00ccb2` | 189 个文件、1,742 个测试通过，退出码 0。 |
| `npm exec --yes --package=bun -- bun run --cwd studio build` | `bf00ccb2` | TypeScript 与 Vite 构建通过，退出码 0。 |
| `git diff --check` | 完整恢复分支 | 通过。 |

已通过真实服务/数据库路径的测试覆盖管理员与套餐边界、他人项目/参考图/蒙版拒绝、报价绑定、并发受理与执行认领、输出持久化、失败冲正、超时和重启恢复。独立生成请求由 Go 与 Studio 共用 JSON fixture 验证。非本地存储的媒体授权通过真实 FileHandler/Service 和 SQLite 验证，只有当前用户持久化且已完成的精确结果 key 获准读取。

### 本地界面验收

通过 CUA 在 Vite 与严格本地 mock API 下完成，使用仓库品牌 PNG 作为图片字节，未调用付费图片 Provider。

- 管理员入口、能力/尺寸/质量与单张生成、报价/提交/轮询/完成、预览下载均可用。
- 内部图片端点拒绝缺少 bearer token 的请求；画布、历史缩略图、预览和编辑器实际通过认证 blob 加载，下载字节与源 PNG 一致。
- 提交被接受后故意断开响应（代理返回 502），刷新恢复同一任务和图片；日志仅有一次 generate POST，未重复生成。
- 高质量、`2:3:2K`、webp、压缩 70、不透明背景，以及参考图/蒙版历史输入在重新生成时保留；界面提示可清除历史输入。
- 从未选项目的全部历史恢复项目 A 后，选择器显示项目 A，quote 和 generate 都保留其项目 ID；空项目历史仍发送空 ID。
- 参考图上传/注册、矩形蒙版与局部编辑通过；实际导出 PNG 检查确认选中区域 alpha 为 0，擦除及未选区域为不透明白色。
- 编辑模式提示词 `A + 空格 + B` 得到 `A B`；停止等待后刷新仍查询原任务。
- 390×844 下高级选项和清除按钮可操作，页面宽度与 scrollWidth 均为 390；临时 viewport 已恢复，验证服务和浏览器标签已关闭。

截图与压缩验证材料保存在本次任务的本地可视化目录 `designer-restored.jpg`、`designer-mask.jpg`、`designer-mobile.jpg`、`designer-history.jpg`、`designer-project-history.jpg` 和 `designer-verification-evidence.tar.gz`。完整命令输出、失败/通过证据和审查报告已归档；仅清理本计划的临时 SDD 目录。

### 验证边界与已知情况

- 未进行真实付费 Provider、生产 OSS 或在线 MySQL 联调；MySQL 迁移以字段/索引结构与幂等建表约定验证。
- 旧版本已经 DropTable 删除的数据无法通过本次代码恢复重建；新迁移保留仍存在的历史数据。
- 当前查询 Handler 将数据库查询错误也映射为 404，因此刷新查询失败时保留操作标识，不自动重发生成。持久不存在的操作可能需要用户显式停止等待。
- 中间一次全量 Go 和一次全量 Studio 运行失败；后续完整重跑通过。Studio 失败在未修改的 Dashboard 热点测试，原因未证实；未修改无关代码来掩盖失败。
- 原有 macOS/libvips 链接目标、Vite native-config 和 vendor chunk 大小警告保留，不影响最终构建结果。

### Rulings I made（按发生顺序保留的执行取舍）

| 取舍 | 依据 | 判断错误时的代价 |
| --- | --- | --- |
| 直接采用子代理实施，不再询问流程选择。 | 用户明确指定 Superpowers 并批准方案；流程可逆。 | 若用户期望其他过程，会多出协调开销，功能范围未变。 |
| 用 bash 和显式输出路径调用无执行权限的技能脚本。 | 避免修改共享安装文件。 | 错误路径可能遗漏审查材料；已验证产物。 |
| 先观察行为测试失败，再复用历史代码。 | 恢复已知功能，避免无必要重写。 | 旧契约可能不兼容；已通过当前接口测试和独立审查校正。 |
| 接受三项后端审查问题，完整保留高级参数和参考/蒙版历史上下文；MySQL 采用结构验证。 | 满足历史重用与安全错误输出，界面显式显示保留输入。 | 线上 MySQL 差异仍需部署环境验证。 |
| 补齐并发/报价拒绝、超时和上传 finalization 的真实服务回归。 | 关键行为原先缺少直接覆盖。 | 增加测试维护成本，没有扩大产品功能。 |
| 纯报价绑定测试修复后先定向验证，将全量运行留到集成阶段。 | 生产代码未变，最终完整检查仍执行。 | 隐藏回归会延后发现；最终全量已通过。 |
| 保留分支/工作区，不另行询问合并或发布。 | 已批准范围明确排除推送、合并和部署。 | 功能尚未进入主线或生产环境。 |
| 刷新 GET 返回 404 时保留恢复标识。 | 当前 Handler 不区分记录不存在与数据库错误，避免丢失已接受任务。 | 永久不存在的 ID 可能保留，需显式停止等待；不会自动付费重试。 |
| 最后审查发现项目归属丢失后增加一次限定修复和复核。 | 完整历史重用属于已批准功能；完成用户任务优先于技能默认的一轮修复上限。 | 增加一次审查开销；仅一行生产修复及针对性测试，无功能扩展。 |
