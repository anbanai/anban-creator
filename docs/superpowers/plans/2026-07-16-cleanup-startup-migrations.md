# Startup Migration Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove completed or one-time schema migrations from server startup and provide explicit MySQL SQL for the two schema changes introduced on 2026-07-15 that did not complete.

**Architecture:** Keep GORM `AutoMigrate` for additive model synchronization, but remove all custom historical migration calls from `model.AutoMigrate`. Store the pending destructive/data migration as a reviewed one-time SQL file under the existing `server/migrations` directory.

**Tech Stack:** Go, GORM, MySQL 8, Go tests.

---

### Task 1: Define the manual SQL contract

**Files:**
- Create: `server/migrations/migrations_test.go`
- Create: `server/migrations/20260716_pending_startup_migrations.sql`
- Delete: `server/migrations/20260629_author_writer.sql`
- Delete: `server/migrations/20260706_credit_transactions_metadata.sql`
- Delete: `server/migrations/20260706_user_billing_multiplier.sql`

- [ ] Add a failing test that requires the SQL file to use `DROP CHECK` for both artifact state constraints, delete duplicate agent feedback rows while retaining the latest row, and create `idx_agent_feedback_task_agent` as a unique composite index.
- [ ] Run `go test ./server/migrations -count=1` and confirm it fails because the SQL file is missing.
- [ ] Add the one-time MySQL SQL and rerun the package test until it passes.
- [ ] Delete previously executed SQL files so the directory contains only pending work.

### Task 2: Remove historical startup migrations

**Files:**
- Modify: `server/model/model.go`
- Delete: `server/model/task_file_migrate.go`
- Delete: `server/model/agent_feedback_migrate.go`
- Delete: `server/model/agent_feedback_migrate_test.go`
- Delete: `server/model/task_workspace_migrate.go`
- Modify: `server/model/task_file_execution_test.go`
- Modify: `server/model/task_workspace_test.go`

- [ ] Remove all four custom migration calls from `AutoMigrate`.
- [ ] Delete migration-only implementations and tests.
- [ ] Preserve fresh-schema tests for execution-scoped task-file uniqueness, collected artifact states, agent-feedback uniqueness, and absence of `tasks.cleaned_up_at`.
- [ ] Run `go test ./server/model -count=1` and fix only cleanup-related failures.

### Task 3: Verify the server surface

**Files:**
- Verify only.

- [ ] Run `gofmt` on changed Go files.
- [ ] Run `go test ./...`.
- [ ] Run `go build -o /tmp/anban-creator-server ./server`.
- [ ] Review `git diff` and confirm unrelated work remains untouched.
