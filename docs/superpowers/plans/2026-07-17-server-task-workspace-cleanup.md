# Server Task Workspace Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the legacy workspace archive contract so every managed task writes and delivers one canonical `output/` artifact tree owned by the server task lifecycle.

**Architecture:** `WorkspaceService` becomes a stateless resolver that accepts only managed requests with both `content_type` and `task_id`, and the MCP server exposes only `prepare_workspace`. Shipped workflows validate and report `$DIR` without moving files; task execution IDs, `task_files`, and OSS remain the only persistence and versioning boundary.

**Tech Stack:** Go 1.24, MCP Go SDK, Bash/Python hook scripts, Claude Code/Codex/OpenClaw plugin assets, TypeScript, Bun.

---

## File Map

- `server/service/workspace.go`: managed task workspace path contract only.
- `server/service/workspace_test.go`: required task identity and canonical output tests.
- `server/mcp/workspace_tools.go`: `prepare_workspace` registration and handler only.
- `server/mcp/workspace_tools_test.go`: MCP schema, removal, and taskless request tests.
- `server/main.go`: stateless workspace service construction.
- `server/agent/runtime_policy.go`: managed required-tool list without archive.
- `server/agent/managed_hooks_test.go`: canonical `output/` quality-gate behavior.
- `server/agent/server_workspace_contract_test.go`: cross-distribution forward-only contract.
- `server/mcp/seednote_hook_test.go`: Seednote finalization without archive ownership.
- `claudecode/hooks/seednote-quality-gate.sh`, `codex/hooks/seednote-quality-gate.sh`: inspect canonical `output/` only.
- `claudecode/scripts/archive-seednote-workspace.sh`: delete obsolete implementation.
- `claudecode/agents/*.md`, `codex/agents/*.toml`: retain deliverables in `$DIR`.
- `claudecode/skills/*/SKILL.md`, `openclaw/skills/*/SKILL.md`, `codex/skills/*/SKILL.md`: synchronized server-task-only workflow contract.
- `claudecode/README.md`, `openclaw/README.md`, `codex/install/agents-registration.toml`: describe delivery instead of a local archive phase.
- `claudecode/hooks/hooks.json`, `codex/hooks/hooks.json`: completion checks without archive directories.
- `openclaw/src/hooks/handler.ts`: remove archive result summarization.
- Plugin manifests and `claudecode/CHANGELOG.md`: patch release metadata.

### Task 1: Make WorkspaceService Managed-Task Only

**Files:**
- Create: `server/service/workspace_test.go`
- Modify: `server/service/workspace.go`
- Modify: `server/main.go:284`

- [ ] **Step 1: Write the failing service tests**

```go
package service

import (
	"strings"
	"testing"
)

func TestWorkspacePrepareRequiresManagedTaskIdentity(t *testing.T) {
	svc := NewWorkspaceService()
	tests := []struct {
		name, contentType, taskID, wantError string
	}{
		{name: "missing content type", taskID: "task-1", wantError: "content_type is required"},
		{name: "missing task id", contentType: "seednote", wantError: "task_id is required"},
		{name: "blank task id", contentType: "seednote", taskID: "  ", wantError: "task_id is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Prepare(tt.contentType, tt.taskID)
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("Prepare error = %v, want %q", err, tt.wantError)
			}
		})
	}
}

func TestWorkspacePrepareReturnsCanonicalTaskOutput(t *testing.T) {
	result, err := NewWorkspaceService().Prepare("seednote", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != "output" {
		t.Fatalf("path = %q, want output", result.Path)
	}
}
```

- [ ] **Step 2: Run the service test and confirm RED**

Run: `go test ./server/service -run '^TestWorkspacePrepare' -count=1`

Expected: FAIL because `NewWorkspaceService` still requires two constructor arguments and taskless preparation still has a local fallback.

- [ ] **Step 3: Replace the service with the minimal contract**

```go
package service

import (
	"fmt"
	"strings"
)

type PrepareResult struct {
	Path string `json:"path"`
}

type WorkspaceService struct{}

func NewWorkspaceService() *WorkspaceService { return &WorkspaceService{} }

func (s *WorkspaceService) Prepare(contentType, taskID string) (*PrepareResult, error) {
	if strings.TrimSpace(contentType) == "" {
		return nil, fmt.Errorf("content_type is required")
	}
	if strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("task_id is required")
	}
	return &PrepareResult{Path: "output"}, nil
}
```

