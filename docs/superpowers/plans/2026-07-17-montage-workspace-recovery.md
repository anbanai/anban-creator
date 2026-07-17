# Montage Workspace Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a dedicated Montage runtime image and run OpenMontage from a writable, resume-safe root inside the task NAS workspace.

**Architecture:** The image contains an immutable OpenMontage template and revision marker. The Kubernetes init container materializes that template once into `/workspace/openmontage`; later attempts verify rather than overwrite it. The runner keeps `/workspace` as the artifact root but uses `/workspace/openmontage` as Montage's Claude `cwd` and submodule path.

**Tech Stack:** Docker, Make, Kubernetes init containers, Go Agent SDK wrapper, NAS PVC, Go contract tests

---

### Task 1: Define content and Montage image build surfaces

**Files:**
- Modify: `Dockerfile.agent`
- Create: `Dockerfile.agent-montage`
- Modify: `Makefile`
- Modify: `server/agent/docker_runtime_contract_test.go`

- [ ] **Step 1: Write failing packaging tests**

Require the common runtime contract:

```go
for _, path := range []string{"Dockerfile.agent", "Dockerfile.agent-montage"} {
    body := readRepoFile(t, filepath.Join(root, path))
    for _, want := range []string{
        "ARG CLAUDE_CODE_VERSION=2.1.208",
        "COPY --from=builder /out/anban",
        "COPY claudecode/",
        "ENTRYPOINT",
    } {
        if !strings.Contains(body, want) {
            t.Fatalf("%s missing %q", path, want)
        }
    }
}
```

The Montage Dockerfile must also contain `COPY third_party/OpenMontage/`, install its Python/Node requirements, run registry discovery, and write `/app/third_party/OpenMontage/.anban-source-revision` from a required build arg.

- [ ] **Step 2: Run and verify failure**

```bash
go test ./server/agent -run 'TestDockerRuntimeProfiles|TestMontageRuntimeImageContract' -count=1
```

Expected: FAIL because `Dockerfile.agent-montage` does not exist.

- [ ] **Step 3: Keep the content image focused**

Retain the existing runner, Claude plugin, Agent-Reach, shared skills, and common media tools in `Dockerfile.agent`. Remove the OpenMontage copy and Montage-only environment variable from this image.

- [ ] **Step 4: Create the Montage image**

Start from the same base/runtime pattern and versions as `Dockerfile.agent`. Add:

```dockerfile
ARG OPENMONTAGE_REVISION
COPY third_party/OpenMontage/ /app/third_party/OpenMontage/
RUN test -n "$OPENMONTAGE_REVISION" && \
    printf '%s\n' "$OPENMONTAGE_REVISION" > /app/third_party/OpenMontage/.anban-source-revision
```

Install the upstream locked requirements and render dependencies using commands documented by the pinned OpenMontage revision. Finish with registry discovery and a pipeline-manifest load check; image build fails if either fails.

- [ ] **Step 5: Add build targets**

```make
MONTAGE_AGENT_IMAGE ?= creator-agent-montage:latest

docker-montage-agent-image:
	@git submodule update --init --recursive third_party/OpenMontage claudecode
	docker build -f Dockerfile.agent-montage \
	  --build-arg OPENMONTAGE_REVISION=$$(git -C third_party/OpenMontage rev-parse HEAD) \
	  -t $(MONTAGE_AGENT_IMAGE) .
```

- [ ] **Step 6: Run contract tests and commit**

```bash
go test ./server/agent -run 'TestDockerRuntimeProfiles|TestMontageRuntimeImageContract' -count=1
git add Dockerfile.agent Dockerfile.agent-montage Makefile server/agent/docker_runtime_contract_test.go
git commit -m "feat(agent): split content and montage images"
```

Expected: PASS.

### Task 2: Materialize a writable OpenMontage root

**Files:**
- Modify: `server/agent/kubernetes_job.go`
- Modify: `server/agent/kubernetes_executor_test.go`

- [ ] **Step 1: Write failing init-script tests**

Add table cases for content, new Montage, matching Montage resume, and mismatched revision. The Montage script contract is:

```sh
set -eu
template=/app/third_party/OpenMontage
runtime=/workspace/openmontage
staging=/workspace/.openmontage-init
if [ ! -e "$runtime" ]; then
  rm -rf "$staging"
  mkdir -p "$staging"
  cp -a "$template/." "$staging/"
  mv "$staging" "$runtime"
fi
test -f "$runtime/.anban-source-revision"
cmp -s "$template/.anban-source-revision" "$runtime/.anban-source-revision"
chown -R 1000:1000 "$runtime"
```

The content case must not reference OpenMontage.

- [ ] **Step 2: Run and verify failure**

```bash
go test ./server/agent -run 'TestWorkspaceInitScript|TestBuildKubernetesJobInitializesMontageRoot' -count=1
```

