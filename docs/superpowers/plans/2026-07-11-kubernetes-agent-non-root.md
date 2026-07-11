# Kubernetes Agent Non-Root Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Run managed Kubernetes agent containers as the existing non-root `node` user so Claude Code can use the configured permission mode.

**Architecture:** Preserve the root-only workspace initializer, then assign UID 1000 only to the main agent container through its Kubernetes `SecurityContext`. This mirrors both local Docker execution paths without changing the shared runner or Studio UI.

**Tech Stack:** Go 1.26, Kubernetes core/v1 Pod API, standard Go testing

---

### Task 1: Enforce the non-root Kubernetes agent contract

**Files:**
- Modify: `server/agent/kubernetes_executor_test.go`
- Modify: `server/agent/kubernetes_executor.go`

- [ ] **Step 1: Write the failing pod-spec assertion**

Add this assertion to `TestKubernetesPodSpecUsesConfiguredImageAndPVC` after verifying the main container exists:

```go
agentContainer := pod.Spec.Containers[0]
if agentContainer.SecurityContext == nil || agentContainer.SecurityContext.RunAsUser == nil || *agentContainer.SecurityContext.RunAsUser != 1000 {
	t.Fatalf("agent container security context = %#v, want node user 1000", agentContainer.SecurityContext)
}
```

- [ ] **Step 2: Run the targeted test and verify RED**

Run: `go test ./server/agent -run TestKubernetesPodSpecUsesConfiguredImageAndPVC -count=1`

Expected: FAIL because the main container security context is nil.

- [ ] **Step 3: Apply the minimal pod configuration**

In `buildAgentPod`, define the existing non-root UID and attach it only to the main container:

```go
nodeUser := int64(1000)
```

```go
SecurityContext: &corev1.SecurityContext{RunAsUser: &nodeUser},
```

Keep `rootUser := int64(0)` and the init container security context unchanged.

- [ ] **Step 4: Run the targeted test and verify GREEN**

Run: `go test ./server/agent -run 'TestKubernetesPodSpecUsesConfiguredImageAndPVC|TestKubernetesWorkspaceInitRunsAsRootAndChownsWorkdir' -count=1`

Expected: PASS, proving the agent runs as UID 1000 while workspace initialization remains root.

- [ ] **Step 5: Run surface and repository verification**

Run:

```bash
go test ./server/agent
go test ./...
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
```

Expected: all commands exit successfully.

- [ ] **Step 6: Commit the implementation**

```bash
git add server/agent/kubernetes_executor.go server/agent/kubernetes_executor_test.go docs/superpowers/plans/2026-07-11-kubernetes-agent-non-root.md
git commit -m "fix(agent): run kubernetes agents as node"
```
