# ACK wcfLink and Seednote deployment with Yunxiao

`deploy/k8s/ack-sidecars.yaml` is the sole Yunxiao deployment workflow for the
wcfLink and Seednote integrations. The Server deployment, wcfLink, and
Seednote must receive the same `namespace` pipeline variable. That makes their
in-cluster endpoints exactly `http://wcflink:18070` and
`http://seednote:18060/mcp`.

This consolidation removes a duplicate deployment source; it does not reduce
the processes' actual CPU or memory consumption. wcfLink and Seednote remain
separate one-replica Deployments deliberately: they have independent images,
lifecycles, health checks, resource limits, and persistent state, and must not
share one Pod.

wcfLink has no official published image or Dockerfile upstream. This
repository's `Dockerfile.wcflink` is the supported build source: it
fetches `v0.1.0` and verifies commit
`fb0999b81043c91e8fddb780eb2ecf03f1f8588f`. Seednote pulls a configured,
immutable `xpzouying/xiaohongshu-mcp` digest directly from Docker Hub. Do not
build or mirror Seednote in this workflow.

## Prerequisites

- ACK has at least one `amd64` node and a default `StorageClass`.
- ACK nodes can reach Docker Hub for the public Seednote image.
- The Yunxiao ACK/Kubernetes service connection provides the target `kubectl`
  context, and the command runner has Bash, `kubectl`, and `envsubst`.
- The Yunxiao Docker build/push task has an ACR service connection for build and
  push credentials. The Kubernetes `imagePullSecret` is a separate, existing
  pull credential in the target namespace. Both target ACR but serve different
  mechanisms; do not put registry credentials in this repository.

## Pipeline variables

Use the exact variable names already used by `server/Deployment.yaml` for the
shared namespace and pull secret:

| Variable | Example |
| --- | --- |
| `namespace` | `anbanai-prod` |
| `imagePullSecret` | `anban-acr-pull` |
| `wcflink_image_repo` | `registry.cn-hangzhou.aliyuncs.com/anban/wcflink:20260720-abc1234` |
| `seednote_image_repo` | `xpzouying/xiaohongshu-mcp@sha256:<64 lowercase hex>` |
| `wcflink_storage_size` | `10Gi` |
| `seednote_storage_size` | `10Gi` |

## Stage 1: build and push wcfLink

Set `wcflink_image_repo` before the pipeline run to the desired immutable ACR
reference. In Yunxiao's built-in Docker build/push task, use the ACR service
connection, set the build context to `.`, Dockerfile to
`Dockerfile.wcflink`, and set its destination image field to that exact
`wcflink_image_repo` pipeline variable. Use a non-`latest` tag, preferably the
source commit SHA, and never overwrite that tag. Digest and untagged references
are rejected because the destination must be known before the push. Stage 2
reads the same pre-set pipeline variable. The Kubernetes `imagePullSecret` is
not a build credential and is used only by the wcfLink Pod at pull time. There
is no Seednote build or mirror stage.

The upstream Seednote `docker-release` workflow publishes its version input as
both the version tag and `latest`. Resolve a chosen version tag to its digest
before setting `seednote_image_repo`; version tags, including `latest`, are not
accepted by this deployment. For example:

```sh
seednote_tag=v2026.06.12.1403-5c43e3d
docker pull "xpzouying/xiaohongshu-mcp:$seednote_tag"
docker image inspect --format '{{index .RepoDigests 0}}' "xpzouying/xiaohongshu-mcp:$seednote_tag"
```

Use the returned `xpzouying/xiaohongshu-mcp@sha256:<64 lowercase hex>` value
for `seednote_image_repo` after verifying it is the intended upstream digest.

## Stage 2: deploy

Add a Bash command task after Stage 1. Use this exact script:

