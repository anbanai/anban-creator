# ACK sidecar deployment with Yunxiao

The iLink and Seednote sidecars have independent build and deployment paths:

- `deploy/k8s/ack-sidecar-ilink.yaml`
- `deploy/k8s/ack-sidecar-seednote.yaml`

Deploying or rolling back one sidecar does not apply, restart, or rebuild the
other. All image references are supplied by ACK pipeline variables; ACK may use
an image tag or an immutable digest according to its release policy. The
manifests use `imagePullPolicy: Always` so a recreated Pod fetches the image
reference selected by ACK.

The Server and both sidecars must be deployed into the same `namespace`. Their
in-cluster URLs are `http://sidecar-ilink:18070` and
`http://sidecar-seednote:18060`.

## Build images

Both repository-owned Dockerfiles fetch the latest upstream default branch by
default:

| Sidecar | Dockerfile | Upstream source |
| --- | --- | --- |
| iLink | `deploy/docker/Dockerfile.sidecar-ilink` (`master`) | `https://github.com/lich0821/wcfLink.git` |
| Seednote | `deploy/docker/Dockerfile.sidecar-seednote` (`main`) | `https://github.com/xpzouying/xiaohongshu-mcp.git` |

In separate Yunxiao Docker build/push tasks, set the build context to this
repository root and select the corresponding Dockerfile. Set the task's
destination image to the variable used by its deployment task. Build for
`linux/amd64`, because both ACK manifests select amd64 nodes. Enable the
task's no-cache/pull options when building an upstream default branch; otherwise Docker may reuse a
cached clone instead of fetching the current upstream commit.

For local or scriptable builds, use:

```bash
make docker-sidecar-ilink-image \
  SIDECAR_ILINK_IMAGE=chengdu.personal.cr.aliyuncs.com/bx_anbanai/sidecar-ilink:latest

make docker-sidecar-seednote-image \
  SIDECAR_SEEDNOTE_IMAGE=chengdu.personal.cr.aliyuncs.com/bx_anbanai/sidecar-seednote:latest
```

To build a different upstream revision, override `SIDECAR_ILINK_REF` or
`SIDECAR_SEEDNOTE_REF`; to use a mirror, override the matching `*_REPO`
variable. The defaults deliberately use the original upstream repositories.

## Pipeline variables

| Variable | Example |
| --- | --- |
| `namespace` | `anbanai-prod` |
| `imagePullSecret` | `anban-acr-pull` |
| `sidecar_ilink_image_repo` | `chengdu.personal.cr.aliyuncs.com/bx_anbanai/sidecar-ilink:latest` |
| `sidecar_ilink_storage_size` | `10Gi` |
| `sidecar_seednote_image_repo` | `chengdu.personal.cr.aliyuncs.com/bx_anbanai/sidecar-seednote:latest` |
| `sidecar_seednote_storage_size` | `10Gi` |
| `server_app_label` | `anban-creator-server` |

`imagePullSecret` is a Kubernetes pull secret already present in the target
namespace. It is distinct from the ACR service connection used by Yunxiao to
build and push images.

## Deploy iLink

Use a dedicated Bash command task for iLink:

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
kubectl -n "$namespace" get endpoints sidecar-ilink
```

The PVC is `sidecar-ilink-state`. Do not reduce its requested size. If its
requested size differs from the pipeline variable, reconcile expansion or data
migration before applying the manifest.

## Deploy Seednote

Use a separate Bash command task for Seednote:

```bash
set -euo pipefail

export namespace="${namespace:?set the ACK namespace}"
export imagePullSecret="${imagePullSecret:?set the existing ACR pull secret name}"
export sidecar_seednote_image_repo="${sidecar_seednote_image_repo:?set the Seednote image reference}"
export sidecar_seednote_storage_size="${sidecar_seednote_storage_size:?set the Seednote PVC size}"
export server_app_label="${server_app_label:?set the Server Pod app label}"

kubectl create namespace "$namespace" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "$namespace" get secret "$imagePullSecret" >/dev/null

envsubst '${namespace} ${imagePullSecret} ${sidecar_seednote_image_repo} ${sidecar_seednote_storage_size} ${server_app_label}' \
  < deploy/k8s/ack-sidecar-seednote.yaml | kubectl apply -f -
