# Archestra ACK 部署

这套目录让云效流水线直接安装和升级 Archestra 官方 Helm Chart。Archestra
默认使用独立的 `archestra` namespace（可用 `ARCHESTRA_NAMESPACE` 覆盖）、
`archestra-platform` release、内置 PostgreSQL 和持久化卷，不复用本项目的
Server/Studio 数据库。本集群当前的 release 安装在 `anbanai-prod`；覆盖
namespace 时，Studio 的 `ARCHESTRA_HOST` 必须使用同一 namespace 的 FQDN；
否则 Studio nginx 会在启动时因解析不到上游而失败。首期应用文件
使用 PostgreSQL 存储，避免 Web 和 Worker 共同挂载 `ReadWriteOnce` 卷造成 ACK
跨节点调度问题。

## 云效配置

在云效流水线中配置以下安全变量：

- `ARCHESTRA_ADMIN_EMAIL`：首次启动创建的管理员邮箱。
- `ARCHESTRA_ADMIN_PASSWORD`：首次启动创建的管理员密码。

流水线执行节点还需要：

- Helm 3+，支持 OCI Chart。
- `kubectl`。
- 已授权 ACK Service Connection 或 kubeconfig。
- 可以访问 Archestra Helm OCI Registry、Docker Hub/Bitnami 镜像仓库。

云效 Shell 步骤：

```bash
ARCHESTRA_NAMESPACE=anbanai-prod bash deploy/archestra/deploy.sh
```

可选覆盖变量：

```bash
ARCHESTRA_VERSION=1.4.12
# 本集群实际 namespace；脚本默认是 archestra，覆盖时必须与 Studio 的
# ARCHESTRA_HOST 保持一致。
ARCHESTRA_NAMESPACE=anbanai-prod
ARCHESTRA_RELEASE=archestra-platform
ARCHESTRA_WAIT_TIMEOUT=15m
KUBE_CONTEXT=your-ack-context
```

首次部署前可以只渲染并检查 Helm 请求，不修改 ACK：

```bash
ARCHESTRA_NAMESPACE=anbanai-prod DRY_RUN=true bash deploy/archestra/deploy.sh
```

脚本默认使用稳定版本 `1.4.12`。生产升级时通过 `ARCHESTRA_VERSION` 固定具体版本，
不要使用 `latest`。

## 首次验证

部署成功后，先从本地建立临时端口转发：

```bash
kubectl -n anbanai-prod port-forward svc/archestra-platform 3000:3000 9000:9000
```

打开 <http://127.0.0.1:3000> 登录。后端就绪检查：

```bash
curl http://127.0.0.1:9000/ready
kubectl -n anbanai-prod get pods,svc
kubectl -n anbanai-prod get jobs
```

首次安装时数据库迁移由 Archestra 启动流程完成；升级时 Helm migration hook
会在 Web/Worker rollout 前运行。成功的 hook 可能按 Helm 策略自动清理。

## 通过 Studio 前端 nginx 暴露 Web

Archestra Web 由 Studio 镜像内的 nginx 按**主机名**反向代理，而不是子路径：

- `studio/default.conf.template` 增加了 `server_name ${ARCHESTRA_PUBLIC_HOST}`
  的 server 块，把整站代理到 `${ARCHESTRA_HOST}:${ARCHESTRA_PORT}`；本集群为
  `archestra-platform.anbanai-prod.svc.cluster.local:3000`，更换 namespace 时
  同步修改 `studio/Deployment.yaml`。
- `studio/Deployment.yaml` 已设置 `ARCHESTRA_PUBLIC_HOST=archestra.anbanai.com`。
- 访问地址：<https://archestra.anbanai.com>；`anbanai.com/archestra` 只做
  302 入口跳转。`*.anbanai.com` 证书已覆盖该主机名。

**为什么不能直接放在 `anbanai.com/archestra` 下**：Archestra 前端是 Next.js，
`next.config.ts` 没有 `basePath`/`assetPrefix`，HTML、RSC 导航、静态资源和 API
调用全部使用根路径（`/_next/...`、`/api/...`、`/v1`、`/ws`）。放到子路径后会与
Studio 的 SPA fallback、`/api/`、`/mcp`、`/ws` 冲突，仅靠 nginx 前缀重写无法
可靠修复。

**还需要的集群侧配置**：

1. ACK 负载均衡/Ingress 增加主机规则：`archestra.anbanai.com` → Studio
   Service（`${micro_service_name}-svc`，当前为 `creator-studio-web-svc`）的
   `8080` 端口。也可以直接指向同 namespace 的 `archestra-platform:3000`，
   此时流量不经过 Studio nginx。DNS 的 `*.anbanai.com` 已解析到同一 SLB，
   证书也已覆盖，无需额外申请。
2. 设置 Archestra 的公开地址并重新执行部署脚本：

   ```bash
   ARCHESTRA_NAMESPACE=anbanai-prod \
   ARCHESTRA_FRONTEND_URL=https://archestra.anbanai.com \
   ARCHESTRA_API_BASE_URL=https://archestra.anbanai.com \
   bash deploy/archestra/deploy.sh
   ```

   必须带上实际 namespace，否则脚本会按默认值在 `archestra` 里新建一套
   release，和当前 Service 不是同一套。

## 重要说明

管理员邮箱和密码从云效安全变量写入 `archestra-bootstrap` Secret。密码只在
Archestra 首次创建用户时生效；之后修改该 Secret 不会重置已有账号密码。

Archestra 自身仍未创建公网 Ingress，Service 保持 `ClusterIP`；对外入口复用
Studio 的 nginx，见上一节。如果以后改走独立 Ingress，再在 values 覆盖中开启
`archestra.ingress` 并配置 `ARCHESTRA_FRONTEND_URL`、`ARCHESTRA_API_BASE_URL`。

正式环境建议切换到外部 PostgreSQL，并单独
备份数据库、认证 Secret 和加密密钥。若后续切换到文件系统存储，必须先准备
ACK 的 `ReadWriteMany` StorageClass，再配置 `archestra.fileStorage.provider=filesystem`。

后续接入本项目时，保持 Archestra 的数据库和 release 独立（当前与 Studio 同
namespace，但资源不复用），再通过 Ingress、MCP、LLM Proxy 或本项目 Server 的
配置逐步建立连接，避免发布互相影响。

## 升级与回滚

升级使用同一 release 重复执行部署脚本：

```bash
ARCHESTRA_VERSION=1.4.12 bash deploy/archestra/deploy.sh
```

查看历史并回滚：

```bash
helm -n anbanai-prod history archestra-platform
helm rollback archestra-platform <REVISION> -n anbanai-prod --wait
```
