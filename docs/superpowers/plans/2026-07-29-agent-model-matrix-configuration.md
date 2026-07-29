# Agent Model Matrix Configuration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace hard-coded single-model Agent profiles with operator-configured Anthropic-compatible providers, five-role Claude Code model matrices, typed Claude environment controls, frozen task snapshots, and updated billing/UI/runtime contracts.

**Architecture:** Keep `cost_effective`, `balanced`, and `maximum_quality` as stable product identities while resolving Provider, models, and Claude controls from strict Server configuration. Freeze non-sensitive schema-v2 snapshots and fingerprints on tasks, resolve live credentials only at bootstrap, normalize terminal model usage through frozen aliases, and fail closed when Provider configuration or model costs are missing. Publish new immutable retail and Provider-cost catalogs, then update both clients and all three TypeScript Agent images to the same contract.

**Tech Stack:** Go 1.24, Fiber v3, GORM/MySQL, YAML v3, SHA-256, Bun/TypeScript, Claude Agent SDK, React 19/Vite/TanStack Query, Vue/uni-app, Docker.

**Approved Design:** `docs/superpowers/specs/2026-07-29-agent-model-matrix-configuration-design.md`

---

## Execution Preconditions

- Execute in an isolated worktree created with `superpowers:using-git-worktrees`; do not develop directly in the current `main` worktree.
- Base the worktree on local `main`, including design commit `2d2271c0`.
- Preserve the original worktree's user-owned untracked plans:
  - `docs/superpowers/plans/2026-07-22-managed-mcp-request-timeout.md`
  - `docs/superpowers/plans/2026-07-29-server-internal-llm-boundary.md`
- Observe each failing test before implementation, and commit after each green task.
- Do not alter Designer request schemas, Designer Provider routing, or historical catalog rows already persisted in the database.

## File Responsibility Map

| Responsibility | Files |
| --- | --- |
| Provider/Profile config | `server/config/config.go`, config tests, `server/config.yaml`, `server/config.example.yaml` |
| Snapshot/fingerprint/execution schema | `server/model/agent_profile.go`, `server/model/task.go`, `server/model/task_execution.go` |
| Registry and availability | `server/service/agent_profiles.go`, `server/main.go` |
| Lifecycle and Bootstrap | task/plan/billing services and handlers, `server/service/agent_bootstrap.go` |
| Database cutover | `server/migrations/20260728_agent_execution_profiles.sql` |
| Billing catalogs | `server/billing/products.yaml`, `server/billing/costs.yaml` |
| TypeScript runtime | `agent-ts/src/bootstrap.ts`, `agent-ts/src/runner.ts` |
| Studio | Profile/task types, selector, frozen task detail, API mocks/tests |
| Miniapp | matching types, selector, task detail, parity test |
| Deployment/runtime smoke | `.env.example`, `server/Deployment.yaml`, `deploy/docker/runtime-smoke*`, three TS Dockerfiles |

### Task 1: Introduce Strict Provider and Profile Configuration

**Files:**
- Modify: `server/config/config.go`
- Modify: `server/config/claude_runtime_config_test.go`
- Modify: `server/config/agent_profile_config_test.go`
- Modify: `server/config.yaml`
- Modify: `server/config.example.yaml`
- Modify: `.env.example`
- Modify: `server/Deployment.yaml`

- [ ] **Step 1: Replace the old fixture and write failing omission/false/zero tests**

Use this complete Profile portion in `validClaudeConfigYAML`:

```yaml
claude:
  providers:
    moonshot:
      protocol: anthropic
      base_url: https://api.moonshot.cn/anthropic
      auth_token: profile-secret-token
  execution_profiles:
    maximum_quality:
      provider: moonshot
      description: flagship
      models:
        default: kimi-k3[1m]
        opus: kimi-k3[1m]
        fable: kimi-k3[1m]
        sonnet: kimi-k3[1m]
        haiku: kimi-k3[1m]
      model_usage_aliases:
        kimi-k3: kimi-k3
        kimi-k3[1m]: kimi-k3
      claude:
        max_thinking_tokens: 0
        enable_tool_search: false
```

Add this test:

```go
func TestClaudeControlsPreserveOmittedFalseAndZero(t *testing.T) {
    cfg, err := loadClaudeConfigYAML(t, validClaudeConfigYAML)
    if err != nil { t.Fatal(err) }
    controls := cfg.Claude.ExecutionProfiles["maximum_quality"].Claude
    if controls.MaxThinkingTokens == nil || *controls.MaxThinkingTokens != 0 {
        t.Fatalf("max_thinking_tokens = %#v", controls.MaxThinkingTokens)
    }
    if controls.EnableToolSearch == nil || *controls.EnableToolSearch {
        t.Fatalf("enable_tool_search = %#v", controls.EnableToolSearch)
    }
    if controls.MaxOutputTokens != nil {
        t.Fatalf("omitted max_output_tokens = %#v", controls.MaxOutputTokens)
    }
}
```

- [ ] **Step 2: Add strict-field failing tests**

Add table cases for an empty model role, `effort_level: extreme`, `autocompact_pct_override: 101`, `subagent_model: ""`, unknown nested fields, and removed top-level `claude.env`. Assert the error contains the exact YAML field path.

```go
func TestClaudeConfigRejectsRemovedGenericEnv(t *testing.T) {
    body := strings.Replace(validClaudeConfigYAML, "  executor: docker", "  env:\n    CLAUDE_CODE_AUTO_COMPACT_WINDOW: 1000\n  executor: docker", 1)
    _, err := loadClaudeConfigYAML(t, body)
    if err == nil || !strings.Contains(err.Error(), `unknown claude config field "env"`) {
        t.Fatalf("NewConfig error = %v", err)
    }
}
```

- [ ] **Step 3: Run focused tests and verify RED**

```bash
go test ./server/config -run 'TestClaude(Controls|ProfileConfig|ConfigRejectsRemoved)' -count=1
```

Expected: FAIL because Provider config, model matrices, typed controls, and removal of `env` are absent.

- [ ] **Step 4: Implement strict typed config**

Add these types and delete Profile-level `ModelID`, `ContextWindow`, `ReasoningEffort`, `ThinkingRequired`, `BaseURL`, `AuthToken`, plus `ClaudeConfig.Env`:

