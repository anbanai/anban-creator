# Archestra ACK 部署

这套目录让云效流水线直接安装和升级 Archestra 官方 Helm Chart。Archestra
使用独立的 `archestra` namespace、`archestra-platform` release、内置
PostgreSQL 和持久化卷，不复用本项目的 Server/Studio 数据库。首期应用文件
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
bash deploy/archestra/deploy.sh
```

可选覆盖变量：

```bash
ARCHESTRA_VERSION=1.4.12
ARCHESTRA_NAMESPACE=archestra
ARCHESTRA_RELEASE=archestra-platform
ARCHESTRA_WAIT_TIMEOUT=15m
KUBE_CONTEXT=your-ack-context
```

首次部署前可以只渲染并检查 Helm 请求，不修改 ACK：

```bash
DRY_RUN=true bash deploy/archestra/deploy.sh
```

脚本默认使用稳定版本 `1.4.12`。生产升级时通过 `ARCHESTRA_VERSION` 固定具体版本，
不要使用 `latest`。

## 首次验证

部署成功后，先从本地建立临时端口转发：

```bash
kubectl -n archestra port-forward svc/archestra-platform 3000:3000 9000:9000
```

打开 <http://127.0.0.1:3000> 登录。后端就绪检查：

```bash
curl http://127.0.0.1:9000/ready
kubectl -n archestra get pods,svc
kubectl -n archestra get jobs
```

首次安装时数据库迁移由 Archestra 启动流程完成；升级时 Helm migration hook
会在 Web/Worker rollout 前运行。成功的 hook 可能按 Helm 策略自动清理。

## 重要说明

管理员邮箱和密码从云效安全变量写入 `archestra-bootstrap` Secret。密码只在
Archestra 首次创建用户时生效；之后修改该 Secret 不会重置已有账号密码。

首期未创建公网 Ingress，Service 类型为 `ClusterIP`。接入域名时，在 values
覆盖中设置 `ARCHESTRA_FRONTEND_URL`、`ARCHESTRA_API_BASE_URL`，例如：

```bash
ARCHESTRA_FRONTEND_URL=https://archestra.example.com \
ARCHESTRA_API_BASE_URL=https://archestra.example.com \
bash deploy/archestra/deploy.sh
```

再按 ACK Ingress 和 TLS 方案开启 `archestra.ingress`。正式环境建议切换到外部 PostgreSQL，并单独
备份数据库、认证 Secret 和加密密钥。若后续切换到文件系统存储，必须先准备
ACK 的 `ReadWriteMany` StorageClass，再配置 `archestra.fileStorage.provider=filesystem`。

后续接入本项目时，保持 Archestra 的 namespace、数据库和 release 独立，再通过
Ingress、MCP、LLM Proxy 或本项目 Server 的配置逐步建立连接，避免首期发布互相影响。

## 升级与回滚

升级使用同一 release 重复执行部署脚本：

```bash
ARCHESTRA_VERSION=1.4.12 bash deploy/archestra/deploy.sh
```

查看历史并回滚：

```bash
helm -n archestra history archestra-platform
helm rollback archestra-platform <REVISION> -n archestra --wait
```
