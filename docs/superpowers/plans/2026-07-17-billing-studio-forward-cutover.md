# Billing Studio And Forward Cutover Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the fixed-price wallet, debt, quote, and referral experience and delete every legacy billing surface in one forward-only release.

**Architecture:** Studio displays fixed SKU prices and the wallet's paid, promotional, debt, and net buckets. Task and standalone-operation submission obtains a short-lived quote; running tasks never display payment-required states. The backend cutover drops legacy dynamic billing and creates only the new append-only wallet/cost schema.

**Tech Stack:** React 19, TypeScript, Vite 8, TanStack Query, React Router, shadcn/Base UI, Vitest, Go/GORM/MySQL migration SQL.

---

## File Map

- Create `studio/src/lib/api/billing.ts`: wallet, transactions, quotes, referral APIs.
- Modify `studio/src/lib/schemas.ts`: strict fixed-billing DTO schemas.
- Create `studio/src/pages/BillingPage.tsx`: wallet buckets, debt state, transaction history, and referral status.
- Modify router/navigation components: replace `/credits` with `/billing`.
- Modify task/plan/resume forms: quote fixed task SKUs and submit `quote_id`.
- Modify Designer/video forms: quote standalone operation SKUs.
- Delete sign-in, recharge bonus, membership, token/USD usage, multiplier, hold, and payment-required UI.
- Create `server/migrations/forward/20260717_fixed_sku_billing.sql`: destructive schema cutover.
- Modify backend legacy models/routes/config: remove old billing contract after all callers move.

### Task 1: Add Strict Studio Billing Types And Client

**Files:**
- Create: `studio/src/lib/api/billing.ts`
- Create: `studio/src/lib/api/billing.test.ts`
- Modify: `studio/src/lib/schemas.ts`
- Modify: `studio/src/lib/api/index.ts`

- [ ] **Step 1: Write failing schema and client tests**

```ts
it('parses debt-aware wallet without legacy fields', () => {
  const wallet = billingWalletSchema.parse({
    paid: 600,
    promotional: 100,
    debt: 400,
    balance: 300,
  })
  expect(wallet.balance).toBe(300)
  expect('reserved' in wallet).toBe(false)
})
```

Test quote expiry/SKU snapshot, transaction kinds, referral status, top-up absence from public mutations, and rejection of token price, USD cost, multiplier, reserved, payment-required, and shortfall fields.

- [ ] **Step 2: Run tests and verify RED**

Run: `cd studio && bun run test -- src/lib/api/billing.test.ts`

Expected: FAIL because the schemas and client do not exist.

- [ ] **Step 3: Implement typed schemas and API methods**

```ts
export const billingWalletSchema = z.object({
  paid: z.number().int().nonnegative(),
  promotional: z.number().int().nonnegative(),
  debt: z.number().int().nonnegative(),
  balance: z.number().int(),
})

export type BillingQuote = {
  id: string
  catalogId: string
  skuId: string
  priceCredits: number
  expiresAt: string
}
```

Expose `getWallet`, `listTransactions`, `createQuote`, and `getReferral`. Keep admin top-up outside Studio because recharge is currently performed through the authenticated API.

- [ ] **Step 4: Run tests**

Run: `cd studio && bun run test -- src/lib/api/billing.test.ts`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add studio/src/lib/api/billing.ts studio/src/lib/api/billing.test.ts studio/src/lib/api/index.ts studio/src/lib/schemas.ts
git commit -m "feat(studio): add fixed billing API client"
```

### Task 2: Build The Debt-Aware Billing Page

**Files:**
- Create: `studio/src/pages/BillingPage.tsx`
- Create: `studio/src/pages/BillingPage.test.tsx`
- Modify: `studio/src/App.tsx`
- Modify: navigation/account components and tests
- Delete: legacy `CreditsPage` after route replacement

- [ ] **Step 1: Write failing page tests**

Test paid/promotional/debt/net values, negative net balance, debt repayment message, nearest promotion expiry, transaction labels, referral status, pagination, loading/error/empty states, `/billing` route, `/credits` absence, and no recharge/sign-in/membership/runtime-cost controls.

- [ ] **Step 2: Run tests and verify RED**

Run: `cd studio && bun run test -- src/pages/BillingPage.test.tsx`

Expected: FAIL because the page and route do not exist.

- [ ] **Step 3: Implement the operational billing page**

Use a dense four-column summary on desktop and a readable two-column/mobile stack. Debt is visually distinct from spendable balances. Transactions use icons and exact fixed labels; there are no nested cards or marketing sections. Link debt-state actions to the manual top-up contact/process already used by operations, without exposing an unauthenticated admin endpoint.

- [ ] **Step 4: Run page tests**

Run: `cd studio && bun run test -- src/pages/BillingPage.test.tsx`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A studio/src/pages studio/src/App.tsx studio/src/components
git commit -m "feat(studio): add debt-aware billing page"
```