```go
type ClaudeProviderConfig struct {
    Protocol  string `yaml:"protocol" json:"protocol"`
    BaseURL   string `yaml:"base_url" json:"base_url"`
    AuthToken string `yaml:"auth_token" json:"-"`
}

type ClaudeModelMatrixConfig struct {
    Default string `yaml:"default" json:"default"`
    Opus    string `yaml:"opus" json:"opus"`
    Fable   string `yaml:"fable" json:"fable"`
    Sonnet  string `yaml:"sonnet" json:"sonnet"`
    Haiku   string `yaml:"haiku" json:"haiku"`
}

type ClaudeControlsConfig struct {
    EffortLevel             *string `yaml:"effort_level" json:"effort_level,omitempty"`
    AlwaysEnableEffort      *bool   `yaml:"always_enable_effort" json:"always_enable_effort,omitempty"`
    MaxContextTokens        *int    `yaml:"max_context_tokens" json:"max_context_tokens,omitempty"`
    MaxOutputTokens         *int    `yaml:"max_output_tokens" json:"max_output_tokens,omitempty"`
    MaxThinkingTokens       *int    `yaml:"max_thinking_tokens" json:"max_thinking_tokens,omitempty"`
    DisableAdaptiveThinking *bool   `yaml:"disable_adaptive_thinking" json:"disable_adaptive_thinking,omitempty"`
    DisableThinking         *bool   `yaml:"disable_thinking" json:"disable_thinking,omitempty"`
    AutoCompactWindow       *int    `yaml:"auto_compact_window" json:"auto_compact_window,omitempty"`
    AutocompactPctOverride  *int    `yaml:"autocompact_pct_override" json:"autocompact_pct_override,omitempty"`
    Disable1MContext        *bool   `yaml:"disable_1m_context" json:"disable_1m_context,omitempty"`
    SubagentModel           *string `yaml:"subagent_model" json:"subagent_model,omitempty"`
    EnableToolSearch        *bool   `yaml:"enable_tool_search" json:"enable_tool_search,omitempty"`
}

type ClaudeExecutionProfileConfig struct {
    Provider          string                  `yaml:"provider" json:"provider"`
    Description       string                  `yaml:"description" json:"description"`
    Models            ClaudeModelMatrixConfig `yaml:"models" json:"models"`
    ModelUsageAliases map[string]string       `yaml:"model_usage_aliases" json:"model_usage_aliases"`
    Claude            ClaudeControlsConfig    `yaml:"claude" json:"claude"`
}
```

Use `yaml.Node` allowlists on Provider, Profile, Models, and Controls. Require five non-empty roles. Validate `low|medium|high|max`, positive context/output/compact values, non-negative thinking tokens, percentage `1..100`, and non-empty optional strings. Omitted or YAML `null` remains nil.

- [ ] **Step 5: Replace deployment examples**

Use the approved DeepSeek/GLM/Kimi examples in both config files. Replace `ANBAN_KIMI_API_KEY` in `.env.example` and `server/Deployment.yaml` with:

```dotenv
ANBAN_MOONSHOT_ANTHROPIC_BASE_URL=https://api.moonshot.cn/anthropic
ANBAN_MOONSHOT_API_KEY=
ANBAN_ZHIPU_ANTHROPIC_BASE_URL=https://open.bigmodel.cn/api/anthropic
ANBAN_ZHIPU_API_KEY=
```

Retain DeepSeek and Doubao variables. Remove the old Kimi Code endpoint/key name and the YAML `claude.env` block.

- [ ] **Step 6: Run config tests and commit**

```bash
go test ./server/config -count=1
git add server/config/config.go server/config/claude_runtime_config_test.go server/config/agent_profile_config_test.go server/config.yaml server/config.example.yaml .env.example server/Deployment.yaml
git commit -m "feat(config): add provider model matrix profiles"
```

Expected: tests PASS and secret-redaction output contains no token.

### Task 2: Add Schema-v2 Snapshots and Stable Fingerprints

**Files:**
- Modify: `server/model/agent_profile.go`
- Modify: `server/model/task.go`
- Modify: `server/model/task_execution.go`
- Modify: `server/model/task_execution_profile_test.go`
- Modify: `server/model/task_billing_visibility_test.go`

- [ ] **Step 1: Write failing canonical fingerprint and schema tests**

```go
func TestAgentProfileSnapshotFingerprintIsCanonical(t *testing.T) {
    disabled := false
    first := AgentProfileSnapshot{
        SchemaVersion: 2, ProfileID: "maximum_quality", DisplayName: "极致效果",
        Provider: "moonshot", Protocol: "anthropic",
        Models: AgentModelMatrix{Default: "kimi-k3[1m]", Opus: "kimi-k3[1m]", Fable: "kimi-k3[1m]", Sonnet: "kimi-k3[1m]", Haiku: "kimi-k3[1m]"},
        Claude: AgentClaudeControls{EnableToolSearch: &disabled},
        ModelUsageAliases: map[string]string{"kimi-k3[1m]": "kimi-k3", "kimi-k3": "kimi-k3"},
    }
    second := first
    second.ModelUsageAliases = map[string]string{"kimi-k3": "kimi-k3", "kimi-k3[1m]": "kimi-k3"}
    a, err := AgentProfileFingerprint(first)
    if err != nil { t.Fatal(err) }
    b, err := AgentProfileFingerprint(second)
    if err != nil { t.Fatal(err) }
    if a != b || len(a) != 64 { t.Fatalf("fingerprints=%q %q", a, b) }
}
```

Add JSON assertions for `schema_version`, `models`, `claude`, aliases, and the absence of `base_url`, `auth_token`, `secret`, and `endpoint`.

- [ ] **Step 2: Run model tests and verify RED**

```bash
go test ./server/model -run 'TestAgentProfileSnapshot|TestTaskExecutionAgentProfile' -count=1
```

Expected: FAIL because schema-v2 types and fingerprints do not exist.

- [ ] **Step 3: Implement snapshot value objects and canonical hashing**

```go
type AgentModelMatrix struct {
    Default string `json:"default"`
    Opus string `json:"opus"`
    Fable string `json:"fable"`
    Sonnet string `json:"sonnet"`
    Haiku string `json:"haiku"`
}

type AgentProfileSnapshot struct {
    SchemaVersion int `json:"schema_version"`
    ProfileID string `json:"profile_id"`
    DisplayName string `json:"display_name"`
    Provider string `json:"provider"`
    Protocol string `json:"protocol"`
    Models AgentModelMatrix `json:"models"`
    Claude AgentClaudeControls `json:"claude"`
    ModelUsageAliases map[string]string `json:"model_usage_aliases"`
}
```

Define `AgentClaudeControls` with the same 12 pointer fields as Task 1. Implement `ValidateAgentProfileSnapshot`. For `AgentProfileFingerprint`, convert aliases to a sorted `[]struct{Raw, Canonical string}`, marshal a canonical struct, hash with SHA-256, and encode lowercase hex.

