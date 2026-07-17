# Fixed SKU Billing And Cost Ledger Design

**Date:** 2026-07-17
**Status:** Draft for written review
**Compatibility:** Forward-only replacement

## Summary

Anban will sell versioned, fixed-price products while separately measuring the actual cost of every provider call. Users will never be charged from token counts, provider-reported monetary estimates, tier multipliers, or post-execution overages.

The billing boundary is:

1. A versioned SKU defines the exact user-visible credit price and included limits.
2. The server quotes and reserves that fixed amount before execution.
3. Successful durable delivery captures the hold once; failure releases it in full.
4. Provider usage is written to a separate immutable cost ledger and never changes the user wallet.
5. Paid-credit revenue, provider cost, and promotional acquisition cost are combined into margin facts for operating decisions.

This design supersedes `2026-04-10-credits-system-design.md` and the runtime-billing portion of `2026-07-15-agent-runtime-billing-and-failure-artifact-collection-design.md`. The artifact-collection design remains valid.

## Confirmed Product Policy

- User pricing is fixed and SKU-based.
- Provider usage remains precisely metered for cost and margin reporting.
- Funding is manual recharge through an authenticated admin API.
- There are no subscriptions, online payments, recharge bonuses, daily sign-in rewards, postpaid balances, negative balances, or hidden overages.
- Paid and promotional credits are separate credit lots.
- Verified registration receives limited, expiring onboarding credits.
- An inviter receives expiring promotional credits only after the invitee's first real recharge of at least CNY 10.
- Promotional credits recognize no revenue. Their attributable provider cost is acquisition cost.
- Automatic retries remain inside the original hold and cannot create a second user charge.
- A completed purchase produces one final visible capture transaction.

## Goals

- Make every user charge predictable before execution.
- Make every yuan of cash, promotional credit, provider cost, and recognized revenue traceable.
- Prevent both accidental overcharging and unbounded provider-cost exposure.
- Calculate profitability by SKU, task, model, user, promotion, and date.
- Fail startup when an enabled route is missing either a cost profile or an exact SKU mapping.
- Make all financial state transitions idempotent and recoverable after process or queue failure.

## Non-Goals

- Subscription plans, online checkout, invoices, tax documents, or payment-provider integration.
- Usage-based retail billing or user-visible token prices.
- Provider-cost passthrough, account-specific price multipliers, or tier multipliers.
- Automatic migration of ambiguous legacy balances.
- Deploying OpenMeter, Flexprice, Lago, or another external billing platform in the first implementation.
- Preserving old credit APIs, transaction types, configuration keys, or database contracts.

## Financial Units And Invariants

Financial calculations must not use floating point.

- Credits use signed `int64`, but wallet invariants prevent balances below zero.
- Cash received is stored in integer CNY fen.
- Provider cost and recognized revenue are stored in integer micro-CNY.
- Decimal configuration values are parsed with an exact decimal type and converted to integer micro-CNY.
- The recharge exchange is exact: CNY 1.00 produces 1,000 paid credits, so one fen produces 10 credits.
- Credits are not currency and cannot be withdrawn, transferred, or converted back to cash.
- Wallet balance is a projection of immutable entries; it is not the accounting source of truth.
- The sum of lot available, reserved, consumed, and expired credits must equal the lot's original credits.
- Every hold must end in exactly one terminal state: captured or released.
- Every capture must reference one hold, one quote, one SKU version, and the exact lot allocations reserved by that hold.

## Architecture

### Product Catalog

The product catalog is the only source of user-visible prices. Each immutable SKU version defines:

- exact credit price;
- workflow or media operation;
- allowed request dimensions;
- included media, attempts, and execution limits;
- maximum provider cost in CNY;
- durable-delivery predicate;
- promotion eligibility.

Changing price, limits, provider allowance, or delivery semantics creates a new SKU version. Existing quotes and holds continue to reference the old version. Runtime formulas cannot synthesize retail prices.

### Wallet Ledger And Credit Lots

The wallet contains immutable lots:

- `paid_topup`: purchased credits with actual cash received and external payment reference;
- `onboarding`: expiring promotional credits issued after verified registration;
- `referral`: expiring promotional credits issued after a referred user's first qualifying top-up.

Eligible promotional lots are reserved first by earliest expiry, followed by paid lots in FIFO order. A hold stores its exact allocation across lots. Capture consumes that allocation; release restores it. The Studio displays paid, promotional, reserved, and available balances separately.