### Task 3: Quote And Admit Task Workflows

**Files:**
- Modify: task creation pages/components and tests
- Modify: plan creation pages/components and tests
- Modify: task resume/retry components and tests

- [ ] **Step 1: Write failing quote-flow tests**

Test quote loading after billable options settle, fixed total display, quote refresh on option changes, expiry refresh before submit, `quote_id` submission, debt disabling new task creation, insufficient balance response, scheduled plan quote-at-run copy, resume/retry no new quote, and no actual-usage estimate.

- [ ] **Step 2: Run tests and verify RED**

Run: `cd studio && bun run test -- src/pages src/components --testNamePattern='billing quote|task admission'`

Expected: FAIL because forms still use legacy pricing or omit quotes.

- [ ] **Step 3: Implement one reusable quote hook**

```ts
export function useBillingQuote(input: BillingQuoteInput | null) {
  return useQuery({
    queryKey: ['billing-quote', input],
    queryFn: () => billingApi.createQuote(input!),
    enabled: input !== null,
    staleTime: 0,
  })
}
```

Render one fixed credit total near the submit command. Disable new task submission when wallet debt is positive, but never add billing controls to running-task or retry/resume views.

- [ ] **Step 4: Run workflow tests**

Run: `cd studio && bun run test -- src/pages src/components --testNamePattern='billing quote|task admission'`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add studio/src/pages studio/src/components studio/src/hooks
git commit -m "feat(studio): quote fixed task prices"
```

### Task 4: Quote Standalone Designer And Video Operations

**Files:**
- Modify: Designer pages/components and tests
- Modify: standalone video pages/components and tests

- [ ] **Step 1: Write failing standalone quote tests**

Test route/model/quality/size/duration option fingerprints, exact SKU price, quote refresh, debt rejection, insufficient balance, quote ID submission, and fixed price unchanged after provider usage. Confirm accepted-task MCP operations are not separately preflighted in Studio because they execute inside the task.

- [ ] **Step 2: Run tests and verify RED**

Run: `cd studio && bun run test -- src/pages src/components --testNamePattern='standalone billing|designer quote|video quote'`

Expected: FAIL because standalone forms use dynamic formulas or no quote.

- [ ] **Step 3: Apply the shared quote hook**

Map validated option schemas to the backend quote fingerprint. Show the returned fixed credit price and expiry state. Do not expose internal provider model prices or calculated cost.

- [ ] **Step 4: Run standalone tests**

Run: `cd studio && bun run test -- src/pages src/components --testNamePattern='standalone billing|designer quote|video quote'`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add studio/src/pages studio/src/components
git commit -m "feat(studio): quote standalone media prices"
```

### Task 5: Remove Legacy Billing UI

**Files:**
- Delete/modify: sign-in reward UI and API hooks
- Delete/modify: recharge bonus and membership UI
- Delete/modify: token/USD usage pages and types
- Delete/modify: payment-required/shortfall/hold UI
- Modify: tests, mocks, navigation, and labels

- [ ] **Step 1: Add a failing forbidden-surface test**

Scan production Studio sources for `/credits`, daily sign-in, recharge bonus, membership multiplier, `agent_runtime`, `total_cost_usd`, token price, reserved balance, hold, payment required, shortfall, and dynamic final-charge copy.

- [ ] **Step 2: Run tests and verify RED**

Run: `cd studio && bun run test -- src/test/billing-cutover.test.ts`

Expected: FAIL with remaining legacy surfaces.

- [ ] **Step 3: Delete legacy surfaces and update mocks**

