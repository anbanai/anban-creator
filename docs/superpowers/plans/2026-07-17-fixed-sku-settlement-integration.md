# Fixed SKU Settlement Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Connect fixed task and premium-operation SKUs to every production execution path while guaranteeing that accepted tasks continue through wallet debt.

**Architecture:** Task creation validates an immutable quote and appends one prepaid task charge atomically. Executors receive no balance or cost budget. Premium MCP tools persist a fixed-charge settlement intent with durable output; an idempotent outbox worker applies the charge and may create debt without changing the successful tool result. Terminal platform failures enqueue an exact reversal with the terminal state.

**Tech Stack:** Go 1.25, Fiber v3, GORM transactions, MCP tools, local/Docker/Kubernetes agent execution.

---

## File Map

- Modify `server/model/task.go`: immutable task SKU/quote/charge identity and terminal billing reason.
- Modify `server/model/image.go` and video generation models: operation SKU/charge identity.
- Modify `server/service/task.go`: atomic quote admission, task insertion, and charge.
- Modify `server/service/plan.go` and scheduler: quote each execution at run admission.
- Modify agent execution services: remove runtime wallet settlement and add terminal task reversal policy.
- Modify `server/mcp/image_tools.go` and `video_tools.go`: successful-output fixed charges with debt allowed.
- Modify Designer and standalone media services: prepaid standalone-operation admission.
- Delete runtime reservation/refund/payment-required paths after all callers move.

### Task 1: Persist Immutable Retail Identity

**Files:**
- Modify: `server/model/task.go`
- Modify: `server/model/image.go`
- Modify: relevant video generation models
- Modify: `server/database/database.go`
- Add: matching model tests

- [ ] **Step 1: Write failing schema tests**

```go
func TestTaskPersistsRetailIdentity(t *testing.T) {
	task := model.Task{BillingQuoteID:"q1", BillingCatalogID:"retail-v1", BillingSKUID:"task.seednote.standard.v1", BillingChargeID:"c1", BillingPriceCredits:5000}
	assertRoundTrip(t, &task)
}
```

Add tests for operation SKU/charge IDs, terminal billing reason codes, uniqueness of task charge identity, and absence of cost USD/payment-required/shortfall fields in the new models.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./server/model ./server/database -run 'TestTaskPersistsRetailIdentity|Test.*OperationBillingIdentity' -count=1`

Expected: FAIL because immutable retail identity is absent.

- [ ] **Step 3: Add fields and indexes**

```go
BillingQuoteID     string `gorm:"type:char(36);not null"`
BillingCatalogID   string `gorm:"type:varchar(96);not null"`
BillingSKUID       string `gorm:"type:varchar(128);not null"`
BillingChargeID    string `gorm:"type:char(36);not null;uniqueIndex"`
BillingPriceCredits int64 `gorm:"not null"`
BillingTerminalReason string `gorm:"type:varchar(48)"`
```

Use nullable fields only where rows can exist before cutover migration. The forward-cutover SQL makes them non-null for every newly created billable resource.

- [ ] **Step 4: Run model tests**

Run: `go test ./server/model ./server/database -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/model server/database/database.go
git commit -m "feat(billing): persist fixed retail identity"
```

### Task 2: Atomically Admit And Charge New Tasks

**Files:**
- Modify: `server/service/task.go`
- Modify: `server/service/task_test.go`
- Modify: `server/handler/task.go`
- Modify: `server/handler/task_test.go`

- [ ] **Step 1: Write failing creation tests**

Test valid quote, expired quote, fingerprint mismatch, existing debt, insufficient promotional eligibility, sufficient mixed balance, concurrent creation, duplicate request replay, database failure rollback, and successful task insertion plus exactly one charge.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./server/service ./server/handler -run 'TestCreateTask.*Billing|TestTaskQuote' -count=1`

Expected: FAIL because task creation does not require a fixed quote or atomic charge.

- [ ] **Step 3: Implement one admission transaction**

```go
func (s *TaskService) Create(ctx context.Context, req CreateTaskRequest) (*model.Task, error) {
	return s.repo.Transaction(ctx, func(tx repository.Repository) (*model.Task, error) {
		quote, err := s.billingCatalog.ValidateQuote(ctx, tx, req.UserID, req.QuoteID, req.BillingFingerprint())
		if err != nil { return nil, err }
		task := buildTask(req, quote)
		if err := tx.Tasks().Create(ctx, task); err != nil { return nil, err }
		charge, err := s.billingWallet.ChargeTaskAdmissionInTx(ctx, tx, task.ID, quote)
		if err != nil { return nil, err }
		task.BillingChargeID = charge.ID
		if err := tx.Tasks().UpdateBillingIdentity(ctx, task); err != nil { return nil, err }
		return task, nil
	})
}
```

Do not pass wallet balance, debt, estimated tokens, or provider-cost budgets into execution bootstrap.

