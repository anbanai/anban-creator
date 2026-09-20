# 托管任务产物交付可靠性设计

日期：2026-09-20。状态：已实施并通过本地验证与独立复审；未部署。

## 已确认的故障

任务 `58363dcb-3625-4559-b381-be43a8b828f8` 在本地生成了 `output/image-review.md`。上传准备服务为该文件调用 STS AssumeRole 时遭遇 TCP connection reset，返回 HTTP 503。Runner 没有重试 prepare，跳过该文件并提交其余 15 个文件；Server 最终按缺少必需产物判失败。

另有两条阶段进度 HTTP 400。它们证明进度被拒绝，但不足以确定是阶段 ID 不匹配还是状态顺序错误。当前失败归属规则会把终止错误附到 active 或首个 pending 工作阶段，从而显示“选题研究失败”。

此前本地发现的 Stop Hook 二次结束放行缺口，以及 Pack 必需文件清单与专用校验的差异，均不是本次日志证明的触发原因，不并入此次实施范围。不能以删除 `image-review.md` 必需性来绕过故障。

## 目标和边界

- 托管 OSS 产物上传不再申请或向 Runner 下发 STS 凭据。
- 单次可恢复网络故障不导致必需产物永久遗漏；恢复在本次运行的上传期限内完成，不重跑创作。
- Server 继续决定必需交付、文件状态、任务终态和结算；Runner 仅负责字节传输与有限传输重试。
- 失败时保留已成功上传产物，公开诊断明确指出上传失败；没有阶段证据时不把错误归给选题。
- 不保留托管产物的旧 STS 回退、双响应协议或重复上传实现。
- Studio 素材大文件分片上传仍是 STS 的当前产品用途。本设计不删除这项业务能力。
- 非 OSS 存储继续使用现有 stream 传输；这是当前存储适配能力，不是兼容路径。
- 不新增常驻 Agent、共享工作目录、Server 执行创作步骤或新的通用工作流引擎。

## 方案比较

| 方案 | 成本与问题 | 决策 |
| --- | --- | --- |
| 托管 OSS 产物只用签名 PUT URL | 删除一次无必要 STS 网络请求和 Runner 的 OSS SDK；保留现有不可变存储验收 | 采用 |
| 继续 STS，增加缓存与重试 | 仍需签发临时凭据、管理过期和权限，保留两套实际重叠的上传路径 | 不采用 |
| 所有文件经 Server 中转 | 大图和视频占用 Server 带宽、连接与内存压力；放弃现有对象存储直传优势 | 仅用于现有非 OSS 存储 |

## 上传协议

托管产物 prepare 使用独立的 Go/TypeScript 响应类型，脱离浏览器 `DirectUploadPrepareResult`：

```ts
type ArtifactPrepareResponse =
  | { upload_required: false; key: string }
  | {
      upload_required: true;
      key: string;
      upload_url: string;
      method: "PUT";
      headers: Record<string, string>;
      expires_at: string;
      max_size: number;
    };
```

Server 在授权后查询最终对象，匹配 execution、规范路径、SHA-256、大小后可返回复用结果；否则创建短期 staging 会话并签名 PUT。响应不含 region、bucket、endpoint 或任何 STS 字段。prepare 的参数改用仅包含有效期和时钟的专用配置，不接受 CredentialIssuer。

保留 SHA-256 绑定头、对象大小检查、staging 到不可变最终对象的条件提升以及任务/execution 权限校验。签名 PUT 必须使用返回的完整签名头，不改写 Content-Type，不跟随重定向。只有 Server 验证并提升后的对象可成为业务交付证据。

Runner 删除 `ali-oss`、类型声明和 STS fallback；删除未使用的 `Reporter.putPreparedArtifact`，实际上传入口只保留 `artifacts.ts`。

## 传输重试与幂等

统一请求错误类型记录 operation、HTTP status、稳定 error code、request ID、网络错误码和是否可重试；不保留完整签名 URL或原始响应内容。Server 业务错误通过现有 `ErrorWithCode` 返回。

- 自动重试：连接重置、连接/读超时、临时 DNS 错误，以及 HTTP 408、429、500、502、503、504。
- 不重试：用户取消、execution 过期/越权、普通 400/401/403/404/409、签名/哈希/大小不匹配和其他确定性协议错误。
- 对 manifest 的短期 finalization claim 冲突继续用明确的可重试 503 code；不能把任意 409 当临时失败。
- 每个逻辑请求最多 4 次尝试，指数退避加 full jitter：`random(0, min(2000ms, 250ms * 2^retryIndex))`。有效 Retry-After 作为等待下限；超过剩余期限则直接耗尽，不提前再次请求。
- 保留现有上传阶段默认 120 秒、可配置上限 300 秒；所有文件及重试共用同一个 deadline，不能每次重置。prepare 的单次请求上限 15 秒、manifest 30 秒；PUT/stream 受剩余阶段期限约束。
- 每次请求的 timeout AbortSignal 与用户取消/阶段耗尽分开分类。仅单次超时可重试，用户取消与阶段 deadline 不可重试。
- PUT/stream 每次尝试重新打开并验证专属文件句柄，从 offset 0 创建流；销毁传输流仅关闭该次句柄，哈希快照句柄保持独立。继续校验文件快照，文件变化与网络重试有各自独立且有限的计数。
- PUT 重试复用同一个 staging key，字节完全一致；签名剩余期限不足时在同一预算内重新 prepare。403 仅在 OSS 明确返回签名已过期的机器码时允许一次刷新，其他 403 立即失败。
- prepare 重试可能分配新的短期 staging 会话；丢失响应产生的空会话由现有 TTL 清理。这是安全的有界重复分配，不宣称 prepare exactly-once，不为此新建数据库幂等框架。
- manifest 重试提交冻结后的完全相同集合，复用已有 sealed manifest 内容相等校验；成功响应丢失后再次提交必须成功，内容不同必须拒绝。complete 同样重放完全相同结果。Server 将原始完成输入的 SHA-256 与规范化终态原子保存；终态重试先比较输入摘要并续跑持久 finalizer，不重复校验已变为 retained/delivered 的文件集合。不同输入仍冲突。
- 重试由传输层唯一负责，禁止 Reporter 和 artifacts 两层叠加重试。重构现有不区分错误的一律重试 helper。