Delete `ArchiveResult`, `WorkspaceService.Archive`, archive path generation, local fallback fields, and archive-only sanitization. Change server construction to:

```go
workspaceSvc := service.NewWorkspaceService()
```

- [ ] **Step 4: Format and verify GREEN**

Run: `gofmt -w server/service/workspace.go server/service/workspace_test.go server/main.go`

Run: `go test ./server/service -run '^TestWorkspacePrepare' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit the service contract**

```bash
git add server/service/workspace.go server/service/workspace_test.go server/main.go
git commit -m "refactor(server): make workspace task scoped"
```

### Task 2: Remove archive_workspace From MCP and Runtime Readiness

**Files:**
- Create: `server/mcp/workspace_tools_test.go`
- Modify: `server/mcp/workspace_tools.go`
- Modify: `server/mcp/mcp_test.go:315-328`
- Modify: `server/agent/runtime_policy_test.go`
- Modify: `server/agent/runtime_policy.go:275-289`

- [ ] **Step 1: Write the failing MCP tests**

```go
package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/service"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestWorkspaceToolSurfaceIsManagedTaskOnly(t *testing.T) {
	tools := listMCPToolsForTest(t, NewMCPHandler(nil, "test-key", nil))
	foundPrepare := false
	for _, raw := range tools {
		tool := raw.(map[string]any)
		name, _ := tool["name"].(string)
		if name == "archive_workspace" {
			t.Fatal("archive_workspace must not be registered")
		}
		if name != "prepare_workspace" {
			continue
		}
		foundPrepare = true
		schema := tool["inputSchema"].(map[string]any)
		required := schema["required"].([]any)
		got := make([]string, 0, len(required))
		for _, item := range required {
			got = append(got, item.(string))
		}
		if strings.Join(got, ",") != "content_type,task_id" {
			t.Fatalf("required = %v", required)
		}
	}
	if !foundPrepare {
		t.Fatal("prepare_workspace is not registered")
	}
}