- [ ] **Step 4: Replace durable execution fields**

Add `Task.AgentProfileFingerprint` as non-null `char(64)`. Replace `TaskExecution.ModelID`, `Protocol`, `ReasoningEffort`, and `ContextWindow` with:

```go
ExecutionProfile string `gorm:"type:varchar(40);not null;index" json:"execution_profile"`
Provider string `gorm:"type:varchar(80);not null;index" json:"provider"`
ModelMatrix AgentModelMatrix `gorm:"type:json;serializer:json;not null" json:"model_matrix"`
ClaudeControls AgentClaudeControls `gorm:"type:json;serializer:json;not null" json:"claude_controls"`
ProfileFingerprint string `gorm:"type:char(64);not null;index" json:"profile_fingerprint"`
```

Change `NewTaskExecutionAgentProfile` to accept snapshot plus fingerprint and copy these final fields.

- [ ] **Step 5: Run model tests and commit**

```bash
go test ./server/model -count=1
git add server/model/agent_profile.go server/model/task.go server/model/task_execution.go server/model/task_execution_profile_test.go server/model/task_billing_visibility_test.go
git commit -m "feat(model): freeze agent model matrix snapshots"
```

Expected: PASS with no single-model execution GORM columns.

### Task 3: Rebuild the Registry Around Providers and Costs

**Files:**
- Modify: `server/service/agent_profiles.go`
- Modify: `server/service/agent_profiles_test.go`
- Modify: `server/main.go`
- Modify: `server/main_test.go`

- [ ] **Step 1: Write failing arbitrary-Provider and availability tests**

```go
func TestAgentProfileRegistryDoesNotHardCodeProviderIdentity(t *testing.T) {
    costs := billing.CostCatalog{Models: map[string]billing.ModelCostConfig{"zhipu/glm-5.2": {PricingType: "token"}}}
    registry, err := NewAgentProfileRegistryFromConfig(
        map[string]config.ClaudeProviderConfig{"zhipu": {Protocol: "anthropic", BaseURL: "https://open.bigmodel.cn/api/anthropic", AuthToken: "secret"}},
        map[string]config.ClaudeExecutionProfileConfig{"cost_effective": {
            Provider: "zhipu",
            Models: config.ClaudeModelMatrixConfig{Default: "glm-5.2", Opus: "glm-5.2", Fable: "glm-5.2", Sonnet: "glm-5.2", Haiku: "glm-5.2"},
            ModelUsageAliases: map[string]string{"glm-5.2": "glm-5.2"},
        }}, costs,
    )
    if err != nil { t.Fatal(err) }
    got, err := registry.ResolveForTier("cost_effective", model.TierFree)
    if err != nil || got.Provider != "zhipu" || got.Models.Default != "glm-5.2" { t.Fatalf("got=%#v err=%v", got, err) }
}
```

Add a second test with mapped `cost_effective` and unmapped `balanced`; assert only `balanced` is unavailable with `agent_model_cost_unmapped`.

Add `TestAgentProfileRegistryConstructionDoesNotProbeProviderNetwork` with an unreachable but syntactically valid HTTPS endpoint and a complete cost mapping; assert construction succeeds without starting an HTTP server or dialing the endpoint.

- [ ] **Step 2: Write failing retry resolution tests**

Freeze K2.7, change current `maximum_quality` to K3, rotate the Moonshot token, and assert `ResolveRuntime` keeps K2.7 but returns the new token. Delete Moonshot and assert `ErrAgentProviderUnavailable`, not fallback.

- [ ] **Step 3: Run registry tests and verify RED**

```bash
go test ./server/service -run 'TestAgentProfileRegistry|TestResolveRuntime' -count=1
```

Expected: FAIL because Provider/model identities are hard-coded and retry compares against current models.

- [ ] **Step 4: Implement stable product metadata and dynamic records**

Keep only this hard-coded product map:

```go
var agentProfileProducts = map[string]struct{ DisplayName string; MinTier model.Tier }{
    "cost_effective": {"性价比", model.TierFree},
    "balanced": {"平衡型", model.TierPro},
    "maximum_quality": {"极致效果", model.TierEnterprise},
}
```

Build three rows from Provider/Profile config. Missing blocks become `profile_configuration_missing`. Validate aliases for all distinct matrix values and `subagent_model`; require `costs.Models[provider+"/"+canonical]`. Use `agent_provider_unavailable` and `agent_model_cost_unmapped` availability reasons. `ResolveRuntime` validates snapshot/fingerprint, resolves only the frozen Provider's live connection, and copies frozen models/controls/aliases without comparing current Profile models.

- [ ] **Step 5: Load the billing bundle once before registry construction**

```go
billingBundle, err := serverbilling.LoadBundle(cfg.BillingRuntime.ConfigDir)
if err != nil { log.Fatal().Err(err).Msg("load billing bundle") }
agentProfiles, err := service.NewAgentProfileRegistryFromConfig(cfg.Claude.Providers, cfg.Claude.ExecutionProfiles, billingBundle.Costs)
if err != nil { log.Fatal().Err(err).Msg("invalid agent execution profile configuration") }
```

Change `buildBillingRuntime` to accept this bundle and remove its second disk load. Add a startup test proving registry and catalog use the same in-memory bundle.

- [ ] **Step 6: Run tests and commit**

```bash
go test ./server/service -run 'TestAgentProfileRegistry|TestResolveRuntime' -count=1
go test ./server -run 'Test.*Billing.*Bundle|TestAgentExecutionProfile' -count=1
git add server/service/agent_profiles.go server/service/agent_profiles_test.go server/main.go server/main_test.go
git commit -m "feat(agent): resolve profiles from provider registry"
```

Expected: PASS and no expected Provider/Model tuple remains.

### Task 4: Revise the Forward-only Database Migration

**Files:**
- Modify: `server/migrations/20260728_agent_execution_profiles.sql`
- Modify: `server/migrations/migrations_test.go`
- Modify: `server/main.go`
- Modify: `server/main_test.go`

- [ ] **Step 1: Replace migration expectations with final fields**

Require these fragments in `TestAgentExecutionProfilesMigration`:

```go
required := []string{
    "ADD COLUMN `agent_profile_fingerprint` char(64) NULL",
    "'schema_version', 2",
    "'models', JSON_OBJECT(",
    "'default', 'doubao-seed-evolving'",
    "'claude', JSON_OBJECT()",
    "ADD COLUMN `execution_profile` varchar(40) NULL",
    "ADD COLUMN `model_matrix` json NULL",
    "ADD COLUMN `claude_controls` json NULL",
    "ADD COLUMN `profile_fingerprint` char(64) NULL",
    "DROP COLUMN `model_id`",
    "DROP COLUMN `protocol`",
    "DROP COLUMN `reasoning_effort`",
    "DROP COLUMN `context_window`",
}
```

Build the historical snapshot with `model.AgentProfileFingerprint`, assert the resulting 64-character constant appears in the SQL, and assert there is no `SHA2(CAST(agent_profile_snapshot AS CHAR)` expression. Also assert nullable additions precede backfill, backfill precedes non-null constraints, and old columns are dropped last.

- [ ] **Step 2: Run migration tests and verify RED**

```bash
go test ./server/migrations ./server -run 'TestAgentExecutionProfilesMigration|TestAgentExecutionProfileSchemaReadiness' -count=1
```

Expected: FAIL because SQL/readiness target the intermediate single-model schema.

- [ ] **Step 3: Rewrite the migration**

Backfill historical tasks as `balanced` with schema version 2, Provider `volcengine_ark`, all five roles `doubao-seed-evolving`, empty `claude`, and alias `doubao-seed-evolving -> doubao-seed-evolving`. Compute its fingerprint once with the Go canonical implementation, embed that exact lowercase constant in SQL, and make the migration test recompute and compare it. Do not hash MySQL's JSON rendering. Backfill execution rows with:

```sql
SET `execution`.`execution_profile` = `task`.`execution_profile`,
    `execution`.`provider` = JSON_UNQUOTE(JSON_EXTRACT(`task`.`agent_profile_snapshot`, '$.provider')),
    `execution`.`model_matrix` = JSON_EXTRACT(`task`.`agent_profile_snapshot`, '$.models'),
    `execution`.`claude_controls` = JSON_EXTRACT(`task`.`agent_profile_snapshot`, '$.claude'),
    `execution`.`profile_fingerprint` = `task`.`agent_profile_fingerprint`;
```

Then enforce non-null fields/indexes and drop the four intermediate execution columns.

- [ ] **Step 4: Update startup readiness**

Require `Task.AgentProfileFingerprint` plus `TaskExecution.ExecutionProfile`, `Provider`, `ModelMatrix`, `ClaudeControls`, and `ProfileFingerprint`. Remove readiness checks for `ModelID`, `Protocol`, `ReasoningEffort`, and `ContextWindow`.

- [ ] **Step 5: Run tests and commit**

```bash
go test ./server/migrations ./server -run 'TestAgentExecutionProfilesMigration|TestAgentExecutionProfileSchemaReadiness' -count=1
git add server/migrations/20260728_agent_execution_profiles.sql server/migrations/migrations_test.go server/main.go server/main_test.go
git commit -m "feat(db): migrate agent profile matrices"
```

Expected: PASS.

### Task 5: Freeze Profiles Across Task, Plan, Quote, Retry, and Clone

**Files:**
- Modify: `server/service/task.go`
- Modify: `server/service/task_retry.go`
- Modify: `server/service/task_execution_reconcile.go`
- Modify: `server/service/task_dispatch.go`
- Modify: `server/service/plan.go`
- Modify: `server/service/billing_catalog.go`
- Modify: `server/service/task_test.go`
- Modify: `server/service/task_dispatch_test.go`
- Modify: `server/service/plan_test.go`
- Modify: `server/service/billing_catalog_test.go`
- Modify: `server/handler/task.go`, `server/handler/plan.go`, `server/handler/billing.go`
- Modify: `server/handler/task_test.go`
- Modify: `server/handler/plan_test.go`
- Modify: `server/handler/billing_test.go`
- Modify: `server/handler/ai_entry_test.go`

- [ ] **Step 1: Add failing create and clone tests**

```go
func TestTaskCreateFreezesMatrixAndFingerprint(t *testing.T) {
    svc := newProfileTaskService(t, registryUsing("balanced", "zhipu", "glm-5.2"))
    tasks, err := svc.CreateManual(context.Background(), CreateManualParams{
        UserID: proUserID, ProjectID: projectID, Type: model.PlatformArticle,
        ExecutionProfile: "balanced", Prompt: "write",
    })
    if err != nil { t.Fatal(err) }
    task := tasks[0]
    if task.AgentProfileSnapshot.Models.Default != "glm-5.2" || len(task.AgentProfileFingerprint) != 64 {
        t.Fatalf("snapshot=%#v fingerprint=%q", task.AgentProfileSnapshot, task.AgentProfileFingerprint)
    }
}
```

Add a clone test inserting a source snapshot with `kimi-k2.7-code`, configuring current `maximum_quality` as `kimi-k3[1m]`, cloning with the same Profile ID, and asserting only the clone uses K3.

- [ ] **Step 2: Add failing retry and plan tests**

Assert retry execution copies the task's original matrix/fingerprint after current Profile changes. Assert each scheduled task freezes configuration at trigger time. Assert a deleted frozen Provider returns `ErrAgentProviderUnavailable` before creating an execution.

- [ ] **Step 3: Add failing structured-error tests**

For task, plan, and quote handlers, assert response `code` for:

```text
agent_profile_not_found
agent_profile_unavailable
agent_profile_access_denied
agent_profile_snapshot_invalid
agent_profile_snapshot_conflict
agent_provider_unavailable
agent_model_cost_unmapped
billing_profile_sku_not_found
```

Use 403 for access denial, 409 for snapshot conflict, 422 for unavailable cost/Provider/SKU dependencies, and 400 for invalid Profile input.

- [ ] **Step 4: Run lifecycle tests and verify RED**

```bash
go test ./server/service ./server/handler -run 'Test(Task.*Profile|Task.*Fingerprint|Task.*Clone|Task.*Retry|Plan.*Profile|.*AgentProfile.*Error)' -count=1
```

Expected: FAIL because final fingerprints and structured errors are not propagated.

- [ ] **Step 5: Implement one freezing helper and wire every creation path**

```go
func (p AgentExecutionProfile) Freeze() (model.AgentProfileSnapshot, string, error) {
    snapshot := model.AgentProfileSnapshot{
        SchemaVersion: 2, ProfileID: p.ID, DisplayName: p.DisplayName,
        Provider: p.Provider, Protocol: p.Protocol, Models: p.Models,
        Claude: cloneClaudeControls(p.Claude),
        ModelUsageAliases: cloneModelUsageAliasTargets(p.ModelUsageAliases),
    }
    fingerprint, err := model.AgentProfileFingerprint(snapshot)
    return snapshot, fingerprint, err
}
```

