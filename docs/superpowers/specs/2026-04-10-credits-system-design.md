# Credits System Design

## Overview

Add a credits-based billing system to anbanwriter. Users earn free credits via daily sign-in (1024/day), spend credits to generate content, and can purchase additional credits through customer service (WeCom). Credits are deducted at task creation and refunded on failure.

## Pricing Table

| Operation | Credits |
|-----------|---------|
| Article task (article) | 500 |
| Seednote task (seednote) | 400 |
| Per image (1024px) | 50 |
| Per image (2K) | 80 |
| Per image (4K) | 150 |
| Daily sign-in | +1024 |
| Admin grant | Custom amount |

**Examples:**
- 1 article + 1 cover (2K) + 3 content images (1024px) = 500 + 80 + 150 = 730 credits
- 1 seednote + 6 images (2K) = 400 + 480 = 880 credits
- Daily sign-in (1024) covers roughly 1 article task or 1 seednote task with images

Pricing is configurable via `server/config.yaml` under the `credits` section.

## Data Model

### User Model Changes

Add to `server/model/user.go`:

```
CreditsBalance int `gorm:"default:0" json:"credits_balance"`
```

### New Model: CreditTransaction

File: `server/model/credit_transaction.go`

```
CreditTransaction struct {
    ID           uint       `gorm:"primaryKey;autoIncrement"`
    UserID       string     `gorm:"type:char(36);index;not null"`
    Type         string     `gorm:"type:varchar(20);not null"` // sign_in, task_deduct, task_refund, admin_grant
    Amount       int        `gorm:"not null"`                  // positive=income, negative=expense
    BalanceAfter int        `gorm:"not null"`                  // balance after this transaction
    TaskID       *string    `gorm:"type:char(36);index"`       // nullable, linked task
    Description  string     `gorm:"type:varchar(500)"`
    CreatedAt    time.Time  `gorm:"index"`
}
```

### Config Section

Add to `server/config/config.go`:

```yaml
credits:
  daily_sign_in: 1024
  task_costs:
    article: 500
    seednote: 400
  image_costs:
    "1024": 50
    "2k": 80
    "4k": 150
```

Mapped to Go struct with `map[string]int` for task_costs and image_costs.

## Core Business Flows

### 1. Daily Sign-In

- **API**: `POST /api/v1/credits/sign-in`
- **Logic**:
  1. Query `CreditTransaction` where `type=sign_in AND user_id=X AND DATE(created_at)=TODAY`
  2. If found, return error "already signed in today"
  3. In transaction: add 1024 to `user.credits_balance`, create `CreditTransaction` with `type=sign_in`, `amount=1024`
- **Response**: new balance, sign-in status

### 2. Task Creation with Credits Deduction

In `server/service/task.go` — `CreateManual()`:

1. Receive `quantity` parameter (1-5, default 1)
2. Look up base cost from config by task type (channel.Platform)
3. **Image cost**: Deducted as a flat per-task image fee based on the task type's typical image count and resolution, configured as `image_fee` per task type. This is NOT per-actual-image (since image count is determined at execution time). The fixed image fee approximates typical usage.
   - Alternatively: charge only the base task fee at creation, then deduct actual image costs after execution (additional deduction on completion).
   - **Decision**: Charge base task fee only at creation. Image costs are NOT pre-deducted — they are included in the base fee as a bundled estimate.
4. Per-set cost = base_cost (includes estimated image generation)
5. Total cost = per_set_cost * quantity
6. Check `user.credits_balance >= total_cost`
7. In DB transaction:
   - Deduct `total_cost` from `user.credits_balance`
   - Create `CreditTransaction` with `type=task_deduct`, `amount=-total_cost`
   - Create `quantity` Task records (each linked to the transaction via task_id)
8. Return tasks with remaining balance

### 3. Task Failure Refund

In `server/service/task_execution.go` — failure handling (`HandleExecutionFailure()`):

1. When task status transitions to `failed` (after max retries exhausted):
   - Query original deduction transaction by `task_id`
   - In transaction: add back the deducted amount to `user.credits_balance`
   - Create `CreditTransaction` with `type=task_refund`, `amount=<original_deduction>`, `task_id=<task_id>`
2. Note: if `quantity > 1`, each task fails independently and gets its own refund

### 4. Admin Credit Grant

- **API**: `POST /api/v1/admin/credits/grant`
- **Auth**: Admin API key (configured in server config, not user JWT)
- **Request body**: `{ "user_id": "...", "amount": 5000, "description": "WeCom purchase - order #123" }`
- **Logic**:
  1. Validate user exists
  2. In transaction: add amount to `user.credits_balance`
  3. Create `CreditTransaction` with `type=admin_grant`, `amount=<amount>`

### 5. Query APIs

| API | Description |
|-----|-------------|
| `GET /api/v1/credits/balance` | Returns current credits_balance |
| `GET /api/v1/credits/transactions?page=1&page_size=20` | Paginated transaction history |
| `GET /api/v1/credits/sign-in/status` | Returns `{ signed_in_today: bool }` |
| `POST /api/v1/credits/sign-in` | Execute sign-in, returns new balance |
| `POST /api/v1/admin/credits/grant` | Admin grants credits (API key auth) |

### 6. Quantity (Sets) Parameter

- Frontend task creation form adds a quantity selector (1-5, default 1)
- Each quantity unit creates a separate Task record
- Each task is independently billed and executed
- The create endpoint accepts `quantity` in the request body
- If quantity=3 and user has enough for 2 but not 3, reject entirely (all-or-nothing)

## Frontend Changes

### Dashboard Page (`studio/src/pages/DashboardPage.tsx`)

Add credits card to stats row:
- Display current `credits_balance`
- Sign-in button: "签到 +1024" (disabled with "已签到" if already signed in today)
- Fetch balance and sign-in status on page load

### Task Creation Form (`studio/src/pages/TasksPage.tsx`)

In the "New Task" dialog:
- Add quantity selector (number input or button group: 1/2/3/4/5, default 1)
- Add estimated cost display: "预估消耗: {base_cost} + {images} × {quantity} = {total} 积分"
- Show remaining balance after creation: "余额: {balance} → {balance - total}"
- Disable submit button when balance insufficient, show "积分不足" tooltip

### Credits Page (`studio/src/pages/CreditsPage.tsx`)

New page accessible from sidebar:
- Balance display with sign-in button
- Recharge info section: WeCom customer service QR code / contact info
- Transaction history table: type badge, amount (green for income, red for expense), balance after, description, timestamp
- Pagination

### Navigation

Add "积分" entry to sidebar navigation.

## API Route Summary

```
# User-facing (JWT auth)
GET    /api/v1/credits/balance
GET    /api/v1/credits/transactions
GET    /api/v1/credits/sign-in/status
POST   /api/v1/credits/sign-in

# Admin (API key auth)
POST   /api/v1/admin/credits/grant
```

## Migration Strategy

1. `AutoMigrate` adds `credits_balance` column to `users` table (default 0)
2. `AutoMigrate` creates `credit_transactions` table
3. No data migration needed — existing users start with 0 balance, can sign in to earn credits

## Error Handling

| Scenario | Response |
|----------|----------|
| Insufficient balance | 402 with `{ "error": "insufficient_credits", "required": N, "current": M }` |
| Already signed in today | 409 with `{ "error": "already_signed_in" }` |
| Invalid quantity (0, >5, negative) | 400 with validation error |
| Admin grant to non-existent user | 404 |

## Out of Scope

- WeCom payment API integration (manual admin process)
- Credit expiration (credits never expire)
- Referral bonuses
- Tiered pricing / subscription plans
- Credit gifting between users