```bash
set -euo pipefail

export namespace="${namespace:?set the ACK namespace in Yunxiao}"
export imagePullSecret="${imagePullSecret:?set the existing ACR pull secret name in Yunxiao}"
export wcflink_image_repo="${wcflink_image_repo:?set the immutable wcfLink ACR image reference in Yunxiao}"
export seednote_image_repo="${seednote_image_repo:?set the immutable Seednote Docker Hub image reference in Yunxiao}"
export wcflink_storage_size="${wcflink_storage_size:?set the wcfLink PVC size in Yunxiao}"
export seednote_storage_size="${seednote_storage_size:?set the Seednote PVC size in Yunxiao}"

if [[ ${#namespace} -gt 63 || ! "$namespace" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]]; then
  echo "invalid namespace: $namespace" >&2
  exit 1
fi
if [[ ${#imagePullSecret} -gt 63 || ! "$imagePullSecret" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]]; then
  echo "invalid imagePullSecret: $imagePullSecret" >&2
  exit 1
fi
if [[ ! "$wcflink_image_repo" =~ ^[a-z0-9][a-z0-9._/-]*:[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || [[ "$wcflink_image_repo" != */*:* ]] || [[ "$wcflink_image_repo" == *:latest ]]; then
  echo "wcflink_image_repo must use a non-latest tag after the final slash" >&2
  exit 1
fi
if [[ ! "$seednote_image_repo" =~ ^xpzouying/xiaohongshu-mcp@sha256:[a-f0-9]{64}$ ]]; then
  echo "seednote_image_repo must be xpzouying/xiaohongshu-mcp@sha256:<64 lowercase hex>" >&2
  exit 1
fi
if [[ ! "$wcflink_storage_size" =~ ^[1-9][0-9]*(Mi|Gi|Ti)$ ]] || [[ ! "$seednote_storage_size" =~ ^[1-9][0-9]*(Mi|Gi|Ti)$ ]]; then
  echo "storage sizes must be positive Mi, Gi, or Ti quantities" >&2
  exit 1
fi

kubectl create namespace "$namespace" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "$namespace" get secret "$imagePullSecret" >/dev/null

quantity_to_mi() {
  local quantity="$1"
  local number
  local unit
  local multiplier
  local maximum
  if [[ ! "$quantity" =~ ^([1-9][0-9]*)(Mi|Gi|Ti)$ ]]; then
    return 1
  fi
  number="${BASH_REMATCH[1]}"
  unit="${BASH_REMATCH[2]}"
  case "$unit" in
    Mi) multiplier=1; maximum=8796093022207 ;;
    Gi) multiplier=1024; maximum=8589934591 ;;
    Ti) multiplier=1048576; maximum=8388607 ;;
  esac
  # Equal-length positive decimal strings compare lexicographically by value.
  if [[ ${#number} -gt ${#maximum} ]] || [[ ${#number} -eq ${#maximum} && "$number" > "$maximum" ]]; then
    return 1
  fi
  printf '%d\n' "$((10#$number * multiplier))"
}

preflight_pvc_storage() {
  local pvc_name="$1"
  local desired_storage="$2"
  local pvc_ref
  local actual_storage
  local actual_storage_mi
  local desired_storage_mi
  if ! desired_storage_mi=$(quantity_to_mi "$desired_storage"); then
    echo "pipeline storage quantity for $pvc_name is unsupported: $desired_storage" >&2
    return 1
  fi
  if ! pvc_ref=$(kubectl -n "$namespace" get pvc "$pvc_name" --ignore-not-found -o name); then
    echo "failed to inspect existing PVC $pvc_name before apply" >&2
    return 1
  fi
  if [[ -z "$pvc_ref" ]]; then
    return 0
  fi
  if ! actual_storage=$(kubectl -n "$namespace" get pvc "$pvc_name" -o jsonpath='{.spec.resources.requests.storage}'); then
    echo "failed to read storage request for existing PVC $pvc_name" >&2
    return 1
  fi
  if ! actual_storage_mi=$(quantity_to_mi "$actual_storage"); then
    echo "existing PVC $pvc_name has unsupported storage quantity: $actual_storage" >&2
    return 1
  fi
  if [[ "$actual_storage_mi" != "$desired_storage_mi" ]]; then
    echo "PVC storage mismatch for $pvc_name: existing=$actual_storage pipeline=$desired_storage; expand or migrate the PVC separately before this deploy" >&2
    return 1
  fi
}
preflight_pvc_storage wcflink-state "$wcflink_storage_size"
preflight_pvc_storage seednote-data "$seednote_storage_size"

envsubst '${namespace} ${imagePullSecret} ${wcflink_image_repo} ${seednote_image_repo} ${wcflink_storage_size} ${seednote_storage_size}' < deploy/k8s/ack-sidecars.yaml | kubectl apply -f -
kubectl -n "$namespace" rollout status deployment/wcflink --timeout=10m
kubectl -n "$namespace" rollout status deployment/seednote --timeout=10m
kubectl -n "$namespace" get deployment,pod,service,pvc -l 'app in (wcflink,seednote)'
kubectl -n "$namespace" get endpoints wcflink seednote
```

