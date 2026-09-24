# Studio 更新发布

Studio 的 HTML 与带 hash 的 JS/CSS 构成一个版本。旧标签页会继续请求旧 chunk；仅切换 Pod、缩短缓存或自动刷新不能保证这些文件仍可访问。

## 当前实现

- Docker 镜像从 `STUDIO_PREVIOUS_IMAGE` 继承 `/assets/`，然后叠加新构建。旧 JS、CSS、字体原样保留；旧 HTML 不覆盖新 HTML。
- `index.html` 和 SPA 回退响应使用 `Cache-Control: no-store, must-revalidate`。hash 资源仍缓存一年，缺失资源返回 404。
- Deployment 与 Service 都按 `app + version` 匹配。`version_switch` 必须是每次发布唯一的 ID（例如提交 SHA 加构建编号），不能循环使用 blue/green 两个名字。
- `studio/deploy.py` 先创建候选 Deployment/HPA，等待 nginx 的 HTML readiness，通过旧资源逐文件 SHA-256 校验后才切换 Service。Service 使用 resourceVersion 比较后替换，防止并发发布互相覆盖。
- 路由加载失败时，浏览器用禁用缓存的请求读取当前 HTML。只有入口 JS 已变化、在线且能够持久化防循环标记时才自动刷新。对同一个目标版本只尝试一次，不同目标间至少间隔五分钟。成功加载其他页面不会重置标记；恢复失败时可手动刷新。

## 更新流水线

以下步骤替换现有“构建镜像 → 直接 apply 整个 Deployment.yaml”的更新步骤。需要 Docker、Python 3.9+、kubectl，以及现有镜像仓库权限。首次安装可以直接 apply 模板；已有站点更新必须经过发布脚本。

首次迁移时，如果活动 Service 仍只选择 `app`，先确认它的就绪 Pod 全部属于同一个当前版本，将 Service selector 补上这个**当前版本**的 `version` 标签，再创建任何候选实例。不要把候选版本填进去。可以在云控制台编辑 Service selector，或使用 `kubectl edit service <服务名> -n <命名空间>`。脚本会拒绝未完成此迁移的安装；现有 Deployment 的 selector 无需原地修改，后续使用全新名称创建，避免 Kubernetes immutable selector 错误。

1. 从当前 Service 的就绪 Pod 解析实际运行的镜像 digest：

   ```bash
   export STUDIO_PREVIOUS_IMAGE="$(python3 studio/deploy.py active-image \
     --namespace "$namespace" --service "${micro_service_name}-svc")"
   ```

2. 使用该 digest 构建新镜像。`STUDIO_IMAGE_TAG` 使用唯一 tag，不能覆盖已有发布：

   ```bash
   docker build -f deploy/docker/Dockerfile.studio \
     --build-arg STUDIO_PREVIOUS_IMAGE="$STUDIO_PREVIOUS_IMAGE" \
     -t "$STUDIO_IMAGE_TAG" .
   docker push "$STUDIO_IMAGE_TAG"
   export image_repo="$(docker image inspect "$STUDIO_IMAGE_TAG" --format '{{index .RepoDigests 0}}')"
   ```

3. 设置全新的 `version_switch`，沿用流水线中的其他模板变量，渲染并执行门禁切流：

   ```bash
   envsubst < studio/Deployment.yaml > /tmp/studio-rendered.yaml
   python3 studio/deploy.py release /tmp/studio-rendered.yaml
   ```

不要在这之前或之后再次直接 apply 整个模板，否则会绕开门禁。脚本要求候选 Deployment 名称尚不存在、镜像按 digest 固定、单个 nginx 容器配置 readiness；现有安装使用的可变 tag 会通过 Pod imageID 解析为 digest。若当前 Service 同时匹配多个不同镜像，脚本拒绝发布，需先明确恢复到单个活动版本。

更新失败时保留旧 Service，候选资源留供排查；再次发布使用新的 ID。Kubernetes RBAC 需要读取 Service/Pods/Deployment、创建或更新 Deployment/HPA、pods/exec（读取和验证静态文件）、替换 Service。发布不会删除旧 Deployment；运维在确认切流和回滚窗口结束后再清理旧实例/HPA。

## 回滚与保留

为保护已经打开新版的标签页，回滚也作为一次新发布：检出目标旧源码，仍以**当前活动镜像**为 `STUDIO_PREVIOUS_IMAGE` 重建，使用新的 release ID 走同一门禁。不要直接切回缺少新版本资源的旧镜像。

当前方案在镜像中累计保留所有历史 hash 资源，避免猜测浏览器标签页寿命。需监控镜像大小；未来若改为 OSS/CDN 共享资源库，可制定明确的会话保留期并定期回收。在完成迁移前不要重置继承链或删除历史资源。默认 `STUDIO_PREVIOUS_IMAGE=nginx:alpine` 只适用于首次安装和本地构建，更新脚本会拒绝用它更新已有站点。

CDN/Ingress 必须尊重 HTML 的 no-store，不缓存 404，并继续提供继承的 `/assets/`。首次启用本方案时，需要清除此前缓存的 HTML 和负缓存；不需要清除正常的 hash 资源缓存。后端 API 仍须兼容保留期内旧版前端。

Service selector 更新在 API 层采用并发保护，但 EndpointSlice、Ingress 和已有连接不会同时切换。极短的传播窗口内，新 HTML 仍可能向旧实例请求新入口 JS；旧实例没有新资源，React 此时也尚未运行。HTML 内置独立的加载失败提示与刷新按钮，避免空白页。完全消除此窗口需先将新旧资源合集发布到所有来源，或切换为共享 OSS/CDN 资源库；当前方案不承诺所有发布窗口绝无 404。

本地代码合并不会自动改动云效流水线或线上集群。按以上步骤接入后，在预发布保持旧标签页打开，更新并访问之前未打开过的页面，验证无资源 404、无强制刷新和输入丢失，再切生产。

## 验证

```bash
python3 -m unittest discover -s studio -p '*_test.py'
cd studio && bun run test && bun run build
```
