# Fixed SKU Billing Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build strict billing catalogs and an append-only wallet that supports fixed task admission, accepted-task operation debt, debt-first top-up, exact reversals, and referral promotions.

**Architecture:** A projected wallet account contains non-negative paid, promotional, and debt buckets backed by immutable lots, charges, allocations, and entries. Quotes resolve immutable SKU versions; task charges require full spendable balance, while accepted-task operation charges may create debt. No hold, reserve, capture, release, or dynamic-cost types exist.

**Tech Stack:** Go 1.25, Fiber v3, GORM, MySQL, YAML v3, integer credits, integer micro-CNY.

---

## File Map

- Create `server/billing/money.go`: exact decimal-to-micro-CNY parsing and arithmetic.
- Create `server/billing/config.go`: strict billing-file schemas, loader, and cross-file validation.
- Create `server/billing/testdata/valid/`: approved policy, products, costs, and promotions fixtures.
- Modify `server/config/config.go`: mandatory `billing_runtime` and typed Claude configuration.
- Create `server/model/billing_wallet.go`: account, lot, entry, charge, allocation, settlement outbox, quote, catalog, and referral models.
- Create `server/repository/billing.go`: transactional billing repository.
- Modify `server/repository/repository.go`: expose billing repository in root and transaction repositories.
- Create `server/service/billing_catalog.go`: catalog publication, SKU resolution, and quotes.
- Create `server/service/billing_wallet.go`: top-up, task charge, operation charge, reversal, outbox processing, expiry, and projection rebuild.
- Create `server/service/billing_referral.go`: first-paid-top-up referral issuance.
- Create `server/handler/billing.go`: wallet, transaction, quote, referral, and admin top-up APIs.
- Modify `server/router/router.go`, `server/main.go`, and matching tests: wire billing services and routes.

### Task 1: Add Exact Types And Strict Configuration

**Files:**
- Create: `server/billing/money.go`
- Create: `server/billing/money_test.go`
- Create: `server/billing/config.go`
- Create: `server/billing/config_test.go`
- Modify: `server/config/config.go`

- [ ] **Step 1: Write failing exact-money and strict-loader tests**

```go
func TestParseMicroCNYIsExact(t *testing.T) {
	got, err := billing.ParseMicroCNY("0.30")
	if err != nil || got != 300_000 { t.Fatalf("got=%d err=%v", got, err) }
}

func TestLoadBundleRejectsUnknownField(t *testing.T) {
	_, err := billing.LoadBundle(fixtureWith("policy.yaml", "unknown: true\n"))
	if !errors.Is(err, billing.ErrInvalidConfig) { t.Fatalf("err=%v", err) }
}
```

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./server/billing ./server/config -run 'TestParseMicroCNY|TestLoadBundle' -count=1`

Expected: FAIL because the package and strict loader do not exist.

- [ ] **Step 3: Implement exact parsing and mandatory root configuration**

```go
type RuntimeConfig struct {
	ConfigDir   string `yaml:"config_dir"`
	AdminAPIKey string `yaml:"admin_api_key"`
}

type Bundle struct {
	Policy     PolicyCatalog
	Products   ProductCatalog
	Costs      CostCatalog
	Promotions PromotionCatalog
}
```

Use `yaml.Decoder.KnownFields(true)`, reject trailing documents, parse decimals without `float64`, resolve `config_dir` relative to the root configuration file, and fail startup when any required file is absent.

- [ ] **Step 4: Run focused tests**

Run: `go test ./server/billing ./server/config -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/billing server/config/config.go
git commit -m "feat(billing): add strict catalog configuration"
```

### Task 2: Publish The Approved Billing Files

**Files:**
- Create: `server/billing/policy.yaml`
- Create: `server/billing/products.yaml`
- Create: `server/billing/costs.yaml`
- Create: `server/billing/promotions.yaml`
- Create: `server/billing/catalog_contract_test.go`
- Modify: `server/config.yaml`
- Modify: `server/config.example.yaml`

- [ ] **Step 1: Write failing contract tests**

```go
func TestProductionBillingBundleMatchesPolicy(t *testing.T) {
	b := loadProductionBundle(t)
	if !b.Policy.AcceptedTask.ContinueWhenBalanceNegative { t.Fatal("negative continuation disabled") }
	if b.Policy.TopUp.RepayDebtFirst != true { t.Fatal("debt-first top-up disabled") }
	assertNoLegacyBillingKeys(t, b)
}
```

Add cases proving every workflow/options tuple resolves one task SKU, every premium route resolves one operation SKU, every provider model resolves one cost profile, promotions cannot repay debt, and no product contains provider-cost budgets.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./server/billing -run TestProductionBillingBundleMatchesPolicy -count=1`

Expected: FAIL because production catalogs do not exist.

- [ ] **Step 3: Add the four strict catalogs and root pointer**

Copy the approved examples from the design, expand all production task/image/video variants, add evidence/effective timestamps to cost rows, and configure:

```yaml
billing_runtime:
  config_dir: "./billing"
  admin_api_key: "${ANBAN_BILLING_ADMIN_API_KEY}"
```

