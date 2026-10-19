# ACK iLink sidecar deployment with Yunxiao

The iLink sidecar has an independent build and deployment path:

- `deploy/k8s/ack-sidecar-ilink.yaml`

The manifest uses `imagePullPolicy: Always`; all image references are supplied
by ACK pipeline variables. Build for `linux/amd64` because the manifest selects
amd64 nodes.

## Build

```bash
make docker-sidecar-ilink-image \
  SIDECAR_ILINK_IMAGE=chengdu.personal.cr.aliyuncs.com/bx_anbanai/sidecar-ilink:latest
```

The build accepts `SIDECAR_ILINK_REPO` and `SIDECAR_ILINK_REF` overrides for a
different upstream revision or mirror.

## Pipeline variables

| Variable | Example |
| --- | --- |
| `namespace` | `anbanai-prod` |
| `imagePullSecret` | `anban-acr-pull` |
| `sidecar_ilink_image_repo` | `chengdu.personal.cr.aliyuncs.com/bx_anbanai/sidecar-ilink:latest` |
| `sidecar_ilink_storage_size` | `10Gi` |

## Deploy

```bash
set -euo pipefail

export namespace="${namespace:?set the ACK namespace}"
export imagePullSecret="${imagePullSecret:?set the existing ACR pull secret name}"
export sidecar_ilink_image_repo="${sidecar_ilink_image_repo:?set the iLink image reference}"
export sidecar_ilink_storage_size="${sidecar_ilink_storage_size:?set the iLink PVC size}"

kubectl create namespace "$namespace" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "$namespace" get secret "$imagePullSecret" >/dev/null
envsubst '${namespace} ${imagePullSecret} ${sidecar_ilink_image_repo} ${sidecar_ilink_storage_size}' \
  < deploy/k8s/ack-sidecar-ilink.yaml | kubectl apply -f -
kubectl -n "$namespace" rollout restart deployment/sidecar-ilink
kubectl -n "$namespace" rollout status deployment/sidecar-ilink --timeout=10m
kubectl -n "$namespace" get deployment,pod,service,pvc -l app=sidecar-ilink
```

The Server reaches iLink at `http://sidecar-ilink:18070` and should be
configured with `ANBAN_ILINK_ENABLED=true` and
`ANBAN_ILINK_BASE_URL=http://sidecar-ilink:18070`.