### Provider Cost Ledger

Every provider call writes an append-only cost event containing:

- provider request ID, task ID, execution attempt ID, user ID, and SKU version;
- provider, exact model ID, operation, and cost-catalog version;
- normalized usage dimensions plus the raw provider usage payload;
- source currency, source amount, FX snapshot, and exact `cost_cny_micros`;
- evidence type: `provider_usage`, `accrual_estimate`, `provider_invoice`, or `adjustment`;
- reference to a prior event when correcting or reconciling cost.

A correction appends a compensating event; it never updates the original. Provider cost never deducts credits and never changes a captured user price.

### Margin Facts

Margin facts are append-only accounting events derived from wallet captures and provider cost events. A rebuildable reporting projection aggregates:

- recognized paid-credit cash revenue;
- provider cost;
- promotional credits consumed;
- provider cost attributed to promotional credits as acquisition cost;
- provider cost from released or otherwise uncaptured executions as platform failure cost;
- paid gross profit and paid gross margin;
- fully loaded contribution after promotional acquisition cost.

For mixed paid and promotional captures, provider cost is attributed by the captured-credit ratio. Provider cost from a released hold has no captured-credit allocation and is classified as platform failure cost. Reports group by SKU, task, provider, model, user, promotion, terminal outcome, and UTC accounting date.

## Quote, Reserve, Capture, And Release

### Quote

The server resolves exactly one active SKU from the user's chosen workflow and options. A quote snapshots the SKU version, fixed price, included limits, cost budget, promotion eligibility, and a 15-minute expiry. A request that maps to zero or multiple SKUs is rejected before task creation.

### Reserve

Creating the task atomically creates a hold and allocates the fixed price across eligible lots. Insufficient available balance rejects the request without creating a task. Holds cannot overdraw, and concurrent requests serialize on the wallet account.

### Execution

The execution receives the hold ID, SKU version, and remaining provider-cost budget. All automatic retries share that context. Provider calls reserve internal cost budget before dispatch and settle it from measured usage afterward. Crossing the hard cost budget aborts the execution, marks it as a platform-budget failure, and releases the user's hold in full.

Provider and media operations included by a task SKU inherit the parent hold and cost budget. They never create child user holds or separate user charges. A standalone image or video action uses its own media SKU only when it was initiated outside a task package. An Agent attempting to exceed a task's included media count is stopped before the extra provider request; the task then fails delivery and releases the parent hold.

### Capture

Capture occurs only after the SKU's durable-delivery predicate is verified. The task, artifact manifest, and required output files must already be committed. Capture consumes the original lot allocations and creates one visible user transaction. Replaying capture returns the existing result.

### Release

Queue rejection, model failure, timeout, cancellation, invalid provider usage, missing durable output, or internal cost-budget exhaustion releases the full hold. Retries cannot create additional charges. Replaying release returns the existing result. Capture and release are mutually exclusive under a database uniqueness constraint.

### Reconciliation

A five-minute reconciler repairs expired quotes, orphaned holds, missing projections, and interrupted terminal transitions. Holds older than their execution deadline plus 30 minutes are released unless a verified durable delivery is already awaiting capture. Reconciliation uses the same idempotent service methods as live execution.

## Recharge And Promotion Flows

### Paid Top-Up

The only funding endpoint is `POST /api/v1/admin/billing/topups`. It requires the admin API key, user ID, integer `cash_fen`, external reference, and idempotency key. The service derives credits from the fixed exchange and creates a `paid_topup` lot. Callers cannot supply the credit amount independently.

The same idempotency key and payload returns the original lot. Reusing a key with different cash, user, or external reference returns a conflict. Arbitrary admin grants are removed.

### Onboarding

Successful verified registration issues one 5,000-credit onboarding lot that expires after seven days. It can be combined with paid credits but is restricted to the explicitly configured introductory SKUs. Re-registration, login, and invite-code attachment cannot issue it again.

### Referral

An invite code is optional during registration and records an immutable inviter relationship. No reward is issued at registration. The inviter receives one 5,000-credit referral lot after the invitee's first paid top-up of at least CNY 10. The lot expires after 30 days. Self-invitation is rejected, an invitee can trigger only one reward, and one inviter can receive at most ten rewards.

## Configuration Contract

Billing configuration is split from runtime routing:

```text
billing/policy.yaml
billing/promotions.yaml
billing/products.yaml
billing/costs.yaml
billing/margins.yaml
```

All files use strict YAML decoding. Unknown keys, duplicate IDs, invalid decimals, missing references, and inconsistent budgets fail startup.

### `billing/policy.yaml`

```yaml
schema_version: 1
currency: CNY

credit:
  exchange:
    cash_fen: 100
    credits: 1000
  allow_negative: false
  balances: [paid, promotional]
  spending_order: [promotional_expiring_first, paid_fifo]

topup:
  mode: admin_api_only
  minimum_cash_fen: 1000
  require_idempotency_key: true
  require_external_reference: true
  recharge_bonus: false
  arbitrary_grant: false

settlement:
  quote_ttl: 15m
  hold_grace_after_execution: 30m
  capture_condition: durable_delivery_verified
  failure_action: full_release
  retries_share_same_hold: true
  hidden_overages: false
  partial_charge: false
  cost_budget_exceeded: abort_and_release

reconciliation:
  interval: 5m
  duplicate_capture_action: return_existing
```

### `billing/promotions.yaml`

```yaml
schema_version: 1

programs:
  onboarding:
    trigger: verified_registration
    lot_type: onboarding
    credits: 5000
    expires_after: 7d
    max_provider_cost_cny_per_recipient: "3.00"
    once_per_user: true
    eligible_skus:
      - task.seednote.standard.v1
      - task.article.standard.v1
      - image.seedream.pro.standard.v1

  referral:
    trigger: invitee_first_paid_topup
    minimum_topup_cash_fen: 1000
    recipient: inviter
    lot_type: referral
    credits: 5000
    expires_after: 30d
    max_provider_cost_cny_per_recipient: "3.00"
    max_rewards_per_inviter: 10
    reject_self_invitation: true
    eligible_skus:
      - task.seednote.standard.v1
      - task.article.standard.v1
      - image.seedream.pro.standard.v1

disabled:
  - daily_sign_in
  - recharge_bonus
  - registration_cash_value
  - admin_bonus
```

### `billing/products.yaml`

The initial catalog deliberately uses conservative task budgets. Prices are operating defaults, not formulas; later changes require new SKU versions.

```yaml
schema_version: 1
catalog_id: retail-cn-2026-07-17
status: active

skus:
  task.seednote.standard.v1:
    operation: task.seednote
    price_credits: 20000
    max_provider_cost_cny: "8.00"
    included_images: 6
    max_attempts: 2
    delivery: seednote_artifacts_verified

  task.article.standard.v1:
    operation: task.article
    price_credits: 25000
    max_provider_cost_cny: "10.00"
    included_images: 3
    max_attempts: 2
    delivery: article_artifacts_verified

  task.ecommerce.standard.v1:
    operation: task.ecommerce
    price_credits: 30000
    max_provider_cost_cny: "12.00"
    included_images: 6
    max_attempts: 2
    delivery: ecommerce_artifacts_verified

  task.videocreator.720p-5s.v1:
    operation: task.videocreator
    video_provider_model: volcengine_ark/doubao-seedance-2-0-fast-260128
    price_credits: 30000
    max_provider_cost_cny: "12.00"
    input_mode: no_video
    output_resolution: 720p
    output_duration_seconds: 5
    included_videos: 1
    max_attempts: 2
    delivery: video_artifact_verified

  task.videoeditor.standard.v1:
    operation: task.videoeditor
    price_credits: 15000
    max_provider_cost_cny: "6.00"
    max_attempts: 2
    delivery: edited_video_artifact_verified

  task.montage.standard.v1:
    operation: task.montage
    price_credits: 15000
    max_provider_cost_cny: "6.00"
    max_attempts: 2
    delivery: montage_artifacts_verified

  task.viral_analysis.standard.v1:
    operation: task.viral_analysis
    price_credits: 10000
    max_provider_cost_cny: "4.00"
    max_attempts: 2
    delivery: analysis_artifact_verified

  image.seedream.pro.standard.v1:
    operation: image.generate
    provider_model: volcengine_ark/doubao-seedream-5-0-pro-260628
    price_credits: 1000
    output_size_tier: 2K
    max_reference_images: 1
    max_provider_cost_cny: "0.60"
    delivery: generated_image_persisted

  image.seedream.pro.multi-reference.v1:
    operation: image.generate
    provider_model: volcengine_ark/doubao-seedream-5-0-pro-260628
    price_credits: 1500
    output_size_tier: 2K
    min_reference_images: 2
    max_reference_images: 10
    max_provider_cost_cny: "0.78"
    delivery: generated_image_persisted

  image.gpt-image-2.medium.v1:
    operation: image.generate
    provider_model: wangcai_openai/gpt-image-2-t
    quality: medium
    mode: text_to_image
    output_size: 1024x1024
    max_reference_images: 0
    price_credits: 1000
    max_provider_cost_cny: "0.40"
    delivery: generated_image_persisted

  image.gpt-image-2.high.v1:
    operation: image.generate
    provider_model: wangcai_openai/gpt-image-2-t
    quality: high
    mode: text_to_image
    output_size: 1024x1024
    max_reference_images: 0
    price_credits: 4000
    max_provider_cost_cny: "1.60"
    delivery: generated_image_persisted
```

