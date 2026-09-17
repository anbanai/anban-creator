# Server-Owned Go Module Layout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the repository's only Go module and all former `app/` packages under `server/` while preserving Server behavior and root Make targets.

**Architecture:** `server/` becomes the Go module root with module path `github.com/anbanai/anban-creator/server`. The six reusable packages retain their package boundaries under `server/app/`, and the Docker runtime smoke test moves into the Server module while continuing to inspect root-level deployment assets.

**Tech Stack:** Go 1.27, Go modules, GNU Make, GitHub Actions, Docker multi-stage builds.

**Spec:** `docs/superpowers/specs/2026-09-17-server-go-module-layout-design.md`

## Global Constraints

- Use `server/app/*`, not `server/internal/app/*`.
- Keep existing Server package import paths unchanged; only former `github.com/anbanai/anban-creator/app/*` imports change.
- Do not merge former App packages into Server domain packages.
- Do not create a root `go.work` or a second Go module.
- Preserve every unrelated working-tree change and exclude it from migration commits.
- Keep repository-root Make targets as the supported developer interface.
- Do not preserve compatibility aliases for the old App import paths.

---

### Task 1: Lock The Target Module Layout With A Failing Contract Test

**Files:**
- Create: `server/module_layout_contract_test.go`

**Interfaces:**
- Consumes: repository filesystem layout and the contents of `server/go.mod`, `Makefile`, `.github/workflows/ci.yml`, and `deploy/docker/Dockerfile.server`.
- Produces: `TestServerOwnsGoModuleLayout` and `TestServerModuleToolingTargetsServerRoot`, which define the migration's structural acceptance criteria.

- [ ] **Step 1: Write the failing layout contract test**

Create `server/module_layout_contract_test.go` in package `main` with tests that:

```go
package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func moduleLayoutRepoRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve module layout test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), ".."))
}

func moduleLayoutRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestServerOwnsGoModuleLayout(t *testing.T) {
	root := moduleLayoutRepoRoot(t)
	for _, path := range []string{"app", "go.mod", "go.sum"} {
		if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Errorf("repository root %s must not exist after Server module migration", path)
		}
	}
	for _, path := range []string{"server/app", "server/go.mod", "server/go.sum"} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Errorf("required Server module path %s: %v", path, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "server", "go.mod"))
	if err == nil && !strings.HasPrefix(string(data), "module github.com/anbanai/anban-creator/server\n") {
		t.Errorf("server/go.mod has unexpected module declaration")
	}

	legacyImport := "github.com/anbanai/anban-creator/" + "app/"
	err = filepath.WalkDir(filepath.Join(root, "server"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		if strings.Contains(moduleLayoutRead(t, path), legacyImport) {
			t.Errorf("%s still imports the old App package path", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestServerModuleToolingTargetsServerRoot(t *testing.T) {
	root := moduleLayoutRepoRoot(t)
	cases := []struct {
		path     string
		required []string
	}{
		{
			path: "Makefile",
			required: []string{
				"go -C server test -v ./...",
				"go -C server fmt ./...",
				"go -C server vet ./...",
				"go -C server build -o ../$(BINDIR)/$(BINARY) .",
			},
		},
		{
			path: ".github/workflows/ci.yml",
			required: []string{
				"cache-dependency-path: server/go.sum",
				"go -C server test -race ./...",
			},
		},
		{
			path: "deploy/docker/Dockerfile.server",
			required: []string{
				"WORKDIR /build/server",
				"COPY server/go.mod server/go.sum ./",
				"COPY server ./",
				`go build -ldflags="-s -w" -o /anban-creator-server .`,
			},
		},
	}
	for _, tc := range cases {
		body := moduleLayoutRead(t, filepath.Join(root, tc.path))
		for _, required := range tc.required {
			if !strings.Contains(body, required) {
				t.Errorf("%s missing %q", tc.path, required)
			}
		}
	}
}
```

- [ ] **Step 2: Run the focused test and verify the expected failure**

Run:

```bash
go test ./server -run 'TestServer(OwnsGoModuleLayout|ModuleToolingTargetsServerRoot)' -count=1
```

Expected: FAIL because root `app/`, `go.mod`, and `go.sum` still exist and the
new Server module files and tooling strings do not exist yet.

---

### Task 2: Move The Go Module And App Packages