Remove the old routes, API methods, schemas, components, copy, and test fixtures. Replace balance fetches with `billingApi.getWallet` and insufficient/debt actions with `/billing`.

- [ ] **Step 4: Run Studio tests and build**

Run: `cd studio && bun run test && bun run build`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A studio
git commit -m "refactor(studio): remove legacy billing UI"
```

### Task 6: Add The Destructive Forward Migration

**Files:**
- Create: `server/migrations/forward/20260717_fixed_sku_billing.sql`
- Create: `server/migrations/forward/20260717_fixed_sku_billing_test.go`
- Modify/delete: legacy billing models, repositories, handlers, routes, and root YAML fields

- [ ] **Step 1: Write failing migration and removal tests**

Test creation of new wallet/charge/cost/margin/catalog tables and indexes; absence of holds/reservations; removal of legacy credit transaction, multiplier, sign-in, recharge tier, runtime charge, token/USD task fields, payment-required, and shortfall schema; and optional opening-balance import only through explicit top-up entries.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./server/migrations/forward ./server/... -run 'TestFixedSKUBillingCutover|TestNoLegacyBillingContract' -count=1`

Expected: FAIL because the migration and deletions are incomplete.

- [ ] **Step 3: Implement one-way SQL and backend deletion**

The migration creates new tables first, validates catalog availability, imports only an operator-provided opening paid-balance file when configured, and then drops legacy tables/columns. It does not infer balances from ambiguous historical dynamic charges and has no down migration.

Delete backend compatibility DTOs, routes, config, and code after the SQL contract is established. Keep provider-cost admin routes private and new wallet routes public/authenticated as specified.

- [ ] **Step 4: Run migration, full tests, and builds**

```bash
go test ./...
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
cd studio && bun run test && bun run build
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A server agent app studio go.mod go.sum
git commit -m "refactor(billing): cut over to fixed SKU ledger"
```

### Task 7: Run Release Verification And Browser QA

**Files:**
- Modify only files needed to fix verification findings

- [ ] **Step 1: Run static forbidden-key scans**

```bash
rg -n 'credits_per_cny|tier_multipliers|billing_multiplier|daily_sign_in|register_bonus|agent_runtime|goal_mode_multiplier|total_cost_usd|payment_required|billing_shortfall|billing_holds|ReserveCredits' server studio --glob '*.go' --glob '*.ts' --glob '*.tsx' --glob '*.yaml'
```

Expected: no production hits outside historical migrations/docs explicitly allowlisted by the contract test.

- [ ] **Step 2: Run complete automated verification**

```bash
go test ./...
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
cd studio && bun run test
cd studio && bun run build
```

Expected: all commands exit `0`.

- [ ] **Step 3: Start Studio and API for browser verification**

Use the repository's normal local configuration and a seeded test account. Verify no secret appears in browser/network logs.

- [ ] **Step 4: Verify desktop and mobile workflows**

Use the browser testing skill to capture and inspect:

1. positive wallet with paid and promotional balances;
2. fixed task quote and successful task creation;
3. accepted task premium operation creating debt without interrupting execution;
4. debt wallet disabling a new task;
5. paid top-up API repayment reflected in Studio;
6. referral status and expiring promotion;
7. standalone media quote and admission;
8. no sign-in, bonus, multiplier, hold, token cost, or payment-required UI.

Check 1440x900 and 390x844 viewports for overflow, overlap, stale quote submission, loading/error states, and transaction label clarity.

- [ ] **Step 5: Fix findings and rerun all verification**

Expected: automated suites remain green and browser screenshots show no functional or layout regressions.

- [ ] **Step 6: Commit final verification fixes**

```bash
git add -A
git commit -m "test(billing): verify fixed SKU cutover"
```

## Plan 4 Completion Gate

- Studio shows paid, promotional, debt, and signed net balance.
- Debt disables only new admission and never changes running-task state.
- Task and standalone submissions use fixed quotes.
- Manual API top-up repayment appears correctly; no online checkout is introduced.
- Referral remains as the only promotional acquisition flow.
- Legacy sign-in, bonus, multiplier, dynamic usage, hold, and payment-required surfaces are absent.
- Forward migration contains no compatibility or rollback path.
- Full Go/Studio verification and desktop/mobile browser QA pass.
