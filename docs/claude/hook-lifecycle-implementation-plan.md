# Claude Code Hook Lifecycle Correction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> 文档归属：Anban 父仓库维护资料，不属于 Claude Code 插件运行时上下文。

**Goal:** Make Claude Code completion gates run on the lifecycle they can actually observe, and ensure final title/feedback MCP side effects have exactly one tool-capable owner.

**Architecture:** Plugin `SubagentStop` keeps only deterministic command gates for interactive plugin subagents. The server SDK installs the same command gates as `Stop` hooks when a plugin agent runs as the managed main `--agent` session. Business agents own final summaries and MCP side effects; prompt hooks no longer pretend to read files or call tools.

**Tech Stack:** Go, `claude-agent-sdk-go`, Claude Code plugin JSON, Bash/Python quality-gate scripts, Go contract tests.

---

## Scope

Included:

- Remove Claude Code `SubagentStop` prompt hooks and the global `TaskCompleted` prompt hook.
- Anchor plugin-scoped SubagentStop matchers.
- Run existing Seednote, VideoCreator, and VideoEditor command gates in both plugin-subagent and managed-main lifecycles.
- Move Seednote title finalization and missing agent feedback calls into the owning agents.
- Update Claude plugin development docs, manifest, marketplace version, changelog, and parent contract tests.

Excluded:

- Agent/umbrella-Skill orchestration deduplication.
- Skill preload reduction.
- Memory and `maxTurns` tuning.
- New mechanical gates for Agent types that do not already have scripts.
- `seedance-20` changes or third-party migration.
- Codex hook redesign. Its hook runtime needs a separate platform-specific audit.

## File Map

Create:

- None during implementation; this plan and the two audit documents already exist.

Modify in the parent repository:

- `server/agent/managed_hooks.go`: map managed task types to existing command gates and execute them through one shared Stop callback.
- `server/agent/managed_hooks_test.go`: table-driven coverage for all managed gate mappings and unsupported task types.
- `server/agent/claude_plugin_best_practices_test.go`: enforce supported Hook roles, anchored matchers, and Agent feedback ownership.
- `server/mcp/seednote_hook_test.go`: move title/feedback ownership assertions from prompt Hook to Seednote Agent.
- `server/agent/video_contract_test.go`: stop requiring feedback calls in prompt hooks; require command gates plus Agent-owned feedback.
- `server/agent/plugin_binary_contract_test.go`: update the expected Claude plugin version.
- `docs/claude/gpt-5.6-prompt-guidance.md`: retain the prompt-engineering reference outside the distributed plugin.
- `docs/claude/agent-skill-optimization-audit.md`: retain the plugin audit outside the distributed plugin.
- `docs/claude/hook-lifecycle-implementation-plan.md`: retain this implementation record outside the distributed plugin.

Modify in the `claudecode` plugin repository:

- `hooks/hooks.json`: retain SessionStart plus three command-only SubagentStop gates; remove prompt and TaskCompleted hooks.
- `agents/wechatarticle.md`: submit final feedback after delivery validation.
- `agents/seednote.md`: finalize the accepted title before archive and submit final feedback once.
- `agents/designer.md`: submit final feedback after the consistency report.
- `agents/live-slicer.md`: submit final feedback after the delivery report.
- `skills/seednote/SKILL.md`: stop claiming that a Hook performs title finalization; point to the owning Agent workflow.
- `docs/plugin-development.md`: document actual prompt/command Hook capabilities and main-agent Stop wiring.
- `.claude-plugin/plugin.json`: patch version bump.
- `.claude-plugin/marketplace.json`: keep marketplace version synchronized.
- `CHANGELOG.md`: document lifecycle and feedback ownership corrections.

## Task 1: Lock the Correct Hook Contract With Failing Tests

**Files:**

- Modify: `server/agent/claude_plugin_best_practices_test.go`
- Modify: `server/mcp/seednote_hook_test.go`
- Modify: `server/agent/video_contract_test.go`

- [ ] **Step 1: Add a structural Hook-role test**

Add a JSON-decoding test to `server/agent/claude_plugin_best_practices_test.go` that requires:

```go
func TestClaudeCodeCompletionHooksUseSupportedRoles(t *testing.T) {
	var cfg struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	raw := readRepoFile(t, "../../claudecode/hooks/hooks.json")
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("hooks.json must be valid JSON: %v", err)
	}
	if _, ok := cfg.Hooks["TaskCompleted"]; ok {
		t.Fatal("TaskCompleted must not perform whole-workflow completion checks")
	}
	want := map[string]string{
		"^anban:seednote$":     "seednote-quality-gate.sh",
		"^anban:videocreator$": "videocreator-quality-gate.sh",
		"^anban:videoeditor$":  "videoeditor-quality-gate.sh",
	}
	groups := cfg.Hooks["SubagentStop"]
	if len(groups) != len(want) {
		t.Fatalf("SubagentStop groups = %d, want %d", len(groups), len(want))
	}
	for _, group := range groups {
		script, ok := want[group.Matcher]
		if !ok {
			t.Fatalf("unexpected SubagentStop matcher %q", group.Matcher)
		}
		if len(group.Hooks) != 1 || group.Hooks[0].Type != "command" || !strings.HasSuffix(group.Hooks[0].Command, script) {
			t.Fatalf("SubagentStop/%s = %#v, want command %s", group.Matcher, group.Hooks, script)
		}
	}
}
```

- [ ] **Step 2: Tighten matcher validation**

Replace the existing prefix-only assertion in `TestClaudeCodeSubagentHooksUsePluginScopedMatchers` with exact anchoring:

```go
if !strings.HasPrefix(group.Matcher, "^anban:") || !strings.HasSuffix(group.Matcher, "$") {
	t.Fatalf("SubagentStop matcher %q must be an anchored plugin-scoped agent name", group.Matcher)
}
```

- [ ] **Step 3: Change Seednote ownership expectations**

Replace `TestSeednoteFinalTitleOwnership` so it asserts:

```go
func TestSeednoteFinalizationOwnership(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	read := func(path string) string {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(raw)
	}

	agentBody := read(filepath.Join(root, "claudecode", "agents", "seednote.md"))
	for _, want := range []string{"save_template", "save_eligible", "finalize_task_title", "submit_agent_feedback"} {
		if !strings.Contains(agentBody, want) {
			t.Fatalf("seednote agent missing %q", want)
		}
	}
	hooks := read(filepath.Join(root, "claudecode", "hooks", "hooks.json"))
	for _, forbidden := range []string{"finalize_task_title", "submit_agent_feedback", `"type": "prompt"`} {
		if strings.Contains(hooks, forbidden) {
			t.Fatalf("Claude hooks must not own tool-capable workflow action %q", forbidden)
		}
	}
	skill := read(filepath.Join(root, "claudecode", "skills", "seednote", "SKILL.md"))
	if strings.Contains(skill, "hook 统一负责") {
		t.Fatal("seednote skill must not assign title finalization to a Hook")
	}
}
```

- [ ] **Step 4: Update video Hook expectations**

In `server/agent/video_contract_test.go`, replace the expected list in `TestSplitVideoHookQualityGatesAreRegistered` with:

```go
for _, want := range []string{
	`"matcher": "^anban:videocreator$"`,
	`"matcher": "^anban:videoeditor$"`,
	"videocreator-quality-gate.sh",
	"videoeditor-quality-gate.sh",
} {
	if !strings.Contains(text, want) {
		t.Fatalf("claudecode hooks missing %q", want)
	}
}
```

Retain the existing Agent-body assertions for `submit_agent_feedback`; remove only the assumptions that feedback text lives in `hooks.json`.

- [ ] **Step 5: Run the tests and verify they fail for the intended reasons**

Run:

```bash
go test ./server/agent ./server/mcp -run 'TestClaudeCodeCompletionHooksUseSupportedRoles|TestClaudeCodeSubagentHooksUsePluginScopedMatchers|TestSeednoteFinalizationOwnership|TestSplitVideoHookQualityGatesAreRegistered' -count=1
```

