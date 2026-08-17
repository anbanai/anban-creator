# Task Resume Bootstrap Repair Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let resumed managed executions safely refresh Server-owned runtime settings and deliver execution-scoped supplemental instructions to Claude.

**Architecture:** Extend each bootstrap file with an explicit `replace_existing` ownership policy. The TypeScript materializer stages and prechecks every file, atomically replaces only flagged regular files with rollback, and retains immutable conflict behavior everywhere else. A shared prompt helper appends the same resume instructions for managed and local execution.

**Tech Stack:** Go 1.24, Fiber service contracts, TypeScript 5.9, Bun, Node.js filesystem APIs, Claude Agent SDK.

---

## File Map

- `server/service/agent_bootstrap.go`: add the bootstrap replacement flag, restrict it to runtime settings, and emit it for `settings.json`.
- `server/service/agent_bootstrap_test.go`: pin the Server ownership policy.
- `agent-ts/src/bootstrap.ts`: validate and type the new bootstrap file field and execution-scoped resume path.
- `agent-ts/test/bootstrap.test.ts`: cover accepted and rejected replacement flags and resume paths.
- `agent-ts/src/workspace.ts`: stage, precheck, atomically replace, and roll back managed files.
- `agent-ts/test/workspace.test.ts`: reproduce the production settings conflict and protect immutable and rollback behavior.
- `agent-ts/src/resume.ts`: own the shared continuation prompt text.
- `agent-ts/src/local.ts`: reuse the shared helper for legacy local resume files.
- `agent-ts/src/runner.ts`: append the managed execution's `resume_context_path` to the SDK prompt.
- `agent-ts/test/resume.test.ts`: test the pure prompt contract.
- `agent-ts/test/runner.test.ts`: prove managed prompt construction consumes the execution-scoped path.

## Task 1: Extend The Server Bootstrap Contract

**Files:**
- Modify: `server/service/agent_bootstrap.go`
- Modify: `server/service/agent_bootstrap_test.go`

- [ ] **Step 1: Write the failing Server ownership test**

In the existing bootstrap response test, add assertions after building the
`paths` map:

```go
settings := paths[".anban-creator/settings.json"]
if !settings.ReplaceExisting {
	t.Fatal("runtime settings must be replaceable across resumed executions")
}
for path, file := range paths {
	if path != ".anban-creator/settings.json" && file.ReplaceExisting {
		t.Fatalf("bootstrap path %q unexpectedly permits replacement", path)
	}
}
```

Add a validation case that constructs a replaceable non-settings path and
expects `ValidateBootstrapFiles` to reject it:

```go
files := []BootstrapFile{{
	Path: "attachments/input.txt", Text: "input", Mode: 0644,
	ReplaceExisting: true,
}}
if err := ValidateBootstrapFiles(files); err == nil {
	t.Fatal("replaceable non-settings bootstrap file accepted")
}
```

- [ ] **Step 2: Run the targeted Go tests and verify RED**

Run:

```bash
go test ./server/service -run 'TestAgentBootstrap|TestValidateBootstrapFiles' -count=1
```

Expected: compilation fails because `BootstrapFile.ReplaceExisting` does not
exist.

- [ ] **Step 3: Implement the explicit replacement policy**

Extend `BootstrapFile`:

```go
type BootstrapFile struct {
	Path            string `json:"path"`
	Text            string `json:"text,omitempty"`
	DownloadURL     string `json:"download_url,omitempty"`
	Mode            uint32 `json:"mode"`
	ExpectedSize    int64  `json:"expected_size,omitempty"`
	MaxBytes        int64  `json:"max_bytes,omitempty"`
	ReplaceExisting bool   `json:"replace_existing,omitempty"`
}
```

Mark only the settings payload:

```go
files = append(files, BootstrapFile{
	Path: ".anban-creator/settings.json", Text: string(settings), Mode: 0600,
	ReplaceExisting: true,
})
```

After normalizing each path in `ValidateBootstrapFiles`, reject replacement
for any other path:

```go
if file.ReplaceExisting && filepath.ToSlash(clean) != ".anban-creator/settings.json" {
	return fmt.Errorf("bootstrap file %q cannot replace existing workspace content", clean)
}
```

- [ ] **Step 4: Run the targeted Go tests and verify GREEN**

Run:

```bash
go test ./server/service -run 'TestAgentBootstrap|TestValidateBootstrapFiles' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit the Server contract**

```bash
git add server/service/agent_bootstrap.go server/service/agent_bootstrap_test.go
git commit -m "fix(agent): mark runtime settings replaceable"
```

## Task 2: Safely Replace Managed Bootstrap Files

**Files:**
- Modify: `agent-ts/src/bootstrap.ts`
- Modify: `agent-ts/src/workspace.ts`
- Modify: `agent-ts/test/bootstrap.test.ts`
- Modify: `agent-ts/test/workspace.test.ts`

- [ ] **Step 1: Write failing TypeScript contract tests**

Add bootstrap validation coverage:

```ts
test("validates the managed replacement flag", () => {
  const accepted = validResponse();
  accepted.files = [{ path: ".anban-creator/settings.json", text: "new", mode: 0o600, replace_existing: true }];
  expect(validateBootstrapResponse("execution-1", accepted).files?.[0]?.replace_existing).toBe(true);

  const rejected = validResponse();
  rejected.files = [{ path: ".anban-creator/settings.json", text: "new", mode: 0o600 }];
  (rejected.files[0] as unknown as Record<string, unknown>).replace_existing = "true";
  expect(() => validateBootstrapResponse("execution-1", rejected)).toThrow("replace_existing");
});
```

Add workspace behavior tests:

```ts
test("replaces a differing managed settings file", async () => {
  const root = await mkdtemp(join(tmpdir(), "anban-workspace-"));
  roots.push(root);
  await mkdir(join(root, ".anban-creator"));
  await writeFile(join(root, ".anban-creator", "settings.json"), "old", { mode: 0o600 });
  await materializeBootstrapFiles(root, [{
    path: ".anban-creator/settings.json", text: "new", mode: 0o600,
    replace_existing: true,
  }]);
  expect(await readFile(join(root, ".anban-creator", "settings.json"), "utf8")).toBe("new");
});

test("does not replace managed settings when another target conflicts", async () => {
  const root = await mkdtemp(join(tmpdir(), "anban-workspace-"));
  roots.push(root);
  await mkdir(join(root, ".anban-creator"));
  await writeFile(join(root, ".anban-creator", "settings.json"), "old", { mode: 0o600 });
  await writeFile(join(root, "immutable.txt"), "old");
  await expect(materializeBootstrapFiles(root, [
    { path: ".anban-creator/settings.json", text: "new", mode: 0o600, replace_existing: true },
    { path: "immutable.txt", text: "different", mode: 0o644 },
  ])).rejects.toThrow("conflicts");
  expect(await readFile(join(root, ".anban-creator", "settings.json"), "utf8")).toBe("old");
});
```

Keep the existing immutable conflict and symlink-parent tests unchanged.

- [ ] **Step 2: Run TypeScript tests and verify RED**

Run:

```bash
cd agent-ts && bun test test/bootstrap.test.ts test/workspace.test.ts
```

Expected: the validator rejects the unknown field and materialization reports
the existing settings conflict.

- [ ] **Step 3: Validate the new wire field**

Extend `BootstrapFile` with:

```ts
replace_existing?: boolean;
```

At the start of each `preflightBootstrapFiles` iteration, validate the runtime
shape:

```ts
if (!isRecord(file) || !hasOnlyKeys(file, [
  "path", "text", "download_url", "mode", "expected_size", "max_bytes", "replace_existing",
])) throw new Error("bootstrap file contains unknown fields");
if (file.replace_existing !== undefined && typeof file.replace_existing !== "boolean") {
  throw new Error("bootstrap file replace_existing must be boolean");
}
```

Also validate `resume_context_path` against the execution-scoped path when it
is present:

```ts
if (data.resume_context_path !== undefined) {
  const expected = `.anban-creator/resume/executions/${executionID}/latest.md`;
  if (data.resume_context_path !== expected) throw new Error("bootstrap resume context path is invalid");
}
```

- [ ] **Step 4: Implement staged replacement with rollback**

In `materializeBootstrapFiles`, create separate `incoming` and `backups`
directories under the private staging root. Build a commit list during the
existing all-file precheck:

```ts
type BootstrapCommit = {
  kind: "create" | "replace";
  target: string;
  staged: string;
  backup?: string;
};
```

For an existing regular file:

```ts
if (await filesEqual(staged, target)) continue;
if (!file.replace_existing) throw new Error(`bootstrap target conflicts with existing file: ${target}`);
commits.push({ kind: "replace", target, staged, backup: join(backups, String(commits.length)) });
```

For a missing target, append a `create` commit. Apply commits only after every
target passes precheck. A replacement commit performs:

```ts
await rename(commit.target, commit.backup!);
try {
  await rename(commit.staged, commit.target);
} catch (error) {
  await rename(commit.backup!, commit.target);
  throw error;
}
```

Track applied commits. On failure, process them in reverse: remove created
targets; for replacements remove the new target and rename the backup to the
original path. Successful completion lets the final staging cleanup remove all
backups.

- [ ] **Step 5: Run TypeScript tests and verify GREEN**

Run:

```bash
cd agent-ts && bun test test/bootstrap.test.ts test/workspace.test.ts
```

Expected: PASS.

- [ ] **Step 6: Commit the runtime materializer**

```bash
git add agent-ts/src/bootstrap.ts agent-ts/src/workspace.ts agent-ts/test/bootstrap.test.ts agent-ts/test/workspace.test.ts
git commit -m "fix(agent-ts): refresh managed bootstrap settings"
```

## Task 3: Deliver Supplemental Resume Instructions

**Files:**
- Create: `agent-ts/src/resume.ts`
- Create: `agent-ts/test/resume.test.ts`
- Modify: `agent-ts/src/local.ts`
- Modify: `agent-ts/src/runner.ts`
- Modify: `agent-ts/test/runner.test.ts`

- [ ] **Step 1: Write failing prompt tests**

Create `agent-ts/test/resume.test.ts`:

```ts
import { describe, expect, test } from "bun:test";
import { appendResumeContextToPrompt } from "../src/resume.js";

