# 托管任务产物交付可靠性 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 移除托管产物的 STS 依赖，让可恢复上传错误在有限期限内恢复，并准确展示无法恢复的交付错误。

**Architecture:** OSS 使用单一签名 PUT 协议，非 OSS 继续 stream。Runner 负责可重放字节传输，Server 复用既有不可变对象、manifest 幂等、终态与结算机制；进度和公开错误以结构化证据驱动。

**Tech Stack:** Go/Fiber/GORM、TypeScript/Bun/Claude Agent SDK、React/Vitest、现有 OSS storage provider。

**Spec:** `docs/superpowers/specs/2026-09-20-artifact-delivery-reliability-design.md`

## Global Constraints

- 用户已授权实施、Review 后合并到本地 main。代码已实施，以下步骤保留原验收设计；实际完成情况以文末执行记录为准。线上任务重跑、staging 和生产发布不在本次执行范围。
- 不保留托管产物的旧 STS 回退、双响应协议或重复上传实现。
- Studio 素材大文件分片上传仍是 STS 的当前产品用途。本设计不删除这项业务能力。
- 非 OSS 存储继续使用现有 stream 传输；这是当前存储适配能力，不是兼容路径。
- Server 继续决定必需交付、文件状态、任务终态和结算；Runner 仅负责字节传输与有限传输重试。
- 不新增常驻 Agent、共享工作目录、Server 执行创作步骤或新的通用工作流引擎。
- 单个逻辑请求最多 4 次尝试，阶段预算默认 120 秒、最大 300 秒；各层不能乘法叠加重试。
- 新测试只验证可观测行为、故障恢复与权限边界；不使用真实凭据或付费生成服务。
- 每个任务先添加会失败的行为测试，再实现、运行针对测试并独立提交。执行时遵循仓库的 Go/Studio 全量验证要求。
- 本计划不修改 harness；若实施中确需改 Agent 指令，先明确新增范围，修改 canonical pack 并生成、校验，同步递增两个 native manifest 版本。

## 实施顺序与接口

依次执行 1 → 2 → 3 → 4 → 5 → 6。Task 1 同一提交修改 Server 与 Runner 协议；Task 2 定义的结构化传输失败是 Task 3 的输入；Task 4 定义的 outcome 是 Task 5 的输入。

### Task 1：拆出托管产物签名上传协议

**Files**

- 修改：`server/service/task_artifact_upload.go`、`server/handler/agent.go`、`server/main.go`。
- 修改/测试：`server/service/task_artifact_upload_test.go`、`server/handler/agent_test.go`。
- 修改：`agent-ts/src/reporter.ts`、`agent-ts/src/artifacts.ts`、`agent-ts/package.json`、`agent-ts/package-lock.json`。
- 删除：`agent-ts/src/types/ali-oss.d.ts`。
- 新增测试：`agent-ts/test/artifact-upload-protocol.test.ts`。

**Interfaces**

- 新 Go 类型 `TaskArtifactPrepareResult` 与 spec 中 `ArtifactPrepareResponse` 一一对应。
- 新配置 `TaskArtifactUploadConfig{ExpiresSeconds int; Now func() time.Time}`；由 handler 从 storage 设置提取。此服务不再接收 CredentialIssuer。
- AgentHandler 改为 `SetTaskArtifactUploadConfig`，`server/main.go` 注入专用配置；删除 AgentHandler 的旧 `SetDirectUploadConfig`，浏览器 UploadHandler 继续使用自身配置。
- `PrepareTaskArtifactUpload(..., cfg TaskArtifactUploadConfig, req TaskArtifactPrepareRequest) (*TaskArtifactPrepareResult, error)`。

- [ ] 写红灯测试：用现有 fake OSS provider（签名正常）和完全未配置 STS 的环境调用 prepare，断言成功、会话存在，响应不含任何 STS 字段；最终对象匹配时不创建新会话。

```go
// 沿用 task_artifact_upload_test.go 的 fixture 构造 svc/task/execution。
got, err := svc.PrepareTaskArtifactUpload(ctx, task.ID, task.UserID, executionID,
    TaskArtifactUploadConfig{ExpiresSeconds: 900}, req)
if err != nil { t.Fatal(err) }
raw, err := json.Marshal(got)
if err != nil { t.Fatal(err) }
for _, forbidden := range []string{"sts_access_key_id", "sts_access_key_secret", "sts_security_token", "endpoint", "bucket"} {
    if bytes.Contains(raw, []byte(`"`+forbidden+`"`)) { t.Fatalf("unexpected field %s", forbidden) }
}
```