Expected: FAIL because initialization is currently task-agnostic.

- [ ] **Step 3: Extract a focused script builder**

```go
func kubernetesWorkspaceInitScript(taskType string) string
```

The common prefix fixes `/workspace`, runtime home, and project-memory permissions. Only `model.PlatformMontage` appends copy-once and revision verification.

- [ ] **Step 4: Preserve existing state**

Use a staging directory on the same NAS filesystem, remove only a stale staging path, and rename it into place. If the final runtime exists without a marker or carries a different marker, exit non-zero without modifying it.

- [ ] **Step 5: Run tests and commit**

```bash
go test ./server/agent -run 'TestWorkspaceInitScript|TestBuildKubernetesJob' -count=1
git add server/agent/kubernetes_job.go server/agent/kubernetes_executor_test.go
git commit -m "feat(agent): persist writable montage runtime"
```

Expected: PASS.

### Task 3: Run Montage from the writable root

**Files:**
- Modify: `agent/runner.go`
- Modify: `agent/runner_contract_test.go`
- Modify: `server/agent/runtime_names.go`

- [ ] **Step 1: Write failing runner tests**

Extract and test:

```go
func runtimeCwd(workspace, taskType string) string
func montageRuntimePath(workspace string) string
```

Expected values:

```go
runtimeCwd("/workspace", model.PlatformMontage) == "/workspace/openmontage"
runtimeCwd("/workspace", model.PlatformSeednote) == "/workspace"
montageRuntimePath("/workspace") == "/workspace/openmontage"
```

Capture SDK options and assert Montage receives `ANBAN_MONTAGE_SUBMODULE_PATH=/workspace/openmontage` while other task types do not.

- [ ] **Step 2: Run and verify failure**

```bash
go test ./agent -run 'TestRuntimeCwd|TestRunnerOptionsUseWritableMontageRoot' -count=1
```

Expected: FAIL because all sessions use `r.cfg.Workspace` and the image path.

- [ ] **Step 3: Implement runtime CWD selection**

Use `runtimeCwd` in `claudecode.WithCwd`. Keep `ExecutionResult.WorkDir` and artifact upload rooted at `r.cfg.Workspace` so the uploader scans `/workspace/output` rather than the OpenMontage source tree.

- [ ] **Step 4: Override the Montage path explicitly**

For Montage only, append:

```go
claudecode.WithEnvVar(serveragent.MontageSubmoduleEnvName, montageRuntimePath(r.cfg.Workspace))
```

Platform-owned identity and runtime variables are applied after provider env so provider configuration cannot override the path.

- [ ] **Step 5: Run tests and commit**

```bash
go test ./agent ./server/agent -count=1
git add agent/runner.go agent/runner_contract_test.go server/agent/runtime_names.go
git commit -m "feat(montage): enforce upstream project root"
```

Expected: PASS.

### Task 4: Verify artifact and resume boundaries

**Files:**
- Modify: `agent/artifact_upload_test.go`
- Modify: `server/service/agent_bootstrap_test.go`
- Modify: `server/service/task_execution_complete_test.go`
- Modify: `docs/montage-upgrade.md`

- [ ] **Step 1: Add replacement-Job persistence coverage**

Create a workspace containing:

```text
openmontage/projects/task-1/checkpoint_assets.json
.anban-runtime-home/.claude/projects/session.jsonl
output/final.mp4
```

Run bootstrap/materialization twice and assert checkpoint and session bytes remain unchanged while new resume context is added.

- [ ] **Step 2: Add artifact scanning coverage**

Assert that when `/workspace/output` exists, direct upload includes `output/final.mp4` and excludes `openmontage/`, `.anban-runtime-home/`, and project checkpoints unless the agent explicitly registers them through MCP.

- [ ] **Step 3: Run focused tests**

```bash
go test ./agent -run 'TestArtifactUploader|TestBootstrap' -count=1
go test ./server/service -run 'Test.*Resume|Test.*Execution' -count=1
```

Expected: PASS.

- [ ] **Step 4: Document operations**

Document the two image variables, immutable digest requirement, OpenMontage revision build arg, `/workspace/openmontage` layout, `/tmp` exclusion, PVC deletion behavior, and live verification commands for Pod `imageID` and mounted PVC names.

- [ ] **Step 5: Run full verification and commit**

```bash
go test ./...
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
git diff --check
git add agent server/agent server/service docs/montage-upgrade.md
git commit -m "test(montage): verify resumable workspace contract"
```

Expected: all commands exit 0.

- [ ] **Step 6: Build both images**

```bash
make docker-agent-image
make docker-montage-agent-image
```

Expected: both builds complete and embedded health checks pass. If Docker is unavailable, record it as an unverified deployment prerequisite rather than claiming image verification.