**Files:**
- Move: `app/` -> `server/app/`
- Move: `go.mod` -> `server/go.mod`
- Move: `go.sum` -> `server/go.sum`
- Move: `deploy/docker/runtime-smoke_test.go` -> `server/runtime_smoke_test.go`
- Modify: `server/go.mod`
- Modify: `server/runtime_smoke_test.go`
- Modify: `server/app/draft/service.go`, `server/app/image/gemini.go`, `server/app/image/openai.go`, `server/app/image/openai_test.go`, `server/app/image/processor.go`, `server/app/image/processor_test.go`, `server/app/image/provider.go`, `server/app/image/provider_test.go`, `server/app/image/volcengine.go`, `server/app/image/volcengine_test.go`, and `server/app/wechat/service.go`
- Modify: `server/agent/config_builder.go`, `server/agent/config_builder_test.go`, `server/agent/executor.go`, `server/agent/executor_test.go`, `server/agent/reference_asset_materialize_unix_test.go`, and `server/agent/resume_contract.go`
- Modify: `server/config/config.go`, `server/handler/resource.go`, `server/mcp/publishing_tools.go`, `server/mcp/publishing_tools_test.go`, `server/mcp/tools_test.go`, `server/platform/wechat_analytics.go`, `server/platform/wechat_analytics_detail_test.go`, and `server/resolver/resolve.go`
- Modify: `server/service/channel_test.go`, `server/service/content_render.go`, `server/service/image.go`, `server/service/image_provider_cost_test.go`, `server/service/image_test.go`, `server/service/plan_test.go`, `server/service/publishing.go`, `server/service/render_template.go`, `server/service/task_artifact_upload_test.go`, `server/service/task_execution_complete.go`, `server/service/task_execution_complete_test.go`, `server/service/task_image.go`, `server/service/task_image_operations.go`, `server/service/task_image_test.go`, `server/service/task_publication_recovery_test.go`, `server/service/wechat_publication.go`, `server/service/wechat_publication_test.go`, `server/service/wechat_tracking.go`, and `server/service/wechat_tracking_test.go`

**Interfaces:**
- Consumes: the package APIs currently exported by `app/config`, `app/converter`, `app/draft`, `app/image`, `app/wechat`, and `app/writer`.
- Produces: behavior-identical packages at `github.com/anbanai/anban-creator/server/app/{config,converter,draft,image,wechat,writer}` and a complete Server module rooted at `server/`.

- [ ] **Step 1: Move tracked files without editing package bodies**

Run:

```bash
mkdir -p server/app
git mv app/config app/converter app/draft app/image app/wechat app/writer server/app/
rmdir app
git mv go.mod server/go.mod
git mv go.sum server/go.sum
git mv deploy/docker/runtime-smoke_test.go server/runtime_smoke_test.go
```

- [ ] **Step 2: Change the module declaration and former App imports**

Change the first line of `server/go.mod` to:

```go
module github.com/anbanai/anban-creator/server
```

Mechanically replace all Go imports beginning with:

```text
github.com/anbanai/anban-creator/app/
```

with:

```text
github.com/anbanai/anban-creator/server/app/
```

Do not rewrite existing `github.com/anbanai/anban-creator/server/*` imports.

- [ ] **Step 3: Adapt the moved runtime smoke test to its new location**

In `server/runtime_smoke_test.go`, change `package docker` to `package main` and
change `runtimeSmokeRepoRoot` to return one parent directory from the test file:

```go
return filepath.Clean(filepath.Join(filepath.Dir(filename), ".."))
```

Keep all reads of `deploy/docker/*` unchanged relative to the computed root.

- [ ] **Step 4: Format moved and import-edited Go sources**

Run:

```bash
gofmt -w server/app server/runtime_smoke_test.go server/module_layout_contract_test.go
```

- [ ] **Step 5: Run package-level tests from the new module root**

Run:

```bash
go -C server test ./app/... ./config ./agent ./resolver ./platform ./service ./mcp
```

Expected: package compilation reaches tooling contract assertions; no package
reports an unresolved old App import.

---

### Task 3: Retarget Build, CI, Docker, And Module Contract Tests

**Files:**
- Modify: `Makefile`
- Modify: `.github/workflows/ci.yml`
- Modify: `.github/workflows/release.yml`
- Modify: `deploy/docker/Dockerfile.server`
- Modify: `server/agent/docker_runtime_contract_test.go`
- Modify: `server/agent/sdk_fork_contract_test.go`
- Test: `server/module_layout_contract_test.go`

**Interfaces:**
- Consumes: the Server module root and import paths produced by Task 2.
- Produces: root Make commands, CI jobs, release builds, Docker builds, and contract tests that all operate on `server/`.

- [ ] **Step 1: Update existing contract-test expectations before production tooling**

Change `server/agent/sdk_fork_contract_test.go` to read
`server/go.mod`. Change the Server Docker contract in
`server/agent/docker_runtime_contract_test.go` to require these build-stage
fragments:

```text
WORKDIR /build/server
COPY server/go.mod server/go.sum ./
COPY server ./
go build -ldflags="-s -w" -o /anban-creator-server .
```

- [ ] **Step 2: Run the focused tooling tests and verify failure**

Run:

```bash
go -C server test ./agent . -run 'Test.*(Docker|GoModule|ModuleLayout|ModuleTooling|SDK).*' -count=1
```

Expected: FAIL because the Makefile, workflows, and Dockerfile still target the
old root module.

- [ ] **Step 3: Retarget root Make commands**

Use `go -C server` for test, fmt, vet, dependency, coverage, Agent Pack
commands, Server build, and Server test targets. Run golangci-lint with
`cd server && golangci-lint run ./...`. Build the binary to the repository-level
path with:

```make
go -C server build -o ../$(BINDIR)/$(BINARY) .
```

- [ ] **Step 4: Retarget GitHub Actions**

Set `cache-dependency-path: server/go.sum` on Go setup steps. Run Go dependency,
build, vet, test, and release commands with `go -C server`. Keep release
artifacts in the existing repository-level `bin/` directory by using `../bin/`
output paths from the Server module.

- [ ] **Step 5: Retarget the Server Docker build**

Use this builder layout in `deploy/docker/Dockerfile.server`:

```dockerfile
WORKDIR /build/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server ./
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /anban-creator-server .
```

Update final-stage billing copies from `/build/server/billing/*`; runtime paths
and the image entry command remain unchanged.

- [ ] **Step 6: Run focused tooling contracts**

Run:

```bash
go -C server test ./agent . -run 'Test.*(Docker|GoModule|ModuleLayout|ModuleTooling|SDK).*' -count=1
```

Expected: PASS.

---

### Task 4: Update Active Documentation And Normalize Dependencies

**Files:**
- Modify: `AGENTS.md`
- Modify: `CLAUDE.md`
- Modify: `README.md`
- Modify: comments in `server/model/project.go`, `server/config/config.go`, `server/service/image.go`, `server/agent/config_builder.go`, `server/resolver/resolve.go`, and `server/resources/themes/*.yaml` that name the old root App layout
- Modify: `server/go.mod`
- Modify: `server/go.sum`

**Interfaces:**
- Consumes: the completed module layout and root Make interface.
- Produces: accurate developer instructions and a dependency graph generated from the relocated Server module.

- [ ] **Step 1: Replace active layout and command documentation**

Describe `server/app/` as Server-owned reusable packages. Change direct commands
from root-scoped `go test ./...` and `go build ... ./server` forms to
`cd server && go test ./...` and `cd server && go build -o /tmp/anban-creator-server .`.
Keep Make command examples unchanged where their target name does not change.

- [ ] **Step 2: Update source and resource comments**

Replace references such as `app/converter`, `app/image`, and `app/writer` with
their `server/app/*` paths. Do not alter container paths such as `/app/data` or
Codex configuration paths such as `~/.codex`.

- [ ] **Step 3: Tidy the relocated module**

Run:

```bash
go -C server mod tidy
```

- [ ] **Step 4: Verify old package and layout references are gone**

Run:

```bash
rg -n 'github\.com/anbanai/anban-creator/app/' server
rg -n '(^|[^[:alnum:]_.-])app/' AGENTS.md CLAUDE.md README.md server --glob '!server/app/**'
```

Expected: the first search returns no matches; the second returns only
intentional container paths or `server/app/*` documentation, with no stale root
App package references.

---

### Task 5: Full Verification, Scope Review, And Main Integration

**Files:**
- Verify: all migration paths from Tasks 1-4
- Preserve: every pre-existing unrelated modification shown by the baseline `git status --short`

**Interfaces:**
- Consumes: the completed migration.
- Produces: a verified migration commit directly on `main`; no merge commit is needed because execution starts on `main`.

- [ ] **Step 1: Run complete Go tests**

Run:

```bash
go -C server test ./...
```

Expected: PASS with zero failing packages.

- [ ] **Step 2: Run vet and build**

Run:

```bash
go -C server vet ./...
go -C server build -o /tmp/anban-creator-server .
```

Expected: both commands exit 0.

- [ ] **Step 3: Run repository contract targets**

Run:

```bash
make agent-pack-check
make docker-runtime-smoke
```

Expected: Agent Pack generation is clean; Docker smoke passes or reports its
existing explicit Docker-CLI skip behavior.

- [ ] **Step 4: Verify repository structure and diff hygiene**

Run:

```bash
test ! -e app
test ! -e go.mod
test ! -e go.sum
test -d server/app
test -f server/go.mod
test -f server/go.sum
git diff --check
git status --short
```

Compare the final status with the recorded baseline. Confirm unrelated Server
lifecycle files remain modified or untracked exactly as user-owned work and are
not staged.

- [ ] **Step 5: Stage only migration-owned paths and commit on main**

Stage the plan, module moves, former App files, runtime smoke test move, exact
import-edited files, tooling files, documentation files, contract tests, and
comment-only resource files by explicit path. Do not use `git add server` or
`git add -A`.

`server/service/task_artifact_upload_test.go` overlaps pre-existing user work.
Construct its staged blob from `HEAD` with only the old App import replaced,
then stage that blob with `git update-index --cacheinfo`; do not stage the
working-tree version of that file. Verify its staged diff contains only the
single import-path change before committing.

Commit with:

```bash
git commit -m "refactor: make server the Go module root"
```

- [ ] **Step 6: Verify the commit and branch state**

Run:

```bash
git branch --show-current
git show --stat --oneline --summary HEAD
git diff HEAD^ --check
git status --short
```

Expected: branch is `main`; the migration commit contains no unrelated
lifecycle work; remaining dirty files are the same user-owned files recorded
before implementation.
