# Agent Profile Unified Envs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rename Agent execution profiles to `effective`, `balanced`, and `quality`, and make each profile's strict `envs` map the only Claude Code runtime configuration source.

**Architecture:** A shared Go environment contract validates and redacts the 19 supported Claude variables. Tasks freeze a schema-v3 non-secret environment snapshot; Bootstrap merges only the current auth token into that frozen snapshot. Operational data is migrated with expand/backfill/contract phases, while immutable historical billing records remain unchanged and a new catalog serves new requests.

**Tech Stack:** Go 1.26, Fiber v3, GORM/MySQL 8, YAML v3, Bun/TypeScript, React 19, Vue/UniApp, Vitest.

---

## File Map

- `server/model/claude_profile_env.go`: canonical Go allowlist, validation, redaction, model extraction.
- `server/model/agent_profile.go`: schema-v3 frozen snapshot and canonical fingerprint.
- `server/config/config.go`: strict `execution_profiles.<id>.envs` YAML contract.
- `server/service/agent_profiles.go`: product registry, capability projection, freeze/runtime resolution.
- `server/service/agent_bootstrap.go`: token merge and secret-free Bootstrap construction.
- `server/model/task_execution.go`: durable non-secret `profile_envs` execution fields.
- `server/migrations/20260730_agent_profile_envs_expand.sql`: additive schema phase.
- `server/migrations/20260730_agent_profile_envs_expire_quotes.sql`: expires only unconsumed old-catalog Quotes.
- `server/migrations/agent_profile_envs_backfill.go`: resumable operational-data conversion.
- `server/cmd/agent-profile-envs-backfill/main.go`: maintenance-window backfill command.
- `server/migrations/20260730_agent_profile_envs_contract.sql`: final constraints and obsolete-column removal.
- `server/billing/products.yaml`: immutable v7 catalog using new profile IDs.
- `agent/bootstrap.go`: Go Agent schema-v3 Bootstrap validation.
- `agent-ts/src/bootstrap.ts`: TypeScript Agent schema-v3 Bootstrap validation.
- `studio/src/types/agent-profile.ts`, `studio/src/lib/schemas.ts`: Studio contract.
- `miniapp/src/types/agent-profile.ts`, `miniapp/scripts/parity-test.mjs`: Miniapp contract and parity.
- `server/config.yaml`, `server/config.example.yaml`, `.env.example`, `README.md`: deployment examples and runbook.

### Task 1: Canonical Claude Environment Contract and Snapshot v3

**Files:**
- Create: `server/model/claude_profile_env.go`
- Create: `server/model/claude_profile_env_test.go`
- Modify: `server/model/agent_profile.go`
- Modify: `server/model/task_execution_profile_test.go`
- Modify: `server/model/task_billing_visibility_test.go`

- [ ] **Step 1: Write failing environment-contract tests**

Add table-driven tests for all 19 keys, required connection/model keys, optional omission, strict booleans/integers, HTTPS URL rules, size limits, and token redaction:

```go
func TestValidateClaudeProfileEnvs(t *testing.T) {
	valid := map[string]string{
		"ANTHROPIC_BASE_URL": "https://api.example.com/anthropic",
		"ANTHROPIC_AUTH_TOKEN": "secret",
		"ANTHROPIC_MODEL": "model-default",
		"ANTHROPIC_DEFAULT_OPUS_MODEL": "model-opus",
		"ANTHROPIC_DEFAULT_FABLE_MODEL": "model-fable",
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "model-sonnet",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL": "model-haiku",
		"CLAUDE_CODE_EFFORT_LEVEL": "high",
		"CLAUDE_CODE_DISABLE_THINKING": "false",
		"MAX_THINKING_TOKENS": "0",
	}
	if err := ValidateClaudeProfileEnvs(valid, true); err != nil {
		t.Fatalf("valid envs rejected: %v", err)
	}
	if _, ok := RedactClaudeProfileEnvs(valid)["ANTHROPIC_AUTH_TOKEN"]; ok {
		t.Fatal("redacted envs contain ANTHROPIC_AUTH_TOKEN")
	}
}
```

- [ ] **Step 2: Run the tests and verify RED**

Run: `GOCACHE=/tmp/anban-profile-envs-go-cache go test ./server/model -run 'TestValidateClaudeProfileEnvs|TestAgentProfileSnapshot' -count=1`

Expected: FAIL because `ValidateClaudeProfileEnvs`, `RedactClaudeProfileEnvs`, and schema v3 do not exist.

- [ ] **Step 3: Implement the shared contract**

Define these stable APIs and constants:

```go
const (
	ClaudeEnvAuthToken   = "ANTHROPIC_AUTH_TOKEN"
	ClaudeEnvBaseURL     = "ANTHROPIC_BASE_URL"
	ClaudeEnvModel       = "ANTHROPIC_MODEL"
	ClaudeProfileSchemaV3 = 3
)

func ClaudeProfileEnvKeys() []string
func ValidateClaudeProfileEnvs(envs map[string]string, requireAuthToken bool) error
func RedactClaudeProfileEnvs(envs map[string]string) map[string]string
func CloneClaudeProfileEnvs(envs map[string]string) map[string]string
func ClaudeProfileReferencedModels(envs map[string]string) []string
```

Change the snapshot to:

```go
type AgentProfileSnapshot struct {
	SchemaVersion     int               `json:"schema_version"`
	ProfileID         string            `json:"profile_id"`
	DisplayName       string            `json:"display_name"`
	Provider          string            `json:"provider"`
	Protocol          string            `json:"protocol"`
	Envs              map[string]string `json:"envs"`
	ModelUsageAliases map[string]string `json:"model_usage_aliases"`
}
```

