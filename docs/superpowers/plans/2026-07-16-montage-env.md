# Montage Environment Configuration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `provider_env` with unrestricted `env` throughout the Montage runtime contract.

**Architecture:** Keep environment selection explicit in `montage.env`, but treat keys as upstream-owned opaque names. Preserve the existing Montage-only injection boundary and redact all values outside the task process.

**Tech Stack:** Go 1.x, YAML v3, Go tests, Claude Code/OpenClaw/Codex plugin assets

---

### Task 1: Server Configuration Contract

**Files:**
- Modify: `server/config/montage_test.go`
- Modify: `server/config/config.go`
- Modify: `server/config.yaml`
- Modify: `server/config.example.yaml`

- [ ] **Step 1: Write failing configuration tests**

Rename test fixtures and assertions to `Env`, add `NEW_PROVIDER_TOKEN`, and assert `Validate()` accepts it. Add YAML decoding coverage for `montage.env` and ensure defaults initialize an empty map.

- [ ] **Step 2: Verify the tests fail**

Run: `go test ./server/config -run 'TestMontage.*Env' -count=1`

Expected: FAIL because `MontageConfig.Env` and `RedactedEnv` do not exist and the unrestricted key is rejected.

- [ ] **Step 3: Implement the server contract**

Rename `ProviderEnv` to `Env` with `yaml:"env"`, rename redaction to `RedactedEnv`, and remove `IsSupportedMontageProviderEnv` plus its allowlist. Update both YAML examples to use `env` and document arbitrary upstream-owned keys.

- [ ] **Step 4: Verify server configuration tests pass**

Run: `go test ./server/config -run 'TestMontage' -count=1`

Expected: PASS.

### Task 2: Execution and MCP Propagation

**Files:**
- Modify: `server/service/task_test.go`
- Modify: `server/service/task_execution.go`
- Modify: `server/agent/montage_contract_test.go`
- Modify: `server/agent/docker_executor_naming_test.go`
- Modify: `server/agent/executor.go`
- Modify: `server/agent/docker_executor.go`
- Modify: `server/service/agent_bootstrap.go`
- Modify: `server/service/agent_bootstrap_test.go`
- Modify: `server/mcp/tools_test.go`
- Modify: `server/mcp/tools.go`

- [ ] **Step 1: Write failing propagation tests**

Use `NEW_PROVIDER_TOKEN` in service, executor, Docker, Kubernetes bootstrap, and MCP fixtures. Rename expected fields from `provider_env` to `env`, and assert MCP JSON contains configured booleans but never secret values.

- [ ] **Step 2: Verify the propagation tests fail**

Run: `go test ./server/service ./server/agent ./server/mcp -run 'Montage.*Env|MontageProfile' -count=1`

Expected: FAIL because execution options and MCP still use `ProviderEnv` and filter unknown keys.

- [ ] **Step 3: Implement unrestricted propagation**

Rename execution fields and helpers to `MontageEnv` and `montageEnvForTask`. Copy every non-empty configured entry for Montage tasks and remove supported-key filtering. Add an `env` field to the authenticated Kubernetes bootstrap response, populated only for Montage tasks. Return redacted data under MCP key `env`.

- [ ] **Step 4: Verify propagation tests pass**

Run: `go test ./server/service ./server/agent ./server/mcp -run 'Montage' -count=1`

Expected: PASS.

### Task 3: Standalone Agent Contract

**Files:**
- Modify: `agent/config_test.go`
- Modify: `agent/config.go`
- Modify: `agent/runner.go`

- [ ] **Step 1: Write a failing future-key test**

Put `NEW_PROVIDER_TOKEN` in a Montage bootstrap response and assert `jobRuntimeConfig` retains it. Assert the same map is omitted when the response task type is article, and assert the runner passes the explicit config map to the Claude subprocess.

- [ ] **Step 2: Verify the standalone-agent test fails**

Run: `go test ./agent -run 'Test.*MontageEnv' -count=1`

Expected: FAIL because bootstrap and runtime config do not carry an explicit Montage environment.

- [ ] **Step 3: Implement generic process forwarding**

Add `Env` to the bootstrap response and agent config, copy it only for Montage jobs, and pass the explicit map through the SDK options. Delete process-environment scanning and its fixed allowlist.

- [ ] **Step 4: Verify standalone-agent tests pass**

Run: `go test ./agent -run 'Test.*MontageEnv' -count=1`

Expected: PASS.

### Task 4: Plugin Distribution Contract

**Files:**
- Modify: `claudecode/agents/montage.md`
- Modify: `claudecode/skills/montage/SKILL.md`
- Modify: `claudecode/.claude-plugin/plugin.json`
- Modify: `openclaw/skills/montage/SKILL.md`
- Modify: `openclaw/openclaw.plugin.json`
- Modify: `codex/agents/montage.toml`
- Modify: `codex/skills/montage/SKILL.md`
- Modify: `codex/.codex-plugin/plugin.json`

- [ ] **Step 1: Update contract tests to reject legacy names**

Extend the relevant MCP/plugin parity assertions so `env_keys` is required and `provider_env`/`provider_env_keys` are absent.

- [ ] **Step 2: Verify distribution tests fail**

Run: `go test ./server/mcp ./server/agent -run 'Montage|Plugin' -count=1`

Expected: FAIL while plugin files still reference `provider_env_keys`.

- [ ] **Step 3: Update all plugin assets**

Rename manifest fields to `env_keys`, preserve the instruction never to write secret values, and patch-bump all three affected plugin manifests.

- [ ] **Step 4: Verify targeted contract tests pass**

Run: `go test ./server/mcp ./server/agent -run 'Montage|Plugin' -count=1`

Expected: PASS.

### Task 5: Repository Verification

**Files:**
- Verify all modified files

- [ ] **Step 1: Format Go files**

Run: `gofmt -w agent/bootstrap.go agent/bootstrap_test.go agent/config.go agent/config_test.go agent/job.go agent/job_test.go agent/runner.go server/config/config.go server/config/montage_test.go server/service/agent_bootstrap.go server/service/agent_bootstrap_test.go server/service/task_execution.go server/service/task_test.go server/agent/executor.go server/agent/docker_executor.go server/agent/montage_contract_test.go server/agent/docker_executor_naming_test.go server/mcp/tools.go server/mcp/tools_test.go`

- [ ] **Step 2: Prove the legacy contract is gone**

Run: `rg -n 'provider_env|ProviderEnv|providerEnv|supportedMontageProviderEnv|IsSupportedMontageProviderEnv' agent server claudecode openclaw codex`

Expected: no matches.

- [ ] **Step 3: Run full Go verification**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 4: Build both Go binaries**

Run: `go build -o /tmp/anban-creator-server ./server`

Run: `go build -o /tmp/anban ./agent`

Expected: both commands exit 0.