- [ ] **Step 4: Run service and handler tests**

Run: `go test -race ./server/service ./server/handler -run 'TestCreateTask.*Billing|TestTaskQuote' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/service/task.go server/service/task_test.go server/handler/task.go server/handler/task_test.go
git commit -m "feat(billing): charge fixed price at task admission"
```

### Task 3: Quote Scheduled, Resumed, And Retried Work Correctly

**Files:**
- Modify: `server/service/plan.go`
- Modify: `server/service/plan_test.go`
- Modify: `server/scheduler/plan_checker.go`
- Modify: `server/scheduler/plan_checker_test.go`
- Modify: task resume/retry services and tests

- [ ] **Step 1: Write failing lifecycle tests**

Test plan creation without a future price lock, quote resolution at each scheduled run, debt/insufficient balance preventing only that new run, same-task retry with no charge, same-task resume with no charge, explicit duplicate-as-new-task with a new charge, and concurrent scheduler idempotency.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./server/service ./server/scheduler -run 'Test.*(PlanBilling|RetryBilling|ResumeBilling)' -count=1`

Expected: FAIL because legacy deduction paths still own these transitions.

- [ ] **Step 3: Implement lifecycle rules**

Plans store workflow/options, not a permanent retail price. Each scheduled task execution resolves a current quote and uses Task 2 admission. Retry/resume keeps the original task, SKU snapshot, and charge. No retry path calls the wallet.

- [ ] **Step 4: Run lifecycle tests**

Run: `go test -race ./server/service ./server/scheduler -run 'Test.*(PlanBilling|RetryBilling|ResumeBilling)' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/service/plan.go server/service/plan_test.go server/scheduler
git commit -m "feat(billing): align plans retries and resumes"
```

### Task 4: Charge Image MCP Operations After Durable Success

**Files:**
- Modify: `server/mcp/image_tools.go`
- Modify: `server/mcp/image_tools_test.go`
- Modify: image storage/result services and tests

- [ ] **Step 1: Write failing MCP tests**

```go
func TestGenerateImageAcceptedTaskCanCreateDebt(t *testing.T) {
	f := newImageToolFixture(t, walletState{Paid:100})
	r := f.callGenerateImage(t, acceptedTask("t1"), successfulPersistedImage())
	if r.Error != nil { t.Fatal(r.Error) }
	if got := f.wallet(t).DebtCredits; got != 400 { t.Fatalf("debt=%d", got) }
	assertSettlementIntentCount(t, f.db, "t1", 1)
	f.processSettlementOutbox(t)
	assertChargeCount(t, f.db, "t1", 1)
}
```

Add provider failure, persistence failure, duplicate tool call, retry with same call ID, distinct successful calls, route/SKU mismatch, transient wallet failure repaired by outbox, and proof that no pre-dispatch wallet check occurs.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./server/mcp -run 'TestGenerateImage.*Billing|TestGenerateImageAcceptedTaskCanCreateDebt' -count=1`

Expected: FAIL because image tools still use dynamic deductions or balance checks.

- [ ] **Step 3: Resolve fixed SKU before dispatch and charge after persistence**

The tool validates route-to-SKU mapping before the provider call. Persist immutable output evidence and an accepted-operation settlement intent in one database transaction. Attempt immediate outbox processing, but return the durable image even when wallet settlement is temporarily unavailable or the resulting net balance is negative.

- [ ] **Step 4: Run image MCP tests**

Run: `go test -race ./server/mcp -run 'TestGenerateImage.*Billing|TestGenerateImageAcceptedTaskCanCreateDebt' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/mcp/image_tools.go server/mcp/image_tools_test.go
git commit -m "feat(billing): charge fixed image operations"
```

### Task 5: Charge Video And Other Premium MCP Operations

**Files:**
- Modify: `server/mcp/video_tools.go`
- Modify: `server/mcp/video_tools_test.go`
- Modify: other premium MCP tools selected by the production catalog
- Add: matching tests

- [ ] **Step 1: Write failing table-driven tests**

Cover every video resolution/duration/audio SKU, successful debt creation, provider failure, output validation failure, duplicate completion callback, premium understanding operation, and operational-call limit. Assert limits return workflow errors without querying wallet or estimating provider money.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./server/mcp -run 'Test.*Premium.*Billing|TestVideo.*Billing' -count=1`

Expected: FAIL because formulas and legacy deductions remain.

- [ ] **Step 3: Apply the shared successful-output charge boundary**

Every premium tool resolves exactly one SKU from validated options and persists/verifies the deliverable plus settlement intent atomically. The outbox applies one accepted-task operation charge. Delete per-token, per-pixel retail formulas and insufficient-balance branches from accepted-task tools.

- [ ] **Step 4: Run premium tool tests**

Run: `go test -race ./server/mcp -run 'Test.*Premium.*Billing|TestVideo.*Billing' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/mcp
git commit -m "feat(billing): charge fixed premium MCP operations"
```

### Task 6: Keep Standalone Designer And Media Prepaid

**Files:**
- Modify: `server/service/image.go`
- Modify: `server/service/image_test.go`
- Modify: standalone video services and tests
- Modify: related handlers and tests

- [ ] **Step 1: Write failing standalone tests**

Test required quote, debt rejection, insufficient balance, provider-not-called on rejection, atomic prepaid charge, successful durable output, platform failure reversal, duplicate request, and no accepted-task overdraft policy.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./server/service ./server/handler -run 'TestStandalone.*Billing|TestDesigner.*Billing' -count=1`

