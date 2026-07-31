# Explicit Claude Profile Environments Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove `claude.execution_profile_env_defaults` and make every configured Claude execution Profile explicitly own the complete runtime environment it resolves to today.

**Architecture:** First expand the currently shared runtime controls into each Profile in the canonical `server/config.example.yaml`, while the existing parser still proves that resolved values are unchanged. Then make the Claude YAML schema reject the removed field and delete all default validation and merge behavior. Existing Profile validation, snapshot freezing, alias derivation, Agent bootstrap, and historical resume paths remain unchanged and stay covered by current tests.

**Tech Stack:** Go, `gopkg.in/yaml.v3`, table-driven Go tests, YAML, Bun/Vitest/Vite repository gates.

---

## File map

- Modify `server/config.example.yaml`: repeat the complete Claude runtime controls in `effective`, `balanced`, and `quality`; retain provider/model/authentication fields and only the non-identity Kimi alias.
- Modify `server/config/config_phase2_test.go`: verify the tracked template has no shared-default field, preserves every resolved runtime-control value, keeps Profile maps independent, and rejects the old field.
- Modify `server/config/config.go`: remove the shared-default field, schema entry, nested validation, helper, cloning, and merge loop.
- Do not modify `server/config.yaml`: it is an ignored local or deployment-generated runtime copy.

### Task 1: Expand the canonical template into self-contained Profiles

**Files:**
- Modify: `server/config/config_phase2_test.go:340-398`
- Modify: `server/config.example.yaml:233-289`

- [ ] **Step 1: Strengthen the canonical-template test before changing YAML**

In `TestConfigExampleLoadsAsCompleteConfiguration`, immediately after reading `raw`, add:

```go
	if strings.Contains(string(raw), "\n  execution_profile_env_defaults:") {
		t.Fatal("config.example.yaml contains removed claude.execution_profile_env_defaults")
	}
```

Replace the current mutation/default-storage assertion at the end of the test with:

