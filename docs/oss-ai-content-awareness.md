# OSS 视频素材内容感知

## OSS 和内容感知的关系

两者不是前后替换的两套存储：

```text
Server 上传视频
        |
        v
OSS Bucket 保存原始视频对象
        |
        v
OSS AI 内容感知异步扫描同一个对象
        |
        v
生成摘要、详细描述和向量索引
        |
        v
Server 通过 MetaQuery 查询，再校验数据库归属和可用状态
```

也就是说，必须先把视频对象成功写入 OSS，OSS 才能对它做内容感知。内容感知不会复制一份视频，也不会替代 Server 的素材登记和权限状态。Server 仍然保存素材行、用户归属、项目归属、`queued`/`available` 状态和索引确认时间。

当前实现复用 `storage.endpoint`、`storage.bucket_name`、`storage.region` 和 OSS 凭证。内容感知查询的 Bucket 就是原始视频存储使用的 Bucket。

## 是否需要单独存储空间

当前代码让原始素材和内容感知查询复用同一个 Bucket。媒体库使用 `storage.media_library_prefix` 指定的独立目录，默认 `assets/media-library/`。其他用途的直传对象仍在 `assets/users/` 下；不要把 OSS 内容感知过滤范围配置为整个 Bucket 或通用上传目录。

## YAML 配置和 OSS 控制台

`server/config.example.yaml` 包含实际配置项：

```yaml
storage:
  media_library_prefix: "assets/media-library/"
  ai_content_awareness:
    enabled: false
    video_enabled: false
    query_timeout: 10s
    max_results: 20
```

该配置项控制新素材的最终对象键和 Server 语义查询范围。对象路径为 `<media_library_prefix>users/<user_id>/<upload_id>/<filename>`，例如 `assets/media-library/users/user-1/upload-1/clip.mp4`。省略配置时使用默认值；末尾的 `/` 会自动补齐。不允许绝对路径、空目录段、`.`、`..`、URL/通配符字符，或与 `uploads/`、`assets/users/` 重叠的目录。

**YAML 不会自动开通 OSS 内容感知，也不会修改 OSS 过滤规则。** 切换 Bucket 时，先确认新 Bucket 的地域与授权，再在新 Bucket 的“数据索引-向量检索”中把文件前缀过滤值设为 YAML 中的 `media_library_prefix`，只启用视频内容感知，完成 `AliyunMetaQueryDefaultRole` 授权，最后将上述两个开关设为 `true` 并重启 Server。切换 Bucket 本身不需要更改前缀。启动日志会打印 Bucket、前缀和 Server 查询开关，便于核对；它不代表已探测或验证云端配置。

不要通过扩大 OSS 扫描范围来掩盖路径不一致。Server 的数据库归属过滤只决定搜索结果，不会阻止 OSS 扫描其他对象并计费。

## 已有对象与部署

已有数据库必须在部署前执行 `server/migrations/20261103_upload_session_media_prefix.sql`；未迁移时 Server 启动检查会提示具体迁移文件并停止。全新数据库由 `model.AutoMigrate` 建表。新上传会话在准备时保存前缀，后续完成上传、重试、URL 校验和清理恢复都使用会话保存的值。已有无前缀字段的会话继续使用原来的 `assets/media-library/`，不会因新配置改写目标。

修改前缀不会自动搬迁任何对象；语义搜索只查询当前配置的前缀。若历史素材位于 `assets/users/` 或另一旧前缀，必须先规划对象及相关素材/上传会话记录的一致性迁移，再切换前缀。仅修改 `assets.storage_key` 不足以保持上传会话校验一致。仍在上传的旧会话会完成到原前缀，也应纳入迁移计划。

如要把媒体库和其他业务拆成两个 Bucket，需要额外实现存储路由、配置与权限校验；当前仅有一套存储配置，修改 `bucket_name` 会切换整体存储，不会自动迁移旧数据。

## 索引和查询流程

启用 `storage.ai_content_awareness.enabled` 和 `video_enabled` 后，OSS Bucket 需要在“数据索引-向量检索”中启用视频内容感知，并完成 `AliyunMetaQueryDefaultRole` 授权。

上传后的首次扫描和增量扫描都是异步的。Server 在 OSS 精确查询确认对象已经进入索引前保持素材为 `queued`，因此索引延迟期间素材不会出现在 MCP 搜索结果，也不能用于新的创作任务。

查询由 Server 使用 OSS MetaQuery semantic 模式执行，固定带 `<media_library_prefix>users/<user_id>/` 前缀和 `video` 媒体类型。MCP 与 Agent 只看到经过 Server 用户归属、用途、删除状态和 `available` 状态校验的素材，不会获得 OSS 凭证。

OSS 内容感知的地域支持和接口能力以阿里云文档为准。视频内容感知按处理的视频时长计费，语义查询和索引能力也可能产生对应的 OSS 服务费用；部署前请确认 Bucket 地域、向量检索开通状态和 RAM 授权。
