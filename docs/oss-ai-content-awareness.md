# OSS 视频素材内容感知

启用 `storage.ai_content_awareness.enabled` 和 `video_enabled` 后，视频素材继续由 Server 登记并存储到阿里云 OSS。OSS Bucket 需要在“数据索引-向量检索”中启用视频内容感知，并完成 `AliyunMetaQueryDefaultRole` 授权。

上传后的首次扫描和增量扫描都是异步的。Server 在 OSS 精确查询确认对象已经进入索引前保持素材为 `queued`，因此索引延迟期间素材不会出现在 MCP 搜索结果，也不能用于新的创作任务。

查询由 Server 使用 OSS MetaQuery semantic 模式执行，固定带 `assets/users/<user_id>/` 前缀和 `video` 媒体类型。MCP 与 Agent 只看到经过 Server 用户归属、用途、删除状态和 `available` 状态校验的素材，不会获得 OSS 凭证。

OSS 内容感知的地域支持和接口能力以阿里云文档为准。视频内容感知按处理的视频时长计费，语义查询和索引能力也可能产生对应的 OSS 服务费用；部署前请确认 Bucket 地域、向量检索开通状态和 RAM 授权。
