# Fixed SKU Billing And Provider Cost Ledger Design

**Date:** 2026-07-17

## Summary

Anban uses two independent ledgers:

1. The retail wallet charges users fixed, versioned SKU prices in credits.
2. The provider cost ledger records measured model, image, and video cost for internal margin reporting.

Retail prices never come from provider token usage. Provider cost never mutates the user wallet. An accepted task is allowed to finish even if successful image, video, or other premium MCP operations make the wallet negative. A user with debt cannot create another task; a later paid top-up repays debt before creating spendable credits.

Claude Code continues to call Ark directly. Its `total_cost_usd` is ignored. Normal terminal executions use the final raw `result.modelUsage` map to record exact per-model input, cache-read, cache-creation, and output token usage. Partial message events are not a billing source because child Agent events are not forwarded in the parent stream.

This design replaces the billing portions of `2026-04-10-credits-system-design.md` and `2026-07-15-agent-runtime-billing-and-failure-artifact-collection-design.md`. Artifact collection remains valid.

## Confirmed Product Policy

- User-facing prices are fixed SKU prices.
- Agent runtime is included in the task-creation SKU and is never charged from tokens.
- Expensive image, video, and other premium MCP operations use separate fixed operation SKUs.
- Task creation is the only balance admission boundary for task execution; standalone premium operations have their own prepaid admission.
- An accepted task never performs another balance sufficiency check.
- Successful operation charges may create wallet debt.
- Debt blocks creation of new tasks, plans that would execute immediately, and standalone premium operations.
- Paid top-ups repay debt first. Promotional credits cannot repay debt.
- Failed provider operations are not charged. Replaying a successful operation cannot charge twice.
- A platform-failed task with no accepted deliverable receives an idempotent reversal of its task-creation charge.
- Automatic retries and resumed execution under the same task do not create another task charge.
- There is no daily sign-in reward, registration gift, recharge bonus, membership price multiplier, or generic admin grant.
- Invitation linkage remains. A fixed referral promotion may be issued once after the invitee's first qualifying paid top-up.
- Existing balances and legacy billing records are not migrated. Cutover is forward-only.

## Goals

- Make every user charge knowable before the charged operation begins.
- Keep accepted tasks stable when later operation charges exceed the wallet balance.
- Make top-up, charge, debt, reversal, expiry, and referral events exactly auditable.
- Calculate provider cost from measured usage and immutable price versions.
- Report revenue, receivables, promotional consumption, provider cost, and margin separately.
- Remove every legacy dynamic runtime deduction and compatibility fallback.

## Non-Goals

- Subscriptions or online payment checkout.
- Real-time provider-cost enforcement.
- User-visible token, USD, cache, provider-model, or multiplier pricing.
- A Claude proxy or budget gateway.
- Recovering exact token cost from an execution that terminates without a final `result.modelUsage`.
- Preserving legacy credit APIs, tables, routes, or response shapes.

## Financial Units

- Credits are signed `int64` values. API JSON uses integers.
- CNY accounting uses signed integer micro-CNY. `1 CNY = 1,000,000 micro-CNY`.
- Configuration decimals are parsed from strings with exact decimal arithmetic.
- Floating-point values are forbidden in wallet, cost, and margin persistence.
- The fixed top-up exchange is `1 CNY = 1,000 credits`.
- Provider prices are internal cost facts, not retail formulas.

## Architecture

### Retail Catalog

Every billable action resolves exactly one active SKU version. A SKU contains:

- stable SKU ID and immutable catalog version;
- operation type and eligible workflow/options;
- fixed credit price;
- charge policy: `task_admission`, `accepted_task_operation`, or `standalone_operation`;
- delivery evidence required before an operation charge;
- operational limits and allowed provider routes where relevant.

Changing a price, workflow mapping, charge policy, or delivery contract publishes a new catalog version. Existing task and charge records keep their original snapshot.

### Wallet Account

Each user has one projected account:

```text
display_balance = paid_credits + promotional_credits - debt_credits
```

The three buckets are non-negative integers:

- `paid_credits`: cash-backed spendable balance;
- `promotional_credits`: expiring, eligibility-constrained acquisition balance;
- `debt_credits`: successful accepted-task operation charges not covered by spendable credits.

The projection is disposable and rebuildable from append-only entries and credit lots. Client requests never supply balance-after values.

### Charge Ordering

For an eligible charge:

1. consume eligible promotional lots by earliest expiry;
2. consume paid lots FIFO;
3. if the policy is `accepted_task_operation`, place any remainder in debt;
4. otherwise reject before creating a charge.

Task admission requires `debt_credits == 0` and eligible spendable credits greater than or equal to the fixed task price. The task charge is recorded atomically with task creation. After acceptance, the executor and MCP tools never ask the wallet whether work may continue.

### Paid Top-Up

A top-up API call requires an immutable external reference and is idempotent within its source. Credits are applied in this order:

1. reduce `debt_credits`;
2. create a non-expiring paid lot for the remainder.

The response reports `debt_repaid_credits` and `paid_credits_added`. A top-up smaller than the debt leaves no spendable paid balance.

### Promotions And Referral

There is no registration gift or sign-in reward. Registration preserves `InvitedBy` when a valid invitation is used. After the invitee's first qualifying paid top-up commits, the inviter and invitee may each receive one fixed promotional lot from the active referral program.

Referral issuance is idempotent by invitee and program. Promotional lots expire, can be restricted to SKU families, and cannot repay debt. A debt user may receive a referral lot, but remains blocked from task admission until paid top-up clears the debt.

### Task Settlement

Task creation performs quote validation, wallet admission, task insertion, and task charge insertion in one database transaction. Execution receives the immutable SKU snapshot but no wallet budget.

Terminal behavior:

- success: keep the task charge;
- user cancellation after execution starts: keep the task charge;
- invalid user input rejected before acceptance: no charge or task;
- platform/provider failure with no accepted deliverable: append one exact reversal;
- retry or resume of the same task: no new task charge;
- explicit creation of a new task: new admission and charge.

Reversal policy is based on terminal reason codes, not free-form error text.

### Premium MCP Settlement

Image, video, and other premium MCP tools resolve a fixed operation SKU before the provider call. They do not reserve credits and do not stop an accepted task because of balance.

The durable-output transaction also appends one idempotent settlement-outbox row using the task ID, attempt ID, tool call ID, and SKU version. The outbox worker applies the wallet charge immediately when possible and retries transient database failures without changing the successful tool result. A failed or output-less operation records provider cost when available but creates no retail charge. Duplicate tool results return the original output and settlement identity.

Standalone premium operations use `standalone_operation`; they require no debt and sufficient balance before dispatch. They cannot overdraw because no accepted parent task exists.

### Operational Limits

Operational limits protect task stability and platform capacity; they are not monetary settlement:

- maximum Agent turns;
- task retry count;
- per-task image generation calls;
- per-task media operation calls;
- image/video understanding calls;
- provider timeout and output-size limits.

Crossing a limit returns a structured workflow error. It never converts measured provider cost into a user charge and never performs a running-cost calculation.

## Provider Cost Ledger

Provider cost events are append-only and never update the wallet. Each event stores:

- provider and exact normalized model ID;
- immutable cost-catalog version;
- request or execution identity and domain idempotency key;
- typed usage evidence;
- exact micro-CNY calculation and currency-rate snapshot;
- source: `provider_response`, `claude_result_model_usage`, `invoice_adjustment`, or `manual_reconciliation`;
- reconciliation status and safe metadata.

Prompts, generated content, authorization headers, tokens, and media URLs are never persisted as cost evidence.

### Claude Code Evidence

Controlled Claude Code 2.1.197 tests established:

- final `result.modelUsage` contains separate entries for the parent Evolving model and a child Haiku-mapped Turbo model;
- each entry contains input, output, cache-read, and cache-creation tokens;
- top-level `result.usage` contains only the parent model;
- parent partial events omit child Agent token events;
- `task_notification.usage.total_tokens` does not exactly reconcile with model usage;
- `total_cost_usd` applies Claude's internal prices to third-party model IDs and materially overstates Ark cost;
- `contextWindow` is Claude metadata, not provider capability evidence.

Therefore:

- ignore `total_cost_usd`, top-level aggregate usage, task notification totals, and context window for accounting;
- do not enable partial messages solely for billing;
- extend the current Go SDK result type to expose typed `modelUsage`;
- normalize aliases such as `doubao-seed-evolving-latest-version` to exact configured provider model IDs;
- write one cost event per model entry when the terminal result arrives;
- mark an execution `unreconciled` when no valid terminal model-usage map is available;
- never charge or block the user because cost evidence is missing.

The current latest `github.com/severity1/claude-agent-sdk-go v0.6.22` does not expose `modelUsage`. Anban uses a minimal reviewed fork until the field is accepted upstream. Replacing the SDK with ad hoc CLI process management is out of scope.

### Media Evidence

Image and video providers use provider response usage where supplied. When fixed provider pricing depends on output dimensions or duration, persisted output metadata is the evidence. Missing evidence marks the cost event unreconciled; it does not synthesize a user price.

### Margin Facts

Each retail charge or reversal and each provider cost event creates immutable reporting facts. Reports separate:

- cash received;
- deferred paid-credit value;
- paid-credit revenue recognized by charges;
- promotional-credit consumption;
- debt/service receivable created and collected;
- provider cost;
- platform-failure cost;
- contribution margin.

Provider cost may arrive after the retail charge. Reports group by task, SKU version, provider, model, user, referral program, terminal outcome, and UTC accounting date without rewriting source events.

## Configuration Contract

Billing configuration lives under a mandatory directory and uses strict YAML decoding. Unknown fields, duplicate IDs, invalid decimals, missing references, and ambiguous route mappings fail startup.

### Root Runtime Configuration

```yaml
billing_runtime:
  config_dir: "./billing"
  admin_api_key: "${ANBAN_BILLING_ADMIN_API_KEY}"

claude:
  provider: "volcengine_ark"
  base_url: "https://ark.cn-beijing.volces.com/api/compatible"
  auth_token: "${CLAUDE_CODE_AUTH_TOKEN}"
  models:
    default: "doubao-seed-evolving"
    opus: "doubao-seed-evolving"
    fable: "doubao-seed-evolving"
    sonnet: "doubao-seed-2-1-pro-260628"
    haiku: "doubao-seed-2-1-turbo-260628"
```

The runtime builder derives the `ANTHROPIC_*` environment variables. Direct duplicate values in an arbitrary `claude.env` map are rejected. Provider credentials are redacted from config APIs and logs.

### `billing/policy.yaml`

```yaml
version: "2026-07-17"
credits_per_cny: 1000
task_admission:
  require_zero_debt: true
  require_full_price: true
accepted_task:
  continue_when_balance_negative: true
  operation_charge_may_create_debt: true
top_up:
  repay_debt_first: true
promotions:
  may_repay_debt: false
task_failure_reversal:
  enabled: true
  reasons: [platform_error, provider_error, execution_timeout, infrastructure_cancelled]
```

### `billing/products.yaml`

```yaml
catalog_id: "retail-2026-07-17-v1"
currency: credits
skus:
  - id: "task.seednote.standard.v1"
    operation: "task.seednote"
    charge_policy: "task_admission"
    price_credits: 5000
    delivery: "seednote_artifacts_verified"

  - id: "task.wechatarticle.standard.v1"
    operation: "task.wechatarticle"
    charge_policy: "task_admission"
    price_credits: 6000
    delivery: "article_artifacts_verified"

  - id: "image.seedream.standard.v1"
    operation: "mcp.generate_image"
    charge_policy: "accepted_task_operation"
    price_credits: 500
    route: "image_generation.content"
    delivery: "persisted_image"

  - id: "image.seedream.standalone.v1"
    operation: "designer.generate_image"
    charge_policy: "standalone_operation"
    price_credits: 500
    route: "image_generation.designer.seedream"
    delivery: "persisted_image"
```