Call it from manual/AI-entry/bulk create, clone, and plan-trigger creation. Quote creation stores the same snapshot JSON. Retry and reconciliation call `NewTaskExecutionAgentProfile(task.AgentProfileSnapshot, task.AgentProfileFingerprint)` and never re-freeze.

- [ ] **Step 6: Implement one handler error mapper**

Map service sentinel errors to the status/code table in Step 3 using the existing response envelope. Include a user-facing hint; never expose config paths, Endpoint, or Secret values.

- [ ] **Step 7: Run lifecycle tests and commit**

```bash
go test ./server/service ./server/handler -run 'Test(Task.*Profile|Task.*Fingerprint|Task.*Clone|Task.*Retry|Plan.*Profile|.*AgentProfile.*Error)' -count=1
git add server/service/task.go server/service/task_retry.go server/service/task_execution_reconcile.go server/service/task_dispatch.go server/service/plan.go server/service/billing_catalog.go server/service/*profile*test.go server/service/task_test.go server/service/task_dispatch_test.go server/service/plan_test.go server/service/billing_catalog_test.go server/handler/task.go server/handler/plan.go server/handler/billing.go server/handler/task_test.go server/handler/plan_test.go server/handler/billing_test.go
git commit -m "feat(agent): freeze profile matrices across task lifecycle"
```

Expected: PASS.

### Task 6: Generate the Dynamic Go Bootstrap Environment

**Files:**
- Modify: `server/service/agent_bootstrap.go`
- Modify: `server/service/agent_bootstrap_profile_test.go`
- Modify: `server/service/agent_bootstrap_test.go`
- Modify: `server/main.go`

- [ ] **Step 1: Add failing environment mapping tests**

```go
func TestAgentRuntimeProfileEmitsConfiguredFalseAndZero(t *testing.T) {
    zero, disabled, pct := 0, false, 85
    runtime := testRuntimeProfile(model.AgentClaudeControls{
        MaxThinkingTokens: &zero,
        EnableToolSearch: &disabled,
        AutocompactPctOverride: &pct,
    })
    env := runtime.RuntimeEnv()
    if env["MAX_THINKING_TOKENS"] != "0" || env["ENABLE_TOOL_SEARCH"] != "false" || env["CLAUDE_AUTOCOMPACT_PCT_OVERRIDE"] != "85" {
        t.Fatalf("runtime env = %#v", env)
    }
    if _, exists := env["CLAUDE_CODE_MAX_OUTPUT_TOKENS"]; exists { t.Fatal("omitted control emitted") }
}
```

Add table cases for all 12 controls and exact assertions for all five model-role variables.

- [ ] **Step 2: Add a failing Bootstrap JSON contract test**

Require `models`, `claude`, `profile_fingerprint`, and structured alias identities. Forbid singular `model_id`, `context_window`, `reasoning_effort`, and `thinking_required`; forbid Endpoint/Token outside `runtime_env`.

- [ ] **Step 3: Run Bootstrap tests and verify RED**

```bash
go test ./server/service -run 'TestAgent(RuntimeProfile|Bootstrap)' -count=1
```

Expected: FAIL on the single-model shape.

- [ ] **Step 4: Implement the allowlisted environment builder**

Always emit:

```go
env := map[string]string{
    "ANTHROPIC_BASE_URL": provider.BaseURL,
    "ANTHROPIC_AUTH_TOKEN": provider.AuthToken,
    "ANTHROPIC_MODEL": snapshot.Models.Default,
    "ANTHROPIC_DEFAULT_OPUS_MODEL": snapshot.Models.Opus,
    "ANTHROPIC_DEFAULT_FABLE_MODEL": snapshot.Models.Fable,
    "ANTHROPIC_DEFAULT_SONNET_MODEL": snapshot.Models.Sonnet,
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": snapshot.Models.Haiku,
}
```

Append only non-nil controls with `strconv.Itoa`/`strconv.FormatBool`. Remove `RuntimeControls` from `AgentBootstrapServiceConfig` and startup wiring.

Use this exact control mapping:

| Go field | Environment variable |
| --- | --- |
| `EffortLevel` | `CLAUDE_CODE_EFFORT_LEVEL` |
| `AlwaysEnableEffort` | `CLAUDE_CODE_ALWAYS_ENABLE_EFFORT` |
| `MaxContextTokens` | `CLAUDE_CODE_MAX_CONTEXT_TOKENS` |
| `MaxOutputTokens` | `CLAUDE_CODE_MAX_OUTPUT_TOKENS` |
| `MaxThinkingTokens` | `MAX_THINKING_TOKENS` |
| `DisableAdaptiveThinking` | `CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING` |
| `DisableThinking` | `CLAUDE_CODE_DISABLE_THINKING` |
| `AutoCompactWindow` | `CLAUDE_CODE_AUTO_COMPACT_WINDOW` |
| `AutocompactPctOverride` | `CLAUDE_AUTOCOMPACT_PCT_OVERRIDE` |
| `Disable1MContext` | `CLAUDE_CODE_DISABLE_1M_CONTEXT` |
| `SubagentModel` | `CLAUDE_CODE_SUBAGENT_MODEL` |
| `EnableToolSearch` | `ENABLE_TOOL_SEARCH` |

- [ ] **Step 5: Run tests and commit**

```bash
go test ./server/service -run 'TestAgent(RuntimeProfile|Bootstrap)' -count=1
git add server/service/agent_bootstrap.go server/service/agent_bootstrap_profile_test.go server/service/agent_bootstrap_test.go server/main.go
git commit -m "feat(agent): emit frozen Claude profile environment"
```

Expected: PASS with nil omitted and explicit zero/false emitted.

### Task 7: Publish Retail v6 and Provider-cost v4

**Files:**
- Modify: `server/billing/products.yaml`
- Modify: `server/billing/costs.yaml`
- Modify: `server/billing/catalog_contract_test.go`
- Modify: `server/billing/config_test.go`
- Modify: `server/service/provider_cost_test.go`
- Modify: `server/service/task_provider_cost_integration_test.go`
- Modify: `server/service/designer_billing_test.go`
- Modify: `studio/src/test/mocks/handlers.ts`
- Modify: `studio/src/pages/TasksPage.test.tsx`
- Modify: `studio/src/pages/PlansPage.test.tsx`
- Modify: `studio/src/components/tasks/TaskFormDialog.test.tsx`
- Modify: `miniapp/scripts/parity-test.mjs`

- [ ] **Step 1: Write failing retail catalog tests**

