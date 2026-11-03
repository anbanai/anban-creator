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

不需要。已有 OSS 存储可以直接复用同一个 Bucket，建议把媒体对象放在 `assets/users/<user_id>/` 前缀下。Server 的语义查询会固定带这个用户前缀，并再次通过数据库过滤，因此同 Bucket 不会因为语义召回而跨用户返回素材。

同 Bucket 是当前代码支持的默认方式，也是部署改动最小的方式。若已有 Bucket 还存放大量非媒体文件、其他业务数据、需要不同保留周期的数据，或者希望单独控制内容感知费用和权限，建议为媒体库建立独立 Bucket。独立 Bucket 属于部署隔离选择，不是功能必需。

如果复用已有 Bucket，不能直接把扫描范围设为整个 `assets/users/`。当前系统的多个直传用途（媒体库、视频编辑、Montage、Hypit 等）都可能使用这个前缀；Server 的数据库过滤只决定搜索结果，不会阻止 OSS 对对象做内容感知并计费。生产应先为媒体库视频使用专属前缀（例如 `assets/media-library/users/<user_id>/`），或给媒体库视频设置专用 `ObjectTag`，再在 OSS 控制台用该前缀或标签配置过滤规则。这个过滤范围配置在 OSS 控制台完成，不是 Server 的 YAML 配置。

当前仓库的既有代码还没有把媒体库对象切换到专属前缀，也没有在上传后写入内容感知专用标签。因此，在完成这项代码调整并迁移需要检索的历史视频前，不建议在生产打开 `ai_content_awareness`；配置示例默认保持关闭。

当前代码没有把“原始存储 Bucket”和“内容感知 Bucket”拆成两套配置。如果要使用独立 Bucket，必须先把视频上传和 MetaQuery 查询分别配置到两个 Bucket，并相应扩展 Server 的配置、初始化、权限校验和测试；只修改 YAML 中的 Bucket 名称不能实现双 Bucket。

## 索引和查询流程

启用 `storage.ai_content_awareness.enabled` 和 `video_enabled` 后，OSS Bucket 需要在“数据索引-向量检索”中启用视频内容感知，并完成 `AliyunMetaQueryDefaultRole` 授权。

上传后的首次扫描和增量扫描都是异步的。Server 在 OSS 精确查询确认对象已经进入索引前保持素材为 `queued`，因此索引延迟期间素材不会出现在 MCP 搜索结果，也不能用于新的创作任务。

查询由 Server 使用 OSS MetaQuery semantic 模式执行，固定带 `assets/users/<user_id>/` 前缀和 `video` 媒体类型。MCP 与 Agent 只看到经过 Server 用户归属、用途、删除状态和 `available` 状态校验的素材，不会获得 OSS 凭证。

OSS 内容感知的地域支持和接口能力以阿里云文档为准。视频内容感知按处理的视频时长计费，语义查询和索引能力也可能产生对应的 OSS 服务费用；部署前请确认 Bucket 地域、向量检索开通状态和 RAM 授权。