Goal mode is disabled at cutover. It can return only after explicit goal-mode SKUs define fixed prices and hard budgets; `goal_mode_multiplier` is deleted.

### `billing/costs.yaml`

The cost catalog stores provider cost only. It is never exposed as retail pricing. The entries below are the confirmed cutover routes. Any other model must be disabled at cutover unless an equally complete cost profile is added before startup.

```yaml
schema_version: 1
cost_catalog_id: provider-cn-2026-07-17
status: active

fx_snapshot:
  USD_CNY:
    rate: "7.20"
    effective_at: 2026-07-17T00:00:00+08:00

models:
  moonshot/kimi-k2.7-code:
    evidence: operator_contract
    currency: USD
    unit: 1000000_tokens
    input: "0.95"
    cache_read_input: "0.19"
    cache_creation_input: "0.95"
    output: "4.00"

  moonshot/kimi-k2.7-code-highspeed:
    evidence: operator_contract
    currency: USD
    unit: 1000000_tokens
    input: "1.90"
    cache_read_input: "0.38"
    cache_creation_input: "1.90"
    output: "8.00"

  volcengine_ark/doubao-seed-evolving:
    evidence: volcengine_model_price_2026-07-13
    currency: CNY
    unit: 1000000_tokens
    input: "6.00"
    cache_read_input: "1.20"
    cache_creation_input: "6.00"
    cache_storage_per_hour: "0.017"
    cache_storage_accrual_hours: "1.00"
    output: "30.00"

  volcengine_ark/doubao-seed-2-1-pro-260628:
    evidence: volcengine_model_price_2026-07-13
    currency: CNY
    unit: 1000000_tokens
    input: "6.00"
    cache_read_input: "1.20"
    cache_creation_input: "6.00"
    cache_storage_per_hour: "0.017"
    cache_storage_accrual_hours: "1.00"
    output: "30.00"

  volcengine_ark/doubao-seed-2-1-turbo-260628:
    evidence: volcengine_model_price_2026-07-13
    currency: CNY
    unit: 1000000_tokens
    input: "3.00"
    cache_read_input: "0.60"
    cache_creation_input: "3.00"
    cache_storage_per_hour: "0.017"
    cache_storage_accrual_hours: "1.00"
    output: "15.00"

  volcengine_ark/doubao-seedream-5-0-pro-260628:
    evidence: volcengine_model_price_2026-07-13
    currency: CNY
    pricing_type: image_pixels
    input_images:
      first_image: "0.00"
      each_additional_image: "0.02"
    output_images:
      max_2360000_pixels: "0.30"
      above_2360000_pixels: "0.60"

  volcengine_ark/doubao-seedance-2-0-fast-260128:
    evidence: volcengine_model_price_2026-07-13
    currency: CNY
    pricing_type: video_seconds
    no_video_input_per_second:
      480p: "0.372"
      720p: "0.800"
    video_input_5s:
      480p: {minimum: "1.99", maximum: "4.42"}
      720p: {minimum: "4.28", maximum: "9.50"}

  wangcai_openai/gpt-image-2-t:
    evidence: operator_contract
    currency: USD
    pricing_type: openai_image_usage
    unit: 1000000_tokens
    text_input: "5.00"
    text_cached_input: "1.25"
    image_input: "8.00"
    image_cached_input: "2.00"
    image_output: "30.00"

cost_events:
  trust_claude_total_cost_usd: false
  require_provider_usage: true
  immutable: true
  invoice_adjustments: append_only
```