No rollout restart is required. Applying a changed `seednote_image_repo`
updates only the Seednote Pod template and rolls only Seednote; changing only
`wcflink_image_repo` rolls only wcfLink.

The storage-size variables are PVC creation-time and steady-state values, not
normal rollout controls. The preflight compares equivalent `Mi`, `Gi`, and
`Ti` quantities semantically, so `1Gi` and `1024Mi` are compatible. Never
shrink either PVC. To expand one, first confirm its StorageClass has
`allowVolumeExpansion: true`, patch or expand the PVC separately and wait for
that operation to finish, then update the corresponding pipeline variable. If
expansion is unsupported, migrate the data to a new PVC. The preflight prevents
a mismatched, unsupported, or int64-byte-overflowing size from reaching the
multi-object apply, including on the first deployment when the PVC is absent.

The Server deployment should use these values in the same namespace:

```sh
ANBAN_SEEDNOTE_BASE_URL=http://seednote:18060
ANBAN_ILINK_ENABLED=true
ANBAN_ILINK_BASE_URL=http://wcflink:18070
```

## First login and verification

Use an administrator's `kubectl` context and the same namespace value. Keep
each port-forward in a separate terminal; neither service is exposed outside
the cluster.

```sh
export namespace=anbanai-prod
kubectl -n "$namespace" port-forward service/wcflink 18070:18070
```

Verify wcfLink through the local forward:

```sh
curl --fail http://127.0.0.1:18070/health/live
```

To perform wcfLink's first account login, start a QR session through that same
administrator-only forward, record its `session_id`, open the downloaded PNG,
and poll until the status is `confirmed`:

```sh
curl --fail --request POST http://127.0.0.1:18070/api/accounts/login/start \
  --header 'Content-Type: application/json' \
  --data '{"base_url":""}'
curl --fail --output wcflink-login.png 'http://127.0.0.1:18070/api/accounts/login/qr?session_id=<session-id>'
curl --fail 'http://127.0.0.1:18070/api/accounts/login/status?session_id=<session-id>'
```

For Seednote login, start a second forward, verify health, then connect an MCP
Inspector to the local `/mcp` endpoint and complete the QR-code flow:

```sh
kubectl -n "$namespace" port-forward service/seednote 18060:18060
curl --fail http://127.0.0.1:18060/health
npx @modelcontextprotocol/inspector
```

Use `http://127.0.0.1:18060/mcp` in the inspector. The `seednote-data` PVC
persists cookies and browser state after successful login.

Both services are unauthenticated. `ClusterIP` prevents direct external
exposure but does not restrict callers inside the cluster. Use administrator-
only port-forwarding and apply a namespace-appropriate NetworkPolicy that
allows only intended workloads; no generic policy is supplied because caller
labels vary by deployment.