Assert `retail-2026-07-29-v6` and this list-price matrix:

```go
want := map[string]map[string]int64{
    "task.article": {"cost_effective": 4800, "balanced": 6000, "maximum_quality": 18000},
    "task.seednote": {"cost_effective": 4000, "balanced": 5000, "maximum_quality": 15000},
    "task.moments": {"cost_effective": 2400, "balanced": 3000, "maximum_quality": 9000},
    "task.ecommerce": {"cost_effective": 2400, "balanced": 3000, "maximum_quality": 9000},
    "task.montage": {"cost_effective": 1600, "balanced": 2000, "maximum_quality": 6000},
}
```

Assert Enterprise maximum-quality prices are 14,400 / 12,000 / 7,200 / 7,200 / 4,800.

- [ ] **Step 2: Write failing Provider-cost tests**

Assert `provider-cost-2026-07-29-v4` and quoted-decimal parsing for:

```text
deepseek/deepseek-v4-flash: USD hit .0028, miss/create .14, output .28
deepseek/deepseek-v4-pro: USD hit .003625, miss/create .435, output .87
moonshot/kimi-k3: CNY hit 2, miss/create 20, output 100
moonshot/kimi-k2.7-code: CNY hit 1.3, miss/create 6.5, output 27
moonshot/kimi-k2.7-code-highspeed: CNY hit 2.6, miss/create 13, output 54
zhipu/glm-5.2: CNY hit 2, miss/create 8, output 28
```

- [ ] **Step 3: Run billing tests and verify RED**

```bash
go test ./server/billing ./server/service -run 'TestAgentProfileCatalogV6|TestProvider.*V4|TestDesigner.*Isolation' -count=1
```

Expected: FAIL on v5/v3 identities and 1.5x maximum prices.

- [ ] **Step 4: Update catalogs and exact evidence**

Write all integer tier prices explicitly. Preserve operation/Designer SKUs and current Doubao/image Provider costs. Add official evidence URLs and `effective_at: "2026-07-29T00:00:00+08:00"` to the six model entries.

- [ ] **Step 5: Prove aggregation, unknown usage, and Designer isolation**

Finalize one execution with K3 plus K2.7 and assert both rows and summed cost. Finalize another with one unknown model and assert `unreconciled` and no zero-cost fact. Assert Designer SKUs retain empty `execution_profile`, unchanged prices/routes, and request schemas without Agent fields.

- [ ] **Step 6: Run billing tests and commit**

```bash
go test ./server/billing ./server/service -run 'TestAgentProfileCatalogV6|TestProvider.*V4|TestDesigner.*Isolation|TestProviderCost.*Multi' -count=1
git add server/billing/products.yaml server/billing/costs.yaml server/billing/catalog_contract_test.go server/billing/config_test.go server/service/provider_cost_test.go server/service/task_provider_cost_integration_test.go server/service/designer_billing_test.go studio/src/test/mocks/handlers.ts studio/src/pages/TasksPage.test.tsx studio/src/pages/PlansPage.test.tsx studio/src/components/tasks/TaskFormDialog.test.tsx miniapp/scripts/parity-test.mjs
git commit -m "feat(billing): publish agent profile v6 pricing"
```

Expected: PASS.
### Task 8: Replace the TypeScript Agent Single-model Contract

**Files:**
- Modify: `agent-ts/src/bootstrap.ts`
- Modify: `agent-ts/src/runner.ts`
- Modify: `agent-ts/test/bootstrap.test.ts`
- Modify: `agent-ts/test/runner.test.ts`

- [ ] **Step 1: Replace the fixture and add failing structural tests**

Use this execution Profile fixture:

```ts
execution_profile: {
  profile_id: "maximum_quality",
  provider: "moonshot",
  protocol: "anthropic",
  models: { default: "kimi-k3[1m]", opus: "kimi-k3[1m]", fable: "kimi-k3[1m]", sonnet: "kimi-k3[1m]", haiku: "kimi-k3[1m]" },
  claude: { max_thinking_tokens: 0, enable_tool_search: false },
  display_name: "极致效果",
  profile_fingerprint: "a".repeat(64),
  runtime_env: {
    ANTHROPIC_BASE_URL: "https://api.moonshot.cn/anthropic",
    ANTHROPIC_AUTH_TOKEN: "secret",
    ANTHROPIC_MODEL: "kimi-k3[1m]",
    ANTHROPIC_DEFAULT_OPUS_MODEL: "kimi-k3[1m]",
    ANTHROPIC_DEFAULT_FABLE_MODEL: "kimi-k3[1m]",
    ANTHROPIC_DEFAULT_SONNET_MODEL: "kimi-k3[1m]",
    ANTHROPIC_DEFAULT_HAIKU_MODEL: "kimi-k3[1m]",
    MAX_THINKING_TOKENS: "0",
    ENABLE_TOOL_SEARCH: "false",
  },
  model_usage_aliases: { "kimi-k3[1m]": { provider: "moonshot", model: "kimi-k3" } },
}
```

Accept arbitrary Provider/model strings when structure agrees. Reject `PATH`, singular `model_id`, a role/environment mismatch, invalid fingerprint, missing alias, and any unknown Claude runtime key.

- [ ] **Step 2: Add failing environment-only runner tests**

```ts
test("does not translate frozen controls into SDK-only options", () => {
  const options = buildQueryOptions(validBootstrap(), "/workspace");
  expect(options).not.toHaveProperty("effort");
  expect(options).not.toHaveProperty("thinking");
  expect(options).not.toHaveProperty("model");
  expect(options.env).toMatchObject({
    ANTHROPIC_MODEL: "kimi-k3[1m]",
    MAX_THINKING_TOKENS: "0",
    ENABLE_TOOL_SEARCH: "false",
  });
});
```

Add two-model terminal usage and mixed mapped/unmapped tests.

- [ ] **Step 3: Run tests and verify RED**

```bash
(cd agent-ts && bun run test)
```

Expected: FAIL because `EXPECTED_PROFILES`, `model_id`, and SDK reasoning translation remain.

- [ ] **Step 4: Implement dynamic validation and environment execution**

Delete `EXPECTED_PROFILES` and `executionReasoningOptions`. Define matrix/control interfaces matching Go. Validate stable Profile ID, protocol, five roles, lowercase hex fingerprint, mandatory environment equality, the exact approved allowlist, and alias Provider equality. Build query options without `model`, `effort`, or `thinking`; retain permissions/hooks/MCP/plugin/turn-limit behavior. Use environment precedence:

```ts
return {
  ...processEnv,
  ...data.env,
  ANBAN_API_KEY: token,
  ANBAN_API_URL: serverURL,
  ANBAN_DEFAULT_PROJECT: data.project_id,
  ...data.execution_profile.runtime_env,
};
```

Never log or serialize `runtime_env`. Keep every mapped terminal row; if any row is invalid or unmapped, return `unreconciled` with diagnostics and never synthesize zero usage.

- [ ] **Step 5: Run complete Agent TS verification and commit**

```bash
(cd agent-ts && bun run test)
(cd agent-ts && bun run typecheck)
(cd agent-ts && bun run build)
(cd agent-ts && npm audit --omit=dev)
git add agent-ts/src/bootstrap.ts agent-ts/src/runner.ts agent-ts/test/bootstrap.test.ts agent-ts/test/runner.test.ts
git commit -m "feat(agent-ts): execute frozen model matrices"
```

Expected: all commands exit 0; audit reports zero production vulnerabilities.

### Task 9: Return Dynamic Capabilities and Update Studio

**Files:**
- Modify: `server/handler/agent_profiles.go`
- Modify: `server/handler/agent_profiles_test.go`
- Modify: `studio/src/types/agent-profile.ts`
- Modify: `studio/src/types/task.ts`
- Modify: `studio/src/lib/schemas.ts`
- Modify: `studio/src/lib/api/agent-profiles.test.ts`
- Modify: `studio/src/components/tasks/ExecutionProfileSelector.tsx`
- Modify: `studio/src/components/tasks/ExecutionProfileSelector.test.tsx`
- Modify: `studio/src/components/tasks/TaskConfigurationDetails.tsx`
- Modify: `studio/src/pages/TaskDetailPage.test.tsx`
- Modify: `studio/src/test/mocks/handlers.ts`
- Verify unchanged: `studio/src/lib/api/designer.test.ts`
- Verify unchanged: `studio/src/components/auth/UserAccountPopover.test.tsx`

- [ ] **Step 1: Add a failing API secrecy/shape test**

Require `provider`, `protocol`, `models`, and `claude`; forbid `model_id`, `model_name`, Endpoint, Token, and `runtime_env`:

```go
if strings.Contains(body, "api.moonshot.cn") || strings.Contains(body, "provider-secret") {
    t.Fatalf("capability leaked connection data: %s", body)
}
```

- [ ] **Step 2: Add failing selector and frozen-detail tests**

```tsx
it('shows a compact all-role label for a uniform matrix', () => {
  render(<ExecutionProfileSelector profiles={[kimiProfile]} value="" onChange={() => {}} />)
  expect(screen.getByText('全部角色：kimi-k3[1m]')).toBeInTheDocument()
})

it('shows split model roles', async () => {
  render(<ExecutionProfileSelector profiles={[deepseekProfile]} value="" onChange={() => {}} />)
  expect(screen.getByText('默认：deepseek-v4-flash')).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: /查看模型配置/ }))
  expect(screen.getByText('Opus：deepseek-v4-pro')).toBeInTheDocument()
})
```

Return current K3 capability but a K2.7 task snapshot; assert task details show K2.7 only.

- [ ] **Step 3: Run focused tests and verify RED**

```bash
go test ./server/handler -run TestAgentProfile -count=1
(cd studio && bun run test -- src/components/tasks/ExecutionProfileSelector.test.tsx src/pages/TaskDetailPage.test.tsx src/lib/api/agent-profiles.test.ts)
```

Expected: FAIL on singular fields.

- [ ] **Step 4: Implement shared TS types and UI**

```ts
export interface AgentModelMatrix {
  default: string; opus: string; fable: string; sonnet: string; haiku: string
}

export interface AgentClaudeControls {
  effort_level?: 'low' | 'medium' | 'high' | 'max'
  always_enable_effort?: boolean
  max_context_tokens?: number
  max_output_tokens?: number
  max_thinking_tokens?: number
  disable_adaptive_thinking?: boolean
  disable_thinking?: boolean
  auto_compact_window?: number
  autocompact_pct_override?: number
  disable_1m_context?: boolean
  subagent_model?: string
  enable_tool_search?: boolean
}
```

Capability uses Provider/protocol/matrix/controls. Snapshot adds schema version and aliases; task carries fingerprint. Keep three selector tiles, show Provider/default, and use an existing Popover/Collapsible for split roles. Task details read only the frozen snapshot and show only configured controls.

- [ ] **Step 5: Verify Studio and Designer, then commit**

```bash
(cd studio && bun run test)
(cd studio && bun run build)
git add server/handler/agent_profiles.go server/handler/agent_profiles_test.go studio/src/types/agent-profile.ts studio/src/types/task.ts studio/src/lib/schemas.ts studio/src/lib/api/agent-profiles.test.ts studio/src/components/tasks/ExecutionProfileSelector.tsx studio/src/components/tasks/ExecutionProfileSelector.test.tsx studio/src/components/tasks/TaskConfigurationDetails.tsx studio/src/pages/TaskDetailPage.test.tsx studio/src/test/mocks/handlers.ts
git commit -m "feat(studio): display configured agent model matrices"
```

Expected: all Studio tests/build pass; Designer tests still prove no `execution_profile` in requests, and Avatar Popover wallet/balance/retry tests remain green.

### Task 10: Update Miniapp Capability and Frozen Detail Contracts

**Files:**
- Modify: `miniapp/src/types/agent-profile.ts`
- Modify: `miniapp/src/types/task.ts`
- Modify: `miniapp/src/components/business/ExecutionProfileSelector.vue`
- Modify: `miniapp/src/pages/tasks/create.vue`
- Modify: `miniapp/src/pages/plans/create.vue`
- Modify: `miniapp/src/pages/tasks/detail.vue`
- Modify: `miniapp/scripts/parity-test.mjs`

- [ ] **Step 1: Add failing parity assertions**

```js
for (const token of ['models: AgentModelMatrix', 'claude: AgentClaudeControls', 'schema_version: 2', 'provider: string']) {
  assert(source.includes(token), `missing ${token}`)
}
for (const legacy of ['model_name: string', 'model_id: string', 'thinking_required']) {
  assert(!agentProfileSource.includes(legacy), `legacy field remains: ${legacy}`)
}
```

Also assert create/plan payloads contain only `execution_profile`, not Provider/models/controls.

- [ ] **Step 2: Run parity and verify RED**

```bash
(cd miniapp && npm run test)
```

Expected: FAIL on singular Profile types.

- [ ] **Step 3: Implement matching types and UI**