Provider evidence identifiers must resolve to operator-controlled evidence metadata containing source URL or contract reference, effective time, and verification time. An expired or absent cost catalog disables the affected route and fails startup when that route is required.

### `billing/margins.yaml`

```yaml
schema_version: 1

catalog_publish:
  minimum_gross_margin: "0.30"
  workflow_target_margin: "0.60"
  reject_missing_cost_profile: true
  reject_unmapped_route: true
  reject_unknown_sku: true

attribution:
  paid_credit_revenue: recognize_on_capture
  promotional_credit_revenue: zero
  promotional_provider_cost: acquisition_cost
  mixed_payment_allocation: captured_credit_ratio

alerts:
  sku_margin_below: "0.30"
  provider_cost_p95_over_budget: true
  unpriced_usage_count_above: 0
  orphan_hold_count_above: 0
```

## Claude Code Model Mapping And Cost Evidence

Claude Code 2.1.187 supports `ANTHROPIC_DEFAULT_FABLE_MODEL`, including custom Fable mappings for third-party providers. The variable remains configured; only the unnecessary context suffix is removed. The exact provider model IDs are:

```yaml
ANTHROPIC_MODEL: doubao-seed-evolving
ANTHROPIC_DEFAULT_OPUS_MODEL: doubao-seed-evolving
ANTHROPIC_DEFAULT_FABLE_MODEL: doubao-seed-evolving
ANTHROPIC_DEFAULT_SONNET_MODEL: doubao-seed-2-1-pro-260628
ANTHROPIC_DEFAULT_HAIKU_MODEL: doubao-seed-2-1-turbo-260628
```

`doubao-seed-evolving` already has a 1024k context window according to the provider model catalog. Provider model environment values therefore use the exact model ID and do not append `[1M]`.

Claude Code's `total_cost_usd` is ignored for both user billing and provider-cost accounting. A controlled test proved that Claude Code can price an unknown third-party model with Anthropic rates. Internal cost is recomputed from provider/model usage and the immutable cost-catalog version.

Cache-creation tokens incur the model's normal input-token rate. Cache storage is a separate token-hour cost. When the provider response does not expose the charged retention period, the cost ledger records a conservative one-hour `accrual_estimate` from the cost profile. Provider invoices later append the difference as an adjustment; neither event changes the user's fixed charge.

## Seedream Cost Measurement

Seedream 5.0 Pro does not return a monetary amount. Its image-generation response returns actual output dimensions and usage dimensions:

- `data[].size` gives actual `width x height`;
- `usage.generated_images` gives successful output count;
- `usage.input_images` gives input image count when available;
- `usage.output_tokens` and `usage.total_tokens` provide additional audit evidence.

The provider cost is:

```text
successful outputs at <= 2,360,000 pixels * CNY 0.30
+ successful outputs above 2,360,000 pixels * CNY 0.60
+ max(input image count - 1, 0) * CNY 0.02
```

The current 2K mapping produces more than 2.36 million pixels; for example, 2048 x 2048 is 4,194,304 pixels. Its provider output cost is therefore CNY 0.60, so a CNY 0.50 retail SKU would lose money before overhead.

The Volcengine provider must preserve response `data[].size`, `usage.generated_images`, `usage.output_tokens`, and `usage.total_tokens`. Input image count is also known from the validated request. If the provider omits output size, the server decodes the persisted image dimensions. If neither source yields trustworthy dimensions, the operation is unbillable, the deliverable is not captured, and the user hold is released.

## Data Model

### `billing_wallet_accounts`

One row per user containing paid, promotional, reserved, and available projections plus a monotonically increasing version for optimistic concurrency. Projections are rebuilt from ledger entries and cannot be edited independently.

### `billing_credit_lots`

Stores lot type, original/available/reserved/consumed/expired credits, cash received in fen, external reference, program ID, expiry, and immutable creation metadata. Paid lots do not expire. Promotional lots require an expiry and eligible-program reference.

### `billing_wallet_entries`

Append-only entries for paid top-up, promotion issue, reserve, capture, release, and expiry. Every entry has an idempotency key, account version, balance projection, and reference to its source entity. There is no generic grant or arbitrary balance-update entry.

### `billing_catalog_versions` And `billing_skus`