- [ ] 运行 `cd server && go test ./service ./handler -run 'Test.*(Artifact|Prepare)' -count=1`，确认新测试因现有 STS 依赖失败。
- [ ] 按以下顺序重写 prepare，删除 credential issuer 调用及响应字段，保留既有授权、签名头、条件提升、会话过期逻辑。

```text
authorize execution → validate path/hash/size → HEAD immutable final
  exact match → {upload_required:false,key}
  missing → persist staging session → sign PUT for staging key → typed response
  storage error → typed retryable/nonretryable error
```

- [ ] Runner 对响应做联合类型校验；需要上传却无 URL 直接报协议错误。删除 STS fallback 和重复的 `putPreparedArtifact`，移除 `ali-oss` 依赖并更新 lockfile。
- [ ] 测试 URL+headers 原样使用、SHA-256 头缺失拒绝、伪造路径/权限拒绝、非 OSS stream 可用；运行针对测试与 Runner typecheck。提交 `fix(artifacts): use signed URLs for managed uploads`。

### Task 2：增加单层、有期限、可重放的传输重试

**Files**

- 新增：`agent-ts/src/transport.ts`、`agent-ts/test/transport.test.ts`。
- 修改：`agent-ts/src/reporter.ts`、`agent-ts/src/artifacts.ts`、`agent-ts/src/main.ts`。
- 测试：`agent-ts/test/reporter.test.ts`、`agent-ts/test/artifacts.test.ts`、`agent-ts/test/main.test.ts`。

**Interfaces**

```ts
type TransferOperation = "prepare" | "put" | "stream" | "manifest" | "complete" | "progress";
type TransferErrorCode = "connection_reset" | "network_timeout" | "network_unavailable"
  | "rate_limited" | "service_unavailable" | "invalid_request" | "unauthorized"
  | "execution_conflict" | "signature_expired" | "integrity_mismatch"
  | "protocol_error" | "deadline_exceeded" | "cancelled";
type TransferFailure = {
  code: TransferErrorCode; operation: TransferOperation; http_status?: number;
  attempts: number; retryable: boolean; request_id?: string;
};
// TypedTransportError extends Error，公开 readonly failure: TransferFailure。
// retryRequest<T>(operation, attempt: (signal: AbortSignal) => Promise<T>,
//   options: {signal: AbortSignal; timeoutMs?: number; maxAttempts: 4}, dependencies): Promise<T>
// dependencies 注入时钟、sleep、random，测试不真实等待。
```

- [ ] 红灯测试注入“首次 503，第二次成功”和“首次 ECONNRESET，第二次成功”，断言每种都是 2 次；400/401/403/409 各只 1 次；4 次 503 耗尽；abort 立即结束；Retry-After 超过阶段预算不能提前重试。

```ts
// 使用 Bun.serve HTTP fixture 和真实 Reporter/uploadWorkspaceArtifacts。
expect(reviewPrepareAttempts).toBe(2);
expect(summary.failures).toEqual([]);
expect(manifestPaths).toContain("output/image-review.md");
expect(generationCalls).toBe(1);
```

- [ ] 运行 `cd agent-ts && bun test test/transport.test.ts test/artifacts.test.ts`，确认现有单次 prepare 行为无法通过。
- [ ] 实现唯一重试层：每次重建请求/流；Reporter 的 prepare、manifest、complete、结构化 progress 和 artifacts 的 PUT/stream 使用同一策略。删除旧不分类重试 helper，禁止外层再套请求重试。
- [ ] 保持 prepare 15 秒、manifest 30 秒的单次上限及共用阶段 deadline；每次创建子 AbortSignal，区分单次超时与父 signal 取消。按 spec 的退避与 Retry-After 算法执行。
- [ ] 对 PUT 明确过期机器码允许一次重新 prepare；其他 403 不刷新。网络失败继续同 staging key；每次从字节 0 读取，关闭旧流，快照变化不得提交错误 hash。
- [ ] 验证 PUT 首次写入成功但响应丢失、大文件第二次请求完整重放、manifest 提交成功响应丢失、stream 对象复用与取消。运行 `bun test test/transport.test.ts test/reporter.test.ts test/artifacts.test.ts test/main.test.ts` 和 typecheck。提交 `fix(runtime): retry artifact transfers within a shared deadline`。

### Task 3：保留可诊断的失败与幂等交付证据

**Files**

- 修改：`agent-ts/src/artifacts.ts`、`agent-ts/src/main.ts`、`agent-ts/src/reporter.ts`。
- 修改：`server/agent/executor.go`、`server/service/task_execution_complete.go`。
- 测试：`server/service/task_execution_complete_test.go`、`server/service/task_artifact_upload_test.go`、`server/repository/task_file_execution_test.go`、`agent-ts/test/main.test.ts`。