Do not add sign-in, registration gifts, recharge bonuses, multipliers, holds, runtime charges, or compatibility aliases.

- [ ] **Step 4: Run catalog tests**

Run: `go test ./server/billing -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/billing server/config.yaml server/config.example.yaml
git commit -m "feat(billing): publish fixed retail catalogs"
```

### Task 3: Add Wallet, Charge, Quote, And Referral Models

**Files:**
- Create: `server/model/billing_wallet.go`
- Create: `server/model/billing_wallet_test.go`
- Modify: `server/database/database.go`

- [ ] **Step 1: Write failing invariant and migration tests**

```go
func TestBillingWalletAccountInvariant(t *testing.T) {
	a := BillingWalletAccount{PaidCredits: 800, PromotionalCredits: 200, DebtCredits: 100}
	if got := a.DisplayBalance(); got != 900 { t.Fatalf("balance=%d", got) }
	if err := a.Validate(); err != nil { t.Fatal(err) }
}

func TestBillingModelsContainNoHoldTypes(t *testing.T) {
	assertMigratedTables(t, "billing_wallet_accounts", "billing_credit_lots", "billing_wallet_entries", "billing_charges", "billing_charge_allocations", "billing_settlement_outbox", "billing_quotes", "billing_catalog_versions", "billing_skus", "billing_referral_issues")
}
```

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./server/model ./server/database -run 'TestBilling' -count=1`

Expected: FAIL because the models are absent.

- [ ] **Step 3: Implement focused models and constraints**

```go
type BillingWalletAccount struct {
	UserID                                  string `gorm:"primaryKey"`
	PaidCredits, PromotionalCredits         int64  `gorm:"not null;default:0"`
	DebtCredits, Version                    int64  `gorm:"not null;default:0"`
}

type BillingCharge struct {
	ID, UserID, SKUID, CatalogID, ResourceType, ResourceID string
	Kind, Policy, Status, IdempotencyScope, IdempotencyKey string
	PriceCredits, PaidCredits, PromotionalCredits, DebtCredits int64
	ReversalOfID *string
}

type BillingSettlementOutbox struct {
	ID, Action, ResourceType, ResourceID, IdempotencyKey, Status string
	Attempts int
	NextAttemptAt time.Time
}
```

Define exact indexes for external top-up identity, charge scope/key, task charge uniqueness, tool-call charge uniqueness, reversal uniqueness, and referral invitee/program uniqueness. All bucket fields remain non-negative.

- [ ] **Step 4: Run model tests**

Run: `go test ./server/model ./server/database -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/model/billing_wallet.go server/model/billing_wallet_test.go server/database/database.go
git commit -m "feat(billing): add debt-aware wallet models"
```

### Task 4: Add The Transactional Billing Repository

**Files:**
- Create: `server/repository/billing.go`
- Create: `server/repository/billing_test.go`
- Modify: `server/repository/repository.go`

- [ ] **Step 1: Write failing locking and idempotency tests**

Test account row locks, ordered eligible-lot selection, immutable entry append, quote locking, external top-up uniqueness, charge replay, reversal uniqueness, settlement-outbox claim/retry, referral uniqueness, and root transaction repository parity.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./server/repository -run TestBillingRepository -count=1`

Expected: FAIL because `BillingRepository` is undefined.

- [ ] **Step 3: Implement the repository contract**

```go
type BillingRepository interface {
	LockAccount(context.Context, string) (*model.BillingWalletAccount, error)
	ListSpendableLots(context.Context, string, string) ([]model.BillingCreditLot, error)
	AppendEntry(context.Context, *model.BillingWalletEntry) error
	CreateCharge(context.Context, *model.BillingCharge, []model.BillingChargeAllocation) error
	FindChargeByKey(context.Context, string, string) (*model.BillingCharge, error)
	LockQuote(context.Context, string) (*model.BillingQuote, error)
	UpdateAccount(context.Context, *model.BillingWalletAccount) error
}
```

Use `clause.Locking{Strength: "UPDATE"}` and expose the same repository from root and transaction-scoped repositories.

- [ ] **Step 4: Run repository tests**

Run: `go test -race ./server/repository -run TestBillingRepository -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/repository/billing.go server/repository/billing_test.go server/repository/repository.go
git commit -m "feat(billing): add transactional wallet repository"
```

### Task 5: Implement Quotes, Charges, Debt, Top-Up, And Reversal

**Files:**
- Create: `server/service/billing_catalog.go`
- Create: `server/service/billing_catalog_test.go`
- Create: `server/service/billing_wallet.go`
- Create: `server/service/billing_wallet_test.go`

- [ ] **Step 1: Write failing service tests**