成功上传其他文件可以继续，但必须先耗尽当前文件的有限重试。最终成功文件集合冻结后才能提交一次语义上的 manifest；后续仅允许幂等重放，禁止 sealed 后补文件。

## 结果与失败归因

`ArtifactUploadFailure` 改成结构化记录：`path`、`operation`（prepare/put/stream/manifest）、`code`、`http_status`、`attempts`、`retryable`、可选安全 request ID。阶段整体失败使用无 path 的 `artifact_finalization_failure`，保留实际 operation、code、status、attempts、retryable、request_id；不捏造文件路径。删除依赖自由文本 reason 解析的协议设计；UI 文案由 Server 根据受控 code 生成。

Server 的判定顺序：

1. 校验已接收、已封存的实际对象与当前 execution 的交付要求。
2. 对缺失必需文件，如果存在匹配路径的上传失败记录，优先诊断为 `artifact_upload_failed`，阶段 `artifact_upload`，结算归类 `platform_error`。运行时报告仅用于诊断，不能替代实际对象验收。
3. 必需交付完整而仅有非必需文件失败时，按现有交付政策成功，附结构化 warning。
4. 缺必需文件且没有上传失败证据时仍为 `deliverable_validation_failed`，不能伪装网络问题。
5. manifest 无法被确认时不能报告交付成功；保留具体传输诊断。complete 无法被确认时维持现有 durable completion/reconciler 机制，禁止凭 Agent 的成功文本结算。

复用 `ExecutionResult`、`TaskOutcome.Diagnostic`，给公开诊断增加稳定 `code`，支持基础设施诊断而不仅是模型供应商诊断。不能把 OSS/Server 故障标成模型 provider 故障。

Stage progress 是独立观测信号：其失败本身不阻止内容交付。对于 artifact_upload/completion 失败，Server 不把错误覆盖到 Agent active/pending 阶段；Studio 在执行进度区显示独立的“产物上传失败”错误块。已确认完成阶段保留；未得到进度证据的阶段仍如实表示缺少更新，不推断为完成。

恢复能力须如实描述：本次运行中的自动重试复用本地文件；运行结束后只保证 Server 已接收文件可保留、供既有恢复流程使用。容器销毁后不能承诺找回从未上传的本地文件，不新增持久宿主机挂载来掩盖此限制。

## 进度上报

Runner 使用 TaskCreate 已登记的 `taskId → stageId` 映射作为 TaskUpdate 缺失 metadata 时的来源。显式 metadata 与映射冲突时拒绝上报并记录诊断；未绑定的细粒度任务保持不驱动平台进度。

保留有序发送与重试，400 等协议错误只发送一次。Server 区分 `progress_unknown_stage`、`progress_out_of_order`、`progress_invalid_state`，诊断记录 execution、stage、state 和错误码。重复 complete 可以更新描述而不倒退状态、不重复设置完成时间；pending 直接 complete 等非法跃迁仍拒绝。

不根据任务文字标题匹配 stage，不伪造之前的 active/complete 事件，不允许乱序请求隐式完成前置阶段。部署回归时收集真实请求与错误码，验证此次两条 400 的具体类别。

## 日志与观测

本次服务端 warn 包含 STS 请求签名查询串。所有本次触及的上传错误日志统一输出 operation、错误码、HTTP 状态、task/execution、相对路径、attempt、duration 和安全 request ID；去掉 query、Authorization、AccessKey、Signature、SecurityToken、上传凭据和响应正文。测试使用虚构凭据断言其不进入日志、API、产物和公开诊断。

优先使用现有结构化日志设施；不为这次修复引入新的监控后端。业务端记录失败类别，网络诊断保留安全的底层码（如 ECONNRESET），不输出带凭据的 `url.Error.Error()`。

## 发布与验收

Server、Runner 的契约同步切换，不提供旧 STS 回退。构建并发布全部托管 runtime images（article、seednote、montage），使用不可变镜像摘要；协调暂停新任务派发、等待旧执行结束、切换 Server 和镜像、恢复派发。Studio 一起更新诊断展示。回滚使用上一组匹配的 Server/Runner/Studio 构建。

验收必须覆盖 STS 不可达而托管上传成功、单次 reset/503 恢复、永久失败保留部分产物、响应丢失的幂等提交、取消及阶段 deadline、大文件流重放、未授权拒绝、400 精确诊断，以及整个任务成功/失败状态和结算结果。真实基础设施 smoke test 另行执行，单元测试不得声称证明云网络稳定。
