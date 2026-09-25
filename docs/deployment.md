# Studio 更新发布

当前先恢复为一个固定的 Studio Deployment（例如 `creator-studio-web-base`），沿用原有 ACS 更新流程。`version_switch` 固定为 `base`；它用于资源名、Pod 标签和日志，不影响 Nginx。Deployment selector 只匹配稳定的 `app` 标签，不能加 `version`，否则会尝试修改已有 Deployment 的不可变 selector。

## 当前更新流程

构建新镜像时继续传入当前线上镜像，让新镜像保留旧标签页可能请求的 hash JS/CSS：

```bash
docker build -f deploy/docker/Dockerfile.studio \
  --build-arg STUDIO_PREVIOUS_IMAGE="$CURRENT_STUDIO_IMAGE" \
  -t "$NEW_STUDIO_IMAGE" .
docker push "$NEW_STUDIO_IMAGE"
```

然后保持 `version_switch=base`，渲染并 apply 模板。Deployment 和 Service 的名字保持不变：

```bash
export version_switch=base
envsubst < studio/Deployment.yaml > /tmp/studio-rendered.yaml
kubectl apply -f /tmp/studio-rendered.yaml
kubectl -n "$namespace" rollout status deployment/creator-studio-web-base --timeout=10m
```

## 已知边界

- `/index.html` 不缓存；带 hash 的 `/assets/` 资源缓存一年。新镜像继承旧 assets 可支持升级后仍打开的旧页面。
- 滚动更新期间旧、新 Pod 会同时接收请求。旧 Pod 尚无新版本 JS 时，个别新页面请求可能短暂得到 404；前端的加载恢复逻辑可尝试恢复，但这套临时方案不能保证切换期间绝无失败。
- 先不使用 `studio/deploy.py` 的逐版本发布流程；它要求每次用新的 `version_switch` 和 Deployment 名称，与当前固定 `base` 更新方式不同。

## 后续改进

要让新旧 Pod 始终提供同一份 hash 资源，可把 `/assets/` 发布到共享 OSS/CDN：先上传新资源，再发布引用它们的 HTML，并按保留期清理旧资源。接入前需要确定 Studio 专用的 OSS 桶或 CDN 域名及流水线发布权限。
