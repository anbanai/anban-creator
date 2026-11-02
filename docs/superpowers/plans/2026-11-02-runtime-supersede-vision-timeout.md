# 运行时对账死循环与图像理解超时：根因与修复计划

日期：2026-11-02（Asia/Shanghai）。用户已批准；本文件为实施记录。

## 目标

让「被取代的 execution」在一次对账内被安全退役，消除每 10s 的
`runtime execution reconciliation item failed` 告警与对账队列饥饿；并修复视觉理解调用未关闭
Kimi thinking、模型白名单过期导致的 180s 无响应超时，补齐可定位的请求级诊断。

范围：仅 `server/`。不改 Studio、runtime-claude、harness，不新增数据库列，不新增公开 API 字段。

## 根因

### A：`task execution is no longer current`

- `server/repository/task_execution.go` 的 `FindReconcilable` 对终态 execution 的条件是
  `finalization_status <> '' AND finalization_status <> 'done' OR cleanup_status = 'pending'`，
  没有年龄上限和重试上限。
- `server/agent/runtime_reconciler.go` 的 `reconcileOne` 对终态执行调用
  `ResumeExecutionFinalization`；当 `tasks.current_execution_id` 已指向更新的 attempt 时返回
  `ErrStaleTaskExecution`。
- 该错误只记 Warn，不持久化任何状态；该行 `updated_at` 不会更新（失败发生在
  `ClaimFinalization` 之前），于是永远满足查询条件，每 10s 再告警。
- 查询 `ORDER BY updated_at ASC LIMIT 50`，批大小 50、间隔 10s。累计 50 行后所有真实执行
  不再被对账（dispatch 恢复、心跳超时、失败终态化、runtime 清理全部停止）。
- 入口：`Retry`/`Resume` 经 `ResetRetryableTask` 要求旧 execution 已 done，但发布视觉恢复
  `enqueueArticleVisualRecovery` 不校验 finalization；旧 attempt 卡在 task 之后的
  slot/dispatch/notification 阶段时被新 attempt 取代，永远无法 finalize。
  `finalization_status='done'` 但已非 current 的旧行（cleanup 未完成）也触发同一告警。

### B：`image understanding provider call failed`

- `duration_ms=180000` 等于 `model_routes.image_understanding.timeout`；
  `http_status=0`、`content_type=""`、`endpoint=""`、`provider_request_id=""` 说明 180s 内
  没有任何响应头，且只有一次尝试。
- `CompleteWithImageResult` 构建 vision 请求时没有套用 `thinking: {"type":"disabled"}`；
  文本与视频路径都有。
- `isKimiThinkingModel` 是硬编码白名单，只到 k2.x；线上 `kimi-k3` 落入 default。
- 非流式 JSON 在生成完成前不返回响应头，长时间 thinking 与供应商挂起表现一致。
- 次要：`detail: "low"` 但允许 10MiB 原图 base64；`image_understanding.timeout` 未校验正数
  （0 等于无超时）；失败日志缺少 operation id、图片体积、请求是否写完。

## 修复

### A 执行生命周期

- `model.TaskExecutionFinalizationSuperseded = "superseded"`。
- Repository：`RetireTerminalFinalization`、`CancelSupersededExecution`（CAS，不写 task/计费/发布）。
- `FindReconcilable` 终态条件排除 superseded；`ClaimFinalization` 排除 superseded。
- Service：`RetireSupersededExecution`（事务内锁 task+execution，确认非 current）；新增
  `agent.ErrExecutionNotCurrent` 并映射四个 reconcile 方法的 stale 错误。
- Reconciler：终态分支跳过 superseded、stale 时退役后照常 cleanup；活动分支把主体抽成
  `reconcileActive`，stale 时退役；退役成功记一条 Info `retired superseded runtime execution`。

### B 图像理解

- 提取 `applyThinkingPolicy` 并用于文本/视频/视觉三条路径；`isKimiThinkingModel` 归一化
  后缀并补 `kimi-k3`。