**Interfaces**

- `ArtifactUploadFailure` = Task 2 `TransferFailure` + 规范化 `path`；manifest 整体错误使用结果的 root error 字段，不捏造文件路径。
- Server 主错误码：`artifact_upload_failed`、`artifact_manifest_failed`；缺件无传输证据仍为 `deliverable_validation_failed`。
- 复用 `ReplacePendingCurrentExecutionPreservingMCPArtifacts` 的 sealed equality 行为，不新增 manifest 表或幂等框架。

- [ ] 写 Server 表驱动红灯用例，并断言任务终态、文件 retained/delivered 状态、诊断和 billing terminal reason：

```text
required file absent + matching upload failure → failed/platform_error/artifact_upload_failed
required file absent + no upload failure → failed/deliverable_validation_failed
required files accepted + optional upload failure → succeeded + warning
reported upload failure but required file already verified → actual file evidence wins
manifest never acknowledged → no success
same sealed manifest submitted twice → success, no duplicate rows
changed sealed manifest or stale execution → conflict, no mutation
```

- [ ] 运行 `cd server && go test ./service ./repository -run 'Test.*(Artifact|Manifest|CompleteCloudExecution)' -count=1`，确认诊断优先级相关用例失败。
- [ ] Server 将缺失的规范路径与上传失败关联，在原有终态判定内生成结构化基础设施诊断；不因 Runner 文本声称成功跳过对象验收，不用含错误的部分集合覆盖已交付集合。
- [ ] Runner 保留创作结果与传输诊断的区别，partial manifest 只在所有文件尝试结束后冻结提交；manifest/complete 重试保持请求完全相同。通过受控 code 生成用户文案，删除自由文本 reason 协议。
- [ ] 测试 retry 不触发第二次生成、不重复结算、过期 staging 清理不删除 final。提交 `fix(tasks): preserve artifact transfer failure provenance`。

### Task 4：修正进度协议和终态归属

**Files**

- 修改：`agent-ts/src/runner.ts`、`agent-ts/src/progress.ts`、`server/handler/agent.go`、`server/service/task_lifecycle.go`。
- 修改：`server/model/task_lifecycle.go`、`server/model/task_outcome.go`、`server/service/task_execution_complete.go`。
- 修改：`server/repository/task.go`、`server/repository/repository.go`；测试 `server/repository/task_file_execution_test.go` 中原子终态路径。
- 测试：`agent-ts/test/runner.test.ts`、`agent-ts/test/progress.test.ts`、`server/handler/agent_lifecycle_test.go`、`server/service/task_lifecycle_test.go`、`server/model/task_lifecycle_test.go`。

**Interfaces**

- `ExecutionDiagnostic.code` 增加平台错误码；`stage=artifact_upload|completion` 为基础设施终止位置。
- 进度错误码固定为 `progress_unknown_stage`、`progress_out_of_order`、`progress_invalid_state`，使用现有 `ErrorWithCode`。
- `NormalizeTaskLifecycleTerminal` 增加明确的终止范围参数（`work` 或 `infrastructure`），服务端调用者传入；不存在参数缺失的兼容重载。
- 同步给 `FinalizeCloudTaskWithArtifactsInTx` 与 `FinalizeTaskForExecution` 增加该类型参数并更新所有调用点；范围由 service 的结构化终态决定，repository 不解析错误文案。原子终态事务及独立 lifecycle finalization 必须使用同一范围，避免事务先错误标记阶段、后续无法纠正。

- [ ] 红灯测试：TaskCreate 登记 metadata，后续 TaskUpdate 无 metadata 仍上报同一 stage；显式冲突拒绝；未绑定任务不产生事件；错误 400 只请求一次并保留明确 code。

```ts
const explicit = progressStageMetadata(input.tool_input.metadata);
const known = taskStages.get(taskID);
if (explicit && known && explicit !== known) return rejectProgressConflict();
const stage = explicit ?? known;
if (!stage) return {};
// rejectProgressConflict：本 Task 新增局部 helper，记录诊断并返回空 Hook output。
```

- [ ] 验证服务端非法跃迁仍被拒绝、重复 complete 更新描述不修改完成时间；进度失败不决定内容任务成败。
- [ ] 基础设施失败终态不改写任何 Agent 阶段为 failed/skipped；保留历史进度快照，由全局诊断表达终止。Studio 不为已终止任务继续显示 active 动画，未确认的状态显示“进度未确认”。普通工作失败保留原有定位策略。
- [ ] 运行针对生命周期、Runner progress 的测试。保留日志中的 stage/state/code 供真实 400 排查，不把猜测写成线上根因。提交 `fix(progress): retain stage identity and separate delivery failures`。