```go
	wantRuntimeEnvs := map[string]map[string]string{
		"effective": {
			"CLAUDE_CODE_EFFORT_LEVEL":                "medium",
			"CLAUDE_CODE_ALWAYS_ENABLE_EFFORT":        "false",
			"CLAUDE_CODE_MAX_CONTEXT_TOKENS":           "1048576",
			"CLAUDE_CODE_MAX_OUTPUT_TOKENS":            "393216",
			"MAX_THINKING_TOKENS":                      "0",
			"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
			"CLAUDE_CODE_DISABLE_AUTO_MEMORY":          "0",
			"CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING":    "false",
			"CLAUDE_CODE_DISABLE_THINKING":             "false",
			"CLAUDE_CODE_AUTO_COMPACT_WINDOW":          "262144",
			"CLAUDE_AUTOCOMPACT_PCT_OVERRIDE":          "80",
			"CLAUDE_CODE_DISABLE_1M_CONTEXT":            "false",
			"ENABLE_TOOL_SEARCH":                       "true",
		},
		"balanced": {
			"CLAUDE_CODE_EFFORT_LEVEL":                "high",
			"CLAUDE_CODE_ALWAYS_ENABLE_EFFORT":        "false",
			"CLAUDE_CODE_MAX_CONTEXT_TOKENS":           "1048576",
			"CLAUDE_CODE_MAX_OUTPUT_TOKENS":            "131072",
			"MAX_THINKING_TOKENS":                      "0",
			"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
			"CLAUDE_CODE_DISABLE_AUTO_MEMORY":          "0",
			"CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING":    "false",
			"CLAUDE_CODE_DISABLE_THINKING":             "false",
			"CLAUDE_CODE_AUTO_COMPACT_WINDOW":          "262144",
			"CLAUDE_AUTOCOMPACT_PCT_OVERRIDE":          "80",
			"CLAUDE_CODE_DISABLE_1M_CONTEXT":            "false",
			"ENABLE_TOOL_SEARCH":                       "true",
		},
		"quality": {
			"CLAUDE_CODE_EFFORT_LEVEL":                "high",
			"CLAUDE_CODE_ALWAYS_ENABLE_EFFORT":        "true",
			"CLAUDE_CODE_MAX_CONTEXT_TOKENS":           "1048576",
			"CLAUDE_CODE_MAX_OUTPUT_TOKENS":            "131072",
			"MAX_THINKING_TOKENS":                      "0",
			"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
			"CLAUDE_CODE_DISABLE_AUTO_MEMORY":          "0",
			"CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING":    "false",
			"CLAUDE_CODE_DISABLE_THINKING":             "false",
			"CLAUDE_CODE_AUTO_COMPACT_WINDOW":          "262144",
			"CLAUDE_AUTOCOMPACT_PCT_OVERRIDE":          "80",
			"CLAUDE_CODE_DISABLE_1M_CONTEXT":            "false",
			"ENABLE_TOOL_SEARCH":                       "true",
		},
	}
	for name, wantEnvs := range wantRuntimeEnvs {
		profile := cfg.Claude.ExecutionProfiles[name]
		for key, want := range wantEnvs {
			if got := profile.Envs[key]; got != want {
				t.Errorf("execution profile %s env %s = %q, want %q", name, key, got, want)
			}
		}
	}
	if got := cfg.Claude.ExecutionProfiles["effective"].ModelUsageAliases; len(got) != 0 {
		t.Fatalf("effective model_usage_aliases = %#v, want empty", got)
	}
	if got := cfg.Claude.ExecutionProfiles["balanced"].ModelUsageAliases; len(got) != 0 {
		t.Fatalf("balanced model_usage_aliases = %#v, want empty", got)
	}
	qualityAliases := cfg.Claude.ExecutionProfiles["quality"].ModelUsageAliases
	if len(qualityAliases) != 1 || qualityAliases["kimi-k3[1m]"] != "kimi-k3" {
		t.Fatalf("quality model_usage_aliases = %#v, want only kimi-k3[1m] alias", qualityAliases)
	}
	effective := cfg.Claude.ExecutionProfiles["effective"]
	effective.Envs["ENABLE_TOOL_SEARCH"] = "mutated"
	if got := cfg.Claude.ExecutionProfiles["balanced"].Envs["ENABLE_TOOL_SEARCH"]; got != "true" {
		t.Fatalf("execution profile env maps share storage: balanced ENABLE_TOOL_SEARCH = %q", got)
	}
```

- [ ] **Step 2: Run the focused test and verify the source contract fails**

Run:

```bash
go test ./server/config -run '^TestConfigExampleLoadsAsCompleteConfiguration$' -count=1
```

Expected: FAIL with `config.example.yaml contains removed claude.execution_profile_env_defaults`.

- [ ] **Step 3: Replace the shared YAML block with explicit values**

Delete the entire `execution_profile_env_defaults` mapping. Keep all current provider/model/authentication lines, then add the following 13 controls to every Profile. `balanced` uses these exact values:

```yaml
        CLAUDE_CODE_EFFORT_LEVEL: "high"
        CLAUDE_CODE_ALWAYS_ENABLE_EFFORT: "false"
        CLAUDE_CODE_MAX_CONTEXT_TOKENS: "1048576"
        CLAUDE_CODE_MAX_OUTPUT_TOKENS: "131072"
        MAX_THINKING_TOKENS: "0"
        CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: "1"
        CLAUDE_CODE_DISABLE_AUTO_MEMORY: "0"
        CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING: "false"
        CLAUDE_CODE_DISABLE_THINKING: "false"
        CLAUDE_CODE_AUTO_COMPACT_WINDOW: "262144"
        CLAUDE_AUTOCOMPACT_PCT_OVERRIDE: "80"
        CLAUDE_CODE_DISABLE_1M_CONTEXT: "false"
        ENABLE_TOOL_SEARCH: "true"
```

Use the same block in `effective`, changing only:

```yaml
        CLAUDE_CODE_EFFORT_LEVEL: "medium"
        CLAUDE_CODE_MAX_OUTPUT_TOKENS: "393216"
```

Use the same block in `quality`, changing only:

```yaml
        CLAUDE_CODE_ALWAYS_ENABLE_EFFORT: "true"
```

Retain only this configured usage alias:

```yaml
      model_usage_aliases:
        "kimi-k3[1m]": "kimi-k3"
```

- [ ] **Step 4: Verify the template and affected packages pass**

Run:

```bash
gofmt -w server/config/config_phase2_test.go
go test ./server/config -run '^TestConfigExampleLoadsAsCompleteConfiguration$' -count=1
go test ./server/config ./server/service -count=1
```

Expected: all commands PASS. The tracked template is self-contained and resolves to the same values while the old parser still exists.

- [ ] **Step 5: Check and commit the self-contained template**

Run:

```bash
git diff --check
git diff -- server/config.example.yaml server/config/config_phase2_test.go
git add server/config.example.yaml server/config/config_phase2_test.go
git diff --cached --check
git commit -m "config: declare explicit Claude profile environments"
```

Expected: the staged diff contains only the canonical template and its contract test; the commit succeeds.

### Task 2: Reject and remove shared Claude Profile defaults

**Files:**
- Modify: `server/config/config_phase2_test.go:9-162`
- Modify: `server/config/config.go:594-721`

- [ ] **Step 1: Replace obsolete merge/default tests with a strict schema test**

Delete these four tests from `server/config/config_phase2_test.go`:

```text
TestClaudeExecutionProfileEnvDefaultsMergeBeforeProfileOverrides
TestClaudeExecutionProfileEnvDefaultsRejectUnknownEnv
TestClaudeExecutionProfileEnvDefaultsRejectProviderAndModelFields
TestClaudeExecutionProfileEnvDefaultsValidateConfiguredValues
```

Remove the now-unused `github.com/anbanai/anban-creator/server/model` import, and add:

```go
func TestClaudeConfigRejectsExecutionProfileEnvDefaults(t *testing.T) {
	body := strings.Replace(
		validClaudeConfigYAML,
		"claude:\n",
		"claude:\n  execution_profile_env_defaults:\n    ENABLE_TOOL_SEARCH: \"true\"\n",
		1,
	)
	_, err := loadClaudeConfigYAML(t, body)
	if err == nil || !strings.Contains(err.Error(), `unknown claude config field "execution_profile_env_defaults"`) {
		t.Fatalf("NewConfig error = %v, want legacy execution_profile_env_defaults rejection", err)
	}
}
```

- [ ] **Step 2: Run the focused test and verify the old field is still accepted**

Run:

```bash
go test ./server/config -run '^TestClaudeConfigRejectsExecutionProfileEnvDefaults$' -count=1
```

Expected: FAIL because `NewConfig` returns `nil` rather than the required unknown-field error.

- [ ] **Step 3: Remove the field and merge behavior from `ClaudeConfig`**

Delete this exact field from `ClaudeConfig`:

```go
	ExecutionProfileEnvDefaults map[string]string `yaml:"execution_profile_env_defaults" json:"-"`
```

Remove `execution_profile_env_defaults` from the `known` map:

```go
	known := map[string]bool{
		"execution_profiles": true, "executor": true, "runtime_images": true,
		"execution_token_secret": true, "plugin_dir": true,
		"sandbox": true, "docker": true, "kubernetes": true, "max_turns": true,
		"task_log_dir": true, "agent_server_url": true,
	}
```

After decoding, return directly:

```go
	type plain ClaudeConfig
	if err := value.Decode((*plain)(c)); err != nil {
		return err
	}
	return nil
```

Delete the defaults lookup at the beginning of `validateClaudeNestedFields`, leaving Profile mapping and `envs` validation unchanged:

```go
func validateClaudeNestedFields(value *yaml.Node) error {
	profiles := yamlMappingValue(value, "execution_profiles")
	if profiles == nil {
		return nil
	}
```

Delete this complete helper and its body:

```go
func validateClaudeExecutionProfileEnvDefaults(defaults map[string]string) error
```

Do not change `validateClaudeEnvFields`, `model.ValidateClaudeProfileEnvs`, Profile service construction, identity alias derivation, snapshot freezing, bootstrap, or historical restoration.

- [ ] **Step 4: Format and prove the strict parser test passes**

Run:

```bash
gofmt -w server/config/config.go server/config/config_phase2_test.go
go test ./server/config -run '^TestClaudeConfigRejectsExecutionProfileEnvDefaults$' -count=1
```

Expected: PASS, with `ClaudeConfig.UnmarshalYAML` rejecting the legacy field before nested decoding.

- [ ] **Step 5: Run config and downstream Profile behavior tests**

Run:

```bash
go test ./server/config ./server/model ./server/agent ./server/service -count=1
```

Expected: PASS. Existing environment validation, snapshot/fingerprint, secret-redaction, identity-alias, Agent-bootstrap, and historical-resume tests remain unchanged and green.

- [ ] **Step 6: Check and commit strict removal**

Run:

```bash
git diff --check
git diff -- server/config/config.go server/config/config_phase2_test.go
git add server/config/config.go server/config/config_phase2_test.go
git diff --cached --check
git commit -m "config: remove Claude profile environment defaults"
```

Expected: the staged diff contains only parser removal and the strict legacy-field contract; the commit succeeds.

### Task 3: Repository-wide verification and deployment handoff

**Files:**
- Verify only; no additional source changes are expected.

- [ ] **Step 1: Prove no tracked Server defaults references remain**

Run:

```bash
rg -n 'ExecutionProfileEnvDefaults|execution_profile_env_defaults|validateClaudeExecutionProfileEnvDefaults' server/config/config.go server/config.example.yaml
rg -n 'execution_profile_env_defaults' server/config/*_test.go
```

Expected: the first command has no matches; the second finds only `TestClaudeConfigRejectsExecutionProfileEnvDefaults` and its test fixture/assertion. The ignored local `server/config.yaml` is deliberately excluded and must not be edited.

- [ ] **Step 2: Run complete Go tests and builds**

Run:

```bash
go test ./...
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
```

Expected: all tests PASS and both binaries build without errors. If a parallel suite reports a SQLite `database table is locked` error, isolate and rerun the failing package with `-count=1` before classifying it.

- [ ] **Step 3: Run complete Studio gates**

Run:

```bash
cd studio && PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH bun run test
cd studio && PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH bun run build
```

Expected: Vitest passes and `tsc -b && vite build` completes. Studio is behaviorally unchanged, but remains a repository integration gate.

- [ ] **Step 4: Inspect the complete branch before requesting integration**

Run:

```bash
git status --short --branch
git diff --check 4933e060..HEAD
git diff --stat 4933e060..HEAD
git diff 4933e060..HEAD -- server/config/config.go server/config/config_phase2_test.go server/config.example.yaml docs/superpowers/specs/2026-07-31-explicit-claude-profile-envs-design.md docs/superpowers/plans/2026-07-31-explicit-claude-profile-envs.md
```

Expected: the branch contains only the design, this plan, explicit template values, strict parser removal, and focused tests. `git status` is clean. Do not merge or push without a later explicit user request.

- [ ] **Step 5: State the deployment contract in the handoff**

Report:

```text
Rebuild only the Server image for this follow-up.
Before rollout, expand the 13 runtime controls into each production Claude execution Profile and remove execution_profile_env_defaults.
Validate the rendered ConfigMap with the new Server binary, then update the Server image and ConfigMap atomically.
No Studio, Article Agent, Seednote Agent, Montage Agent, or wcflink image rebuild is required for this follow-up.
Historical Task/Execution Profile snapshots are unchanged and require no migration.
```