- `imageUnderstandingProviderError` 增加 `TimeoutSource`、`RequestWritten`、`RequestWriteMS`
  （`httptrace.ClientTrace.WroteRequest`）；失败日志增加 `operation_id`、
  `image_data_url_bytes`、`timeout_ms`、`timeout_source`、`request_written`、`request_write_ms`。
- `Validate()` 要求已配置的理解路由 timeout 为正。
- 不自动重试、不放宽 180s；供应商问题走配置处置。

## 验收

- 非 current 终态 attempt 首次对账即标 superseded，之后不再进入 finalization 队列，cleanup 照常。
- 同一 stale 错误不再每 10s 告警；退役只记一条 Info。
- current attempt 的 finalization/重放行为不变（既有测试全绿）。
- `analyze_image` 对 Kimi thinking 模型带 `thinking: disabled`，非 Kimi 不带。
- 视觉超时日志能回答：耗时、route 超时、请求是否写完、图片体积、operation id。
- 理解路由缺 timeout 时启动失败。
- `cd server && go test ./...`、`go build -o /tmp/anban-creator-server .`、`go vet ./...` 全绿。

## 不在范围

- 硬删任务遗留的孤儿 execution。
- finalization 永久失败行的通用退避/隔离（需要新列）。
- 视觉理解自动重试 / fallback 路由（产品级评审）。
- 被取代 attempt 在外部 `CreateDraft` 中途时的既有双写窗口。

## 假设

- 线上理解路由 `tokenplayer_openai` + `kimi-k3` + `timeout: 3m`。
- 该网关对 vision 请求接受 `thinking: {"type":"disabled"}`；若不接受，保留结构改动但移除
  k3 白名单项，转后续路由级配置。
- 若关闭 thinking 后仍满额超时，判定 relay/上游挂起，走配置切换。
- 单副本部署，但退役实现按并发安全设计。
- MySQL `varchar(20)` 无 CHECK 约束，新增枚举值不需要迁移。

## 实施记录

分支：`codex/execution-supersede-vision-timeout`（隔离 worktree
`.worktrees/execution-supersede-vision-timeout`，基于 `b52412a9`）。

提交：

- `c932e0de` fix(execution): add superseded-attempt retirement persistence
- `c12ed869` fix(execution): retire superseded attempts instead of retrying finalization
- `e5063afc` fix(model): disable Kimi thinking for image understanding calls
- `6b6cc6c7` fix(model): diagnose image understanding timeouts and require route timeouts

验证：

- `cd server && go test ./... -count=1` → `EXIT=0`，38 个包 ok，无 FAIL。
- `cd server && go build -o /tmp/anban-creator-server .` → ok。
- `cd server && go vet ./...` → ok。
- 新增测试覆盖：repository 退役 CAS/查询、service 退役与哨兵映射、reconciler 退役终态/活动/已退役、vision thinking 策略、超时诊断、配置超时校验；既有 stale/发布恢复/清理测试保持全绿。

环境说明：linked worktree 的 `harness` 子模块从主检出（同一 pinned commit、工作区干净）
本地克隆并补齐嵌套子模块文件，因为该 worktree 的 `git submodule update --init` 卡在网络；
未复制任何受版本控制的工作区改动。

## 上线检查（需在生产侧完成，不在本分支）

1. 运行主计划 §2.3 的只读 SQL，统计卡死行与队首 50 行占用。
2. 对 `model_routes.image_understanding` 路由做带/不带 `thinking: disabled` 的探测，记录
   status、content-type、耗时；确认网关接受该字段后再发布。
3. 发布后观察：
   - `retired superseded runtime execution` 应在最初数分钟内收敛；
   - `runtime execution reconciliation item failed` 不再出现 `task execution is no longer current`；
   - `image understanding provider call failed` 的 `request_written`、`timeout_source`、
     `image_data_url_bytes`、`operation_id` 字段可用于定位剩余故障。
4. 若关闭 thinking 后仍满额超时且 `request_written=true`，判定为 relay/上游挂起，切换已核验的
   视觉路由；`request_written=false` 则优先排查图片体积与出网链路。