```go
func TestAcceptedTaskOperationMayCreateDebt(t *testing.T) {
	s := newWalletFixture(t, WalletState{Paid: 100})
	c, err := s.ChargeAcceptedOperation(ctx, OperationChargeRequest{UserID:"u1", TaskID:"t1", ToolCallID:"call1", Price:500})
	if err != nil || c.DebtCredits != 400 { t.Fatalf("charge=%+v err=%v", c, err) }
	if got := s.Account(t).DebtCredits; got != 400 { t.Fatalf("debt=%d", got) }
}

func TestTopUpRepaysDebtBeforePaidBalance(t *testing.T) {
	s := newWalletFixture(t, WalletState{Debt: 400})
	r, err := s.TopUp(ctx, TopUpRequest{UserID:"u1", Credits:1000, ExternalRef:"api-1"})
	if err != nil || r.DebtRepaid != 400 || r.PaidAdded != 600 { t.Fatalf("result=%+v err=%v", r, err) }
}
```

Add task admission, debt rejection, insufficient balance, promotional eligibility, promotion-not-paying-debt, concurrent charges, exact replay, conflict replay, expiry, reversal, and projection rebuild cases.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./server/service -run 'TestBilling(Catalog|Wallet)|TestAcceptedTaskOperation|TestTopUpRepaysDebt' -count=1`

Expected: FAIL because services do not exist.

- [ ] **Step 3: Implement the wallet state machine**

```go
func (s *BillingWalletService) ChargeTaskAdmission(ctx context.Context, req TaskChargeRequest) (*model.BillingCharge, error)
func (s *BillingWalletService) ChargeAcceptedOperation(ctx context.Context, req OperationChargeRequest) (*model.BillingCharge, error)
func (s *BillingWalletService) ChargeStandaloneOperation(ctx context.Context, req OperationChargeRequest) (*model.BillingCharge, error)
func (s *BillingWalletService) Reverse(ctx context.Context, chargeID, reason, key string) (*model.BillingCharge, error)
func (s *BillingWalletService) TopUp(ctx context.Context, req TopUpRequest) (*TopUpResult, error)
func (s *BillingWalletService) EnqueueSettlementInTx(ctx context.Context, tx repository.Repository, req SettlementIntent) (*model.BillingSettlementOutbox, error)
func (s *BillingWalletService) ProcessSettlementOutbox(ctx context.Context, limit int) (int, error)
```

All wallet methods lock the account, validate idempotency before mutation, append entries and charge/allocation rows in the same transaction, and update the projection last. Accepted operation is the only method allowed to create debt. Outbox processing claims rows with skip-locked semantics, applies the idempotent charge or reversal, and records retry state without rewriting the source output/task event.

- [ ] **Step 4: Run focused and race tests**

Run: `go test -race ./server/service -run 'TestBilling|TestAcceptedTaskOperation|TestTopUpRepaysDebt' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/service/billing_catalog.go server/service/billing_catalog_test.go server/service/billing_wallet.go server/service/billing_wallet_test.go
git commit -m "feat(billing): implement fixed charges and debt repayment"
```

### Task 6: Add Referral And Foundation APIs

**Files:**
- Create: `server/service/billing_referral.go`
- Create: `server/service/billing_referral_test.go`
- Create: `server/handler/billing.go`
- Create: `server/handler/billing_test.go`
- Modify: `server/router/router.go`
- Modify: `server/main.go`

- [ ] **Step 1: Write failing referral and API tests**

Test first qualifying top-up, duplicate top-up, inviter cap, no registration gift, debt user receiving but not spending promotion, wallet response buckets, quote expiry, constant-time admin authentication, top-up idempotency, and absence of provider-cost fields from public DTOs.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./server/service ./server/handler ./server/router -run 'TestBillingReferral|TestBillingHandler|TestBillingRoutes' -count=1`

Expected: FAIL because the services and routes are absent.

- [ ] **Step 3: Implement referral issuance and routes**

```go
type WalletResponse struct {
	Paid          int64 `json:"paid"`
	Promotional   int64 `json:"promotional"`
	Debt          int64 `json:"debt"`
	Balance       int64 `json:"balance"`
}

// Public
GET  /api/billing/wallet
GET  /api/billing/transactions
POST /api/billing/quotes
GET  /api/billing/referral

// Admin
POST /api/admin/billing/topups
```

Use distinct JSON tags for all wallet fields in the real DTO. Issue referral lots in the same transaction as the first qualifying top-up and persist immutable program/catalog IDs.

- [ ] **Step 4: Run package and full Go tests**

Run: `go test ./server/service ./server/handler ./server/router -count=1 && go test ./...`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/service/billing_referral.go server/service/billing_referral_test.go server/handler/billing.go server/handler/billing_test.go server/router/router.go server/main.go
git commit -m "feat(billing): expose wallet topup and referral APIs"
```

## Plan 1 Completion Gate

- Strict production catalogs load at startup.
- Task admission cannot overdraw and rejects any debt account.
- Accepted-task operation charge can create debt without an insufficient-balance error.
- Paid top-up repays debt before paid balance.
- Promotion cannot repay debt.
- Charges, top-ups, reversals, and referrals are idempotent under concurrency.
- Settlement outbox retries transient operation-charge and reversal failures without duplicating wallet entries.
- No hold/reserve/capture/release or legacy dynamic charge type exists in the new foundation.
- `go test ./...` passes.