Fingerprint the schema version, identity, sorted redacted env pairs, and sorted aliases. Reject any snapshot containing `ANTHROPIC_AUTH_TOKEN`.

- [ ] **Step 4: Run model tests and verify GREEN**

Run: `GOCACHE=/tmp/anban-profile-envs-go-cache go test ./server/model -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/model/claude_profile_env.go server/model/claude_profile_env_test.go server/model/agent_profile.go server/model/task_execution_profile_test.go server/model/task_billing_visibility_test.go
git commit -m "feat(model): define agent profile env snapshot"
```

### Task 2: Strict YAML Profile Envs Configuration

**Files:**
- Modify: `server/config/config.go`
- Modify: `server/config/agent_profile_config_test.go`
- Modify: `server/config/claude_runtime_config_test.go`
- Modify: `server/config.yaml`
- Modify: `server/config.example.yaml`

- [ ] **Step 1: Write failing strict-schema tests**

Add tests proving `providers`, Profile `models`, and Profile `claude` are rejected; `effective/balanced/quality` with `envs` are accepted; and unknown env keys fail with their full YAML path.

```go
func TestClaudeConfigAcceptsOnlyProfileEnvs(t *testing.T) {
	raw := `claude:
  execution_profiles:
    effective:
      provider: deepseek
      description: economical
      envs:
        ANTHROPIC_BASE_URL: https://api.example.com/anthropic
        ANTHROPIC_AUTH_TOKEN: secret
        ANTHROPIC_MODEL: d
        ANTHROPIC_DEFAULT_OPUS_MODEL: o
        ANTHROPIC_DEFAULT_FABLE_MODEL: f
        ANTHROPIC_DEFAULT_SONNET_MODEL: s
        ANTHROPIC_DEFAULT_HAIKU_MODEL: h
      model_usage_aliases: {d: d, o: o, f: f, s: s, h: h}
`
	cfg := decodeClaudeFixture(t, raw)
	if cfg.Claude.ExecutionProfiles["effective"].Envs["ANTHROPIC_MODEL"] != "d" {
		t.Fatal("profile envs were not decoded")
	}
}
```

- [ ] **Step 2: Run config tests and verify RED**

Run: `GOCACHE=/tmp/anban-profile-envs-go-cache go test ./server/config -run 'TestClaudeConfig|TestAgentProfiles' -count=1`

Expected: FAIL because `ClaudeExecutionProfileConfig.Envs` does not exist and old fields are accepted.

- [ ] **Step 3: Replace typed/provider configuration with envs**

Use these structures:

```go
type ClaudeConfig struct {
	ExecutionProfiles map[string]ClaudeExecutionProfileConfig `yaml:"execution_profiles" json:"execution_profiles"`
	Executor string `yaml:"executor"`
	RuntimeImages RuntimeImages `yaml:"runtime_images"`
	ExecutionTokenSecret string `yaml:"execution_token_secret" json:"-"`
	PluginDir string `yaml:"plugin_dir"`
	Sandbox bool `yaml:"sandbox"`
	Docker DockerConfig `yaml:"docker"`
	Kubernetes KubernetesConfig `yaml:"kubernetes"`
	MaxTurns map[string]int `yaml:"max_turns"`
	TaskLogDir string `yaml:"task_log_dir"`
	AgentServerURL string `yaml:"agent_server_url"`
}

type ClaudeExecutionProfileConfig struct {
	Provider string `yaml:"provider" json:"provider"`
	Description string `yaml:"description" json:"description"`
	Envs map[string]string `yaml:"envs" json:"-"`
	ModelUsageAliases map[string]string `yaml:"model_usage_aliases" json:"model_usage_aliases"`
}
```

Call `model.ValidateClaudeProfileEnvs(profile.Envs, true)` from `ClaudeConfig.Validate`. Keep optional envs optional; reject unknown keys and obsolete YAML nodes during unmarshalling.

- [ ] **Step 4: Replace both YAML examples with the approved three profiles**

Use only `effective`, `balanced`, and `quality`; include the full 19-key example for each Profile. Keep credentials as `${ANBAN_*}` expansion expressions and never write literal tokens.

- [ ] **Step 5: Run config tests and verify GREEN**

