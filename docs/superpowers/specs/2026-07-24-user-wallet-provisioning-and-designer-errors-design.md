# User Wallet Provisioning And Designer Billing Errors

**Date:** 2026-07-24

## Goal

Make a billing wallet a required part of every newly created user and prevent
Studio from exposing backend billing identifiers such as
`billing_resource_not_found` to users.

This is a forward-only change. It does not backfill or otherwise support users
created before the new provisioning contract.

## Current Failure

Designer obtains a fixed-SKU quote and then charges the standalone image
operation while creating an image generation record. The charge transaction
locks `billing_wallet_accounts` directly. Authentication flows create `users`
without creating the corresponding wallet row, so the lock can return
`gorm.ErrRecordNotFound`. The shared billing handler currently maps that
persistence error to `billing_resource_not_found`, and Studio displays the
machine identifier in a toast.

## User Provisioning Contract

Introduce one application-service operation for new-user provisioning. Every
authentication flow that creates a user must call this operation instead of
writing through `UserRepository` directly.

The operation runs one repository transaction that:

1. creates the user;
2. creates an empty `BillingWalletAccount` for the same user ID;
3. applies the inviter count update when registration uses an invitation.

Any failure rolls back all three effects. Duplicate-user recovery remains in
the authentication flow, but only a successfully committed provisioning
transaction may be treated as a newly created user.

The provisioning service owns this orchestration because it crosses user,
billing, and invitation repositories. HTTP handlers remain responsible only
for request validation, authentication-provider exchange, and mapping service
results to responses.

## Billing Error Contract

Billing services must translate repository lookup failures at their boundary.
A missing wallet after successful user authentication violates the provisioning
invariant and is an internal ledger failure, not a public resource-not-found
condition.

The existing public billing codes remain authoritative for expected outcomes:

- `40203`: insufficient credits for a standalone operation;
- `40401`: the configured SKU cannot be resolved;
- `50001`: the persisted billing ledger violates its invariant;
- `50000`: an otherwise unclassified internal billing failure.

Detailed repository and billing errors stay in server logs. API responses keep
stable numeric codes and machine-readable message identifiers; clients must not
render those identifiers directly.

## Studio Error Presentation

The shared HTTP error utility must classify API errors by numeric response code
before considering `msg` or `error` text. It returns localized, user-facing
messages for known billing outcomes and a neutral fallback for internal billing
failures.

For Designer specifically:

- insufficient standalone balance is shown as
  `积分余额不足，充值后即可继续生成`;
- the error toast provides an action that navigates to `/billing`;
- internal billing failures are shown as
  `图片服务暂时不可用，请稍后重试`;
- machine identifiers such as `billing_resource_not_found` are never visible.

Other Studio surfaces using the shared error utility receive appropriate common
billing messages without Designer-specific navigation behavior.

## Designer Affordability Preview

Designer loads the current wallet alongside its provider list. The selected
provider already supplies the fixed credit price. The prompt area displays the
price and current available balance in its existing operational control area.

When the available balance is lower than the selected provider price, the
generate command is disabled and its visible state explains that credits are
insufficient. The user can navigate to `/billing` from that state. This check is
only an interaction aid: `/designer/generate` remains the authoritative atomic
admission and charge boundary because the balance can change concurrently.

Wallet-query failure must not be treated as a zero balance. Designer remains
submittable in that case and relies on the server response, while avoiding a
misleading insufficient-balance state.

## Testing

Backend tests must prove:

- every supported new-user creation path commits a user and an empty wallet;
- a wallet-creation or invitation-update failure rolls back the user;
- a missing wallet during standalone charging becomes a ledger/internal billing
  error rather than `billing_resource_not_found`;
- an existing zero-balance wallet returns the standalone insufficient-credit
  error.

Studio tests must prove:

- numeric billing codes map to localized text without exposing machine IDs;
- Designer disables generation only when a successfully loaded balance is below
  the selected fixed price;
- a wallet query failure does not disable generation;
- the insufficient-credit response offers navigation to `/billing`.

Run the relevant focused Go and Studio tests first, followed by `go test ./...`,
the server build, `bun run test`, and `bun run build`.

## Out Of Scope

- Backfilling wallets for existing users.
- Importing historical balances.
- Changing SKU prices, catalog publication, or provider routing.
- Retrying failed image-provider requests.
- Replacing the existing API response envelope.