Mirror Studio interfaces exactly. Show Provider plus uniform “全部角色” or five compact role lines. Map `requires_pro`, `requires_enterprise`, `agent_provider_unavailable`, and `agent_model_cost_unmapped` reasons. Render task details from `task.agent_profile_snapshot`, omit undefined controls, and preserve the existing total billing summary/operation drill-down.

- [ ] **Step 4: Run Miniapp verification and commit**

```bash
(cd miniapp && npm run test)
(cd miniapp && npm run type-check)
git add miniapp/src/types/agent-profile.ts miniapp/src/types/task.ts miniapp/src/components/business/ExecutionProfileSelector.vue miniapp/src/pages/tasks/create.vue miniapp/src/pages/plans/create.vue miniapp/src/pages/tasks/detail.vue miniapp/scripts/parity-test.mjs
git commit -m "feat(miniapp): show dynamic agent model matrices"
```

Expected: PASS.

### Task 11: Update Runtime Smoke and Three TypeScript Images

**Files:**
- Modify: `deploy/docker/runtime-smoke.sh`
- Modify: `deploy/docker/runtime-smoke_test.go`
- Verify/modify: `deploy/docker/Dockerfile.agent-article-ts`
- Verify/modify: `deploy/docker/Dockerfile.agent-seednote-ts`
- Verify/modify: `deploy/docker/Dockerfile.agent-montage-ts`
- Modify: `server/agent/typescript_runtime_docker_contract_test.go`
- Modify: `server/agent/docker_runtime_contract_test.go`

- [ ] **Step 1: Add failing smoke-schema tests**

Assert smoke YAML includes `providers`, `models`, all five roles, aliases, and typed `claude`; forbid Profile-level `model_id`, `base_url`, `auth_token`, and top-level `claude.env`.

```go
for _, required := range []string{"providers:", "models:", "default:", "opus:", "fable:", "sonnet:", "haiku:", "model_usage_aliases:", "claude:"} {
    if !strings.Contains(config, required) { t.Errorf("smoke config missing %q", required) }
}
```

Add a three-Dockerfile consistency test: each copies `agent-ts/package.json` and lockfile, retains optional dependencies, and does not pin a different Claude Agent SDK/Claude Code version.

- [ ] **Step 2: Run static tests and verify RED**

```bash
go test ./deploy/docker ./server/agent -run 'TestRuntimeSmoke|Test.*TypeScript.*Image' -count=1
bash -n deploy/docker/runtime-smoke.sh
```

Expected: FAIL until smoke config uses the matrix schema.

- [ ] **Step 3: Update runtime smoke**

Generate one valid DeepSeek V4 Flash `cost_effective` Profile, retain `execution_profile: "cost_effective"` in article and Montage task bodies, and require only that Profile's credentials. Log Provider/model names but never tokens.

- [ ] **Step 4: Run static tests and all Docker images**

```bash
go test ./deploy/docker ./server/agent -run 'TestRuntimeSmoke|Test.*TypeScript.*Image' -count=1
bash -n deploy/docker/runtime-smoke.sh
make docker-agent-ts-image
make docker-seednote-agent-ts-image
make docker-montage-agent-ts-image
ANBAN_RUNTIME_SMOKE_CLIENT=ts deploy/docker/runtime-smoke.sh article
ANBAN_RUNTIME_SMOKE_CLIENT=ts deploy/docker/runtime-smoke.sh seednote
ANBAN_RUNTIME_SMOKE_CLIENT=ts deploy/docker/runtime-smoke.sh montage
```

Expected: static tests pass; each built image reaches Bootstrap, dispatch, and terminal result. If Docker is unavailable, record that exact gap and do not claim image execution passed.

- [ ] **Step 5: Commit**

```bash
git add deploy/docker/runtime-smoke.sh deploy/docker/runtime-smoke_test.go deploy/docker/Dockerfile.agent-article-ts deploy/docker/Dockerfile.agent-seednote-ts deploy/docker/Dockerfile.agent-montage-ts server/agent
git commit -m "test(runtime): cover dynamic profiles in agent images"
```

### Task 12: Full Verification, Review, and Main Integration

**Files:**
- Review: every file changed since the worktree base
- Preserve: all unrelated user files

- [ ] **Step 1: Format and check the diff**

```bash
gofmt -w $(git diff --name-only --diff-filter=ACM -- '*.go')
git diff --check
```

Expected: no `git diff --check` output.

- [ ] **Step 2: Run complete backend verification**

```bash
go test ./...
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
```

Expected: all exit 0. Isolate and rerun any transient parallel Go failure serially before attributing it.

- [ ] **Step 3: Run complete TypeScript/frontend verification**

```bash
(cd agent-ts && bun run test)
(cd agent-ts && bun run typecheck)
(cd agent-ts && bun run build)
(cd agent-ts && npm audit --omit=dev)
(cd studio && bun run test)
(cd studio && bun run build)
(cd miniapp && npm run test)
(cd miniapp && npm run type-check)
```

Expected: all exit 0; audit reports zero production vulnerabilities.

- [ ] **Step 4: Review the complete diff against the specification**

Check each item explicitly:

```text
No Provider/Model identity hard-coded by Profile
No old single-model field or compatibility parser
No endpoint/token in snapshots, APIs, logs, fixtures, or commits
All five model roles and 12 typed Claude controls
false/0/null/omitted semantics
Retry frozen; clone and plan resolve current configuration
Cost mapping fail-closed; unknown usage unreconciled
Maximum-quality 3.0x and immutable catalog IDs
Designer isolation
Three TypeScript image parity
Migration ordering and final non-null schema
```

Invoke `superpowers:requesting-code-review`; process findings with `superpowers:receiving-code-review`; rerun affected and complete verification after fixes.

- [ ] **Step 5: Commit any review fixes**

Stage only files changed for findings and use:

```bash
git commit -m "fix(agent): address model matrix review"
```

Skip when the worktree is clean.

- [ ] **Step 6: Integrate after verification**

Invoke `superpowers:finishing-a-development-branch`. Then fast-forward the verified branch into the original local `main`, push, and prove publication:

```bash
git -C /Users/medivh/workspace/anbanwriter fetch origin main
git -C /Users/medivh/workspace/anbanwriter merge --ff-only codex/agent-model-matrix-config
git -C /Users/medivh/workspace/anbanwriter push origin main
git -C /Users/medivh/workspace/anbanwriter rev-parse HEAD
git -C /Users/medivh/workspace/anbanwriter rev-parse origin/main
```

Expected: local and remote SHAs are identical. Report Docker validation separately if it could not run locally.