Task and operation prices are business defaults, not calculated markups. Video SKUs enumerate resolution, duration, audio, and model options so one request resolves exactly one SKU.

### `billing/costs.yaml`

```yaml
catalog_id: "provider-cost-2026-07-17-v1"
currency_rates:
  USD: "7.20"
  CNY: "1.00"
models:
  volcengine_ark/doubao-seed-evolving:
    pricing_type: "token"
    currency: "CNY"
    unit: 1000000
    input: "6.00"
    cache_read_input: "1.20"
    cache_creation_input: "6.00"
    output: "30.00"

  volcengine_ark/doubao-seed-2-1-pro-260628:
    pricing_type: "token"
    currency: "CNY"
    unit: 1000000
    input: "6.00"
    cache_read_input: "1.20"
    cache_creation_input: "6.00"
    output: "30.00"

  volcengine_ark/doubao-seed-2-1-turbo-260628:
    pricing_type: "token"
    currency: "CNY"
    unit: 1000000
    input: "3.00"
    cache_read_input: "0.60"
    cache_creation_input: "3.00"
    output: "15.00"

  volcengine_ark/doubao-seedream-5-0-pro-260628:
    pricing_type: "output_pixel_tier"
    currency: "CNY"
    tiers:
      - max_pixels: 2360000
        price: "0.30"
      - price: "0.60"
```

Every price entry includes operator evidence and effective timestamps in the persisted catalog even when omitted from this abbreviated example.

### `billing/promotions.yaml`

```yaml
catalog_id: "promotion-2026-07-17-v1"
programs:
  - id: "referral-first-topup-v1"
    trigger: "invitee_first_paid_topup"
    minimum_topup_cny: "10.00"
    inviter_credits: 1000
    invitee_credits: 1000
    expires_after: 30d
    max_inviter_rewards: 10
    can_repay_debt: false
```

## Data Model

### Wallet

- `billing_wallet_accounts`: projected paid, promotional, debt, version, and timestamps.
- `billing_credit_lots`: paid or promotional origin, remaining credits, eligibility, expiry, and immutable source.
- `billing_wallet_entries`: append-only top-up, promotion, charge, debt, repayment, reversal, and expiry events.
- `billing_charges`: SKU snapshot, task/tool identity, allocations, debt created, status, and idempotency key.
- `billing_charge_allocations`: exact promotional and paid lot consumption.
- `billing_settlement_outbox`: durable operation-charge and task-reversal intents, retry state, and terminal result identity.
- `billing_catalog_versions` and `billing_skus`: immutable published retail catalog.
- `billing_quotes`: short-lived immutable SKU resolution used by task and standalone-operation admission.

There are no hold, reservation, capture, release, payment-required, or billing-shortfall tables.

### Cost And Margin

- `billing_provider_cost_events`: typed usage, calculation snapshot, source, status, and adjustment link.
- `billing_execution_cost_status`: expected execution identity and reconciled/unreconciled state.
- `billing_margin_fact_events`: append-only revenue, receivable, promotion, cost, failure, and reversal facts.

## API Contract

Public APIs:

- `GET /api/billing/wallet`
- `GET /api/billing/transactions`
- `POST /api/billing/quotes`
- existing task-create endpoints with required `quote_id`
- existing standalone media endpoints with required `quote_id`
- `GET /api/billing/referral`

Authenticated admin APIs:

- `POST /api/admin/billing/topups`
- `GET /api/admin/billing/costs`
- `GET /api/admin/billing/margins`
- `GET /api/admin/billing/reconciliation`

Top-up and charge endpoints require idempotency keys. Admin authentication uses a constant-time comparison and never exposes the configured key.