func TestPrepareWorkspaceHandlerRejectsTasklessRequest(t *testing.T) {
	old := svcs
	svcs = &Services{WorkspaceSvc: service.NewWorkspaceService()}
	t.Cleanup(func() { svcs = old })
	result, err := prepareWorkspaceHandler(context.Background(), &mcpsdk.CallToolRequest{
		Params: &mcpsdk.CallToolParamsRaw{Arguments: json.RawMessage(`{"content_type":"seednote"}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("result = %#v, want tool error", result)
	}
	text := result.Content[0].(*mcpsdk.TextContent).Text
	if !strings.Contains(text, "task_id is required") {
		t.Fatalf("text = %q", text)
	}
}
```

Add `archive_workspace` to the removed-tool assertion in `server/mcp/mcp_test.go`.

- [ ] **Step 2: Write the failing runtime-policy test**

```go
func TestManagedRequiredMCPToolsExcludeArchiveWorkspace(t *testing.T) {
	for _, tool := range managedRequiredMCPTools("seednote") {
		if tool == "archive_workspace" {
			t.Fatal("managed runtime still requires removed archive_workspace tool")
		}
	}
}
```

- [ ] **Step 3: Run targeted tests and confirm RED**

Run: `go test ./server/mcp ./server/agent -run 'TestWorkspaceToolSurface|TestPrepareWorkspaceHandlerRejectsTaskless|TestManagedRequiredMCPToolsExcludeArchive' -count=1`

Expected: FAIL because archive remains registered and required, and the MCP schema does not require `task_id`.

- [ ] **Step 4: Keep only the managed prepare tool**

```go
server.AddTool(&mcp.Tool{
	Name:        "prepare_workspace",
	Description: "Returns the canonical task-relative output directory. Managed server tasks must provide task_id; the agent creates the returned directory locally and leaves deliverables in place for task artifact collection.",
	InputSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content_type": map[string]any{"type": "string", "description": "Managed workflow content type"},
			"task_id":      map[string]any{"type": "string", "description": "Required managed task ID"},
		},
		"required": []any{"content_type", "task_id"},
	},
}, prepareWorkspaceHandler)
```

Delete the archive registration and `archiveWorkspaceHandler`. Remove only `"archive_workspace"` from `managedRequiredMCPTools("seednote")`.

- [ ] **Step 5: Format and verify GREEN**

Run: `gofmt -w server/mcp/workspace_tools.go server/mcp/workspace_tools_test.go server/mcp/mcp_test.go server/agent/runtime_policy.go server/agent/runtime_policy_test.go`

Run: `go test ./server/mcp ./server/agent -run 'TestWorkspaceToolSurface|TestPrepareWorkspaceHandlerRejectsTaskless|TestManagedRequiredMCPToolsExcludeArchive|TestValidateManagedMCPStatus' -count=1`

Expected: PASS.

- [ ] **Step 6: Commit the tool-surface removal**

```bash
git add server/mcp/workspace_tools.go server/mcp/workspace_tools_test.go server/mcp/mcp_test.go server/agent/runtime_policy.go server/agent/runtime_policy_test.go
git commit -m "refactor(mcp): remove workspace archive tool"
```

### Task 3: Make the Seednote Gate Validate Canonical Output

**Files:**
- Modify: `server/agent/managed_hooks_test.go`
- Delete: `server/agent/seednote_archive_script_test.go`
- Modify: `claudecode/hooks/seednote-quality-gate.sh`
- Modify: `codex/hooks/seednote-quality-gate.sh`
- Delete: `claudecode/scripts/archive-seednote-workspace.sh`

- [ ] **Step 1: Replace the archive gate test**

First change the shared fixture to write directly into the managed task output:

```go
func writeSeednoteGateFixture(t *testing.T, workspace string, passed bool) string {
	t.Helper()
	dir := filepath.Join(workspace, "output")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"content.md",
		"request-analysis.json",
		"request-analysis.md",
		"reference-analysis.json",
		"reference-analysis.md",
		"image-prompts.md",
		"image-review.md",
		"cover.png",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fixture"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "image-plan.md"), []byte("计划图片数量: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	summary, err := json.Marshal(map[string]any{
		"version": "1.0",
		"outputs": []any{map[string]any{
			"file_name":    "cover.png",
			"verification": map[string]any{"passed": passed, "score": "high"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "reference-usage-summary.json"), summary, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}
```

This makes all existing quality-gate tests exercise the canonical directory instead of silently retaining the old `output/seednote/title` contract. Then replace `TestSeednoteQualityGateBlocksUnarchivedManagedMainSuccess` with:

```go
func TestSeednoteQualityGateAcceptsCanonicalManagedOutput(t *testing.T) {
	workspace := t.TempDir()
	writeSeednoteGateFixture(t, workspace, true)
	if output := strings.TrimSpace(runSeednoteQualityGate(t, workspace)); output != "" {
		t.Fatalf("canonical output was blocked: %s", output)
	}
}

func TestSeednoteArchiveScriptIsRemoved(t *testing.T) {
	path := filepath.Join(repoRoot(t), "claudecode", "scripts", "archive-seednote-workspace.sh")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("obsolete archive script still exists: %v", err)
	}
}
```

- [ ] **Step 2: Run hook tests and confirm RED**

Run: `go test ./server/agent -run 'TestSeednoteQualityGateAcceptsCanonicalManagedOutput|TestSeednoteArchiveScriptIsRemoved' -count=1`

Expected: FAIL because canonical output is blocked and the archive script exists.

- [ ] **Step 3: Simplify both quality-gate copies**

Remove archive directory globbing, `data/workspace` fallback scanning, and the `managed_main_session` archive condition. Keep only `output/failure-state.json` and the canonical directory:

```python
seednote_dir = root / "output"
if not seednote_dir.is_dir():
    block(
        "种子笔记机械闸门：未找到任务输出目录 output/。\n"
        "请确认 prepare_workspace(content_type=\"seednote\", task_id=$TASK_ID) 已执行并创建目录。"
    )
    sys.exit(0)
```

Keep required artifact, image-count, and vision-summary checks. Change remediation from “补齐…和归档” to “补齐规划与 generate_image 原子视觉核验，并将产物保留在 output/”.

- [ ] **Step 4: Delete obsolete implementation and tests**

Delete:

```text
claudecode/scripts/archive-seednote-workspace.sh
server/agent/seednote_archive_script_test.go
```

- [ ] **Step 5: Verify mirrors and GREEN**

Run: `cmp claudecode/hooks/seednote-quality-gate.sh codex/hooks/seednote-quality-gate.sh`

Run: `go test ./server/agent -run 'TestSeednoteQualityGate|TestSeednoteArchiveScriptIsRemoved' -count=1`

Expected: PASS.

- [ ] **Step 6: Commit explicit paths only**

```bash
git -C claudecode add hooks/seednote-quality-gate.sh scripts/archive-seednote-workspace.sh
git -C claudecode commit -m "refactor(seednote): remove workspace archive gate"
git -C codex add hooks/seednote-quality-gate.sh
git -C codex commit -m "refactor(seednote): validate canonical task output"
git add server/agent/managed_hooks_test.go server/agent/seednote_archive_script_test.go claudecode codex
git commit -m "test(agent): enforce canonical seednote output"
```

Do not stage the pre-existing user-owned modified files under `claudecode/docs/`.

### Task 4: Remove Archive Semantics From Shipped Workflows

**Files:**
- Create: `server/agent/server_workspace_contract_test.go`
- Modify: `server/agent/moments_contract_test.go`
- Modify: `server/agent/seednote_skill_contract_test.go`
- Modify: `server/mcp/seednote_hook_test.go`
- Modify: `claudecode/agents/{seednote,ecommerce,moments,wechatarticle}.md`
- Modify: `claudecode/skills/{seednote,ecommerce,article}/SKILL.md`
- Modify: `claudecode/skills/{seednote-visual-design,ecommerce-visual-design,ecommerce-platform-specs}/SKILL.md`
- Modify: `claudecode/skills/seednote/references/examples.md`
- Modify: `claudecode/hooks/hooks.json`
- Modify: `codex/agents/{seednote,ecommerce,moments,wechatarticle}.toml`
- Modify: `codex/skills/{seednote,ecommerce,article}/SKILL.md`
- Modify: `codex/skills/{seednote-visual-design,ecommerce-visual-design,ecommerce-platform-specs}/SKILL.md`
- Modify: `codex/skills/seednote/references/examples.md`
- Modify: `codex/hooks/hooks.json`
- Modify: `openclaw/skills/{seednote,ecommerce,article}/SKILL.md`
- Modify: `openclaw/skills/{seednote-visual-design,ecommerce-visual-design,ecommerce-platform-specs}/SKILL.md`
- Modify: `openclaw/skills/seednote/references/examples.md`
- Modify: `openclaw/src/hooks/handler.ts`
- Modify: `claudecode/README.md`
- Modify: `openclaw/README.md`
- Modify: `claudecode/docs/plugin-development.md`
- Modify: `codex/CODEX.md`
- Modify: `codex/install/agents-registration.toml`

- [ ] **Step 1: Add the failing cross-distribution contract**

```go
package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShippedWorkflowsUseCanonicalServerTaskOutput(t *testing.T) {
	root := repositoryRoot(t)
	paths := []string{
		"claudecode/agents/seednote.md", "claudecode/agents/ecommerce.md", "claudecode/agents/moments.md", "claudecode/agents/wechatarticle.md",
		"claudecode/skills/seednote/SKILL.md", "claudecode/skills/ecommerce/SKILL.md", "claudecode/skills/article/SKILL.md",
		"claudecode/skills/seednote-visual-design/SKILL.md", "claudecode/skills/ecommerce-visual-design/SKILL.md", "claudecode/skills/ecommerce-platform-specs/SKILL.md",
		"claudecode/skills/seednote/references/examples.md", "claudecode/README.md",
		"codex/agents/seednote.toml", "codex/agents/ecommerce.toml", "codex/agents/moments.toml", "codex/agents/wechatarticle.toml",
		"codex/skills/seednote/SKILL.md", "codex/skills/ecommerce/SKILL.md", "codex/skills/article/SKILL.md",
		"codex/skills/seednote-visual-design/SKILL.md", "codex/skills/ecommerce-visual-design/SKILL.md", "codex/skills/ecommerce-platform-specs/SKILL.md",
		"codex/skills/seednote/references/examples.md", "codex/install/agents-registration.toml",
		"openclaw/skills/seednote/SKILL.md", "openclaw/skills/ecommerce/SKILL.md", "openclaw/skills/article/SKILL.md",
		"openclaw/skills/seednote-visual-design/SKILL.md", "openclaw/skills/ecommerce-visual-design/SKILL.md", "openclaw/skills/ecommerce-platform-specs/SKILL.md",
		"openclaw/skills/seednote/references/examples.md", "openclaw/README.md",
		"claudecode/hooks/hooks.json", "codex/hooks/hooks.json", "codex/CODEX.md", "openclaw/src/hooks/handler.ts",
	}
	forbidden := []string{
		"archive_workspace", "ARCHIVE_DIR", "archive-seednote-workspace.sh", `mv "$DIR"/*`,
		"output/seednote/{标题}", "output/ecommerce/{产品名}", "归档全链路", "归档前", "归档目录",
		"成功归档", "归档整理", "归档与", "与归档", "归档交付", "→ 归档",
	}
	for _, rel := range paths {
		body := readRepoFile(t, filepath.Join(root, rel))
		for _, term := range forbidden {
			if strings.Contains(body, term) {
				t.Errorf("%s still contains removed workspace archive term %q", rel, term)
			}
		}
	}
}

func TestArchiveWorkspaceImplementationIsAbsent(t *testing.T) {
	path := filepath.Join(repositoryRoot(t), "claudecode", "scripts", "archive-seednote-workspace.sh")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("removed archive implementation still exists: %v", err)
	}
}
```

Update Moments tests so `prepare_workspace` and `$DIR` remain required while archive becomes forbidden. Move `archive_workspace` from the Seednote required list to its forbidden list.

- [ ] **Step 2: Replace archive validation in Seednote finalization tests**

Delete `validateSeednoteArchiveContract` and its mutation case. Remove archive from title-finalization downstream requirements. Add:

```go
func validateSeednoteCanonicalDeliveryContract(body string) error {
	for _, forbidden := range []string{"archive_workspace", "ARCHIVE_DIR", "archive-seednote-workspace.sh"} {
		if strings.Contains(body, forbidden) {
			return fmt.Errorf("seednote still contains removed archive contract %q", forbidden)
		}
	}
	if !strings.Contains(body, "成果目录（`$DIR`）") {
		return fmt.Errorf("seednote final report must deliver the canonical $DIR")
	}
	return nil
}
```

Use this validator in the ownership and mutation tests. Keep title deduplication, failure-state, template-save, and feedback ownership assertions.

- [ ] **Step 3: Run contract tests and confirm RED**

Run: `go test ./server/agent ./server/mcp -run 'TestShippedWorkflowsUseCanonicalServerTaskOutput|TestArchiveWorkspaceImplementationIsAbsent|TestMomentsAgentAndSkillContracts|TestSeednoteAgentsTreatImageFailures|TestSeednoteFinalizationOwnership' -count=1`

Expected: FAIL across current workflow, hook, and OpenClaw archive references.

- [ ] **Step 4: Rewrite completion around `$DIR`**

For every affected Agent/Skill:

```text
- Remove archive_workspace from tool lists and path-only explanations.
- Remove title/product-derived archive paths, archive scripts, and mv commands.
- Keep prepare_workspace(..., task_id=$TASK_ID) as the only workspace tool.
- Rename archive stages to delivery validation or merge them into final reporting.
- Validate required files directly under $DIR.
- Report $DIR as the result directory.
- Remove resolved failure-state.json immediately before successful delivery where already required.
- Read Seednote template inputs from $DIR without an archive-success condition.
- Preserve title finalization before image generation and all recoverable failure semantics.
- Rename legacy “归档前清理/归档全链路” wording in mirrored visual skills and examples to “交付前清理/交付全链路”; do not rewrite unrelated evidence retention or local download semantics.
```

Include this canonical statement in the owning workflows:

```text
所有产物保留在 prepare_workspace 返回的 `$DIR`。不得在任务完成前移动、复制或按标题重命名产物目录；服务端 task_files、execution_id 与 OSS 负责持久化和版本边界。
```

Apply these exact terminology replacements in the mirrored supporting skills and overview files:

```text
归档前清理目录        -> 交付前清理目录
归档全链路            -> 交付全链路
归档交付              -> 交付
合规与归档            -> 合规与交付
归档与最终报告        -> 交付校验与最终报告
归档工作目录          -> 校验任务成果目录
成果目录 $ARCHIVE_DIR -> 成果目录 $DIR
→ 归档                -> → 交付
```

For Seednote, template saving must read `$DIR/viral-template.json` and `$DIR/template-meta.json` after delivery validation, with no `ARCHIVE_SUCCEEDED` condition. For Ecommerce and Moments, generate the manifest or final summary in `$DIR`, validate it there, then report `$DIR`. Article and WeChat workflow documentation must describe only `prepare_workspace`; they must not imply a second workspace-management tool.

- [ ] **Step 5: Remove archive-only hooks and OpenClaw summaries**

Update hook prompts to inspect only the working directory. Reduce OpenClaw tool summarization to publication:

```ts
function summarizeToolResult(
  toolName: string,
  input: Record<string, any>,
  output: any
): string | null {
  switch (toolName) {
    case "publish_draft":
    case "draft":
      return summarizePublish(input, output)
    default:
      return null
  }
}
```

Delete `summarizeArchive` and helpers reachable only from archive delivery summaries. Keep publish summary behavior unchanged.

- [ ] **Step 6: Update supported-tool documentation**

Remove archive references from tracked `claudecode/docs/plugin-development.md`, `codex/CODEX.md`, `claudecode/README.md`, `openclaw/README.md`, and `codex/install/agents-registration.toml`. Describe the last workflow phase as canonical task delivery. Do not edit or stage the user-owned files currently modified under `claudecode/docs/`.

- [ ] **Step 7: Verify GREEN and TypeScript**

Run: `go test ./server/agent ./server/mcp -run 'TestShippedWorkflowsUseCanonicalServerTaskOutput|TestArchiveWorkspaceImplementationIsAbsent|TestMomentsAgentAndSkillContracts|TestSeednoteAgentsTreatImageFailures|TestSeednoteFinalizationOwnership' -count=1`

Run: `cd openclaw && bun run lint`

Expected: PASS.

- [ ] **Step 8: Commit distributions, then root gitlinks**

```bash
git -C claudecode add README.md agents/seednote.md agents/ecommerce.md agents/moments.md agents/wechatarticle.md skills/seednote/SKILL.md skills/seednote/references/examples.md skills/seednote-visual-design/SKILL.md skills/ecommerce/SKILL.md skills/ecommerce-visual-design/SKILL.md skills/ecommerce-platform-specs/SKILL.md skills/article/SKILL.md hooks/hooks.json docs/plugin-development.md
git -C claudecode commit -m "refactor(workflows): keep task artifacts in canonical output"
git -C codex add agents/seednote.toml agents/ecommerce.toml agents/moments.toml agents/wechatarticle.toml skills/seednote/SKILL.md skills/seednote/references/examples.md skills/seednote-visual-design/SKILL.md skills/ecommerce/SKILL.md skills/ecommerce-visual-design/SKILL.md skills/ecommerce-platform-specs/SKILL.md skills/article/SKILL.md hooks/hooks.json install/agents-registration.toml CODEX.md
git -C codex commit -m "refactor(workflows): remove workspace archive steps"
git -C openclaw add README.md skills/seednote/SKILL.md skills/seednote/references/examples.md skills/seednote-visual-design/SKILL.md skills/ecommerce/SKILL.md skills/ecommerce-visual-design/SKILL.md skills/ecommerce-platform-specs/SKILL.md skills/article/SKILL.md src/hooks/handler.ts
git -C openclaw commit -m "refactor(workflows): use server task artifact delivery"
git add server/agent/server_workspace_contract_test.go server/agent/moments_contract_test.go server/agent/seednote_skill_contract_test.go server/mcp/seednote_hook_test.go claudecode codex openclaw
git commit -m "refactor(agent): remove legacy archive workflow contract"
```

### Task 5: Version the Plugin Distribution Change

**Files:**
- Modify: `claudecode/.claude-plugin/plugin.json`
- Modify: `claudecode/CHANGELOG.md`
- Modify: `codex/.codex-plugin/plugin.json`
- Modify: `openclaw/openclaw.plugin.json`

- [ ] **Step 1: Bump current patch versions**

```text
claudecode: 2.10.63 -> 2.10.64
codex:      2.10.56 -> 2.10.57
openclaw:   2.7.48  -> 2.7.49
```

If a manifest advances before execution, increment its then-current version by one patch instead of reusing or lowering a release.

- [ ] **Step 2: Add the Claude Code changelog entry**

```markdown
## [2.10.64] - 2026-07-17

### Changed

- Removed the legacy workspace archive tool and kept managed task deliverables in the canonical `output/` directory for server-side artifact collection.
- Required `task_id` for workspace preparation and removed standalone local workspace fallback behavior.
```

Use the actual bumped Claude version if it advanced in Step 1.

- [ ] **Step 3: Validate metadata**

Run: `go test ./server/agent -run 'TestClaudePluginManifest|TestPluginManifest|TestNaming' -count=1`

Run: `git -C claudecode diff --check && git -C codex diff --check && git -C openclaw diff --check`

Expected: PASS.

- [ ] **Step 4: Commit metadata and root gitlinks**

```bash
git -C claudecode add .claude-plugin/plugin.json CHANGELOG.md
git -C claudecode commit -m "chore: release workspace contract cleanup"
git -C codex add .codex-plugin/plugin.json
git -C codex commit -m "chore: bump plugin version"
git -C openclaw add openclaw.plugin.json
git -C openclaw commit -m "chore: bump plugin version"
git add claudecode codex openclaw
git commit -m "chore: update plugin distributions"
```

### Task 6: Full Verification and Final Audit

**Files:**
- Verify only; do not modify unrelated files.

- [ ] **Step 1: Prove removed runtime references are gone**

Run:

```bash
rg -n "archive_workspace|ARCHIVE_DIR|archive-seednote-workspace|output/seednote/\{标题\}|output/ecommerce/\{产品名\}|归档全链路|归档前|归档目录|成功归档|归档整理|归档与|与归档|归档交付|→ 归档" \
  server claudecode codex openclaw \
  --glob '!docs/superpowers/**' \
  --glob '!claudecode/docs/agent-skill-optimization-audit.md' \
  --glob '!claudecode/docs/gpt-5.6-prompt-guidance.md' \
  --glob '!claudecode/docs/hook-lifecycle-implementation-plan.md'
```

Expected: no matches in runtime code, tests, tracked workflow assets, or supported-tool documentation. Historical plan documents and preserved user-owned untracked docs are intentionally excluded.

- [ ] **Step 2: Run targeted packages**

Run: `go test ./server/service ./server/mcp ./server/agent -count=1`

Expected: PASS.

- [ ] **Step 3: Run all Go tests**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 4: Build both binaries**

```bash
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
```

Expected: both commands exit 0.

- [ ] **Step 5: Validate OpenClaw TypeScript**

Run: `cd openclaw && bun run lint && bun run build`

Expected: both commands exit 0.

- [ ] **Step 6: Audit formatting and unrelated work**

```bash
git diff --check HEAD~5..HEAD
git status --short
git -C claudecode status --short
git -C codex status --short
git -C openclaw status --short
```

Expected:

- No whitespace errors.
- Root status still shows only pre-existing unrelated billing documents and `go.mod`, if they remain uncommitted.
- `claudecode` may still show the pre-existing user-owned modified docs, but no intended workspace cleanup file remains uncommitted.
- `codex` and `openclaw` are clean after their explicit commits.

- [ ] **Step 7: Review behavior against the spec**

```text
- prepare_workspace requires content_type and task_id.
- prepare_workspace returns output.
- archive_workspace is not registered or required.
- no workflow moves or copies deliverables after generation.
- Seednote quality validation reads output directly.
- task_files and OSS remain the only durable delivery path.
- historical task rows and OSS objects are untouched.
```

No additional cleanup commit is needed when all planned commits are clean and verified.