kubectl -n "$namespace" rollout restart deployment/sidecar-seednote
kubectl -n "$namespace" rollout status deployment/sidecar-seednote --timeout=10m
kubectl -n "$namespace" get deployment,pod,service,pvc -l app=sidecar-seednote
kubectl -n "$namespace" get endpoints sidecar-seednote
kubectl -n "$namespace" get networkpolicy sidecar-seednote-server-only
```

The NetworkPolicy permits TCP/18060 only from Pods labelled with
`server_app_label`. Confirm that the ACK CNI enforces NetworkPolicy before
treating it as an access-control boundary. One-shot Agent Jobs must not receive
the Server's `app` label.

## Server settings

Deploy the Server with the same namespace and these service URLs:

```sh
ANBAN_ILINK_ENABLED=true
ANBAN_ILINK_BASE_URL=http://sidecar-ilink:18070
ANBAN_SEEDNOTE_BASE_URL=http://sidecar-seednote:18060
```

Roll out the Server after the corresponding Service exists. This does not
require re-deploying an unchanged sidecar.

## Manual PVC migration in ACK

Renaming a PVC creates a new claim; Kubernetes cannot rename a bound PVC. Do
not delete the old PVC until the new sidecar has been verified. Migrate each
sidecar independently in an administrator-operated ACK terminal.

For iLink, stop the old deployment and wait for its Pod to terminate:

```sh
kubectl -n "$namespace" scale deployment/wcflink --replicas=0
kubectl -n "$namespace" wait --for=delete pod -l app=wcflink --timeout=10m
```

Create only the new `sidecar-ilink-state` PVC; the first document in the
manifest is the PVC, so do not apply the Deployment yet:

```sh
envsubst '${namespace} ${sidecar_ilink_storage_size}' \
  < deploy/k8s/ack-sidecar-ilink.yaml | sed '/^---$/,$d' | kubectl apply -f -
```

Run a temporary Pod that mounts old `wcflink-state` at `/old` and new
`sidecar-ilink-state` at `/new` (use an image that includes `cp`, such as
`busybox:1.36`):

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: migrate-sidecar-ilink
spec:
  restartPolicy: Never
  containers:
    - name: copy
      image: busybox:1.36
      command: ["sh", "-c", "sleep 3600"]
      volumeMounts:
        - { name: old, mountPath: /old }
        - { name: new, mountPath: /new }
  volumes:
    - name: old
      persistentVolumeClaim: { claimName: wcflink-state }
    - name: new
      persistentVolumeClaim: { claimName: sidecar-ilink-state }
```

After the Pod is Running, copy and inspect the data:

```sh
kubectl -n "$namespace" exec migrate-sidecar-ilink -- sh -c 'cp -a /old/. /new/ && sync && find /new -maxdepth 2 -ls'
kubectl -n "$namespace" delete pod migrate-sidecar-ilink --wait=true
```

For Seednote, repeat those four steps with old deployment `seednote`, old claim
`seednote-data`, new claim `sidecar-seednote-data`, manifest
`ack-sidecar-seednote.yaml`, and temporary Pod name
`migrate-sidecar-seednote`. The temporary Pod must not run while either
sidecar is writing to the same ReadWriteOnce claim. After copying, apply the
new sidecar manifest using its independent deployment command above and verify
its login state.

Only after verification should the old Deployment, Service, and PVC be removed.
To roll back, scale down the new deployment, restore the old Server URL and old
deployment, then keep the new PVC for investigation. This repository supplies
no automatic migration Job because the operator must control namespace access,
claim binding, and the precise cutover window.

## Login and verification

Keep both services as `ClusterIP`; the APIs are unauthenticated. Use
administrator-only port forwarding for initial login:

```sh
export namespace=anbanai-prod
kubectl -n "$namespace" port-forward service/sidecar-ilink 18070:18070
curl --fail http://127.0.0.1:18070/health/live
```

Start iLink's QR login through that forward:

```sh
curl --fail --request POST http://127.0.0.1:18070/api/accounts/login/start \
  --header 'Content-Type: application/json' --data '{"base_url":""}'
curl --fail --output sidecar-ilink-login.png \
  'http://127.0.0.1:18070/api/accounts/login/qr?session_id=<session-id>'
curl --fail 'http://127.0.0.1:18070/api/accounts/login/status?session_id=<session-id>'
```

For Seednote, use a second terminal:

```sh
kubectl -n "$namespace" port-forward service/sidecar-seednote 18060:18060
curl --fail http://127.0.0.1:18060/health
```

Connect an MCP Inspector to `http://127.0.0.1:18060/mcp` and complete its QR
login. The `sidecar-seednote-data` PVC preserves its cookies and browser state.
