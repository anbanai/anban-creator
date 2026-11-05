# 设计师工作台恢复方案

状态：用户已于 2026-11-03 确认「按方案执行」；现已完成实现、全量测试与独立审查。
最终功能提交 `bf00ccb2`，验收结果与执行取舍见[实施计划](../plans/2026-11-03-restore-designer.md#完成记录2026-11-03)。

## 目标与依据

恢复 Studio 独立的「设计师」工作台及支撑其运行的 Server 接口、持久化和计费。
以删除前版本 `31e94c16db3eb0f1e15838a5e7968765b185077d` 的功能与交互为参考，
基于最新主线 `1f1e94ac` 实施，工作分支为 `codex/restore-designer`。

历史证据：

- `b5b87a50`（2026-06-04）首次引入 Designer 图片工作台。
- `711b90bf`（2026-08-14）删除 Studio 页面、Miniapp 页面、Server 接口和模型。
- 当前 `server/main.go` 仍调用 `MigrateDesignerRemoval`，删除
  `designer_references`、`image_generation_results`、`image_generations`。
- 当前图片能力接口使用 `generation_features`，旧版使用 `designer_features`。
- 当前 `image.standard` / `image.professional` 固定 SKU 及单张出图能力仍然存在。
- 当前上传流程使用 UploadSession 和最终 Asset，旧版页面还依赖已废弃的桌面桥接代码。

## 方案选择

| 方案 | 取舍 |
| --- | --- |
| 按旧功能恢复，适配当前主线（推荐） | 复用旧页面、蒙版编辑和行为测试；后端接入当前能力、存储和计费，修改集中且可验证。 |
| 原样还原旧提交 | 无法直接兼容当前导入路径、能力字段、上传流程和启动迁移；整个删除提交还包含无关功能，不能整体回退。 |
| 重新设计为首页 Agent 创作模式 | 会改变用户原来的操作方式并扩大需求，不作为本次恢复方案。 |

## 功能范围

1. 恢复 `/designer` 路由，以及侧边栏、命令面板中的「设计师」入口。
2. 默认延续删除前的管理员访问范围；页面、导航与 Server API 都执行权限校验。
   管理员身份不跳过图片能力的套餐权限、价格和余额检查。
3. 恢复提示词生成、参考图上传/拖放、图片预览、下载、历史记录和从历史重新生成。
4. 恢复画布涂抹与局部重绘，仅在所选能力支持蒙版时开放。
5. 恢复能力、尺寸、质量、输出格式及受支持的水印、背景、压缩选项。
   选项直接取当前 `generation_features`，不重新引入旧配置字段。
6. 保留项目关联选择，也允许不选择项目而独立生成。
7. 沿用现有固定 SKU 的单张生成限制（`n = 1`）；数量显示为 1，前后端一致。
8. 刷新后可继续查询已受理任务；历史和结果按当前用户隔离。

本次范围为 Studio 工作台与 Server。Miniapp、Designer Agent Pack、线稿上色 Skill、
桌面 Tauri 功能和新的批量计费不在本次恢复范围内。
不修改现有首页创作、人设或 Agent 工作流，也不自动发布到外部平台。

## 页面与交互

沿用旧版的参数侧栏、中央画布、底部提示词输入和历史抽屉。
复用当前 Studio 的 AgentPromptInput、附件管理、项目选择、UI 组件、钱包查询及懒加载方式。
适配当前 AppLayout 的可用高度、移动端导航和暗色主题。
下载使用当前 Web 下载机制，去掉旧版 Tauri 依赖。

提交前检查提示词、能力、受支持参数和参考图限制；无可用能力或无价格时明确提示。
余额不足、上传失败、生成失败均给出可理解的错误。
服务端受理后的任务通过记录 ID 查询；停止前端等待不宣称已取消上游生成或已退款。
轮询时限应覆盖所选能力配置的生成超时，不能沿用旧版短于当前 8 分钟能力超时的 6 分钟上限。

## Server 与数据流

恢复独立 DesignerService、薄 HTTP Handler、生成记录仓储和参考图仓储。
Provider 调用继续使用 Server-owned `server/app/image`；不新增 Agent 调度或 MCP 编排。

流程：

1. 从 `/image-capabilities` 读取当前用户可用的图片能力和价格。
2. 参考图/蒙版经现有直传与 finalization 流程固化，注册为当前用户的 DesignerReference。
3. `/designer/quote` 校验能力、参数、项目归属和参考文件，创建当前计费目录的报价。
4. `/designer/generate` 校验报价与请求指纹，在一个事务内扣费并创建生成记录。
5. Server 异步执行，持久化图片和结果后将记录置为完成；前端轮询状态。
6. 生成失败时，失败状态与冲正意图使用现有钱包 settlement outbox 持久化。

恢复所需接口：quote、generate、register-reference、upload-reference、
upload-reference-from-url、history、generations/:id。
所有接口要求登录和管理员身份；查询和文件复用始终校验用户归属。
URL 转参考图仅接受本服务可验证归属的存储对象，不接受任意远程图片 URL。

同一 operation_id 重试只返回原记录；请求内容不同则拒绝。
执行前原子认领记录，防止旧 Handler 每次重试都启动一次上游生成的问题。
复用已有计费冲正机制，结果未持久化不能标记交付成功。
已开始但超时的任务应收敛为失败并提交一次冲正意图，避免服务重启后永久显示生成中；
已经调用上游且结果未知的任务不自动重发生成请求。

## 数据库与上传

停止调用并移除不再适用的 Designer 删除迁移及对应删除断言。
恢复 DesignerReference、ImageGeneration、ImageGenerationResult，并纳入 AutoMigrate。
迁移必须可以重复执行，保留已有设计师记录；新数据库能创建全部所需表。
提供与当前生产迁移约定一致的建表迁移，验证字段和索引与 Go 模型一致。

已被旧版本 DropTable 删除的历史数据无法仅靠恢复代码重建；有现存数据时必须保留。
新增 `designer_reference` 上传用途及限制，复用当前 UploadSession finalization。
参考文件、蒙版和生成结果均保存稳定存储标识，展示时生成可访问 URL。
文件归属由服务端记录和当前存储规则校验，不复用旧版对 URL 路径的简单前缀判断。

## 主要改动位置

- `studio/src/pages/DesignerPage.tsx` 与 `studio/src/components/designer/`：恢复工作台与交互。
- `studio/src/lib/api/designer.ts`、`studio/src/types/designer.ts`、会话状态：恢复接口和类型。
- Studio 路由、navigation、AppLayout、直传用途及对应测试：接入当前应用。
- `server/model/`、`server/repository/`：恢复生成和参考图持久化。
- `server/service/designer*.go`、`server/handler/designer.go`：能力校验、报价、生成、结果查询。
- `server/main.go`、`server/router/router.go`：服务接线、路由权限和启动迁移。
- `server/service/direct_upload.go`、数据库迁移及对应测试：上传与数据保留。

共享配置和图片 Provider 只在恢复功能确有需要时修改，不整体回退历史文件。

## 验收与验证

先编写并运行有意义的失败测试，再实现对应行为。

- 导航：管理员可见并可打开，普通用户不可见且不能调用 API。
- 生成：无参考图生成、参考图生成、支持蒙版的局部重绘成功并可查询/下载。
- 参数：当前尺寸预设、质量、格式、参考数量与单张限制在两端一致。
- 权限：不能读取他人历史、使用他人项目/参考图/蒙版或绕过套餐能力限制。
- 计费：报价失效、指纹不符、余额不足均拒绝；相同请求重试只扣费且执行一次。
- 失败：上游失败、输出不合法、存储失败和执行超时均保存失败结果并进行幂等冲正。
- 迁移：首次启动建表，重复启动不清空结果。
- 页面：刷新恢复轮询、历史重新生成、编辑切换、下载和小屏布局可用。

完成后运行：`cd server && go test ./...`、
`cd server && go build -o /tmp/anban-designer-server .`、
`cd studio && bun run test`、`cd studio && bun run build`。
外部图片 Provider 使用 httptest 或测试替身验证，不以真实付费生成作为自动测试前提。
报告中区分自动测试、界面检查与是否进行过真实 Provider 联调。

## 实施前基线（2026-11-03）

- 已创建独立 worktree 和分支，当前只有本方案文档，未修改功能代码。
- 已按锁文件安装 Studio 依赖，并初始化主线锁定的 harness 及嵌套子模块。
- Studio 全量测试通过：178 个文件、1667 个测试。
- Go 首次全量测试未全绿：部分契约测试缺少未初始化的嵌套子模块；
  `TestReuseMalformedPaidSearchAndFetchRetainsChargeAndBlocksReplay/fetch=true`
  也在该轮失败。
- 补齐子模块后，`go test ./agent ./agentpack ./mcp` 全部通过。
  上述 service 测试单独以 `-count=1` 复测通过，未修改该测试或对应业务代码。
  这些定向结果不代表完整 Go 测试套件已经重新通过；实施后仍需完整重跑。
- 本机无全局 Bun，以上通过 `npm exec --yes --package=bun -- bun ...` 使用 Bun 执行。
