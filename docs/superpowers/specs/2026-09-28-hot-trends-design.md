# Anban 热点雷达设计

## 目标

将 Easel 已验证的微博、抖音、知乎、B站、百度、头条热点源接入 Anban Server，形成全站共享的最新快照。Studio、MCP 和后续服务统一通过 `TrendService` 读取，避免各入口自行访问数据库或上游。

## 数据与新鲜度

`trend_snapshots` 按平台保存一行最新成功快照，记录标准化热点数组、成功抓取时间、最近尝试时间、来源和错误。TTL 不写入数据库，由配置中的 `trends.ttl` 与 `fetched_at` 动态计算，默认 10 分钟；上游请求超时默认 8 秒。

读取未过期快照时直接返回。无快照或已过期时按平台刷新；成功更新完整快照，失败保留旧数据并更新尝试时间和错误，响应标记 `stale`。六个平台并行处理，平台内使用 Redis 分布式锁，Redis 不可用时使用进程内按平台锁和 singleflight。

## 上游适配

每个平台优先使用 60s 源，存在时回退到 xxapi。解析根数组、`data.data`、`data.list`，并兼容标题、热度、链接字段别名。持久化完整结果，`limit` 只在响应层截断。

## API 与 MCP

Server 提供认证保护的 `GET /api/v1/trends` 和强制刷新的 `POST /api/v1/trends/refresh`。两者返回统一的平台分组、更新时间、过期时间、来源及 stale 元数据。MCP `list_trends` 是只读原子工具，不接受绕过 TTL 的参数，并调用同一 `TrendService`。

## Studio 与选题池

Studio 增加 `/trends` 热点雷达页，支持平台过滤、刷新、链接、收藏和“做内容”。收藏复用项目选题池，标题 trim 后按项目去重；“做内容”通过路由 state 复用 Dashboard AI Entry。Dashboard 展示微博和抖音各三条摘要，并显示 stale 状态。

## 验证

上游适配使用 fake fetcher/httptest，不访问真实公益 API。服务、HTTP、MCP、选题池和 Studio 路由/交互均使用现有测试体系验证。