Store immutable published catalog snapshots. A SKU ID plus catalog version is unique. Published rows cannot be updated or deleted while referenced by a quote, hold, settlement, task, or report.

### `billing_quotes`

Stores the selected SKU snapshot, fixed price, limits, cost budget, request fingerprint, and expiry. Repeating the same idempotent request returns the existing quote.

### `billing_holds` And `billing_hold_allocations`

The hold stores quote, task, status, amount, deadline, and terminal transition. Allocations record exact credits reserved from each lot. Constraints prevent an allocation from exceeding lot availability and prevent both capture and release.

### `billing_provider_cost_events`

Stores the immutable provider usage and exact cost calculation. Provider request ID plus provider is unique. Adjustment events reference the original event and may be positive or negative.

### `billing_margin_fact_events`

Stores revenue-recognition, provider-cost, and acquisition-cost facts. Reporting projections are disposable and rebuildable from these events.

## API Contract

User APIs:

```text
GET  /api/v1/billing/wallet
GET  /api/v1/billing/transactions
POST /api/v1/billing/quotes
```

Task and media creation endpoints accept a quote ID and idempotency key. They never accept a client-calculated credit amount.

Admin API:

```text
POST /api/v1/admin/billing/topups
GET  /api/v1/admin/billing/costs
GET  /api/v1/admin/billing/margins
```

Top-up requests contain `user_id`, `cash_fen`, `external_reference`, and `idempotency_key`. Cost and margin APIs are read-only operational reporting surfaces.

## Studio Behavior

- Show paid, promotional, reserved, and available balances separately.
- Show promotional expiry without presenting promotions as cash.
- Show the exact fixed SKU price before task or media creation.
- Show included limits, not token or provider-price formulas.
- Display one pending hold while execution runs and one final capture or release.
- Remove daily sign-in, recharge packages with bonuses, membership price multipliers, dynamic runtime charges, and token-cost explanations.
- Referral UI explains that rewards arrive after the invitee's first qualifying recharge.
- Insufficient balance blocks creation before execution; running tasks never create negative balances.

## Strict Validation

Startup fails when any of the following is true:

- An enabled provider/model route has no active cost profile.
- A billable workflow or user-selectable option maps to zero or multiple active SKUs.
- A promotion references a missing, inactive, or ineligible SKU.
- A promotion's worst-case eligible-SKU consumption can exceed its per-recipient provider-cost budget.
- A SKU's maximum provider cost violates the configured minimum gross margin.
- An exact provider model ID, currency, unit, FX snapshot, or evidence reference is missing.
- A decimal cannot be represented exactly in micro-CNY.
- A SKU permits request dimensions outside the provider capability declaration.
- A deprecated billing key is present.

The removed keys include:

```text
billing.credits_per_cny
billing.tier_multipliers
billing.default_user_multiplier
user.billing_multiplier
credits.daily_sign_in
credits.register_bonus
credits.invite_reward
credits.task_costs
credits.model_costs
credits.agent_runtime_reserve
credits.goal_mode_multiplier
recharge_tiers.*.bonus_credits
```

Claude `total_cost_usd`, legacy agent-runtime deductions, sign-in endpoints, bonus/grant endpoints, and overdraft behavior are removed rather than retained as compatibility paths.

## Error Handling

- Insufficient credits returns `402 insufficient_credits` with fixed required and available amounts.
- Expired or changed quote returns `409 quote_expired`; the client must request a new quote.
- Duplicate idempotent operations return the existing result.
- Reused idempotency keys with a different payload return `409 idempotency_conflict`.
- Missing provider usage, unresolved output dimensions, or absent cost evidence fails the provider operation and releases the hold.
- Cost-budget exhaustion cancels remaining provider work, records the cost incurred so far, and releases the user hold.
- Durable-delivery verification failure releases the hold even when provider cost was incurred.
- Provider cost attached to a released hold is recorded as platform failure cost, never paid revenue or promotional acquisition cost.
- Provider invoice differences append adjustment events and never retroactively alter user charges.
- Reconciliation errors produce operator alerts and retain the nonterminal record for retry.

## Concurrency And Recovery

- Wallet mutation uses one database transaction with account-row locking and deterministic lot order.
- Quote, hold, capture, release, top-up, promotion, referral, and provider-cost events each require domain-specific idempotency keys.
- Task creation and hold creation are atomic.
- Durable task completion and capture use an outbox/finalizer boundary so process death cannot lose the settlement decision.
- Provider request IDs prevent duplicate cost events when response reporting is retried.
- Reconciliation is safe to run concurrently on multiple server instances through row leases and idempotent transitions.