describe("appendResumeContextToPrompt", () => {
  test("keeps a non-resume prompt unchanged", () => {
    expect(appendResumeContextToPrompt("base")).toBe("base");
  });

  test("references the execution-scoped supplemental context", () => {
    const path = ".anban-creator/resume/executions/execution-1/latest.md";
    const prompt = appendResumeContextToPrompt("base", path);
    expect(prompt).toContain(`请先读取 \`${path}\``);
    expect(prompt).toContain("不要清空、删除或整体覆盖已有产物");
  });
});
```

Add a managed-runner case:

```ts
test("builds the managed prompt with resume context", () => {
  const data = {
    ...validBootstrap(),
    resume_session_id: "session-1",
    resume_context_path: ".anban-creator/resume/executions/execution-1/latest.md",
  };
  expect(runner.buildManagedPrompt(data)).toContain(data.resume_context_path);
  expect(runner.buildQueryOptions(data, "/workspace").resume).toBe("session-1");
});
```

- [ ] **Step 2: Run prompt tests and verify RED**

Run:

```bash
cd agent-ts && bun test test/resume.test.ts test/runner.test.ts test/local.test.ts
```

Expected: compilation fails because the shared helper and
`buildManagedPrompt` do not exist.

- [ ] **Step 3: Implement and wire the shared prompt helper**

Create `agent-ts/src/resume.ts`:

```ts
export function appendResumeContextToPrompt(prompt: string, relativePath?: string): string {
  if (!relativePath) return prompt;
  return `${prompt}\n\n继续执行模式：\n- 这是一个基于原任务工作目录的继续执行，不是全新任务。\n- 请先读取 \`${relativePath}\`，理解用户补充指令、补充文件说明和附件相对路径。\n- 基于当前工作目录已有草稿、素材和产物继续完成任务；不要清空、删除或整体覆盖已有产物，除非补充指令明确要求替换。\n- 如果补充文件中存在同名或相近用途文件，优先按 latest.md 中的文件说明区分使用。`;
}
```

In `local.ts`, retain the existing `existsSync` check but delegate prompt text
construction to the helper. In `runner.ts`, add:

```ts
export function buildManagedPrompt(data: Pick<BootstrapResponse, "prompt" | "resume_context_path">): string {
  return appendResumeContextToPrompt(data.prompt, data.resume_context_path);
}
```

Change the SDK query call to:

```ts
query({ prompt: buildManagedPrompt(data), options })
```

- [ ] **Step 4: Run prompt tests and verify GREEN**

Run:

```bash
cd agent-ts && bun test test/resume.test.ts test/runner.test.ts test/local.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit the prompt repair**

```bash
git add agent-ts/src/resume.ts agent-ts/src/local.ts agent-ts/src/runner.ts agent-ts/test/resume.test.ts agent-ts/test/runner.test.ts
git commit -m "fix(agent-ts): deliver resume context to Claude"
```

## Task 4: Full Verification

**Files:**
- Verify all modified files from Tasks 1-3.

- [ ] **Step 1: Run the Agent runtime suite**

```bash
cd agent-ts && bun run test && bun run typecheck && bun run build
```

Expected: all commands exit 0.

- [ ] **Step 2: Run Go tests and build**

```bash
go test ./...
go build -o /tmp/anban-creator-server ./server
```

Expected: both commands exit 0. If repository-owned contract tests reject
pre-existing untracked plugin roots, report that environmental blocker and
also run the targeted changed packages separately.

- [ ] **Step 3: Verify the diff**

```bash
git diff --check HEAD~3..HEAD
git status --short
```

Expected: no whitespace errors; only pre-existing unrelated untracked paths
may remain.

- [ ] **Step 4: Record deployment requirements**

Report that all three managed Agent images must be rebuilt and that ACS should
use an immutable digest or unique tag before validating Task Detail resume in
production.
