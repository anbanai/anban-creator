# 交付文章分享链接

用户已确认：公众号文章、种草笔记的分享链接无需登录即可阅读完整图文；始终读取最新成功交付版本。新执行进行中或失败时保留上一版，成功交付后整体切换。每次打开或刷新取最新内容，不做实时推送。

交付结果旁增加分享入口。每篇作品一个当前链接，可复制、修改期限、关闭。有效期为 1 天、7 天、自定义 1–365 个整数天，默认 7 天。创建或修改从服务端保存时刻按 24 小时/天计算，显示准确到期时间；有效链接修改期限不换地址，过期或关闭后再次分享更换令牌，旧令牌永久无效。

Server 管理身份、分享记录、到期判断、交付筛选和图片访问。只分享支持的内容生成任务的完整 delivered 文件集，不共享 pending、retained、内部文件、提示词、任务元数据或存储直链。沿用冻结交付契约和现有原子交付切换。任务删除或进入 deleting 状态后分享不可读。

Studio 新增公开路由 `/share/:token`，不受登录和工作台布局限制。公众号安全展示交付 HTML；种草笔记展示完整 Markdown 及封面、按数字序号排列的正文图、尾图，正文中已出现的图片不重复展示。手机和桌面均可阅读；有加载、重试、失效和内容不可用状态。无需密码、下载、访问统计或其他交付类型。

正文图片必须映射到本分享的图片接口。允许映射任务文件的相对路径和已知平台/存储 URL，未知图片不请求外网；不代理任意 URL。HTML 使用隔离的无脚本渲染、CSP 和安全清理；Markdown 不执行原始 HTML。分享正文、图片、管理接口禁止缓存；公开页面禁止索引并禁止传递来源；应用和 Nginx 访问日志隐藏分享令牌。已交付文件或配图不可用时不冒充完整内容。

数据库新增任务分享表，唯一任务键、唯一高熵随机令牌、所有者、到期时间、关闭时间和创建/更新时间。无需定时失效任务、回填或新基础设施。分享不更改 Agent、MCP、Harness 或发布流程。

## HTTP contract

接口使用现有 JSON envelope。管理接口通过现有认证并在服务层验证所有权。

- `GET /api/v1/tasks/:id/share` -> `{status: "none" | "active" | "expired" | "revoked", share_path?: string, expires_at?: string}`。路径为 `/share/<token>`，供 Studio 使用当前 origin 生成可复制地址。
- `PUT /api/v1/tasks/:id/share` body `{expires_in_days: number}` -> 同上，状态为 active。1–365 整数，缺失或非法值返回 400。原子创建/修改，避免并发生成多个有效链接。
- `DELETE /api/v1/tasks/:id/share` -> 204；幂等关闭。
- `GET /api/v1/public/shares/:token` -> `{type: "wechat-article" | "seednote", title: string, format: "html" | "markdown", body: string, images: [{id: string, url: string, alt: string, role: string}], expires_at: string}`。仅返回阅读数据。
- `GET /api/v1/public/shares/:token/images/:fileId` -> 对应本次 delivered 图像的字节流，校验 MIME、分享状态、文件归属和交付资格，不重定向存储。
- 未知/已轮换令牌或删除任务：404；到期或关闭：410；交付不可用：409（业务 error_code），基础设施故障：500。管理越权以 404 隐藏资源。

最新文件集使用同一执行的正文和图片。版本切换过程中旧图片 URL 不指向新版本文件；若旧文件不再可读，页面提示重新加载，不混合图文。

## 验收

未登录访问有效链接可直接阅读两种图文。测试覆盖创建、重复改期、关闭、过期边界、轮换、权限隔离、删除、交付切换与失败续作、图片归属与格式、安全渲染、日志脱敏、本地和 OSS 存储读取。Server 全量测试和构建、Studio 全量测试和构建通过；未登录浏览器进行实际页面冒烟。部署顺序：数据库迁移、Server、Studio。
