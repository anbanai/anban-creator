# Configurable Rate Limits Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Server's existing rate-limit tiers configurable through its canonical YAML configuration.

**Architecture:** Add a typed top-level `rate_limit` config with positive request counts and durations, populate secure defaults in `applyDefaults`, and validate it during config loading. Pass those values into the existing middleware at router construction. Include an explicit limiter bucket in Redis keys so layered policies on the same path remain independent; preserve the 429 response contract.

**Tech Stack:** Go, Fiber v3, YAML v3, Redis, existing config and router tests.

**Spec:** Approved conversation request: expose rate limits through configuration with a simple architecture-consistent implementation; the project is new and does not need legacy compatibility.

## Global Constraints

- The Server's YAML file remains the single visible configuration source; environment values are expanded only through `${...}` placeholders written in YAML.
- Preserve endpoint-specific limiter tiers and the 429 response behavior; namespace Redis keys by limiter bucket, IP, and request path.
- Keep rate limiting enabled with secure defaults; counts and windows must validate as positive values.

## Review Focus

- Omitted `rate_limit` section receives defaults for every tier.
- Explicit YAML values and `${ENV:-default}` values reach the router.
- Zero or negative request counts and windows fail configuration validation.
- The `send_code` limiter remains layered with the general auth limiter.
- Protected API, auth, send-code, and agent routes each use their own configured tier.
- Auth-prefix and protected API middleware can both match `/auth/me`; separate bucket names prevent their counters from overlapping.
- Admin Billing has an existing dedicated limiter and remains configurable at its current `10/min` default.

---

### Task 1: Add typed rate-limit configuration

**Files:**
- Modify: `server/config/config.go`
- Test: `server/config/config_phase2_test.go`

**Interfaces:**
- Produces: `Config.RateLimit RateLimitConfig`, with `API`, `Auth`, `SendCode`, `Agent`, and `Admin` fields. Each tier has `Requests int` and `Window time.Duration`.
- Defaults: API `300/1m`, auth `20/1m`, send-code `5/10m`, agent `300/1m`, admin `10/1m`.

- [x] **Step 1: Write failing tests** for omitted-section defaults, explicit YAML/env-expanded values, and rejection of non-positive counts/windows.
- [x] **Step 2: Run** `cd server && go test ./config -run 'Test.*RateLimit' -count=1`; confirm the tests fail because the configuration is not implemented.
- [x] **Step 3: Implement** the typed config, defaults, and positive-value validation; register `rate_limit` as a supported top-level key.
- [x] **Step 4: Run** the targeted config tests and confirm they pass.

### Task 2: Wire configured tiers into the router

**Files:**
- Modify: `server/router/router.go`
- Test: `server/router/router_test.go`
- Modify: `server/config.example.yaml`

**Interfaces:**
- Consumes: `Config.RateLimit` and its four tier values from Task 1.

- [x] **Step 1: Write a failing router test** that configures low per-tier thresholds and verifies the protected API, auth, send-code, and agent limits independently through response headers/429 behavior.
- [x] **Step 2: Run** `cd server && go test ./router -run 'Test.*RateLimit' -count=1`; confirm the current hard-coded values make it fail.
- [x] **Step 3: Replace** hard-coded router values with the corresponding config tiers and document the defaults in `config.example.yaml` using explicit environment placeholders.
- [x] **Step 4: Run** the focused router test, `cd server && go test ./config ./router`, then `cd server && go test ./...` and `cd server && go build -o /tmp/anban-creator-server .`.

> Note: the focused tests and build pass. The full suite is currently blocked by
> this sparse worktree's missing `harness/` submodule files; unrelated contract
> tests fail when they try to read those files.
