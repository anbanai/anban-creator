# ACK Seednote deployment with Yunxiao

Deploy `deploy/k8s/ack-seednote.yaml` from an Alibaba Yunxiao Flow pipeline. This is an internal-only service: the in-cluster application endpoint is `http://seednote:18060` and its MCP endpoint is `http://seednote:18060/mcp`. Do not create an Ingress, LoadBalancer, or NodePort for this workload.

The upstream application has no application-level authentication. `ClusterIP` prevents direct external exposure, but other workloads in the cluster can still connect. Use a trusted cluster network and namespace, or apply a namespace-appropriate NetworkPolicy that permits only intended callers. Do not add a generic NetworkPolicy manifest here because the allowed caller labels are deployment-specific. Require administrator-only port-forwarding.

The deployment pulls the public Docker Hub image `xpzouying/xiaohongshu-mcp:latest` directly. The pipeline has no image-build stage.

## Prerequisites

- The ACK cluster has at least one `amd64` node.
- A default StorageClass is available. If the cluster has no default StorageClass, set `spec.storageClassName` in `deploy/k8s/ack-seednote.yaml` before creating the pipeline.
- Cluster egress can pull images from Docker Hub.
- The Yunxiao ACK/Kubernetes service connection provides a working `kubectl` context for the target cluster.
- The pipeline checks out this repository, and its runner has Bash, `kubectl`, and `envsubst` installed.

## Pipeline variables

Create these Yunxiao pipeline variables:

| Variable | Example |
| --- | --- |
| `namespace` | `anbanai-prod` |
| `seednote_storage_size` | `10Gi` |

## Deployment task

Add one command task after repository checkout and configure the Yunxiao task shell as Bash. Use this exact script:

```bash
set -euo pipefail
export namespace="${namespace:?set the ACK namespace in Yunxiao}"
export seednote_storage_size="${seednote_storage_size:-10Gi}"
if [[ ! "$seednote_storage_size" =~ ^[1-9][0-9]*(Mi|Gi|Ti)$ ]]; then
  echo "invalid seednote_storage_size: $seednote_storage_size" >&2
  exit 1
fi
kubectl create namespace "$namespace" --dry-run=client -o yaml | kubectl apply -f -
envsubst '${namespace} ${seednote_storage_size}' < deploy/k8s/ack-seednote.yaml | kubectl apply -f -
kubectl -n "$namespace" rollout restart deployment/seednote
kubectl -n "$namespace" rollout status deployment/seednote --timeout=10m
kubectl -n "$namespace" get deployment,pod,service,pvc -l app=seednote
kubectl -n "$namespace" get endpoints seednote
```

The Bash validation permits only a positive integer followed by `Mi`, `Gi`, or `Ti`, making multiline/YAML injection through `seednote_storage_size` impossible.

The rollout restart is intentional: `latest` is mutable, and applying unchanged YAML alone does not change the pod template or replace existing pods.

The manifest deliberately runs one replica with a `Recreate` strategy because one Seednote account and browser state must not be used concurrently.

## First login

Run these commands from a machine with the same ACK `kubectl` context. Set `namespace` to the same value used for the Yunxiao pipeline variable; this assignment also applies to the operations commands below.

```sh
export namespace=anbanai-prod
```

Keep the port-forward running in its own terminal:

```sh
kubectl -n "$namespace" port-forward service/seednote 18060:18060
```

Verify health through the local forward; do not assume diagnostic tools are installed inside the application container:

```sh
curl --fail http://127.0.0.1:18060/health
```

In another terminal, launch the MCP Inspector and connect it to `http://127.0.0.1:18060/mcp`. Call `get_login_qrcode`, then complete the account login from the returned QR code.

```sh
npx @modelcontextprotocol/inspector
```

Cookies and browser state persist on the `seednote-data` PVC, so a successful login survives pod recreation.

## Operations and verification

Use the port-forward health request above for HTTP verification. For Kubernetes state and logs, use:

```sh
kubectl -n "$namespace" get deployment,pod,service,pvc -l app=seednote
kubectl -n "$namespace" get endpoints seednote
kubectl -n "$namespace" logs deployment/seednote --tail=200
kubectl -n "$namespace" describe pod -l app=seednote
```

An Anban server in the same namespace reaches Seednote with:

```sh
ANBAN_SEEDNOTE_BASE_URL=http://seednote:18060
```
