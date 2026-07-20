# ACK Seednote Standalone Deployment Design

## Goal

Deploy `xpzouying/xiaohongshu-mcp:latest` as an independent, internal-only
Seednote service in Alibaba Cloud ACK. Yunxiao Flow must be able to deploy or
refresh it without building or pushing an image.

## Scope

The deployment assets will be independent from `deploy/k8s/ack-sidecars.yaml`
and will not deploy `wcflink`, Anban Server, or any public ingress.

The service remains compatible with the existing Anban Server contract:

```text
http://seednote:18060/mcp
```

## Kubernetes Resources

Add `deploy/k8s/ack-seednote.yaml` containing:

- one `PersistentVolumeClaim` for `/app/data`;
- one single-replica `Deployment` named `seednote`;
- one internal `ClusterIP` Service named `seednote` on port `18060`;
- `Recreate` deployment strategy so two browser sessions never overlap;
- an `amd64` node selector because the selected upstream image is amd64;
- an in-memory `/dev/shm` volume for Chromium;
- `/health` readiness and liveness probes;
- resource requests and limits appropriate for a Chromium workload;
- the upstream Docker environment contract for browser state and cookies.

The manifest will use the fixed public image
`xpzouying/xiaohongshu-mcp:latest` with `imagePullPolicy: Always`. It will not
require an ACR image pull secret.

The namespace and PVC size will remain Yunxiao variables, following the
repository's existing `${variable}` plus `envsubst` deployment convention.

## Yunxiao Operation

Add `deploy/k8s/ack-seednote.md` with a ready-to-use Yunxiao shell task. The
task will:

1. set the target namespace and storage size;
2. ensure the namespace exists;
3. render the manifest through `envsubst` and apply it with `kubectl`;
4. restart the Deployment so an unchanged `latest` tag is pulled again;
5. wait for rollout completion and print service, pod, PVC, and endpoint state.

The documentation will also include port forwarding for first-time QR-code
login and commands for health and MCP verification.

## Security And State

No Ingress, public LoadBalancer, NodePort, or external DNS resource will be
created. The upstream service has no application-level authentication, so it
must remain reachable only inside the ACK network or through explicit
administrator port forwarding.

Cookies and browser state survive Pod replacement through the PVC. The
deployment remains at one replica because one account and one persisted browser
state must not be operated concurrently by multiple Pods.

## Verification

Add a Go manifest contract test under `server/` that fails unless the standalone
manifest preserves the fixed image, single replica, `Recreate` strategy,
`ClusterIP` Service, PVC mount, `/dev/shm`, health probes, amd64 selector, and
absence of public exposure resources.

Run the targeted manifest test, the relevant existing ACK manifest tests, YAML
rendering checks, and the full Go test suite before completion.