### Task 5：公开错误展示与日志脱敏

**Files**

- 修改：`studio/src/types/task.ts`、`studio/src/lib/labels.ts`、`studio/src/components/tasks/TaskExecutionRail.tsx`、`studio/src/pages/TaskDetailPage.test.tsx`。
- 修改：`server/handler/agent.go`、`server/service/task_execution_complete.go`。
- 新增：`server/service/artifact_diagnostic.go`、`server/service/artifact_diagnostic_test.go`，负责将存储/网络错误转成安全字段；handler 只编码或记录字段。
- 测试：`agent-ts/test/transport.test.ts`、`server/handler/agent_test.go`。

**Interfaces**

- Studio 从 `outcome.diagnostic.code/stage/summary` 判断上传错误；不正则解析英文 error_message。
- `artifact_upload_failed` 文案为“产物上传失败，已成功上传的文件已保留。”；上传状态独立于创作阶段。
- Go `ArtifactDiagnosticFields` 包含 operation/code/http_status/request_id/network_code，不含 URL、凭据或任意原始 error string。

- [ ] 写 UI 红灯测试：同原故障的 15 个 retained 文件、必需报告上传失败、0 个已确认完成阶段，必须显示上传失败且不显示“选题研究失败”。已完成阶段仍可正常浏览下载。
- [ ] 写脱敏测试：构造虚假签名 URL、Authorization 和 STS token 的 `url.Error`，断言结构化日志、API 和 public outcome 都不包含这些字段的值，仍保留 `connection_reset`。

```text
error input: POST https://storage.invalid/object?Signature=FAKE_SECRET → ECONNRESET
safe output: {operation:prepare, code:connection_reset, network_code:ECONNRESET}
forbidden output: FAKE_SECRET, Authorization value, query string, raw response body
```

- [ ] 修改 Server 错误分类 helper 和 handler 日志，Runner 只记录有界结构化错误；不记录整个 fetch/url error。通过明确 code 构造 UI 错误块，基础设施失败不挂到任意工作阶段。
- [ ] 运行 `cd studio && bun run test -- src/pages/TaskDetailPage.test.tsx` 和相关 Go/Runner 针对测试。提交 `fix(studio): show artifact delivery errors with safe diagnostics`。

### Task 6：故障注入验收与同步发布

**Files**

- 新增：`agent-ts/test/artifact-delivery.integration.test.ts`，本地 HTTP fixtures 与真实传输路径组合测试。
- 更新：本计划的执行结果和发布记录，填写实际构建摘要与验证结果，不改写计划中的验收要求。
- 验证已有部署定义：`deploy/docker/Dockerfile.agent-article`、`deploy/docker/Dockerfile.agent-seednote`、`deploy/docker/Dockerfile.agent-montage`，保持根目录 build context。

- [ ] 集成 fixture 创建包含必需报告的 16 个文件，不调用图片生成服务；分别注入 prepare reset/503、PUT 中途断连、manifest 成功后断连、持续 503、400、取消和 deadline。
- [ ] 断言临时故障最终 16/16、持续故障 15 个保留且准确归因、生成次数恒为 1、无重复清单/结算、所有请求次数和总时限符合上限。
- [ ] 运行并记录完整验证：

```bash
cd server && go test ./...
cd server && go build -o /tmp/anban-creator-server .
cd agent-ts && bun run test
cd agent-ts && bun run typecheck
cd agent-ts && bun run build
cd studio && bun run test
cd studio && bun run build
```

以上命令各自从仓库根目录开始；使用执行工具的 workdir 参数，不能在同一 shell 连续执行相对 cd 导致路径错误。

- [ ] staging 验证：实际 OSS 签名上传、权限校验和 metadata 条件提升；使 STS 不可达，确认托管产物仍成功；验证非 OSS stream 与 Studio 大文件分片上传各自的当前业务路径。
- [ ] 构建并发布三个 runtime image 与 Server/Studio；使用不可变摘要。暂停新任务派发、排空旧执行、同步切换、恢复派发；不引入旧协议 fallback。按匹配构建组回滚。
- [ ] 发布后执行一个小规模种草笔记任务，检查必需文件全部持久化、进度事件均被接受、上传失败指标/日志及费用无重复结算。部署需在环境和授权齐备后执行，不以本地测试替代线上验证。

## 完成定义