Expected: FAIL because `hooks.json` still contains prompt/TaskCompleted hooks, matchers are not anchored, and Seednote Agent does not own finalization/feedback.

- [ ] **Step 6: Commit the failing contract tests**

```bash
git add server/agent/claude_plugin_best_practices_test.go server/mcp/seednote_hook_test.go server/agent/video_contract_test.go
git commit -m "test(agent): define Claude hook lifecycle ownership"
```

## Task 2: Generalize Managed Main-Agent Stop Gates

**Files:**

- Modify: `server/agent/managed_hooks.go`
- Modify: `server/agent/managed_hooks_test.go`

- [ ] **Step 1: Add table-driven tests for the three managed gates**

Replace the Seednote-only setup with a table that covers:

```go
func TestManagedTaskStopHookRunsTaskGateForMainAgent(t *testing.T) {
	tests := []struct {
		taskType  string
		agentType string
		script    string
	}{
		{taskType: "seednote", agentType: "anban:seednote", script: "seednote-quality-gate.sh"},
		{taskType: "videocreator", agentType: "anban:videocreator", script: "videocreator-quality-gate.sh"},
		{taskType: "videoeditor", agentType: "anban:videoeditor", script: "videoeditor-quality-gate.sh"},
	}
	for _, tt := range tests {
		t.Run(tt.taskType, func(t *testing.T) {
			workspace := t.TempDir()
			pluginRoot := t.TempDir()
			hooksDir := filepath.Join(pluginRoot, "hooks")
			if err := os.MkdirAll(hooksDir, 0o755); err != nil {
				t.Fatal(err)
			}
			body := "#!/bin/sh\n" +
				"input=$(cat)\n" +
				"printf %s \"$input\" | grep -q '\"managed_main_session\":true' || exit 2\n" +
				"printf %s \"$input\" | grep -q '\"agent_type\":\"" + tt.agentType + "\"' || exit 2\n" +
				"printf %s '{\"decision\":\"block\",\"reason\":\"gate-ran\"}'\n"
			script := filepath.Join(hooksDir, tt.script)
			if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
				t.Fatal(err)
			}

			option, err := ManagedTaskStopHook(tt.taskType, workspace, pluginRoot)
			if err != nil {
				t.Fatalf("ManagedTaskStopHook: %v", err)
			}
			opts := claudecode.NewOptions(option)
			hooks, ok := opts.Hooks.(map[claudecode.HookEvent][]claudecode.HookMatcher)
			if !ok || len(hooks[claudecode.HookEventStop]) != 1 {
				t.Fatalf("hooks = %#v", opts.Hooks)
			}
			callback := hooks[claudecode.HookEventStop][0].Hooks[0]
			result, err := callback(context.Background(), &claudecode.StopHookInput{}, nil, claudecode.HookContext{})
			if err != nil {
				t.Fatalf("callback: %v", err)
			}
			if result.Decision == nil || *result.Decision != "block" || result.Reason == nil || *result.Reason != "gate-ran" {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}
```

Keep a separate `TestManagedTaskStopHookSkipsTaskTypesWithoutGate` using `article` and an empty plugin root.

- [ ] **Step 2: Run the managed Hook tests and verify new cases fail**

Run:

```bash
go test ./server/agent -run 'TestManagedTaskStopHook' -count=1
```

Expected: FAIL for `videocreator` and `videoeditor`; Seednote continues to pass.

- [ ] **Step 3: Implement a shared gate mapping**

Refactor `server/agent/managed_hooks.go` around this shape:

```go
type managedStopGate struct {
	agentType string
	script    string
}

var managedStopGates = map[string]managedStopGate{
	"seednote":     {agentType: "anban:seednote", script: "seednote-quality-gate.sh"},
	"videocreator": {agentType: "anban:videocreator", script: "videocreator-quality-gate.sh"},
	"videoeditor":  {agentType: "anban:videoeditor", script: "videoeditor-quality-gate.sh"},
}
```

`ManagedTaskStopHook` must:

1. Return a no-op option when `taskType` is absent from the map.
2. Require a non-empty plugin root only for mapped task types.
3. Resolve `hooks/<script>` and run it from the task workspace.
4. Send `agent_type`, `managed_main_session=true`, and `stop_hook_active` as JSON stdin.
5. Preserve the current fail-closed behavior for command errors and invalid JSON.
6. Use the mapped Agent name in error messages instead of hard-coded "Seednote".

- [ ] **Step 4: Run targeted tests**

```bash
go test ./server/agent -run 'TestManagedTaskStopHook|TestSeednoteQualityGate' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit the managed runtime change**

```bash
git add server/agent/managed_hooks.go server/agent/managed_hooks_test.go
git commit -m "fix(agent): run completion gates for managed video agents"
```

## Task 3: Replace Impossible Prompt Hooks With Command Gates

**Files:**

- Modify: `claudecode/hooks/hooks.json`

- [ ] **Step 1: Reduce `hooks.json` to supported roles**

Keep the existing async SessionStart entry. Replace the entire completion section with:

```json
"SubagentStop": [
  {
    "matcher": "^anban:seednote$",
    "hooks": [
      {
        "type": "command",
        "command": "${CLAUDE_PLUGIN_ROOT}/hooks/seednote-quality-gate.sh",
        "args": [],
        "timeout": 30
      }
    ]
  },
  {
    "matcher": "^anban:videocreator$",
    "hooks": [
      {
        "type": "command",
        "command": "${CLAUDE_PLUGIN_ROOT}/hooks/videocreator-quality-gate.sh",
        "args": [],
        "timeout": 30
      }
    ]
  },
  {
    "matcher": "^anban:videoeditor$",
    "hooks": [
      {
        "type": "command",
        "command": "${CLAUDE_PLUGIN_ROOT}/hooks/videoeditor-quality-gate.sh",
        "args": [],
        "timeout": 30
      }
    ]
  }
]
```

Remove `TaskCompleted` completely. Do not add a `Stop` entry to plugin JSON; managed main-agent Stop hooks are installed by the SDK path from Task 2.

- [ ] **Step 2: Validate JSON and run Hook structure tests**

```bash
jq empty claudecode/hooks/hooks.json
go test ./server/agent -run 'TestClaudeCodeHooksUseExecFormForPluginPathCommands|TestClaudeCodeSubagentHooksUsePluginScopedMatchers|TestClaudeCodeCompletionHooksUseSupportedRoles' -count=1
```

Expected: PASS.

- [ ] **Step 3: Commit the Claude Hook configuration**

Commit inside the plugin repository:

```bash
git -C claudecode add hooks/hooks.json
git -C claudecode commit -m "fix(hooks): use command-only completion gates"
```

The final plugin version/changelog commit may squash this commit later if the repository requires one release commit.

## Task 4: Move Finalization and Feedback Into Tool-Capable Agents

**Files:**

- Modify: `claudecode/agents/wechatarticle.md`
- Modify: `claudecode/agents/seednote.md`
- Modify: `claudecode/agents/designer.md`
- Modify: `claudecode/agents/live-slicer.md`
- Modify: `server/agent/claude_plugin_best_practices_test.go`
- Modify: `server/mcp/seednote_hook_test.go`

- [ ] **Step 1: Add an Agent feedback ownership test**

Add a table-driven test over all `claudecode/agents/*.md` that requires a final `submit_agent_feedback` call for each Agent. Match a call containing the expected `agent_name`; do not count a tool name merely listed in prose.

```go
func TestClaudeAgentsOwnFinalFeedback(t *testing.T) {
	agents := []string{
		"designer", "ecommerce", "live-slicer", "moments", "montage",
		"seednote", "videocreator", "videoeditor", "wechatarticle",
	}
	for _, name := range agents {
		t.Run(name, func(t *testing.T) {
			body := readRepoFile(t, filepath.Join("../../claudecode/agents", name+".md"))
			pattern := regexp.MustCompile(`(?s)submit_agent_feedback\([^)]*?agent_name\s*=\s*["']?` + regexp.QuoteMeta(name))
			matches := pattern.FindAllStringIndex(body, -1)
			if len(matches) != 1 {
				t.Fatalf("%s agent feedback calls = %d, want exactly 1", name, len(matches))
			}
		})
	}
}
```

- [ ] **Step 2: Run the ownership tests and verify the four missing Agents fail**

```bash
go test ./server/agent ./server/mcp -run 'TestClaudeAgentsOwnFinalFeedback|TestSeednoteFinalizationOwnership' -count=1
```

Expected: FAIL for `wechatarticle`, `seednote`, `designer`, and `live-slicer`, plus Seednote title ownership.

- [ ] **Step 3: Move Seednote title finalization before archive**

In `agents/seednote.md`, update the archive stage so the accepted `$FINAL_TITLE` is finalized before `archive_workspace`:

```text
调用 finalize_task_title(task_id=$TASK_ID, title=$FINAL_TITLE)。重复标题时改写标题，
同步更新 content.md 首行后重试，最多 3 次；每次后续归档都使用服务端已接受的标题。
非重复错误写入 failure-state.json，包含 stage=finalize_title、error_code、message
和 resume_from=finalize_title，然后停止，不创建与系统标题不一致的归档目录。
```

At the final report stage, add exactly one `submit_agent_feedback` call for `agent_name="seednote"`.

- [ ] **Step 4: Add final feedback to the other missing Agents**

For each Agent, add one call after its existing final validation/report:

- `wechatarticle`: summary includes selected template, draft status, and vision pass rate.
- `designer`: summary includes image count, consistency status, and manual-review count.
- `live-slicer`: summary includes successful/failed clips, output directory, and recoverable warnings.

Use the existing shared score shape:

```text
submit_agent_feedback(
  task_id=$TASK_ID,
  agent_name="<agent>",
  scores={quality:N, completeness:N, efficiency:N},
  errors="<errors>",
  optimizations="<optimizations>",
  summary="<workflow-specific summary>"
)
```

- [ ] **Step 5: Remove the obsolete Seednote Skill claim**

The duplicate Seednote umbrella Skill has since been deleted. Keep final title
deduplication and persistence owned only by the Seednote Agent archive stage.

- [ ] **Step 6: Run ownership and affected contract tests**

```bash
go test ./server/agent ./server/mcp -run 'TestClaudeAgentsOwnFinalFeedback|TestSeednoteFinalizationOwnership|TestSeednote|TestSplitVideoHookQualityGatesAreRegistered' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit Agent ownership changes**

Commit plugin files first:

```bash
git -C claudecode add agents/wechatarticle.md agents/seednote.md agents/designer.md agents/live-slicer.md skills/seednote/SKILL.md
git -C claudecode commit -m "fix(agents): own finalization and feedback"
```

Then commit parent contract tests:

```bash
git add server/agent/claude_plugin_best_practices_test.go server/mcp/seednote_hook_test.go server/agent/video_contract_test.go
git commit -m "test(agent): enforce workflow side-effect ownership"
```

## Task 5: Document and Version the Claude Plugin Change

**Files:**

- Modify: `claudecode/docs/plugin-development.md`
- Modify: `claudecode/.claude-plugin/plugin.json`
- Modify: `claudecode/.claude-plugin/marketplace.json`
- Modify: `claudecode/CHANGELOG.md`
- Modify: `server/agent/plugin_binary_contract_test.go`

- [ ] **Step 1: Update plugin development guidance**

Document these rules in `docs/plugin-development.md`:

```text
- Plugin SubagentStop covers plugin agents spawned as subagents; managed --agent sessions use SDK Stop hooks.
- command hooks perform deterministic file/schema/quantity checks.
- prompt hooks only return ok/reason from hook input and cannot read files or call MCP tools.
- final summaries, title finalization, and submit_agent_feedback belong to tool-capable Agent stages.
- TaskCompleted is per task item, not whole-workflow completion.
```

- [ ] **Step 2: Apply the patch version bump**

Change:

```json
"version": "2.10.61"
```

in both `claudecode/.claude-plugin/plugin.json` and the plugin entry in `claudecode/.claude-plugin/marketplace.json`.

- [ ] **Step 3: Add the changelog entry**

Add:

```markdown
## [2.10.61] - 2026-07-15

### Fixed

- Replaced tool-incapable completion prompt hooks with deterministic command gates, removed per-task whole-workflow feedback, and made managed main agents run the same Seednote/Video completion gates as interactive plugin subagents.
- Moved Seednote title finalization and final workflow feedback into the tool-capable Agent stages.

### Added

- Documented GPT-5.6 prompt guidance and the Claude Agent/Skill optimization audit.
```

- [ ] **Step 4: Update the parent binary contract version**

Change the Claude plugin expected version in `server/agent/plugin_binary_contract_test.go` from `2.10.60` to `2.10.61`.

- [ ] **Step 5: Validate plugin metadata**

```bash
go test ./server/agent -run 'TestClaudePlugin|TestPluginBinary' -count=1
claude plugin validate --strict ./claudecode
```

Expected: Go tests PASS and Claude plugin validation reports the plugin as valid.

- [ ] **Step 6: Commit the release metadata and docs**

```bash
git -C claudecode add .claude-plugin/plugin.json .claude-plugin/marketplace.json CHANGELOG.md docs/plugin-development.md
git -C claudecode commit -m "docs: record prompt and hook lifecycle guidance"
```

```bash
git add claudecode server/agent/plugin_binary_contract_test.go docs/claude/gpt-5.6-prompt-guidance.md docs/claude/agent-skill-optimization-audit.md docs/claude/hook-lifecycle-implementation-plan.md
git commit -m "chore(plugin): release Claude hook lifecycle correction"
```

## Task 6: Full Verification

**Files:**

- No new files.

- [ ] **Step 1: Run focused Hook and contract tests**

```bash
go test ./server/agent ./server/mcp -run 'TestManagedTaskStopHook|TestSeednoteQualityGate|TestClaudeCode|TestClaudeAgentsOwnFinalFeedback|TestSeednoteFinalizationOwnership|TestSplitVideoHookQualityGatesAreRegistered|TestPluginBinary' -count=1
```

Expected: PASS with zero failures.

- [ ] **Step 2: Run all Go tests**

```bash
go test ./...
```

Expected: PASS with zero failing packages.

- [ ] **Step 3: Build both Go binaries**

```bash
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
```

Expected: both commands exit 0 and create the two binaries.

- [ ] **Step 4: Validate plugin JSON and shell scripts**

```bash
jq empty claudecode/hooks/hooks.json claudecode/.claude-plugin/plugin.json claudecode/.claude-plugin/marketplace.json
bash -n claudecode/hooks/seednote-quality-gate.sh claudecode/hooks/videocreator-quality-gate.sh claudecode/hooks/videoeditor-quality-gate.sh
claude plugin validate --strict ./claudecode
```

Expected: all commands exit 0.

- [ ] **Step 5: Check diffs and repository boundaries**

```bash
git -C claudecode diff --check HEAD~3..HEAD
git diff --check HEAD~3..HEAD
git -C claudecode status --short
git status --short --ignore-submodules=all
```

Expected: no whitespace errors; both repositories are clean except unrelated pre-existing user changes. Do not add or modify `skills/seedance-20/`.

- [ ] **Step 6: Verify release metadata and ownership mechanically**

```bash
rg -n '"version": "2.10.61"' claudecode/.claude-plugin/plugin.json claudecode/.claude-plugin/marketplace.json
rg -n 'type": "prompt"|TaskCompleted|finalize_task_title|submit_agent_feedback' claudecode/hooks/hooks.json
rg -n 'finalize_task_title|submit_agent_feedback' claudecode/agents/seednote.md
```

Expected:

- Both metadata files contain `2.10.61`.
- The Hook search returns no matches.
- Seednote Agent contains both finalization and feedback calls.

## Rollback Boundary

If the managed video gates cause a runtime regression, revert only the `videocreator` and `videoeditor` entries from `managedStopGates`; keep the Hook prompt removal and Agent-owned feedback because prompt hooks cannot perform those tool-capable actions. Any rollback must preserve Seednote's existing managed Stop gate and rerun the full verification set.
