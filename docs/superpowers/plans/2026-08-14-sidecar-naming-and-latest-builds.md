# Sidecar Naming and Latest Builds Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build both integrations from repository-owned latest-upstream Dockerfiles and rename deployable resources to `sidecar-ilink` and `sidecar-seednote`.

**Architecture:** The Dockerfiles fetch the requested upstream `main` ref while building. Kubernetes, Compose, and Server defaults use the new deployment identities and DNS names. ACK controls final image tag/digest variables; PVC copying is explicitly manual.

**Tech Stack:** Docker BuildKit, Docker Compose, Kubernetes YAML, Bash, Go tests.

## Global Constraints

- iLink defaults to `https://github.com/lich0821/wcfLink.git` at `master`.
- Seednote defaults to `https://github.com/xpzouying/xiaohongshu-mcp.git` at `main`.
- Do not pin a source revision in repository defaults.
- ACK image variables can contain an operator-selected tag or digest.
- Do not add a migration Job, hook, or automatic deletion.
- Keep services internal and local Compose ports loopback-only.
- Retain upstream `WCFLINK_*` environment variables inside the iLink container.

---

## File Structure

- `deploy/docker/Dockerfile.sidecar-ilink`: latest iLink source build.
- `deploy/docker/Dockerfile.sidecar-seednote`: latest Seednote source build and browser checksum verification.
- `Makefile`: `docker-sidecar-ilink-image` and `docker-sidecar-seednote-image`.
- `docker-compose.yml`: renamed local services, images, data directories, and Server DNS.
- `deploy/k8s/ack-sidecar-ilink.yaml`: renamed iLink PVC, Deployment, and Service.
- `deploy/k8s/ack-sidecar-seednote.yaml`: renamed Seednote PVC, Deployment, Service, and NetworkPolicy.
- `server/Deployment.yaml`, `server/config.example.yaml`: renamed default DNS endpoints.
- `server/k8s_ack_sidecars_contract_test.go`, `server/agent/docker_runtime_contract_test.go`: renamed manifest, Dockerfile, Makefile, Compose, and DNS contracts.
- `deploy/k8s/ack-sidecars.md`: build, deployment, manual migration, rollback, and cleanup instructions.

### Task 1: Establish failing contracts

**Files:**
- Modify: `server/k8s_ack_sidecars_contract_test.go`
- Modify: `server/agent/docker_runtime_contract_test.go`

**Interfaces:**
- Produces: failing checks for the new manifest paths, resources, Dockerfiles, Make targets, and service DNS names.

- [ ] **Step 1: Write renamed ACK/DNS assertions**

Require:

```go
"../deploy/k8s/ack-sidecar-ilink.yaml"
"../deploy/k8s/ack-sidecar-seednote.yaml"
"sidecar-ilink-state"
"sidecar-seednote-data"
"sidecar-ilink"
"sidecar-seednote"
"sidecar-seednote-server-only"
"http://sidecar-ilink:18070"
"http://sidecar-seednote:18060"
```

- [ ] **Step 2: Write renamed Docker/Compose/Make assertions**

Require:

```go
"SIDECAR_ILINK_IMAGE ?= anban-creator-sidecar-ilink:latest"
"SIDECAR_SEEDNOTE_IMAGE ?= anban-creator-sidecar-seednote:latest"
"docker-sidecar-ilink-image:"
"docker-sidecar-seednote-image:"
"deploy/docker/Dockerfile.sidecar-ilink"
"deploy/docker/Dockerfile.sidecar-seednote"
"sidecar-ilink:"
"sidecar-seednote:"
```

Require the Dockerfiles to contain their upstream URL, `main`, `git fetch --depth 1`, and Seednote's `sha256sum -c` browser verification.

- [ ] **Step 3: Verify RED**

Run:

```bash
go test ./server -run 'TestACK|TestServerDeployment|TestConfigExample' -count=1
go test ./server/agent -run 'TestComposeAndMakefile|TestSeednoteSidecarBuild' -count=1
```