## Cutover

This is a forward-only cutover:

1. Stop new task creation and drain or cancel all in-flight tasks.
2. Reconcile all legacy reserves, refunds, and terminal task states.
3. Export legacy users with positive balances for operator review.
4. Recreate only verified real-cash balances through the new top-up API using their actual cash references.
5. Do not import unidentified bonus, sign-in, multiplier-derived, or runtime-derived balances.
6. Deploy the new tables, strict configuration, APIs, and Studio together.
7. Remove legacy columns, transaction types, endpoints, configuration, and UI in the same release.

There is no dual-write period, fallback reader, or legacy balance adapter.

## Security And Audit

- The top-up endpoint requires the existing admin API-key boundary and must never log that key.
- External references and idempotency keys are indexed and unique within their domain.
- Admin reports require explicit administrative authorization.
- Raw provider payloads are sanitized for secrets and personal content before cost-ledger persistence.
- Every financial event records actor, source service, request ID, timestamp, and immutable correlation IDs.
- No API accepts balance-after, revenue, provider cost, or margin values supplied by the client.

## Testing Strategy

### Configuration

- Strict decoding rejects every removed key and unknown key.
- Every enabled route requires exactly one active cost profile.
- Every billable option resolves exactly one active SKU.
- Margin, promotion, currency, decimal, capability, and evidence validation fail closed.

### Wallet And Settlement

- Paid and promotional lots preserve all conservation invariants.
- FEFO promotional allocation and FIFO paid allocation are deterministic.
- Concurrent reserves cannot overspend one account or lot.
- Capture and release are mutually exclusive, idempotent, and crash recoverable.
- Automatic retries never change the hold amount or create another capture.
- Orphan reconciliation converges to one valid terminal state.

### Recharge And Promotions

- Cash fen deterministically produces credits and recognized lot value.
- Top-up idempotency rejects mismatched replay payloads.
- Onboarding is issued once and expires correctly.
- Referral is issued only after the first qualifying real top-up.
- Self-referral, duplicate invitee reward, and inviter-limit abuse are rejected.
- Promotional consumption recognizes zero revenue and records acquisition cost.
- Promotion configuration proves its per-recipient worst-case provider cost is bounded.

### Provider Cost

- Claude `total_cost_usd` never affects wallet or cost-ledger calculation.
- Token categories use the exact provider/model cost profile and FX snapshot.
- Seedream uses actual output pixels, successful output count, and reference count.
- A 2048 x 2048 Seedream output costs CNY 0.60 before additional references.
- Task-included provider and media operations consume only the parent task budget and never create a second user charge.
- GPT Image usage requires provider usage and records exact token categories.
- Duplicate provider response reporting creates one cost event.
- Invoice reconciliation appends adjustment events without modifying captures.

### Product And Delivery

- Quote snapshots remain valid after a newer catalog is published.
- Cost-budget exhaustion aborts execution and releases the hold.
- Durable delivery captures once; failed or missing delivery releases fully.
- Goal mode and unmapped variants are unavailable rather than dynamically multiplied.
- Studio never displays a provider-derived or token-derived user charge.

### Verification

Run targeted configuration, repository, service, handler, MCP, Agent, and provider tests, then:

```bash
go test ./...
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
cd studio && bun run test
cd studio && bun run build
```

## Acceptance Criteria

- A user can know the complete fixed charge before starting any billable action.
- No execution outcome can create a second or larger user charge.
- No wallet can become negative.
- Failed, cancelled, timed-out, unbillable, or budget-exhausted work releases the hold fully.
- Every enabled provider call has immutable usage, price evidence, exact cost, and reconciliation identity.
- Paid revenue, provider cost, promotional acquisition cost, profit, and margin are reportable by all required dimensions.
- Seedream costs use actual output pixels and reference count; the API is not expected to return money.
- `ANTHROPIC_DEFAULT_FABLE_MODEL` remains supported and maps to the exact Evolving provider ID.
- Old sign-in, bonus, multiplier, dynamic runtime, overdraft, and compatibility paths no longer exist.
- Strict startup validation prevents unpriced routes and unmapped billable actions from entering production.