- 故障链中的 STS 请求已从托管产物上传路径删除。
- 同一任务的一次连接重置/503 可自动恢复，必需报告不会因一次错误被永久漏掉。
- 永久上传失败明确展示其阶段与原因，已有产物保留，不伪装为创作失败。
- 传输重试有统一时限与幂等验收，用户取消立即生效。
- 已验证的进度协议问题修复，真实 400 可定位到具体受控错误码。
- 没有凭据、签名查询串或原始云服务响应泄露到日志/API/产物。
- Server、Runner、Studio 验证通过，所有生产 runtime images 使用匹配的新构建。

## 执行记录（2026-09-20）

本次按 Server 上传、Runner 传输和 Server outcome/Studio 三个文件边界并行实施，并进行独立代码审查；跨端协议作为同一变更集提交，避免中间提交暴露不匹配接口。未引入旧协议兼容路径。

| 计划范围 | 实现与验收位置 | 状态 |
| --- | --- | --- |
| Task 1 | 专用 prepare 类型、签名 PUT、移除 ali-oss；现有 upload/reporter 测试覆盖协议，浏览器 STS 保留 | 已实现 |
| Task 2 | transport.ts；prepare/PUT/stream/manifest/complete/progress 有界重试、重新打开传输句柄 | 已实现，补充用例通过 |
| Task 3 | 路径匹配的必需上传失败与可选 warning；清单整体结构化故障；完成输入原子摘要 | 已实现 |
| Task 4 | TaskCreate 映射回退、400 分类、重复 complete 描述更新、终止范围贯穿原子 repository | 已实现 |
| Task 5 | 独立上传故障展示、未确认进度、受控诊断字段、云 URL/凭据脱敏 | 已实现 |
| Task 6 本地 | HTTP 故障注入、各层回归、全量验证、独立复审 | 全量验证与独立复审通过 |
| Task 6 云环境 | OSS/staging smoke、镜像发布、生产回归、原任务恢复 | 未执行 |

审查额外发现并纳入修复：

- 文件验收失败并转为 retained 后，重复 complete 曾重新计算出不同错误。新增失败复现测试，改为终态 CAS 同步保存服务端完成输入摘要；相同请求复用终态，不同请求返回冲突，结算仍由原有 finalizer 幂等执行。
- 大文件首轮传输中断曾关闭共用快照句柄，第二轮因 EBADF 失败。每次传输改用独立且经过身份验证的句柄，并补充大文件早期拒绝/断连测试。独立审查的 32 MiB 故障复现已确认完整重放。
- Retry-After 超过阶段剩余时间时，原先只有 helper 单测传入 deadlineAt，生产调用未接入。修复为贯穿主入口与每种传输调用的共享绝对期限，避免可选文件耗尽其他文件和清单的预算。
- 包装清单错误和阶段取消曾丢失类型信息，Go DTO 也未保存阶段级故障。统一保留 `artifact_finalization_failure`，由 Server 按受控字段投射公共诊断。
- 基线命名契约扫描发现两处 Studio 测试 fixture 使用旧品牌名称；仅将测试值与断言改为 Anban，不改变产品代码或测试规则。

测试布局与初始计划有调整：新增协议断言放入已有 service/handler/reporter 测试，进度测试放入现有 handler/runner 测试；不为文件命名形式增加重复测试。端到端云环境验收不以本地 fixture 冒充。

本地验证结果：

- Server：`go test ./...` 与 `go build -o /tmp/anban-artifact-delivery-server .` 通过。
- Runner：全量 154 项测试、TypeScript typecheck、build 通过。
- Studio：102 个测试文件、875 项测试、build 通过；fixture 名称调整后 TaskDetailPage 的 57 项测试再次通过。现有 Vite 大 chunk 提示不阻断构建。
- 全量差异通过 `git diff --check`；harness 与 humanizer submodule 无改动。
- 本地故障验证覆盖 prepare 503、PUT/stream 早期失败完整重放、manifest 重放、持续错误、取消与时限、过长 Retry-After 后继续交付其他文件；Server 测试覆盖缺件归因、保留产物、终态重放与既有幂等结算。

部署限制：本次没有构建/推送生产镜像，没有执行真实 OSS staging smoke，没有重跑原任务。上线仍需按上文同步发布匹配的 Server、三个 Runner 镜像和 Studio；不宣称本地测试已验证实际云网络或恢复原任务未上传文件。

最终独立复审：无剩余阻塞项；最后一轮针对测试 46 项全部通过，确认共享 deadline 的生产接入、长 Retry-After、prepare 取消诊断和响应体清理。按用户授权合并到本地 main，不推送远端。