Expected: FAIL because standalone paths do not use the new charge policy.

- [ ] **Step 3: Implement standalone admission**

Standalone premium work validates quote, requires zero debt and full eligible balance, appends a `standalone_operation` charge, and dispatches only after the transaction commits. Reverse only on configured platform/provider terminal reasons with no durable output.

- [ ] **Step 4: Run standalone tests**

Run: `go test -race ./server/service ./server/handler -run 'TestStandalone.*Billing|TestDesigner.*Billing' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/service server/handler
git commit -m "feat(billing): make standalone media prepaid"
```

### Task 7: Reverse Only Eligible Terminal Task Failures

**Files:**
- Modify: agent execution completion services and tests
- Modify: cloud callback handlers and tests
- Modify: cancellation services and tests

- [ ] **Step 1: Write failing terminal-policy tests**

Table-test success, user cancellation before start, user cancellation after start, platform error, provider error, execution timeout, infrastructure cancellation, invalid user input before admission, durable partial delivery, duplicate callbacks, and local/cloud parity.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./server/service ./server/handler -run 'Test.*TerminalBilling|Test.*TaskReversal' -count=1`

Expected: FAIL because runtime settlement still dynamically charges/refunds or uses text matching.

- [ ] **Step 3: Implement reason-code policy**

```go
func ShouldReverseTaskCharge(reason TerminalReason, durableDelivery bool) bool {
	if durableDelivery { return false }
	switch reason {
	case TerminalPlatformError, TerminalProviderError, TerminalExecutionTimeout, TerminalInfrastructureCancelled:
		return true
	default:
		return false
	}
}
```

Local and cloud completion persist the terminal reason and reversal intent atomically. The outbox calls one idempotent reversal method. Reversal restores original paid/promotional allocations and reduces debt only if the original charge created debt.

- [ ] **Step 4: Run terminal tests**

Run: `go test -race ./server/service ./server/handler -run 'Test.*TerminalBilling|Test.*TaskReversal' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/service server/handler
git commit -m "feat(billing): reverse platform-failed task charges"
```

### Task 8: Delete Legacy Execution Billing

**Files:**
- Modify/delete: `server/service/credit.go` and tests
- Modify/delete: legacy credit repository/model code
- Modify: all callers found by the removal scan

- [ ] **Step 1: Add a failing forbidden-symbol contract test**

Reject production references to `SettleAgentRuntime`, `DeductForOperation`, `ReserveCredits`, `payment_required`, `billing_shortfall`, `total_cost_usd`, runtime credit multipliers, and accepted-task insufficient-balance branches.

- [ ] **Step 2: Run the contract test and verify RED**

Run: `go test ./server/... -run TestNoLegacyExecutionBilling -count=1`

Expected: FAIL with the remaining call sites.

- [ ] **Step 3: Remove legacy code and route every caller to fixed settlement**

Delete dynamic runtime settlement, token-price retail fallback, image/video retail formulas, reservation/refund helpers, payment-required delivery locks, and compatibility DTOs. Preserve unrelated business services in focused files when deleting a large legacy service.

- [ ] **Step 4: Run scans, full tests, and builds**

```bash
rg -n 'SettleAgentRuntime|DeductForOperation|ReserveCredits|payment_required|billing_shortfall|total_cost_usd|tier_multipliers|billing_multiplier' server --glob '*.go' --glob '*.yaml'
go test ./...
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
```

Expected: scan has no production hits; tests and builds pass.

- [ ] **Step 5: Commit**

```bash
git add -A server agent app
git commit -m "refactor(billing): remove legacy execution charging"
```

## Plan 3 Completion Gate

- Task creation atomically admits and charges one fixed task SKU.
- Existing task retry/resume never charges again.
- Accepted tasks contain no balance or provider-cost checks.
- Every successful premium MCP output charges one fixed SKU and may create debt.
- Durable output remains successful when wallet settlement is transiently unavailable; the outbox repairs it.
- Failed premium calls never create retail charges.
- Standalone premium actions remain prepaid and cannot overdraw.
- Eligible platform failures reverse task charges exactly once.
- Legacy dynamic execution billing symbols are absent.
- Full Go tests and server/agent builds pass.
