# Provider Cost And Margin Ledger Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Record exact internal provider cost and contribution margin without using provider cost to charge users or interrupt accepted tasks.

**Architecture:** Claude Code calls Ark directly and reports terminal per-model usage through `result.modelUsage`; image and video providers report typed response/output evidence. An append-only cost ledger calculates integer micro-CNY using immutable price versions. Missing terminal evidence creates an unreconciled admin status and never mutates the wallet.

**Tech Stack:** Go 1.25, Anban-maintained `claude-agent-sdk-go` fork, GORM, MySQL, exact integer accounting.

---

## File Map

- Modify `go.mod` and `go.sum`: select the reviewed SDK fork revision that exposes `ResultMessage.ModelUsage`.
- Modify forked SDK `internal/shared/message.go`: typed model-usage map and JSON coverage; submit the same patch upstream.
- Modify `server/agent/executor.go`: retain terminal per-model usage and discard Claude monetary/context metadata.
- Modify `server/agent/executor_test.go`: parent plus child-model fixtures and missing-result coverage.
- Modify `server/config/config.go`, `server/config.yaml`, and `server/config.example.yaml`: typed direct-Ark Claude contract and role mappings.
- Create `server/model/billing_cost.go`: provider cost, execution reconciliation, and margin facts.
- Create `server/repository/billing_cost.go`: append/query cost and margin events.
- Create `server/service/provider_cost.go`: typed calculators and cost event service.
- Create `server/service/provider_margin.go`: revenue/cost/receivable fact projection.
- Modify image/video providers and MCP tools: preserve safe provider usage and durable-output metadata.
- Create `server/handler/billing_admin.go`: admin cost, margin, and reconciliation reports.

### Task 1: Expose Terminal Model Usage From The Go SDK

