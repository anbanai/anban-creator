# Sidecar Naming and Latest Builds Design

## Goal

Replace the legacy wcfLink and Seednote sidecar deployment identities with
`sidecar-ilink` and `sidecar-seednote`, and build both sidecar images from
repository-owned Dockerfiles that track the latest upstream `main` branch.

## Scope

This change covers the repository-owned local Docker Compose setup, ACK
manifests, Server defaults, Makefile targets, Docker build definitions,
contract tests, and ACK operating documentation.

It does not change the Seednote product/runtime profile named `seednote`, the
iLink product channel configuration named `ilink`, or their Go package names.
Those names are application concepts rather than deployable sidecar resource
identities.

## Build Architecture

The repository will own two Dockerfiles under `deploy/docker/`:

- `Dockerfile.sidecar-ilink` fetches `https://github.com/lich0821/wcfLink.git`
  at its default branch during image construction and compiles its server.
- `Dockerfile.sidecar-seednote` fetches
  `https://github.com/xpzouying/xiaohongshu-mcp.git` at its default branch,
  compiles its Go server, and preloads the browser using the upstream project's
  browser version and checksum contract.

Both Dockerfiles accept an optional repository URL and Git ref build argument
for troubleshooting or controlled rebuilds. Their defaults are the upstream
repositories and `main`. They intentionally do not pin a commit, tag, or
source digest: the requested behavior is to build the latest upstream source.

The Makefile will expose `docker-sidecar-ilink-image` and
`docker-sidecar-seednote-image`, using image variables named
`SIDECAR_ILINK_IMAGE` and `SIDECAR_SEEDNOTE_IMAGE`. The old
`docker-wcflink-image`, `docker-seednote-sidecar-image`, and temporary
Seednote build script will be removed. The default local image names will be
`anban-creator-sidecar-ilink:latest` and
`anban-creator-sidecar-seednote:latest`.

ACK owns the final deployed image reference. Pipeline variables are allowed to
use tags or digests according to the operator's release policy; repository
templates do not require a digest.

## Deployment Naming

All deployable sidecar identities change together:

| Legacy identity | New identity |
| --- | --- |
| `wcflink` Deployment, Service, labels, and container | `sidecar-ilink` |
| `wcflink-state` PVC and volume | `sidecar-ilink-state` |
| `seednote` Deployment, Service, labels, and container | `sidecar-seednote` |
| `seednote-data` PVC and volume | `sidecar-seednote-data` |
| `seednote-server-only` NetworkPolicy | `sidecar-seednote-server-only` |
| `ack-wcflink.yaml` | `ack-sidecar-ilink.yaml` |
| `ack-seednote.yaml` | `ack-sidecar-seednote.yaml` |

The Server and local Compose configuration use the new DNS names:

```text
ANBAN_ILINK_BASE_URL=http://sidecar-ilink:18070
ANBAN_SEEDNOTE_BASE_URL=http://sidecar-seednote:18060
```

The protocol-facing environment variables inside the iLink container retain
their upstream `WCFLINK_*` names because those are the wcfLink executable's
public configuration interface.

ACK pipeline variables become `sidecar_ilink_image_repo`,
`sidecar_seednote_image_repo`, `sidecar_ilink_storage_size`, and
`sidecar_seednote_storage_size`.

## Data Migration and Rollback

PVC names are immutable, so a complete naming migration requires operator-run
data copying in ACK. Repository manifests will create only the new PVCs; no
data-migration Job, hook, or automatic deletion will be shipped.

The operations guide will require the operator to:

1. scale the old Deployment to zero;
2. create the new PVC without starting the renamed Deployment;
3. run an administrator-created temporary Pod that mounts both claims and
   copies `/app/state` from `wcflink-state` to `sidecar-ilink-state`, and
   `/app/data` from `seednote-data` to `sidecar-seednote-data`;
4. verify the copied files, delete the temporary Pod, and deploy the renamed
   sidecar;
5. retain the old Deployment, Service, and PVC for a defined observation
   period; and
6. delete the old resources only after health checks and persisted login state
   are confirmed.

Rollback is explicit: scale the renamed Deployment down, restore the Server
URLs to the old service names, and scale the old Deployment up. The old PVC is
not deleted before the observation period ends.

## Security and Operational Constraints

- Sidecar Services remain `ClusterIP`; no public ingress is added.
- Both ACK Pods continue using the configured image pull secret and `amd64`
  node selector.
- The Seednote NetworkPolicy continues permitting only the labeled Server Pods
  to reach TCP/18060, but follows the new sidecar name and label.
- The iLink and Seednote HTTP APIs remain unauthenticated. Local Compose ports
  stay loopback-only and ACK access remains internal/administrator
  port-forwarded.
- The new Seednote Dockerfile must preserve browser artifact checksum
  verification and preload the browser outside the persistent `/app/data`
  mount.

## Verification

Contract tests will assert the renamed manifest paths, resource names, labels,
PVC claims, Server defaults, Compose service DNS names, Dockerfile build
contracts, and Makefile targets. Validation must include:

```sh
go test ./...
git diff --check
docker compose config --quiet
```

The Dockerfile source fetch cannot be treated as reproducible because it tracks
the upstream branch by design. The image pushed by ACK is controlled by the
operator-provided image variable.
