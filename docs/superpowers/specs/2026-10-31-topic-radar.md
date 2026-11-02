# 选题雷达设计规范

日期：2026-10-31
状态：Approved for implementation

## 目标

为一个 Project/persona 按配置时间采集热点、搜索历史、历史选题、Profile 和历史表现，运行托管 Agent 生成带证据和简介的短视频选题；结果持久化到 Studio，并自动进入 TopicPool，同时按用户配置的 webhook 渠道推送。

## 边界

Server 拥有身份、Project/用户授权、输入快照、幂等、调度、状态机、TopicPool、脚本任务、通知和重试。Agent 只读取冻结上下文并写 Pack 合同内的文件 artifact，不直接写权威状态或调用 webhook。Studio 只消费 Server read model。MVP 通知只支持飞书、企业微信、钉钉 webhook；不包含计费、SKU、每日/用户/Project 上限。

## 身份与运行

- Agent identity 为 `topic-radar`，channel 为 `topic-radar`。
- Digest task kind/type 为 `topic_digest` / `topic-digest`；短视频脚本为 `topic_script` / `topic-script`。
- Scheduled occurrence key 为 `sched:<subscription_id>:<planned RFC3339 UTC time>`；manual run 使用 UUID 或客户端 idempotency key。数据库唯一性是权威，Asynq 唯一性只做 transport dedupe。
- 同一 subscription 仅允许一个 `queued`/`running` digest；显式 retry 创建新 run，不复用成功记录。
- Digest 状态：`queued -> preparing -> dispatched/running -> finalizing -> succeeded|failed`；另有 dispatch status `pending|enqueued|claimed|failed`。

## 数据模型

`TopicSubscription` 保存 User/Project、启用状态、timezone、schedule、输入来源配置、运行时 profile、权重和 script policy。

`TopicDigest` 保存 occurrence/request key、planned/run timestamps、冻结输入 snapshot/hash、Agent task/execution 关联、状态、error 和统计。

`TopicSuggestion` 保存标题、简介、来源摘要/refs、七维评分、视频适配评估、排序、证据状态，并关联 digest。

`TopicSuggestionAdoption` 是 durable adoption outbox：`pending|processing|adopted|duplicate|failed`、lease、attempt、TopicPoolID/error；worker 使用 CAS/lease，重复标题为 terminal duplicate，并链接已存在 TopicPool 行。TopicPool 增加 normalized `DedupeKey`、SourceDigestID、SourceSuggestionID、SourceLabel；Project+dedupe key 由数据库唯一约束保护。

`TopicScriptJob` 是独立一等实体：Suggestion/Digest/Project/User、target platform、duration、aspect ratio、request fingerprint、status/attempt/error、Task/Execution/artifact refs/hash/timestamps。脚本失败不改变 digest 成功状态。

Notification channel 保存 provider/config secret（只在服务端），delivery 保存 digest/channel/payload version、不可变渲染 payload snapshot、claim/lease/attempt/status/error。Studio 永不返回 webhook secret。

## Agent artifact

Digest 必须写 `output/topic-digest.json`，可写 `output/topic-evidence.json`；脚本必须写 `output/topic-script.json`（可带 markdown/subtitles）。JSON 有稳定 schema_version、来源/数据时间/缺失字段和质量状态。来源 ref 类型为 type/label/platform/ref_id/title/url/rank/hot/fetched_at/freshness。Server 校验 refs 来自冻结 snapshot；无可解析来源的候选被拒绝，外部 ref 可展示但不计跨平台 bonus。

默认评分权重：traffic 25%、account fit 20%、differentiation 15%、timeliness 10%、monetization 15%、production cost 8%、compliance risk 7%。

## API 与 Studio

认证 `/api/v1` 下提供 subscription CRUD/启停、manual run/retry、digest timeline/detail、adoption/script read models、script request/retry、notification channel CRUD/test。项目授权由 service 校验，handler 保持薄。Studio `/topic-radar` 显示运行状态、时间线、来源证据、简介、评分、自动入库状态和脚本 drawer；不显示选题勾选/采纳按钮。TopicPoolDialog 显示来源 provenance。Dashboard 提供“今日选题”摘要入口，Settings 管理 webhook。

## 失败与恢复

Digest artifact 校验失败使 digest failed；adoption、script、notification 各自独立可重试并可由 reconcile worker 恢复。Provider 配置错误为 terminal invalid；网络/429/5xx 为有限退避重试。所有外部副作用在 durable row 提交后发生，不在数据库事务内 enqueue/runtime/webhook。

## 验收

必须覆盖 occurrence 幂等、并发 guard、UTC 归一、source 校验、TopicPool race-safe dedupe、adoption duplicate/retry、脚本 fingerprint/独立失败、通知 payload/重试和 webhook byte limit；运行 `cd server && go test ./...`, `go build -o /tmp/anban-creator-server .`, `cd studio && bun run test`, `bun run build`，并运行相关 MCP 合同测试。