Expected: failure because legacy paths, names, targets, and Dockerfiles remain.

### Task 2: Replace Docker build definitions and Makefile targets

**Files:**
- Create: `deploy/docker/Dockerfile.sidecar-ilink`
- Create: `deploy/docker/Dockerfile.sidecar-seednote`
- Delete: legacy iLink sidecar Dockerfile
- Delete: `scripts/build-seednote-sidecar.sh`
- Modify: `Makefile`

**Interfaces:**
- Consumes: Task 1 build contracts.
- Produces: root-context build targets for `sidecar-ilink` and `sidecar-seednote`.

- [ ] **Step 1: Implement the latest-upstream iLink Dockerfile**

Use:

```dockerfile
ARG ILINK_REPO=https://github.com/lich0821/wcfLink.git
ARG ILINK_REF=master
RUN git init . \
    && git remote add origin "$ILINK_REPO" \
    && git fetch --depth 1 origin "$ILINK_REF" \
    && git checkout --detach FETCH_HEAD
```

Preserve the static binary, `/app/state`, `WCFLINK_*` defaults, and port `18070`.

- [ ] **Step 2: Implement the latest-upstream Seednote Dockerfile**

Fetch `SEEDNOTE_REPO` / `SEEDNOTE_REF` into `/src`, build there, and use its `browser/browser_version.txt` in the runtime stage. Preserve:

```dockerfile
curl -fsSL "${BASE}/SHA256SUMS" | grep " linux-x64.tar.xz$" | awk '{print $1"  /tmp/browser.tar.xz"}' | sha256sum -c -
```

Set `XDG_CACHE_HOME=/app/cache`, `HOME=/app/data/home`, `XDG_CONFIG_HOME=/app/data/config`, use `tini`, expose `18060`, and retain `CMD ["./app"]`.

- [ ] **Step 3: Implement Makefile target replacement**

Set:

```make
SIDECAR_ILINK_IMAGE ?= anban-creator-sidecar-ilink:latest
SIDECAR_SEEDNOTE_IMAGE ?= anban-creator-sidecar-seednote:latest
SIDECAR_ILINK_REPO ?= https://github.com/lich0821/wcfLink.git
SIDECAR_ILINK_REF ?= master
SIDECAR_SEEDNOTE_REPO ?= https://github.com/xpzouying/xiaohongshu-mcp.git
SIDECAR_SEEDNOTE_REF ?= main
```

Add `docker-sidecar-ilink-image` and `docker-sidecar-seednote-image` root-context build commands to `.PHONY`, `docker-images`, and help. Remove `docker-wcflink-image`, `docker-seednote-sidecar-image`, and legacy variables.

- [ ] **Step 4: Verify GREEN**

Run:

```bash
go test ./server/agent -run 'TestComposeAndMakefile|TestSeednoteSidecarBuild' -count=1
make -n docker-sidecar-ilink-image docker-sidecar-seednote-image
```

### Task 3: Rename Compose, Kubernetes, and Server endpoints

**Files:**
- Create: `deploy/k8s/ack-sidecar-ilink.yaml`
- Create: `deploy/k8s/ack-sidecar-seednote.yaml`
- Delete: `deploy/k8s/ack-wcflink.yaml`
- Delete: `deploy/k8s/ack-seednote.yaml`
- Modify: `docker-compose.yml`
- Modify: `server/Deployment.yaml`
- Modify: `server/config.example.yaml`
- Modify: `server/config/live_slice_config_test.go`
- Modify: `server/k8s_ack_sidecars_contract_test.go`

**Interfaces:**
- Consumes: Task 1 and Task 2 interfaces.
- Produces: aligned iLink and Seednote service DNS across all runtime surfaces.

- [ ] **Step 1: Rename the iLink manifest**

Create a manifest with PVC `sidecar-ilink-state`, Deployment and Service `sidecar-ilink`, app label `sidecar-ilink`, and variables `${sidecar_ilink_image_repo}` and `${sidecar_ilink_storage_size}`. Preserve all existing iLink runtime settings.

