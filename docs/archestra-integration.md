# Archestra 接入说明

Archestra 以独立的 Helm release 部署在 `anbanai-prod`（本仓库不负责安装）。
本仓库只负责把它接到 Studio 前端入口上；仓库存量里的 Archestra Helm 安装脚本
已移除，不要在本仓库执行 Archestra 部署。

## 访问链路

```text
Browser
  -> https://archestra.anbanai.com
  -> ACK ALB Ingress `creator-studio-web-archestra` (HTTPS 443)
  -> Studio Service `creator-studio-web-svc:8080`
  -> Studio nginx (`server_name ${ARCHESTRA_PUBLIC_HOST}`)
  -> archestra-platform.anbanai-prod.svc.cluster.local:3000
```

- Ingress 定义在 [studio/Deployment.yaml](../studio/Deployment.yaml) 的最后一个
  文档，由 Studio 流水线 `creator.anbanai.com-prod` 随 Deployment 一起 apply。
- Studio nginx 的整站反代在
  [studio/default.conf.template](../studio/default.conf.template) 的
  `server_name ${ARCHESTRA_PUBLIC_HOST}` 块。
- 主机名、端口和公开地址由 `studio/Deployment.yaml` 的 `ARCHESTRA_*` 环境变量
  提供；Upstream 必须能在 Studio Pod 启动时解析。

## 为什么不能放在 `anbanai.com/archestra`

Archestra 前端是 Next.js，`next.config.ts` 没有 `basePath`/`assetPrefix`，
HTML、RSC 导航、静态资源和 API 调用全部是根路径（`/_next/...`、`/api/...`、
`/v1`、`/ws`）。放到子路径后会与 Studio 的 SPA fallback、`/api/`、`/mcp`、
`/ws` 冲突，仅靠 nginx 前缀重写无法可靠修复。因此：

- 正式入口是 `https://archestra.anbanai.com`；
- `anbanai.com/archestra` 只在 Studio nginx 里做 302 跳转（见
  `studio/default.conf.template`）。

## Ingress 关键点

- `ingressClassName: alb`：ACK ALB 的默认 IngressClass；如果集群改过名字，
  同步修改。
- `alb.ingress.kubernetes.io/listen-ports: '[{"HTTPS": 443}]'`：只挂 HTTPS，
  复用 443 监听器上已有的 `*.anbanai.com` 证书。
- Ingress 名字固定为 `${micro_service_name}-archestra`，blue/green 发布不会
  重复占用同一 host。
- 如果现有 Studio Ingress 显式绑定
  `alb.ingress.kubernetes.io/load-balancer-id`，需要把同样的 annotation 补到
  新 Ingress，确保复用同一个 ALB。

## Archestra 公开地址

`ARCHESTRA_FRONTEND_URL`、`ARCHESTRA_API_BASE_URL` 是 Archestra 应用自己的
环境变量，配置在**实际安装 Archestra 的 Helm release** 上，不在本仓库：

- UI 是同源访问，默认（未设置）通常可以直接登录；
- 如果出现登录回调/CORS 问题，在 Archestra 的 Helm values 里设置：

```yaml
archestra:
  env:
    ARCHESTRA_FRONTEND_URL: https://archestra.anbanai.com
    ARCHESTRA_API_BASE_URL: https://archestra.anbanai.com
```

## 发布与验证

1. 发布 Studio 流水线（源码 `main`），等待
   `deployment/creator-studio-web-base` rollout 完成。
2. 验证：

```bash
kubectl -n anbanai-prod get ingress creator-studio-web-archestra
curl -sI https://archestra.anbanai.com/
curl -sI https://anbanai.com/archestra | grep -i location
```

3. 浏览器登录后检查 `/_next/` 静态资源无 404、流式输出和 `/ws` 正常。

## 回退

- 删除/停用 `creator-studio-web-archestra` Ingress 即可停止暴露；
- Studio 回退用 `kubectl -n anbanai-prod rollout undo deployment/creator-studio-web-base`，
  或重新发布上一个镜像。
