# Montage Upgrade Procedure

Montage is integrated as a git submodule at `third_party/OpenMontage`.
Anban owns the adapter contract and does not modify upstream Montage source
files during normal feature work.

## Update

```bash
git submodule update --init --recursive
git -C third_party/OpenMontage fetch origin
git -C third_party/OpenMontage checkout origin/main
```

Review the submodule diff:

```bash
git diff --submodule=log
```

## Verify

```bash
go test ./server/agent -run Montage -count=1
go test ./server/service -run Montage -count=1
go test ./server/config -run Montage -count=1
cd studio && bun run test -- src/lib/montage-form.test.ts src/pages/MontageUx.contract.test.ts src/lib/schemas.test.ts
```

## Runtime Images

Production uses three immutable Agent images:

- `ANBAN_AGENT_IMAGE_ARTICLE` (deployed from `article_agent_image_repo`): the minimal Article runtime.
- `ANBAN_AGENT_IMAGE_SEEDNOTE`: the independent Seednote workflow runtime. Xiaohongshu research uses authenticated Anban Server MCP tools backed by the separately deployed `seednote` sidecar.
- `ANBAN_AGENT_IMAGE_MONTAGE`: the Montage runtime with an embedded OpenMontage template.

Build all three images with:

```bash
make docker-agent-image
make docker-seednote-agent-image
make docker-montage-agent-image
```

The Montage image stores a complete read-only OpenMontage template at
`/opt/montage-template`. Publish all three images by digest. Do not deploy
mutable tags as the persisted `task_executions.runtime_image` value.

## Workspace And Resume

Every managed execution receives a fresh container or Kubernetes Job. The task
workspace volume is mounted at `/workspace`, and the runtime owns its workspace
and output. At Kubernetes task startup, the init container atomically copies the
complete image template into a writable `/workspace/openmontage` directory when
it does not already exist. The Agent uses the same materialization contract in
Docker and runs with:

```text
cwd=/workspace/openmontage
ANBAN_MONTAGE_SUBMODULE_PATH=/workspace/openmontage
```

OpenMontage project files, checkpoints, and Claude session state therefore stay
on NAS across Job replacement and explicit task resume. Bootstrap replay only
adds missing task/resume inputs and rejects conflicting files; it does not
overwrite existing checkpoint or session data. A resume attempt reuses the
parent execution's persisted runtime profile and image digest, even if current
server configuration has changed.

`/tmp` is an `emptyDir` and is intentionally not recoverable. Do not place
resume-critical state there. Direct artifact upload uses
`/workspace/openmontage/output`; source trees, checkpoints, `.anban-runtime-home`,
`.claude`, secrets, and dependency caches remain on NAS and are not published as
task artifacts unless the workflow explicitly registers a stable file through
MCP.

Terminal task workspaces remain available for resume. Permanently deleting the
task deletes its task-workspace PVC; deleting the PVC out of band also makes the
original execution state non-resumable. Project memory uses a separate PVC.

Backlot is not exposed as an Anban task page. The platform retains normalized
checkpoint, timeline, run-log, manifest, and delivery artifacts instead.

Verify a live deployment with:

```bash
kubectl -n anbanai-prod get pod <pod> -o jsonpath='{range .status.initContainerStatuses[*]}{.name}{"="}{.imageID}{"\n"}{end}{range .status.containerStatuses[*]}{.name}{"="}{.imageID}{"\n"}{end}'
kubectl -n anbanai-prod get pod <pod> -o jsonpath='{range .spec.volumes[*]}{.name}{"="}{.persistentVolumeClaim.claimName}{"\n"}{end}'
kubectl -n anbanai-prod exec <pod> -- sh -c 'pwd; test -d /workspace/openmontage; test -w /workspace/openmontage; test -d /workspace/openmontage/remotion-composer'
```

## Adapter Rule

If upstream pipeline metadata changes, update only Anban's Montage adapter
mapping and tests. Do not copy Montage internals into Studio schemas.

The stable Anban boundary remains:

- `montage_input` in API and Studio.
- `montage-input.json` in the agent workspace.
- `montage-project.json` as the adapter manifest.
- `final_video` plus `delivery-manifest.json` as required completion deliverables.