- [ ] **Step 2: Rename the Seednote manifest**

Create a manifest with PVC `sidecar-seednote-data`, Deployment and Service `sidecar-seednote`, label `sidecar-seednote`, NetworkPolicy `sidecar-seednote-server-only`, and variables `${sidecar_seednote_image_repo}` and `${sidecar_seednote_storage_size}`. Preserve all existing Seednote runtime settings.

- [ ] **Step 3: Rename Compose resources**

Use services `sidecar-ilink` and `sidecar-seednote`, new Dockerfiles, images `anban-creator-sidecar-ilink:latest` / `anban-creator-sidecar-seednote:latest`, host directories `./data/sidecar-ilink` / `./data/sidecar-seednote`, and new Server URLs.

- [ ] **Step 4: Update Server defaults and config tests**

Set both default URLs to:

```text
http://sidecar-ilink:18070
http://sidecar-seednote:18060
```

- [ ] **Step 5: Verify GREEN**

Run:

```bash
go test ./server -run 'TestACK|TestServerDeployment|TestConfigExample' -count=1
go test ./server/config -run TestLiveSlice -count=1
envsubst '${namespace} ${imagePullSecret} ${sidecar_ilink_image_repo} ${sidecar_ilink_storage_size}' < deploy/k8s/ack-sidecar-ilink.yaml | kubectl apply --dry-run=client -f -
envsubst '${namespace} ${imagePullSecret} ${sidecar_seednote_image_repo} ${sidecar_seednote_storage_size} ${server_app_label}' < deploy/k8s/ack-sidecar-seednote.yaml | kubectl apply --dry-run=client -f -
```

### Task 4: Update operations documentation and verify

**Files:**
- Modify: `deploy/k8s/ack-sidecars.md`
- Modify: `README.md`
- Modify: `AGENTS.md`
- Modify: `docs/montage-upgrade.md`
- Modify: `docs/superpowers/specs/2026-08-11-seednote-server-mcp-external-research-design.md`
- Modify: `docs/superpowers/plans/2026-08-11-seednote-server-mcp-external-research.md`

**Interfaces:**
- Consumes: Task 2 build commands and Task 3 identities.
- Produces: operational migration and rollback guidance with no repository migration Job.

- [ ] **Step 1: Update Yunxiao build/deploy documentation**

Document `Dockerfile.sidecar-ilink`, `Dockerfile.sidecar-seednote`, latest-upstream default refs, `sidecar_ilink_image_repo`, `sidecar_seednote_image_repo`, `sidecar_ilink_storage_size`, `sidecar_seednote_storage_size`, and both new manifest paths. State ACK selects a tag or digest.

- [ ] **Step 2: Document manual PVC migration and rollback**

Give commands to stop old Deployments, create new PVCs, mount old/new claims in an administrator temporary Pod, copy `/app/state` and `/app/data`, verify, deploy renamed sidecars, retain old resources, rollback by restoring old URLs/scaling old workloads, then manually delete old resources after observation. Do not commit migration YAML.

- [ ] **Step 3: Update product-facing deployment references**

Rename only sidecar resources, Dockerfiles, manifest paths, image names, and URLs. Do not rename the Seednote product runtime or iLink application channel.

- [ ] **Step 4: Complete verification**

Run:

```bash
go test ./...
git diff --check
docker compose config --quiet
rg -n 'ack-wcflink|ack-seednote|Dockerfile\.wcflink|docker-wcflink-image|docker-seednote-sidecar-image|http://wcflink:18070|http://seednote:18060' --glob '!docs/superpowers/specs/2026-08-14-sidecar-naming-and-latest-builds-design.md' --glob '!docs/superpowers/plans/2026-08-14-sidecar-naming-and-latest-builds.md' .
```

- [ ] **Step 5: Commit implementation**

```bash
git add Makefile docker-compose.yml deploy/docker deploy/k8s server README.md AGENTS.md docs
git diff --cached --check
git commit -m "refactor: rename deployable sidecars"
```
