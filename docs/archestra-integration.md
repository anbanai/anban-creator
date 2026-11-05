# Archestra 独立入口

Archestra 以独立的 Helm release 部署在 `anbanai-prod`，安装、升级和公网
Ingress 均由 Archestra 的部署流程管理。本仓库的 Studio 只服务静态前端，
不代理 Archestra，不配置 `ARCHESTRA_*` 上游，也不发布 Archestra Ingress。

## 访问链路

```text
Browser
  -> https://archestra.anbanai.com
  -> Archestra 独立 Ingress (HTTPS 443)
  -> archestra-platform Service:3000
```

独立 Ingress 应位于 Archestra Service 所在的命名空间，直接路由到
`archestra-platform:3000`。公网 TLS、上传大小、SSE 超时和 WebSocket 支持
均在这个入口配置，与 Studio 的发布和扩缩容解耦。

Archestra 是 Next.js 应用，静态资源、API 和 WebSocket 使用根路径
（`/_next/`、`/api/`、`/v1`、`/ws`），因此使用独立域名。
Studio 的 `/archestra` 不再提供代理或跳转。

## 公开地址

在实际安装 Archestra 的 Helm release 中维护应用公开地址：

```yaml
archestra:
  env:
    ARCHESTRA_FRONTEND_URL: https://archestra.anbanai.com
    ARCHESTRA_API_BASE_URL: https://archestra.anbanai.com
```

## 从 Studio 反代迁移

1. 在 Archestra 部署流程中准备直接指向其 Service 的独立 Ingress，沿用
   `archestra.anbanai.com` 的 HTTPS 证书和域名。
2. 切换原 `creator-studio-web-archestra` Ingress 的路由归属，移除其对
   `creator-studio-web-svc` 的依赖。避免两个 Ingress 同时争用相同 host/path。
3. 确认独立入口可用后，删除旧 Studio 管理的 Ingress。仅从
   `studio/Deployment.yaml` 删除清单不会自动删除集群中的存量资源。
4. 发布静态 Studio 镜像；Studio Deployment 不再需要 `ARCHESTRA_*` 或
   `BACKEND_*` 环境变量。
5. 在独立入口验证登录回调、`/_next/` 资源、流式输出及 `/ws`。Archestra
   的回退由其部署流程完成，不依赖 Studio 的回滚。