## Studio Behavior

- Show one fixed credit price before task or standalone-operation creation.
- Show paid credits, promotional credits, debt, and net balance.
- When debt is positive, show that paid top-up repays it first and disable new-task submission.
- Do not show token usage, cache prices, provider costs, multipliers, holds, or estimated final charges.
- Running task UI never changes to `payment_required`.
- Transaction labels distinguish task charge, operation charge, debt created, top-up debt repayment, referral reward, reversal, and promotion expiry.
- Remove sign-in, recharge bonus, membership, and legacy `/credits` surfaces.

## Error Contract

- `billing_quote_expired`: refresh quote before admission.
- `billing_debt_outstanding`: paid top-up is required before a new task.
- `billing_insufficient_for_task`: insufficient eligible spendable balance for task admission.
- `billing_insufficient_for_standalone_operation`: standalone operation cannot overdraw.
- `billing_sku_not_found`: request does not resolve exactly one active SKU.
- `billing_charge_conflict`: same idempotency identity has different parameters.
- `billing_config_invalid`: startup validation failed.
- `provider_cost_unreconciled`: admin-only cost status; never a user execution error.

## Concurrency And Idempotency

- Wallet mutations lock the account row and use monotonically increasing versions.
- External top-up references are unique within a source.
- Task charges are unique by task ID and charge kind.
- MCP charges are unique by task, attempt, tool-call, and SKU version.
- Reversals are unique by original charge.
- Durable outputs and terminal task states enqueue settlement intents in the same database transaction; transient wallet failures are retried from the outbox and never invalidate successful output.
- Referral rewards are unique by invitee and program.
- Provider cost base events are unique by provider/request identity or execution/model identity.
- Replays return the existing semantic result; parameter drift returns a conflict.

## Forward Cutover

The release performs one maintenance-window cutover:

1. deploy code that understands only the new contract;
2. publish and validate billing catalogs;
3. create new empty wallet projections;
4. import only explicitly approved opening paid balances as top-up entries;
5. delete legacy credits, multipliers, sign-in rewards, runtime charges, holds, payment-required states, and dynamic pricing APIs;
6. switch Studio to `/billing` and fixed quotes;
7. verify new task admission, accepted-task debt, debt repayment, task reversal, provider cost, and margin reports.

No compatibility readers, dual writes, or fallback prices remain.

## Testing Strategy

Required tests cover:

- strict configuration decoding and cross-file references;
- exact decimal and micro-CNY arithmetic;
- task admission under sufficient, insufficient, promotional, and debt states;
- accepted-task operation overdraft and uninterrupted execution;
- top-up smaller than, equal to, and greater than debt;
- promotion inability to repay debt;
- concurrent task admission and operation charges;
- idempotent top-up, task charge, operation charge, referral, reversal, and cost events;
- platform failure reversal and user-cancel no-reversal policies;
- Evolving plus Turbo `modelUsage` parsing and exact cost calculation;
- missing final model usage marked unreconciled without wallet mutation;
- image/video durable-output charge boundaries;
- full Go tests, server/agent builds, Studio tests/build, and browser QA.

## Acceptance Criteria

- A new task cannot be created with debt or insufficient fixed-price balance.
- Once accepted, a task never stops because its wallet becomes negative.
- Successful premium MCP operations charge their fixed SKU exactly once and may create debt.
- A transient wallet-write failure after durable output is repaired by the settlement outbox without failing the accepted task.
- Paid top-up repays debt first; promotion never repays debt.
- Claude `total_cost_usd` is absent from every wallet and cost calculation.
- Final `modelUsage` records Evolving and child Turbo usage separately.
- Missing terminal usage produces an admin reconciliation state, not a user charge or task failure.
- No Claude gateway or running provider-cost budget exists.
- No legacy sign-in, bonus, multiplier, dynamic runtime deduction, hold, or payment-required path remains.
- Users see fixed prices and wallet debt; administrators see provider cost and contribution margin.