**Files:**
- Modify: Anban SDK fork `internal/shared/message.go`
- Modify: Anban SDK fork `internal/shared/message_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Write the failing upstream-compatible SDK test**

```go
func TestResultMessageDecodesModelUsage(t *testing.T) {
	msg := decodeMessage(t, `{"type":"result","subtype":"success","modelUsage":{"doubao-seed-evolving":{"inputTokens":4807,"outputTokens":198,"cacheReadInputTokens":7792,"cacheCreationInputTokens":0,"costUSD":0.032881,"contextWindow":200000}}}`)
	r := msg.(*ResultMessage)
	u := r.ModelUsage["doubao-seed-evolving"]
	if u.InputTokens != 4807 || u.CacheReadInputTokens != 7792 || u.OutputTokens != 198 { t.Fatalf("usage=%+v", u) }
}
```

- [ ] **Step 2: Run the fork test and verify RED**

Run in the fork: `go test ./internal/shared -run TestResultMessageDecodesModelUsage -count=1`

Expected: FAIL because `ModelUsage` is undefined.

- [ ] **Step 3: Add the minimal typed field**

```go
type ModelUsage struct {
	InputTokens              int64   `json:"inputTokens"`
	OutputTokens             int64   `json:"outputTokens"`
	CacheReadInputTokens     int64   `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int64   `json:"cacheCreationInputTokens"`
	CostUSD                  float64 `json:"costUSD"`
	ContextWindow            int64   `json:"contextWindow"`
}

type ResultMessage struct {
	// existing fields
	ModelUsage map[string]ModelUsage `json:"modelUsage,omitempty"`
}
```

The Anban runtime consumes only token categories. Preserve `CostUSD` and `ContextWindow` for protocol parity but mark them non-authoritative in SDK documentation.

- [ ] **Step 4: Run the fork suite and pin the reviewed revision**

Run in the fork: `go test ./...`

Then update `go.mod` to the immutable fork revision and run `go mod tidy`.

Expected: fork tests and `go mod tidy` pass with no unrelated dependency churn.

- [ ] **Step 5: Commit fork and repository dependency updates separately**

```bash
git add go.mod go.sum
git commit -m "build(agent): use sdk model usage support"
```

### Task 2: Parse And Normalize Claude Per-Model Usage

**Files:**
- Modify: `server/agent/executor.go`
- Modify: `server/agent/executor_test.go`
- Modify: `server/model/task.go`

- [ ] **Step 1: Write failing executor tests from the verified fixture**

```go
func TestExecutionResultUsesTerminalPerModelUsage(t *testing.T) {
	r := collectResult(t, fixtureResult(map[string]claudecode.ModelUsage{
		"doubao-seed-evolving": {InputTokens:4807, CacheReadInputTokens:7792, OutputTokens:198},
		"doubao-seed-2-1-turbo-260628": {InputTokens:875, OutputTokens:3},
	}))
	if len(r.ModelUsage) != 2 { t.Fatalf("usage=%+v", r.ModelUsage) }
	if r.TotalCostUSD != nil { t.Fatalf("Claude cost retained: %v", *r.TotalCostUSD) }
}
```

Add cases proving top-level `Usage`, partial events, `task_notification.total_tokens`, `CostUSD`, and `ContextWindow` cannot create model usage; aliases normalize only through configured exact mappings; duplicate model aliases merge token categories once; and a missing terminal result yields `CostStatusUnreconciled`.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./server/agent -run 'TestExecutionResultUsesTerminalPerModelUsage|TestExecutionResultMissingModelUsage' -count=1`

Expected: FAIL because `ExecutionResult` has no model-usage map and still retains `TotalCostUSD`.

- [ ] **Step 3: Implement authoritative terminal collection**

```go
type ModelTokenUsage struct {
	Provider, Model string
	InputTokens, OutputTokens int64
	CacheReadInputTokens, CacheCreationInputTokens int64
}

type ExecutionResult struct {
	// existing execution fields
	ModelUsage []ModelTokenUsage `json:"model_usage,omitempty"`
	CostStatus string            `json:"cost_status,omitempty"`
}
```

Populate this only from terminal `ResultMessage.ModelUsage`. Remove `TotalCostUSD` and aggregate token fields from the new execution contract. Do not enable partial messages solely for accounting.

- [ ] **Step 4: Run executor tests**

Run: `go test ./server/agent -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/agent/executor.go server/agent/executor_test.go server/model/task.go
git commit -m "feat(agent): retain terminal per-model usage"
```

### Task 3: Make Claude Runtime Configuration Typed And Direct

**Files:**
- Modify: `server/config/config.go`
- Modify: `server/config/config_test.go`
- Modify: `server/config.yaml`
- Modify: `server/config.example.yaml`
- Modify: `server/service/agent_bootstrap.go`
- Modify: `server/service/agent_bootstrap_test.go`
- Modify: `server/agent/docker_executor.go`

- [ ] **Step 1: Write failing configuration and bootstrap tests**

Test exact default/opus/fable/sonnet/haiku mappings, direct Ark base URL, redacted auth token, absence of `[1M]`, rejection of duplicate `claude.env` provider keys, and identical local/Docker/Kubernetes environment construction.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./server/config ./server/service ./server/agent -run 'TestClaude|TestAgentBootstrap.*Claude' -count=1`

Expected: FAIL because the runtime still uses an arbitrary environment map.

- [ ] **Step 3: Implement the typed contract**

```go
type ClaudeConfig struct {
	Provider  string             `yaml:"provider"`
	BaseURL   string             `yaml:"base_url"`
	AuthToken string             `yaml:"auth_token"`
	Models    ClaudeModelsConfig `yaml:"models"`
}

type ClaudeModelsConfig struct {
	Default, Opus, Fable, Sonnet, Haiku string
}
```

Build `ANTHROPIC_BASE_URL`, `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_MODEL`, and the four `ANTHROPIC_DEFAULT_*_MODEL` values from this type. Keep Ark direct; do not add a gateway URL, execution token, provider budget, or provider credential logging.

- [ ] **Step 4: Run focused tests**

Run: `go test ./server/config ./server/service ./server/agent -run 'TestClaude|TestAgentBootstrap.*Claude' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/config server/service/agent_bootstrap.go server/service/agent_bootstrap_test.go server/agent/docker_executor.go
git commit -m "refactor(agent): type direct Ark runtime configuration"
```

### Task 4: Add Exact Cost Models, Calculator, And Ledger

**Files:**
- Create: `server/model/billing_cost.go`
- Create: `server/model/billing_cost_test.go`
- Create: `server/repository/billing_cost.go`
- Create: `server/repository/billing_cost_test.go`
- Create: `server/service/provider_cost.go`
- Create: `server/service/provider_cost_test.go`
- Modify: `server/database/database.go`

- [ ] **Step 1: Write failing exact-cost and idempotency tests**

```go
func TestProviderCostCalculatorUsesExactCategories(t *testing.T) {
	u := TokenUsage{Input:4807, CacheRead:7792, Output:198}
	got := calculator(t).TokenCost("volcengine_ark/doubao-seed-evolving", u)
	if got.MicroCNY != 44_133 { t.Fatalf("microCNY=%d", got.MicroCNY) }
}

func TestMissingClaudeResultIsUnreconciledAndDoesNotTouchWallet(t *testing.T) {
	s := newCostFixture(t)
	before := s.walletSnapshot(t, "u1")
	s.MarkExecutionUnreconciled(ctx, "exec-1", "missing_terminal_model_usage")
	if after := s.walletSnapshot(t, "u1"); after != before { t.Fatalf("wallet changed") }
}
```

Use exact rounding rules in the fixture: sum category numerators first, divide by the catalog unit once, and round cost upward to one micro-CNY per provider event.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./server/model ./server/repository ./server/service -run 'TestProviderCost|TestMissingClaudeResult' -count=1`

Expected: FAIL because cost persistence and calculation do not exist.

- [ ] **Step 3: Implement append-only cost recording**

```go
type RecordTokenCostRequest struct {
	ExecutionID, TaskID, Provider, Model, CatalogID, IdempotencyKey string
	Usage TokenUsage
	Source string
}

func (s *ProviderCostService) RecordTokenUsage(context.Context, RecordTokenCostRequest) (*model.BillingProviderCostEvent, error)
func (s *ProviderCostService) MarkExecutionUnreconciled(context.Context, string, string) error
func (s *ProviderCostService) AppendInvoiceAdjustment(context.Context, AdjustmentRequest) (*model.BillingProviderCostEvent, error)
```

Persist sanitized typed evidence and calculation snapshots. Base events are unique by execution/model or provider/request ID. Adjustments append and reference the original; they never rewrite it.

- [ ] **Step 4: Run model, repository, service, and race tests**

Run: `go test -race ./server/model ./server/repository ./server/service -run 'TestProviderCost|TestMissingClaudeResult' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/model/billing_cost.go server/model/billing_cost_test.go server/repository/billing_cost.go server/repository/billing_cost_test.go server/service/provider_cost.go server/service/provider_cost_test.go server/database/database.go
git commit -m "feat(billing): add provider cost ledger"
```

### Task 5: Record Claude, Image, And Video Cost Evidence

**Files:**
- Modify: `server/service/agent_execution.go`
- Modify: `server/service/agent_execution_test.go`
- Modify: `app/image/*provider*.go`
- Modify: `app/image/*provider*_test.go`
- Modify: `server/mcp/image_tools.go`
- Modify: `server/mcp/image_tools_test.go`
- Modify: `server/mcp/video_tools.go`
- Modify: `server/mcp/video_tools_test.go`

- [ ] **Step 1: Write failing integration tests**

Test one cost event for each Evolving/Turbo model entry, duplicate terminal callbacks, no terminal result, provider failure with measured usage, Seedream output pixel tiers, GPT Image token categories, video duration/resolution evidence, missing media evidence, and absolute absence of wallet mutation.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./server/service ./server/mcp ./app/image -run 'Test.*ProviderCost|Test.*UsageEvidence' -count=1`

Expected: FAIL because runtime and provider results are not connected to the cost ledger.

- [ ] **Step 3: Record cost after each authoritative evidence boundary**

Claude terminal handling loops over `ExecutionResult.ModelUsage` and appends one model event. Image/video adapters reduce provider responses to typed usage and persisted output facts before calling the cost service. A missing evidence path records `unreconciled`; it does not estimate a user charge or fail an otherwise durable user output.

- [ ] **Step 4: Run integration and full Go tests**

Run: `go test ./server/service ./server/mcp ./app/image -count=1 && go test ./...`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/service/agent_execution.go server/service/agent_execution_test.go server/mcp app/image
git commit -m "feat(billing): record provider usage evidence"
```

### Task 6: Add Margin Facts And Admin Reports

**Files:**
- Create: `server/service/provider_margin.go`
- Create: `server/service/provider_margin_test.go`
- Create: `server/handler/billing_admin.go`
- Create: `server/handler/billing_admin_test.go`
- Modify: `server/router/router.go`

- [ ] **Step 1: Write failing accounting and API tests**

Test paid-credit revenue, promotional consumption, debt receivable creation, top-up receivable collection, provider cost arriving before/after retail charge, platform-failure cost, reversal, invoice adjustment, report grouping, integer JSON fields, decimal display strings, and constant-time admin authentication.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./server/service ./server/handler ./server/router -run 'TestMargin|TestBillingAdmin' -count=1`

Expected: FAIL because margin facts and reports do not exist.

- [ ] **Step 3: Implement append-only facts and reports**

```go
func (s *MarginService) RecordRetailCharge(context.Context, string) error
func (s *MarginService) RecordRetailReversal(context.Context, string) error
func (s *MarginService) RecordProviderCost(context.Context, string) error
func (s *MarginService) Reconcile(context.Context, time.Time) (*ReconciliationReport, error)
```

Expose authenticated read-only `/api/admin/billing/costs`, `/margins`, and `/reconciliation`. Reports distinguish cash, deferred paid value, recognized revenue, promotion, receivable, collected debt, provider cost, failure cost, and contribution margin.

- [ ] **Step 4: Run report and full tests**

Run: `go test ./server/service ./server/handler ./server/router -run 'TestMargin|TestBillingAdmin' -count=1 && go test ./...`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/service/provider_margin.go server/service/provider_margin_test.go server/handler/billing_admin.go server/handler/billing_admin_test.go server/router/router.go
git commit -m "feat(billing): add provider margin reporting"
```

## Plan 2 Completion Gate

- Raw terminal `modelUsage` records Evolving and Turbo independently.
- Claude `total_cost_usd`, partial events, top-level usage, task totals, and context window are not accounting inputs.
- Current direct Ark execution remains intact; no gateway or provider-cost budget exists.
- Missing terminal usage is visible as unreconciled and never mutates wallet or task success.
- Image/video cost uses authoritative provider or durable-output evidence.
- Provider cost and margin use integer micro-CNY and immutable catalog versions.
- `go test ./...` and both agent/server builds pass.