Run: `GOCACHE=/tmp/anban-profile-envs-go-cache go test ./server/config -count=1`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add server/config/config.go server/config/agent_profile_config_test.go server/config/claude_runtime_config_test.go server/config.yaml server/config.example.yaml
git commit -m "feat(config): unify Claude settings under profile envs"
```

### Task 3: Registry, Capability API, and Frozen Runtime Resolution

**Files:**
- Modify: `server/service/agent_profiles.go`
- Modify: `server/service/agent_profiles_test.go`
- Modify: `server/handler/agent_profiles_test.go`
- Modify: `server/main.go`

- [ ] **Step 1: Write failing registry tests**

Cover the new IDs and access matrix, token redaction, current-token rotation, frozen non-secret values, model alias/cost checks, and rejection of old IDs.

```go
func TestResolveRuntimeMergesOnlyCurrentToken(t *testing.T) {
	profile := configuredProfile("effective", "old-token")
	registry := newRegistry(t, profile)
	snapshot, fingerprint, err := registry.profiles["effective"].Freeze()
	if err != nil { t.Fatal(err) }
	registry.profiles["effective"].Envs[model.ClaudeEnvAuthToken] = "rotated-token"
	runtime, err := registry.ResolveRuntime("effective", snapshot, fingerprint)
	if err != nil { t.Fatal(err) }
	if runtime.Envs[model.ClaudeEnvAuthToken] != "rotated-token" {
		t.Fatal("runtime did not use current token")
	}
	if _, ok := snapshot.Envs[model.ClaudeEnvAuthToken]; ok {
		t.Fatal("snapshot persisted token")
	}
}
```

- [ ] **Step 2: Run registry tests and verify RED**

Run: `GOCACHE=/tmp/anban-profile-envs-go-cache go test ./server/service ./server/handler -run 'TestAgentProfile|TestResolveRuntime' -count=1`

Expected: FAIL on old IDs/types and missing env behavior.

- [ ] **Step 3: Refactor the registry**

Use product metadata:

```go
var agentProfileProducts = map[string]struct {
	DisplayName string
	MinTier model.Tier
}{
	"effective": {DisplayName: "性价比", MinTier: model.TierFree},
	"balanced": {DisplayName: "平衡型", MinTier: model.TierPro},
	"quality": {DisplayName: "极致效果", MinTier: model.TierEnterprise},
}
```

Replace Provider maps, model matrices, and Claude controls in `AgentExecutionProfile` with `Envs map[string]string`. `Snapshot` redacts Token. `ResolveRuntime` validates the snapshot/Fingerprint, takes only the current same-profile Token, and returns the frozen env map plus Token.

Return a capability projection with `model_name` derived from `ANTHROPIC_MODEL`; do not return raw envs, Endpoint, aliases, or Token:

```go
type AgentProfileCapability struct {
	ID string `json:"id"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Provider string `json:"provider"`
	ModelName string `json:"model_name"`
	MinTier model.Tier `json:"min_tier"`
	Available bool `json:"available"`
	UnavailableReason string `json:"unavailable_reason,omitempty"`
}
```

- [ ] **Step 4: Update Server construction**

Replace:

```go
service.NewAgentProfileRegistryFromConfig(cfg.Claude.Providers, cfg.Claude.ExecutionProfiles, billingBundle.Costs)
```

with:

```go
service.NewAgentProfileRegistryFromConfig(cfg.Claude.ExecutionProfiles, billingBundle.Costs)
```

- [ ] **Step 5: Run registry and handler tests and verify GREEN**

Run: `GOCACHE=/tmp/anban-profile-envs-go-cache go test ./server/service ./server/handler -run 'TestAgentProfile|TestResolveRuntime' -count=1`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add server/service/agent_profiles.go server/service/agent_profiles_test.go server/handler/agent_profiles_test.go server/main.go
git commit -m "feat(agent): resolve frozen profile envs"
```

### Task 4: Execution Persistence Contract

**Files:**
- Modify: `server/model/task_execution.go`
- Modify: `server/model/task_execution_profile_test.go`
- Modify: `server/service/local_executor_claim.go`
- Modify: `server/service/local_executor_claim_test.go`
- Modify: `server/service/task_dispatch.go`
- Modify: `server/service/task_dispatch_test.go`
- Modify: `server/service/task_execution_reconcile.go`

- [ ] **Step 1: Write failing persistence tests**

Assert that `TaskExecution` has `ProfileEnvs`, has no `ModelMatrix` or `ClaudeControls`, copies a deep-cloned redacted map, and preserves the Fingerprint.

```go
func TestNewTaskExecutionAgentProfileCopiesFrozenEnvs(t *testing.T) {
	snapshot := AgentProfileSnapshot{SchemaVersion: 3, ProfileID: "quality", Provider: "moonshot", Protocol: "anthropic", Envs: validRedactedEnvs()}
	execution := NewTaskExecutionAgentProfile(snapshot, strings.Repeat("a", 64))
	snapshot.Envs["ANTHROPIC_MODEL"] = "changed"
	if execution.ProfileEnvs["ANTHROPIC_MODEL"] == "changed" {
		t.Fatal("execution envs alias task snapshot")
	}
}
```

- [ ] **Step 2: Run persistence tests and verify RED**

Run: `GOCACHE=/tmp/anban-profile-envs-go-cache go test ./server/model ./server/service -run 'TestNewTaskExecutionAgentProfile|TestCreateCurrentExecution|TestReplacePreStartExecution|TestLocalExecutor' -count=1`

Expected: FAIL because `ProfileEnvs` does not exist.

- [ ] **Step 3: Replace execution fields and consumers**

Use:

```go
ProfileEnvs map[string]string `gorm:"type:json;serializer:json;not null" json:"profile_envs"`
```

Delete `ModelMatrix` and `ClaudeControls`. Update execution creation, replacement, reconciliation, and local claims to copy only `snapshot.Envs`. Derive the SDK model from `snapshot.Envs[model.ClaudeEnvModel]`.

- [ ] **Step 4: Run persistence tests and verify GREEN**

Run: `GOCACHE=/tmp/anban-profile-envs-go-cache go test ./server/model ./server/service -run 'TestNewTaskExecutionAgentProfile|TestCreateCurrentExecution|TestReplacePreStartExecution|TestLocalExecutor' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/model/task_execution.go server/model/task_execution_profile_test.go server/service/local_executor_claim.go server/service/local_executor_claim_test.go server/service/task_dispatch.go server/service/task_dispatch_test.go server/service/task_execution_reconcile.go
git commit -m "refactor(agent): persist frozen profile envs"
```

### Task 5: Expand, Backfill, and Contract Database Migration

**Files:**
- Create: `server/migrations/20260730_agent_profile_envs_expand.sql`
- Create: `server/migrations/20260730_agent_profile_envs_expire_quotes.sql`
- Create: `server/migrations/20260730_agent_profile_envs_contract.sql`
- Create: `server/migrations/agent_profile_envs_backfill.go`
- Create: `server/migrations/agent_profile_envs_backfill_test.go`
- Create: `server/cmd/agent-profile-envs-backfill/main.go`
- Modify: `server/migrations/migrations_test.go`

- [ ] **Step 1: Write failing migration contract tests**

Test that Expand adds nullable `profile_envs` without dropping old columns, Contract requires preflight assertions before setting NOT NULL/dropping columns, and neither file modifies immutable billing rows. Test the Quote-expiry SQL separately: it may update only `billing_quotes.expires_at` for unconsumed, unexpired schema-v2 Quotes.

```go
func TestAgentProfileEnvsSQLDoesNotRewriteBillingHistory(t *testing.T) {
	for _, name := range []string{"20260730_agent_profile_envs_expand.sql", "20260730_agent_profile_envs_contract.sql"} {
		raw, err := os.ReadFile(name)
		if err != nil { t.Fatal(err) }
		for _, forbidden := range []string{"UPDATE `billing_skus`", "UPDATE `billing_quotes`", "UPDATE `billing_charges`", "UPDATE `billing_wallet_entries`"} {
			if strings.Contains(string(raw), forbidden) { t.Fatalf("%s mutates billing history with %s", name, forbidden) }
		}
	}
}
```

Add `TestAgentProfileEnvsQuoteExpiryIsNarrow` and require these predicates in the dedicated SQL:

```sql
UPDATE `billing_quotes`
SET `expires_at` = CURRENT_TIMESTAMP(3)
WHERE `consumed_at` IS NULL
  AND `expires_at` > CURRENT_TIMESTAMP(3)
  AND JSON_UNQUOTE(JSON_EXTRACT(`agent_profile_snapshot`, '$.schema_version')) = '2';
```

- [ ] **Step 2: Write failing backfill tests**

Use SQLite fixtures for v2 task snapshots. Assert ID conversion, v2 models/controls to string envs, current Profile Base URL insertion, Token exclusion, Go Fingerprint recomputation, execution env backfill, idempotent rerun, and rollback of a failed batch.

```go
func TestBackfillAgentProfileEnvsConvertsV2AndIsIdempotent(t *testing.T) {
	db := openBackfillFixture(t)
	seedV2TaskAndExecution(t, db, "cost_effective")
	options := AgentProfileEnvsBackfillOptions{BatchSize: 1, Profiles: validV3Profiles()}
	if err := BackfillAgentProfileEnvs(context.Background(), db, options); err != nil { t.Fatal(err) }
	if err := BackfillAgentProfileEnvs(context.Background(), db, options); err != nil { t.Fatal(err) }
	task := loadBackfilledTask(t, db)
	if task.ExecutionProfile != "effective" || task.AgentProfileSnapshot.SchemaVersion != 3 {
		t.Fatalf("backfilled task = %#v", task)
	}
	if _, ok := task.AgentProfileSnapshot.Envs[model.ClaudeEnvAuthToken]; ok {
		t.Fatal("backfilled snapshot contains token")
	}
}
```

- [ ] **Step 3: Run migration tests and verify RED**

Run: `GOCACHE=/tmp/anban-profile-envs-go-cache go test ./server/migrations -run 'TestAgentProfileEnvs' -count=1`

Expected: FAIL because the migration files and backfill do not exist.

- [ ] **Step 4: Implement Expand SQL**

The file must contain only additive DDL:

```sql
ALTER TABLE `task_executions`
  ADD COLUMN `profile_envs` json NULL;
```

- [ ] **Step 5: Implement the resumable Go backfill**

Expose:

```go
type AgentProfileEnvsBackfillOptions struct {
	BatchSize int
	Profiles map[string]config.ClaudeExecutionProfileConfig
}

func BackfillAgentProfileEnvs(ctx context.Context, db *gorm.DB, options AgentProfileEnvsBackfillOptions) error
```

Convert operational IDs with `map[string]string{"cost_effective":"effective", "balanced":"balanced", "maximum_quality":"quality"}`. For each v2 snapshot, use its frozen models/controls. Take `ANTHROPIC_BASE_URL` from the corresponding new Profile only when its Provider matches the frozen Provider; otherwise require an explicit repeatable `--legacy-provider-base-url provider=https://endpoint` migration argument. Validate the redacted v3 snapshot, compute the Fingerprint with `model.AgentProfileFingerprint`, and update the task and its executions in one short transaction.

- [ ] **Step 6: Implement the command**

Support exact flags:

```text
-config <server config path>
-batch-size <positive integer, default 500>
-dry-run
-verify-only
```

The command loads `config.NewConfig`, opens MySQL with the configured DSN, prints only counts and IDs, never env values, and exits non-zero on unknown IDs, invalid snapshots, incomplete rows, or a verify-only Fingerprint mismatch.

- [ ] **Step 7: Implement Contract SQL**

Begin with stored-procedure assertions that signal on old operational IDs, v2 task snapshots, null `profile_envs`, or Token presence. Run the command's `-verify-only` mode immediately before this SQL to prove every Fingerprint against the application algorithm. Then run:

```sql
ALTER TABLE `task_executions`
  MODIFY COLUMN `profile_envs` json NOT NULL,
  DROP COLUMN `model_matrix`,
  DROP COLUMN `claude_controls`;
```

Do not alter immutable billing tables. Execute `20260730_agent_profile_envs_expire_quotes.sql` after verify-only and before Contract SQL.

- [ ] **Step 8: Run migration tests and verify GREEN**

Run: `GOCACHE=/tmp/anban-profile-envs-go-cache go test ./server/migrations ./server/cmd/agent-profile-envs-backfill -count=1`

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add server/migrations/20260730_agent_profile_envs_expand.sql server/migrations/20260730_agent_profile_envs_expire_quotes.sql server/migrations/20260730_agent_profile_envs_contract.sql server/migrations/agent_profile_envs_backfill.go server/migrations/agent_profile_envs_backfill_test.go server/migrations/migrations_test.go server/cmd/agent-profile-envs-backfill/main.go
git commit -m "feat(db): migrate agent profiles to env snapshots"
```

### Task 6: Immutable Billing Catalog v7

**Files:**
- Modify: `server/billing/products.yaml`
- Modify: `server/billing/catalog_contract_test.go`
- Modify: `server/billing/profile_selector_test.go`
- Modify: `server/service/billing_catalog_test.go`
- Modify: `server/service/task_fixed_billing_test.go`

- [ ] **Step 1: Write failing catalog tests**

Require `catalog_id: retail-2026-07-30-v7`, exactly `effective/balanced/quality` for every profiled task operation, unchanged tier prices, no old IDs, and no Agent Profile on Designer SKUs.

```go
func TestProfileCatalogV7UsesOnlyNewIDs(t *testing.T) {
	bundle := loadTestBundle(t)
	if bundle.Products.CatalogID != "retail-2026-07-30-v7" { t.Fatalf("catalog = %s", bundle.Products.CatalogID) }
	allowed := map[string]bool{"effective": true, "balanced": true, "quality": true}
	for _, sku := range bundle.Products.SKUs {
		if strings.HasPrefix(sku.Operation, "task.") && sku.ExecutionProfile != "" && !allowed[sku.ExecutionProfile] {
			t.Fatalf("SKU %s uses profile %q", sku.ID, sku.ExecutionProfile)
		}
		if strings.HasPrefix(sku.Operation, "designer.") && sku.ExecutionProfile != "" {
			t.Fatalf("Designer SKU %s has Agent Profile", sku.ID)
		}
	}
}
```

- [ ] **Step 2: Run billing tests and verify RED**

Run: `GOCACHE=/tmp/anban-profile-envs-go-cache go test ./server/billing ./server/service -run 'Test.*Catalog|TestFindSKUByExecutionProfile|Test.*FixedBilling' -count=1`

Expected: FAIL because v6 uses old IDs.

- [ ] **Step 3: Publish v7 in the repository catalog**

Change only `catalog_id` and each Agent SKU's `execution_profile`. Preserve SKU IDs, operations, price credits, tier prices, policies, routes, and delivery fields so this change does not silently reprice tasks.

- [ ] **Step 4: Run billing tests and verify GREEN**

Run: `GOCACHE=/tmp/anban-profile-envs-go-cache go test ./server/billing ./server/service -run 'Test.*Catalog|TestFindSKUByExecutionProfile|Test.*FixedBilling' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/billing/products.yaml server/billing/catalog_contract_test.go server/billing/profile_selector_test.go server/service/billing_catalog_test.go server/service/task_fixed_billing_test.go
git commit -m "feat(billing): publish profile env catalog v7"
```

### Task 7: Bootstrap Contract and Go Agent Consumer

**Files:**
- Modify: `server/service/agent_bootstrap.go`
- Modify: `server/service/agent_bootstrap_profile_test.go`
- Modify: `server/service/agent_bootstrap_test.go`
- Modify: `server/agent/claude_runtime_env.go`
- Modify: `server/agent/claude_runtime_env_test.go`
- Modify: `agent/bootstrap.go`
- Modify: `agent/bootstrap_test.go`
- Modify: `agent/job.go`
- Modify: `agent/job_test.go`
- Modify: `agent/runner_contract_test.go`

- [ ] **Step 1: Write failing Bootstrap tests**

Require this profile payload shape and reject `models`, `claude`, old IDs, unknown envs, missing Token, and Token in stored snapshots:

```json
{
  "profile_id": "quality",
  "provider": "moonshot",
  "protocol": "anthropic",
  "display_name": "极致效果",
  "profile_fingerprint": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "envs": {
    "ANTHROPIC_BASE_URL": "https://api.moonshot.cn/anthropic",
    "ANTHROPIC_AUTH_TOKEN": "runtime-secret",
    "ANTHROPIC_MODEL": "kimi-k3[1m]",
    "ANTHROPIC_DEFAULT_OPUS_MODEL": "kimi-k3[1m]",
    "ANTHROPIC_DEFAULT_FABLE_MODEL": "kimi-k3[1m]",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "kimi-k3[1m]",
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": "kimi-k3[1m]"
  },
  "model_usage_aliases": {
    "kimi-k3[1m]": {"provider":"moonshot","model":"kimi-k3"}
  }
}
```

- [ ] **Step 2: Run Go Bootstrap tests and verify RED**

Run: `GOCACHE=/tmp/anban-profile-envs-go-cache go test ./server/service ./server/agent ./agent -run 'Test.*Bootstrap|TestClaudeRuntimeEnv|TestJob.*Profile' -count=1`

Expected: FAIL on the old schema.

- [ ] **Step 3: Implement the Server Bootstrap payload**

Replace `models`, `claude`, and `runtime_env` with one `envs` field. Build it from `ResolveRuntime`, validate it with `model.ValidateClaudeProfileEnvs(envs, true)`, and pass a defensive copy to the response. Keep aliases structured as `{provider, model}`.

- [ ] **Step 4: Simplify Go Agent validation**

Validate `profile.Envs` against the same Go model contract, require the three new IDs, and use `profile.Envs[model.ClaudeEnvModel]` for the job model. Delete matrix/control cross-check helpers.

- [ ] **Step 5: Run Go Bootstrap tests and verify GREEN**

Run: `GOCACHE=/tmp/anban-profile-envs-go-cache go test ./server/service ./server/agent ./agent -run 'Test.*Bootstrap|TestClaudeRuntimeEnv|TestJob.*Profile' -count=1`

Expected: PASS or only the documented sandbox `httptest` listener denial when running listener-based tests; rerun pure contract tests separately to prove assertions pass.

- [ ] **Step 6: Commit**

```bash
git add server/service/agent_bootstrap.go server/service/agent_bootstrap_profile_test.go server/service/agent_bootstrap_test.go server/agent/claude_runtime_env.go server/agent/claude_runtime_env_test.go agent/bootstrap.go agent/bootstrap_test.go agent/job.go agent/job_test.go agent/runner_contract_test.go
git commit -m "feat(agent): bootstrap unified profile envs"
```

### Task 8: TypeScript Agent Consumer

**Files:**
- Modify: `agent-ts/src/bootstrap.ts`
- Modify: `agent-ts/src/runner.ts`
- Modify: `agent-ts/test/bootstrap.test.ts`
- Modify: `agent-ts/test/runner.test.ts`

- [ ] **Step 1: Write failing schema-v3 tests**

Create fixtures with `profile_id: "effective"` and `envs`. Assert all 19 allowed values survive exactly, unknown values fail, old IDs/fields fail, and malformed usage still produces `unreconciled`.

```ts
test("rejects legacy profile IDs and unknown envs", () => {
  const legacy = bootstrapFixture({ profile_id: "cost_effective" });
  expect(() => validateBootstrapResponse(EXECUTION_ID, legacy)).toThrow("identity is invalid");
  const unknown = bootstrapFixture({ profile_id: "effective", envs: { ...validProfileEnvs(), PATH: "/tmp" } });
  expect(() => validateBootstrapResponse(EXECUTION_ID, unknown)).toThrow("environment is invalid");
});
```

- [ ] **Step 2: Run Agent TS tests and verify RED**

Run: `cd agent-ts && bun run test`

Expected: FAIL because the consumer expects `models`, `claude`, and `runtime_env`.

- [ ] **Step 3: Replace the TypeScript profile type and validator**

Use:

```ts
export interface ExecutionProfile {
  profile_id: 'effective' | 'balanced' | 'quality';
  provider: string;
  protocol: 'anthropic';
  display_name: string;
  profile_fingerprint: string;
  envs: Record<string, string>;
  model_usage_aliases: Record<string, { provider: string; model: string }>;
}
```

Keep one `CLAUDE_PROFILE_ENV_KEYS` set matching Go exactly. Require the seven mandatory variables and validate optional values. `buildExecutionEnvironment` must overlay frozen Profile envs after process and task/Montage envs so they cannot override Claude configuration.

- [ ] **Step 4: Run tests, typecheck, and build**

Run:

```bash
cd agent-ts
bun run test
bun run typecheck
bun run build
```

Expected: 45 or more tests pass; typecheck and build exit 0.

- [ ] **Step 5: Commit**

```bash
git add agent-ts/src/bootstrap.ts agent-ts/src/runner.ts agent-ts/test/bootstrap.test.ts agent-ts/test/runner.test.ts
git commit -m "feat(agent-ts): consume profile env bootstrap"
```

### Task 9: Server Call Sites and Profile ID Cutover

**Files:**
- Modify: `server/handler/task.go`
- Modify: `server/handler/plan.go`
- Modify: `server/handler/ai_entry.go`
- Modify: `server/service/task.go`
- Modify: `server/service/task_agent.go`
- Modify: `server/service/task_resume.go`
- Modify: `server/service/ai_entry.go`
- Modify: `server/scheduler/plan_checker_test.go`
- Modify: `server/handler/agent_profile_errors_test.go`
- Modify: `server/handler/ai_entry_test.go`
- Modify: `server/handler/plan_test.go`
- Modify: `server/handler/task_test.go`
- Modify: `server/handler/task_bulk_test.go`
- Modify: `server/service/ai_entry_test.go`
- Modify: `server/service/plan_test.go`
- Modify: `server/service/task_test.go`
- Modify: `server/service/task_capability_test.go`
- Modify: `server/service/task_artifact_stream_test.go`
- Modify: `server/service/task_execution_complete_test.go`

- [ ] **Step 1: Add failing API and workflow tests**

Assert create, clone, bulk clone, AI entry, plan execution, and retry accept the three new IDs; `cost_effective` and `maximum_quality` return `invalid_agent_execution_profile`; retry keeps schema-v3 envs and Fingerprint.

```go
func TestTaskCreateRejectsLegacyAgentProfileIDs(t *testing.T) {
	for _, profileID := range []string{"cost_effective", "maximum_quality"} {
		t.Run(profileID, func(t *testing.T) {
			response := createTaskRequest(t, map[string]any{"execution_profile": profileID})
			if response.Code != "invalid_agent_execution_profile" {
				t.Fatalf("code = %q", response.Code)
			}
		})
	}
}
```

Use the existing handler test request/response helpers when implementing this assertion; do not add a second HTTP harness.

- [ ] **Step 2: Run the workflow tests and verify RED**

Run: `GOCACHE=/tmp/anban-profile-envs-go-cache go test ./server/handler ./server/service ./server/scheduler -run 'Test.*Profile|Test.*Clone|Test.*Plan|Test.*Retry|Test.*AIEntry' -count=1`

Expected: FAIL where old IDs and snapshot fields remain.

- [ ] **Step 3: Update all Server workflow consumers**

Use `rg -n 'cost_effective|maximum_quality|\.Models|\.Claude|ModelMatrix|ClaudeControls' server` to enumerate every remaining runtime reference. Replace runtime defaults with `effective`, maximum tier with `quality`, and snapshot model reads with `snapshot.Envs[model.ClaudeEnvModel]`. Do not replace historical strings inside the 20260728 migration.

- [ ] **Step 4: Run workflow tests and verify GREEN**

Run: `GOCACHE=/tmp/anban-profile-envs-go-cache go test ./server/handler ./server/service ./server/scheduler -run 'Test.*Profile|Test.*Clone|Test.*Plan|Test.*Retry|Test.*AIEntry' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/handler server/service server/scheduler
git commit -m "refactor(tasks): cut over to new profile ids"
```

### Task 10: Studio Contract and Task Details

**Files:**
- Modify: `studio/src/types/agent-profile.ts`
- Modify: `studio/src/types/task.ts`
- Modify: `studio/src/lib/schemas.ts`
- Modify: `studio/src/lib/api/agent-profiles.ts`
- Modify: `studio/src/components/tasks/ExecutionProfileSelector.tsx`
- Modify: `studio/src/components/tasks/TaskConfigurationDetails.tsx`
- Modify: `studio/src/components/tasks/ExecutionProfileSelector.test.tsx`
- Modify: `studio/src/components/tasks/TaskDetailsSheet.test.tsx`
- Modify: `studio/src/components/tasks/TaskFormDialog.test.tsx`
- Modify: `studio/src/lib/api/agent-profiles.test.ts`
- Modify: `studio/src/lib/pricing.test.ts`
- Modify: `studio/src/lib/schemas.test.ts`
- Modify: `studio/src/lib/task-form.test.ts`
- Modify: `studio/src/lib/command-center.test.ts`
- Modify: `studio/src/lib/studio-ux.test.ts`
- Modify: `studio/src/pages/DashboardPage.ai-entry.test.tsx`
- Modify: `studio/src/pages/PlansPage.test.tsx`
- Modify: `studio/src/pages/TaskDetailPage.test.tsx`
- Modify: `studio/src/pages/TasksPage.test.tsx`
- Modify: `studio/src/test/mocks/handlers.ts`

- [ ] **Step 1: Write failing Studio contract tests**

Assert schemas accept only `effective/balanced/quality`, capability payload uses `model_name`, snapshots use `schema_version: 3` plus non-secret `envs`, and Task details never render `ANTHROPIC_AUTH_TOKEN`.

```ts
it('accepts only the new execution profile IDs', () => {
  const base = { type: 'article' as const, prompt: 'write' }
  expect(rawCreateTaskSchema.safeParse({ ...base, execution_profile: 'effective' }).success).toBe(true)
  expect(rawCreateTaskSchema.safeParse({ ...base, execution_profile: 'quality' }).success).toBe(true)
  expect(rawCreateTaskSchema.safeParse({ ...base, execution_profile: 'cost_effective' }).success).toBe(false)
  expect(rawCreateTaskSchema.safeParse({ ...base, execution_profile: 'maximum_quality' }).success).toBe(false)
})
```

- [ ] **Step 2: Run focused Studio tests and verify RED**

Run:

```bash
cd studio
bun run test -- src/lib/schemas.test.ts src/lib/api/agent-profiles.test.ts src/components/tasks/ExecutionProfileSelector.test.tsx src/components/tasks/TaskDetailsSheet.test.tsx
```

Expected: FAIL on old unions and fields.

- [ ] **Step 3: Implement the new client contract**

Use:

```ts
export type AgentExecutionProfileID = 'effective' | 'balanced' | 'quality'

export interface AgentExecutionProfileCapability {
  id: AgentExecutionProfileID
  display_name: string
  description: string
  provider: string
  model_name: string
  min_tier: 'free' | 'pro' | 'enterprise'
  available: boolean
  unavailable_reason?: string
}

export interface AgentProfileSnapshot {
  schema_version: 3
  profile_id: AgentExecutionProfileID
  display_name: string
  provider: string
  protocol: 'anthropic'
  envs: Record<string, string>
  model_usage_aliases: Record<string, string>
}
```

Selector copy remains 性价比/平衡型/极致效果. Task details derive model, effort, context, and thinking rows from known non-secret env keys and never iterate arbitrary keys into the UI.

- [ ] **Step 4: Update fixtures and run all Studio checks**

Use `rg -l 'cost_effective|maximum_quality|models:|claude:' studio/src` to enumerate fixtures. Change IDs and capability/snapshot shapes, then run:

```bash
cd studio
bun run test
bun run build
```

Expected: all tests and production build pass.

- [ ] **Step 5: Commit**

```bash
git add studio/src
git commit -m "feat(studio): adopt unified profile env contract"
```

### Task 11: Miniapp Contract and Parity

**Files:**
- Modify: `miniapp/src/types/agent-profile.ts`
- Modify: `miniapp/src/types/task.ts`
- Modify: `miniapp/src/components/business/ExecutionProfileSelector.vue`
- Modify: `miniapp/src/pages/tasks/detail.vue`
- Modify: `miniapp/scripts/parity-test.mjs`

- [ ] **Step 1: Make parity tests expect the new contract**

Require the new ID union, `model_name`, schema v3 `envs`, unchanged tier behavior, and absence of Agent Profiles in Designer paths.

```js
assert.match(agentProfileTypes, /'effective' \| 'balanced' \| 'quality'/)
assert.doesNotMatch(agentProfileTypes, /cost_effective|maximum_quality/)
assert.match(agentProfileTypes, /schema_version: 3/)
assert.match(agentProfileTypes, /envs: Record<string, string>/)
```

- [ ] **Step 2: Run parity tests and verify RED**

Run: `cd miniapp && npm run test`

Expected: FAIL because Miniapp types still expose old IDs and fields.

- [ ] **Step 3: Implement Miniapp parity**

Mirror the Studio interfaces exactly. Keep the selector driven by the capability API; display `model_name` and disable unavailable tiers. Task details render only approved summary keys and never a Token.

- [ ] **Step 4: Run Miniapp checks and verify GREEN**

Run:

```bash
cd miniapp
npm run test
npm run type-check
```

Expected: parity contracts pass and typecheck exits 0.

- [ ] **Step 5: Commit**

```bash
git add miniapp/src miniapp/scripts/parity-test.mjs
git commit -m "feat(miniapp): adopt unified profile env contract"
```

### Task 12: Deployment Contracts, Documentation, and Final Verification

**Files:**
- Modify: `.env.example`
- Modify: `README.md`
- Modify: `docker-compose.yml`
- Modify: `server/Deployment.yaml`
- Modify: `deploy/docker/runtime-smoke.sh`
- Modify: `deploy/docker/runtime-smoke_test.go`
- Modify: `server/agent/docker_runtime_contract_test.go`
- Modify: `server/agent/typescript_runtime_docker_contract_test.go`
- Modify: `server/k8s_agent_runtime_test.go`

- [ ] **Step 1: Write failing deployment contract tests**

Require generated smoke config to contain only new IDs and `envs`, verify Server owns credential expansion, and verify runtime images receive only Bootstrap-provided Claude envs rather than static provider secrets.

```go
func TestRuntimeSmokeGeneratedServerConfigUsesProfileEnvs(t *testing.T) {
	raw := readRuntimeSmokeScript(t)
	for _, required := range []string{"effective:", "balanced:", "quality:", "envs:", "ANTHROPIC_AUTH_TOKEN:"} {
		if !strings.Contains(raw, required) { t.Fatalf("missing %q", required) }
	}
	for _, obsolete := range []string{"cost_effective:", "maximum_quality:", "providers:", "models:", "claude:"} {
		if strings.Contains(raw, obsolete) { t.Fatalf("obsolete config %q remains", obsolete) }
	}
}
```

- [ ] **Step 2: Run deployment tests and verify RED**

Run: `GOCACHE=/tmp/anban-profile-envs-go-cache go test ./deploy/docker ./server/agent ./server -run 'Test.*Runtime|Test.*Docker|Test.*Kubernetes' -count=1`

Expected: FAIL on old generated configuration and profile IDs.

- [ ] **Step 3: Update deployment examples and runbook**

Document these production inputs without literal secrets:

```text
ANBAN_DEEPSEEK_ANTHROPIC_BASE_URL
ANBAN_DEEPSEEK_API_KEY
ANBAN_ZHIPU_ANTHROPIC_BASE_URL
ANBAN_ZHIPU_API_KEY
ANBAN_MOONSHOT_ANTHROPIC_BASE_URL
ANBAN_MOONSHOT_API_KEY
```

Explain that deployments may choose other models by changing Profile env values and cost aliases, while the application only recognizes the three product IDs.

- [ ] **Step 4: Run deployment tests and verify GREEN**

Run: `GOCACHE=/tmp/anban-profile-envs-go-cache go test ./deploy/docker ./server/agent ./server -run 'Test.*Runtime|Test.*Docker|Test.*Kubernetes' -count=1`

Expected: PASS, except Docker execution itself may be skipped only when no Docker-compatible CLI exists.

- [ ] **Step 5: Run stale-contract scans**

Run:

```bash
rg -n 'cost_effective|maximum_quality' server agent agent-ts studio miniapp deploy docker-compose.yml README.md .env.example \
  --glob '!server/migrations/20260728_agent_execution_profiles.sql' \
  --glob '!server/migrations/agent_profile_envs_backfill.go' \
  --glob '!server/migrations/agent_profile_envs_backfill_test.go'
rg -n 'ClaudeProviderConfig|ClaudeModelMatrixConfig|ClaudeControlsConfig|\.Models|\.Claude|model_matrix|claude_controls' server agent agent-ts studio miniapp \
  --glob '!server/migrations/20260728_agent_execution_profiles.sql' \
  --glob '!server/migrations/20260730_agent_profile_envs_contract.sql'
```

Expected: no runtime compatibility references; only intentional v2 backfill parsing and historical migration text remain.

- [ ] **Step 6: Run full verification**

```bash
GOCACHE=/tmp/anban-profile-envs-go-cache go test ./...
GOCACHE=/tmp/anban-profile-envs-go-cache go build -o /tmp/anban-creator-server ./server
GOCACHE=/tmp/anban-profile-envs-go-cache go build -o /tmp/anban ./agent
cd agent-ts && bun run test && bun run typecheck && bun run build
cd studio && bun run test && bun run build
cd miniapp && npm run test && npm run type-check
git diff --check
```

Expected: all commands exit 0. If sandbox policy blocks local listeners, isolate and rerun non-listener tests before recording that environment limitation.

- [ ] **Step 7: Build affected images when a container runtime is available**

```bash
make docker-server-image
make docker-agent-image
make docker-seednote-agent-image
make docker-montage-agent-image
```

Expected: all four images build successfully and `make runtime-smoke` passes.

- [ ] **Step 8: Commit deployment updates**

```bash
git add .env.example README.md docker-compose.yml server/Deployment.yaml deploy/docker server/agent/docker_runtime_contract_test.go server/agent/typescript_runtime_docker_contract_test.go server/k8s_agent_runtime_test.go
git commit -m "docs(deploy): document unified profile env rollout"
```

- [ ] **Step 9: Request final review and merge only after clean proof**

Run `git status --short --branch`, inspect `git diff origin/main...HEAD`, verify submodule states, rerun `git diff --check`, and use `superpowers:requesting-code-review`. Resolve every P0-P3 finding, rerun affected tests, then merge and push without force. Prove publication with:

```bash
git rev-parse HEAD
git rev-parse origin/main
git ls-remote origin refs/heads/main
```

Expected: all three SHAs match after publication.
