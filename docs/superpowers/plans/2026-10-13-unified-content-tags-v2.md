# Unified Content Tags v2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Complete the platform-neutral Taxonomy v2 content tagging contract for Server, Agent Packs, hooks, and Studio.

**Architecture:** Server validates and persists evidence-backed semantic tags, derives service-owned dimensions, and exposes only formal v2 tags to analytics. Pack manifests register tagging profiles and required artifacts; the shared content-tagging Skill produces evidence-backed `output/content-tags.json`. Studio groups formal tags and shows evidence state.

**Tech Stack:** Go/Fiber/GORM, YAML Agent Packs, Claude/Codex generated plugin assets, React/TypeScript/Vitest.

**Spec:** User-provided “通用内容标签体系：统一协议、类型扩展、证据治理”.

## Global Constraints

- Taxonomy v2 only; no legacy reads, fallback, or compatibility branches.
- Every content Pack declares tagging; analysis/profile/feedback tasks are not content subjects.
- Agent semantic tags require line-addressable evidence; unknown values stay diagnostic and never enter analytics.
- Server-owned content_type, platform, artifact_role, media_form, and performance cannot be overridden by Agent.
- Public platform tags remain outside `content-tags.json`.
- Run targeted tests plus full Go and Studio verification before completion.

### Task 1: Server taxonomy and persistence

Remove legacy dimensions, complete controlled vocabulary and constraints, preserve missing tagging state, enforce strict analytics filtering, and add tests for ownership/evidence/unknown values/limits.

### Task 2: Pack tagging contract and generated plugin assets

Add `TaggingSpec` to Agent Pack manifests and validation, declare tagging for all content Packs, keep analysis/profile/feedback Packs excluded, update generated Claude/Codex/DSH outputs and plugin versions.

### Task 3: Tagging Skill and completion hook acceptance

Strengthen shared content-tagging Skill and completion metadata behavior; add golden examples/tests for evidence, omissions, and accidental keyword cases.

### Task 4: Studio analytics contract

Ensure v2 formal tags are grouped consistently for article and seednote, expose evidence state, isolate candidate/diagnostic labels, and render an explicit empty-evidence state.

### Task 5: Verification and cleanup

Run targeted and full checks, regenerate catalogs/presets, inspect diff, and fix regressions without restoring v1 behavior.
