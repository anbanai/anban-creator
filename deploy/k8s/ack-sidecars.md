# ACK sidecar deployments

This manifest creates the internal-only sidecar services required by the server:

- `wcflink` at `http://wcflink:18070`
- `seednote` at `http://seednote:18060`

Deploy it into the same namespace as `server/Deployment.yaml`; otherwise the
server must use fully-qualified service DNS names.

## Required pipeline variables

```bash
namespace=anban
imagePullSecret=your-acr-pull-secret
wcflink_image_repo=registry.cn-hangzhou.aliyuncs.com/your-ns/wcflink:tag
seednote_image_repo=xpzouying/xiaohongshu-mcp:tag
wcflink_storage_size=10Gi
seednote_storage_size=10Gi
```

If your ACK cluster has no default `StorageClass`, add `storageClassName` under
both PVC `spec` blocks before deploying.

## Deploy

```bash
envsubst < deploy/k8s/ack-sidecars.yaml | kubectl apply -f -
```

Then deploy the server manifest. The server deployment already injects:

```yaml
ANBAN_SEEDNOTE_BASE_URL: http://seednote:18060
ANBAN_ILINK_ENABLED: "true"
ANBAN_ILINK_BASE_URL: http://wcflink:18070
```

## Verify

```bash
kubectl -n "$namespace" get deploy,svc,pvc seednote wcflink
kubectl -n "$namespace" get endpoints seednote wcflink
```

From the server pod:

```bash
kubectl -n "$namespace" exec -it "$SERVER_POD" -- sh -c 'wget -qO- http://seednote:18060/health'
kubectl -n "$namespace" exec -it "$SERVER_POD" -- sh -c 'wget -qO- http://wcflink:18070/health/live'
```
