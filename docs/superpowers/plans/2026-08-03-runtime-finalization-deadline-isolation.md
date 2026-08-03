# Runtime Finalization Deadline Isolation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give artifact collection and terminal completion independent deadlines so a finished Agent run cannot lose `/agent/complete`, and expose an unacknowledged terminal callback as `completion_report_failed`.

**Architecture:** Keep the existing final hook and artifact manifest protocol. TypeScript and Go each use a cancellable 120-second artifact phase followed by a fresh 20-second completion phase; exit code 2 is the only new cross-process contract, mapped by the existing runtime reconciler. Kubernetes stops deriving normal finalization from Pod grace, while Docker keeps its current one-shot lifecycle.

**Tech Stack:** TypeScript 5.9, Bun, Node.js 22 streams and AbortSignal, Go contexts, Fiber Server services, Docker Engine API, Kubernetes batch/v1 Jobs.

---

## File Map

- `agent-ts/src/main.ts`: parse phase timeouts, sequence independent finalization phases, preserve completion errors, and emit exit code 2.
- `agent-ts/src/artifacts.ts`: propagate cancellation through scan, hash, signed upload, and OSS upload; return the collected file count.
- `agent-ts/test/main.test.ts`: prove timeout parsing, independent phase deadlines, and completion exit classification.
- `agent-ts/test/artifacts.test.ts`: prove pre-cancelled scans and uploads stop without manifest submission.
- `agent/main.go`: replace the shared finalization window, separate shutdown from caller cancellation, and return a typed completion-report error.
- `agent/job.go`: preserve completion-report failure when bootstrap can authenticate but cannot finish.
- `agent/job_test.go`: prove independent Go phase contexts, signal cancellation, and bootstrap completion handling.
- `agent/artifact_upload_test.go`: adapt the existing slow-hash test to the independent budgets.
- `agent/main_test.go`: prove completion-report errors map to process exit code 2.
- `server/agent/kubernetes_job.go`: remove shared timeout injection and its grace-derived calculation.
- `server/agent/kubernetes_executor_test.go`: lock the smaller environment contract and unchanged 30-second Pod grace.
- `server/agent/runtime_reconciler.go`: classify runtime exit code 2 as `completion_report_failed`.
- `server/agent/runtime_reconciler_test.go`: prove the new classification without changing other reasons.
- `server/config.yaml`: clarify that `completion_grace_seconds` is termination/reconciliation grace.
- `server/config.example.yaml`: mirror the production configuration comment.

## Task 1: Isolate TypeScript Runtime Phases

**Files:**
- Modify: `agent-ts/src/main.ts`
- Modify: `agent-ts/src/artifacts.ts`
- Test: `agent-ts/test/main.test.ts`
- Test: `agent-ts/test/artifacts.test.ts`

- [ ] **Step 1: Replace the shared-deadline test with failing timeout and exit-code tests**

Replace `agent-ts/test/main.test.ts` with tests for the public contract that the implementation will expose:

```ts
import { describe, expect, test } from "bun:test";

import {
  CompletionReportError,
  exitCodeForError,
  finalizationTimeouts,
} from "../src/main.js";

describe("finalizationTimeouts", () => {
  test("uses independent defaults", () => {
    expect(finalizationTimeouts({})).toEqual({ artifact: 120_000, completion: 20_000 });
  });

  test("parses independent overrides and rejects invalid values", () => {
    expect(finalizationTimeouts({
      ANBAN_JOB_ARTIFACT_TIMEOUT: "45s",
      ANBAN_JOB_COMPLETION_TIMEOUT: "750ms",
    })).toEqual({ artifact: 45_000, completion: 750 });
    expect(finalizationTimeouts({
      ANBAN_JOB_ARTIFACT_TIMEOUT: "6m",
      ANBAN_JOB_COMPLETION_TIMEOUT: "0s",
    })).toEqual({ artifact: 120_000, completion: 20_000 });
  });
});

describe("exitCodeForError", () => {
  test("reserves exit code two for an unacknowledged completion", () => {
    expect(exitCodeForError(new CompletionReportError(new Error("server unavailable")))).toBe(2);
    expect(exitCodeForError(new Error("bootstrap failed"))).toBe(1);
  });
});
```

- [ ] **Step 2: Run the TypeScript contract test and confirm it fails**

Run:

```bash
cd agent-ts && bun test test/main.test.ts
```

Expected: FAIL because `CompletionReportError`, `exitCodeForError`, and the new `finalizationTimeouts` contract do not exist.

- [ ] **Step 3: Implement timeout parsing and completion error classification**

In `agent-ts/src/main.ts`, replace `FINALIZATION_TIMEOUT_MS`, `COMPLETION_RESERVE_MS`, `finalizationDeadlines`, and `finalizationTimeout` with:

```ts
const ARTIFACT_TIMEOUT_MS = 120_000;
const COMPLETION_TIMEOUT_MS = 20_000;
const MAX_ARTIFACT_TIMEOUT_MS = 300_000;
const MAX_COMPLETION_TIMEOUT_MS = 60_000;

export interface FinalizationTimeouts {
  artifact: number;
  completion: number;
}

export class CompletionReportError extends Error {
  constructor(cause: Error) {
    super(`failed to report completion: ${cause.message}`, { cause });
    this.name = "CompletionReportError";
  }
}

export function exitCodeForError(error: unknown): number {
  return error instanceof CompletionReportError ? 2 : 1;
}

export function finalizationTimeouts(env: NodeJS.ProcessEnv = process.env): FinalizationTimeouts {
  return {
    artifact: parsePhaseTimeout(env.ANBAN_JOB_ARTIFACT_TIMEOUT, ARTIFACT_TIMEOUT_MS, MAX_ARTIFACT_TIMEOUT_MS),
    completion: parsePhaseTimeout(env.ANBAN_JOB_COMPLETION_TIMEOUT, COMPLETION_TIMEOUT_MS, MAX_COMPLETION_TIMEOUT_MS),
  };
}

function parsePhaseTimeout(raw: string | undefined, fallback: number, maximum: number): number {
  if (!raw) return fallback;
  const match = /^(\d+(?:\.\d+)?)(ms|s|m)$/.exec(raw);
  if (!match) return fallback;
  const unit = match[2] === "m" ? 60_000 : match[2] === "s" ? 1_000 : 1;
  const milliseconds = Number(match[1]) * unit;
  return milliseconds > 0 && milliseconds <= maximum ? milliseconds : fallback;
}
```

Replace `abortAt` with a relative timeout helper that optionally follows the shutdown signal:

```ts
function abortAfter(timeout: number, parent?: AbortSignal): AbortController {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(new Error("phase deadline exceeded")), timeout);
  const abortFromParent = () => controller.abort(parent?.reason);
  if (parent?.aborted) abortFromParent();
  else parent?.addEventListener("abort", abortFromParent, { once: true });
  controller.signal.addEventListener("abort", () => {
    clearTimeout(timer);
    parent?.removeEventListener("abort", abortFromParent);
  }, { once: true });
  return controller;
}
```

Do not retain an absolute completion deadline. The completion controller must be created only after artifact work ends.

- [ ] **Step 4: Add failing artifact cancellation tests**

Append to `agent-ts/test/artifacts.test.ts`:

```ts
test("rejects a scan that is already cancelled", async () => {
  const root = await mkdtemp(join(tmpdir(), "anban-ts-artifacts-"));
  roots.push(root);
  await mkdir(join(root, "output"), { recursive: true });
  await writeFile(join(root, "output", "content.md"), "content");
  const controller = new AbortController();
  controller.abort(new Error("shutdown"));

  await expect(scanWorkspaceArtifacts(root, controller.signal)).rejects.toThrow("shutdown");
});
```

Replace the existing `scanWorkspaceArtifacts` import with these imports:

```ts
import type { BootstrapResponse } from "../src/bootstrap.js";
import { scanWorkspaceArtifacts, uploadWorkspaceArtifacts } from "../src/artifacts.js";
import { Reporter } from "../src/reporter.js";
```

Then add:

```ts
test("cancels a signed upload without submitting a partial manifest", async () => {
  const root = await mkdtemp(join(tmpdir(), "anban-ts-artifacts-"));
  roots.push(root);
  await mkdir(join(root, "output"), { recursive: true });
  await writeFile(join(root, "output", "content.md"), "content");
  const controller = new AbortController();
  const originalFetch = globalThis.fetch;
  let manifested = false;
  globalThis.fetch = async (input, init) => {
    const url = String(input);
    if (url.endsWith("/api/v1/agent/artifacts/prepare")) {
      const request = JSON.parse(String(init?.body)) as { sha256: string };
      return new Response(JSON.stringify({ code: 0, data: {
        upload_required: true,
        key: "staging/content.md",
        upload_url: "https://oss.example.test/content.md",
        method: "PUT",
        headers: { "X-Oss-Meta-Sha256": request.sha256 },
        max_size: 1024,
      } }), { status: 200 });
    }
    if (url === "https://oss.example.test/content.md") {
      expect(init?.signal).toBe(controller.signal);
      controller.abort(new Error("shutdown"));
      throw controller.signal.reason;
    }
    if (url.endsWith("/api/v1/agent/artifacts/manifest")) manifested = true;
    return new Response("{}", { status: 200 });
  };
  try {
    const reporter = new Reporter({
      serverURL: "https://creator.example.test",
      executionID: "execution-1",
      workspace: root,
      workloadTokenFile: "/token",
      allowHTTPServer: false,
    }, "execution-token", "task-1");
    const bootstrap = { artifact_transport: { mode: "direct" } } as BootstrapResponse;
    await expect(uploadWorkspaceArtifacts(root, bootstrap, reporter, controller.signal)).rejects.toThrow("shutdown");
    expect(manifested).toBe(false);
  } finally {
    globalThis.fetch = originalFetch;
  }
});
```

- [ ] **Step 5: Run the artifact tests and confirm the new cases fail**

Run:

```bash
cd agent-ts && bun test test/artifacts.test.ts
```

Expected: FAIL because scan/hash/upload do not consistently consume the phase signal.

- [ ] **Step 6: Propagate cancellation through artifact collection**

In `agent-ts/src/artifacts.ts`, change the public signatures to:

```ts
export async function uploadWorkspaceArtifacts(
  workspace: string,
  bootstrap: BootstrapResponse,
  reporter: Reporter,
  signal?: AbortSignal,
): Promise<number>

export async function scanWorkspaceArtifacts(
  workspace: string,
  signal?: AbortSignal,
): Promise<WorkspaceArtifact[]>
```

Then:

1. Pass `signal` from `uploadWorkspaceArtifacts` into `scanWorkspaceArtifacts`, `scanDirectory`, `uploadArtifact`, and `snapshotArtifact`.
2. Call `signal?.throwIfAborted()` before every directory entry and file attempt.
3. Hash with `createReadStream(path, { signal })`.
4. Add `signal` to the signed-URL `fetch` request.
5. For STS uploads, pass an opened read stream to `client.put`, destroy that stream when `signal` aborts, and set the OSS request `timeout` to at most 60 seconds.
6. Make progress best-effort after the manifest is committed, then return
   `files.length`:

```ts
try {
  if (files.length) await reporter.progress(`collected ${files.length} workspace artifact(s)`, signal);
} catch {
  // The manifest is authoritative; a progress-line failure must not undo it.
}
return files.length;
```

The STS upload must follow this cleanup shape so a signal listener cannot leak:

```ts
const stream = createReadStream(path, { signal });
const abortUpload = () => stream.destroy(signal?.reason instanceof Error ? signal.reason : new Error("artifact upload aborted"));
if (signal?.aborted) abortUpload();
else signal?.addEventListener("abort", abortUpload, { once: true });
try {
  await client.put(prepared.key, stream, {
    headers: { ...headers, "Content-Type": headers["Content-Type"] || contentType },
    timeout: 60_000,
  });
} finally {
  signal?.removeEventListener("abort", abortUpload);
  stream.destroy();
}
```

Preserve the existing contextual prefixes (`hash artifact`, `prepare artifact
upload`, `upload artifact`, and `report artifact manifest`) and wrap a top-level
scan failure as `scan workspace artifacts: ...`. Do not add a new error type or
submit a manifest after any file fails.

- [ ] **Step 7: Sequence independent phases in `runJob`**

In `agent-ts/src/main.ts`, replace the current shared-window block with this order:

```ts
const timeouts = finalizationTimeouts();
const artifactStartedAt = Date.now();
const artifactAbort = abortAfter(timeouts.artifact, controller.signal);
try {
  stderr.write(`artifact finalization started: timeout_ms=${timeouts.artifact}\n`);
  const count = await uploadWorkspaceArtifacts(config.workspace, data, reporter, artifactAbort.signal);
  stderr.write(`artifact finalization completed: files=${count} duration_ms=${Date.now() - artifactStartedAt}\n`);
} catch (error) {
  const message = error instanceof Error ? error.message : "artifact finalization failed";
  stderr.write(`artifact finalization failed: duration_ms=${Date.now() - artifactStartedAt} error=${message}\n`);
  if (result.success) result = failure(config.workspace, new Error(`artifact upload failed: ${message}`));
} finally {
  artifactAbort.abort();
}

const completionStartedAt = Date.now();
const completionAbort = abortAfter(timeouts.completion);
let completionError: Error | undefined;
try {
  stderr.write(`completion report started: timeout_ms=${timeouts.completion}\n`);
  await reporter.complete(result, completionAbort.signal);
  stderr.write(`completion report acknowledged: duration_ms=${Date.now() - completionStartedAt}\n`);
} catch (error) {
  completionError = error instanceof Error ? error : new Error("completion report failed");
  stderr.write(`completion report exhausted: duration_ms=${Date.now() - completionStartedAt} error=${completionError.message}\n`);
} finally {
  completionAbort.abort();
}
stdout.write(`${JSON.stringify(result)}\n`);
if (completionError) throw new CompletionReportError(completionError);
return result;
```

Replace the executable entrypoint with:

```ts
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  runJob(process.argv.slice(2))
    .then((result) => { if (!result.success) process.exitCode = 1; })
    .catch((error) => {
      process.stderr.write(`${error instanceof Error ? error.message : "agent job failed"}\n`);
      process.exitCode = exitCodeForError(error);
    });
}
```

An acknowledged failed result still exits 1; only a thrown
`CompletionReportError` exits 2.

- [ ] **Step 8: Run TypeScript tests and type checking**

Run:

```bash
cd agent-ts && bun test test/main.test.ts test/artifacts.test.ts test/reporter.test.ts
cd agent-ts && bun run typecheck
```

Expected: all selected tests PASS and TypeScript exits 0.

- [ ] **Step 9: Commit the TypeScript runtime change**

```bash
git add agent-ts/src/main.ts agent-ts/src/artifacts.ts agent-ts/test/main.test.ts agent-ts/test/artifacts.test.ts
git commit -m "fix: isolate TypeScript finalization deadlines"
```

## Task 2: Isolate Go Runtime Phases And Exit Semantics

**Files:**
- Modify: `agent/main.go`
- Modify: `agent/job.go`
- Test: `agent/main_test.go`
- Test: `agent/job_test.go`
- Test: `agent/artifact_upload_test.go`

- [ ] **Step 1: Write failing tests for independent Go budgets**

Replace the shared-window tests in `agent/job_test.go` with:

```go
func TestFinalizationContextsUseIndependentBudgets(t *testing.T) {
	t.Setenv(jobArtifactTimeoutEnv, "20ms")
	t.Setenv(jobCompletionTimeoutEnv, "80ms")
	timeouts := newFinalizationTimeouts(&Config{ExecutionID: "execution-1"})

	artifactCtx, cancelArtifact := timeouts.artifactContext(context.Background())
	defer cancelArtifact()
	<-artifactCtx.Done()

	completionCtx, cancelCompletion := timeouts.completionContext()
	defer cancelCompletion()
	if completionCtx.Err() != nil {
		t.Fatalf("fresh completion context is already done: %v", completionCtx.Err())
	}
	deadline, ok := completionCtx.Deadline()
	if !ok || time.Until(deadline) < 40*time.Millisecond {
		t.Fatalf("completion did not receive its independent budget: %v", deadline)
	}
}

func TestArtifactContextStopsOnShutdown(t *testing.T) {
	shutdown, cancelShutdown := context.WithCancel(context.Background())
	ctx, cancelArtifact := newFinalizationTimeouts(&Config{ExecutionID: "execution-1"}).artifactContext(shutdown)
	defer cancelArtifact()
	cancelShutdown()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel artifact finalization")
	}
}
```

Add `errors` to the imports in `agent/main_test.go`, then append:

```go
func TestProcessExitCodeReservesTwoForCompletionReportFailure(t *testing.T) {
	if got := processExitCode(&completionReportError{err: errors.New("server unavailable")}); got != 2 {
		t.Fatalf("completion exit code = %d, want 2", got)
	}
	if got := processExitCode(errors.New("runner failed")); got != 1 {
		t.Fatalf("ordinary exit code = %d, want 1", got)
	}
}
```

- [ ] **Step 2: Run targeted Go tests and confirm they fail**

Run:

```bash
go test ./agent -run 'Test(FinalizationContextsUseIndependentBudgets|ArtifactContextStopsOnShutdown|ProcessExitCodeReservesTwo)' -count=1
```

Expected: FAIL because the new timeout fields, contexts, and typed error do not exist.

- [ ] **Step 3: Implement Go timeout parsing and typed completion errors**

In `agent/main.go`, add the `errors` import and replace the shared-window
variables and type with:

```go
var jobArtifactTimeout = 120 * time.Second
var jobCompletionTimeout = 20 * time.Second

const (
	jobArtifactTimeoutEnv = "ANBAN_JOB_ARTIFACT_TIMEOUT"
	jobCompletionTimeoutEnv = "ANBAN_JOB_COMPLETION_TIMEOUT"
	completionReportExitCode = 2
)

type finalizationTimeouts struct {
	job        bool
	artifact  time.Duration
	completion time.Duration
}

type completionReportError struct{ err error }

func (e *completionReportError) Error() string { return "failed to report completion: " + e.err.Error() }
func (e *completionReportError) Unwrap() error { return e.err }

func processExitCode(err error) int {
	var completionErr *completionReportError
	if errors.As(err, &completionErr) {
		return completionReportExitCode
	}
	return 1
}
```

Add the parser and constructor:

```go
func parseFinalizationDuration(name string, fallback, maximum time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 || value > maximum {
		return fallback
	}
	return value
}

func newFinalizationTimeouts(cfg *Config) finalizationTimeouts {
	if cfg == nil || strings.TrimSpace(cfg.ExecutionID) == "" {
		return finalizationTimeouts{}
	}
	return finalizationTimeouts{
		job: true,
		artifact: parseFinalizationDuration(jobArtifactTimeoutEnv, jobArtifactTimeout, 5*time.Minute),
		completion: parseFinalizationDuration(jobCompletionTimeoutEnv, jobCompletionTimeout, time.Minute),
	}
}
```

Implement contexts so completion starts its timeout when called, not when artifact starts:

```go
func (w finalizationTimeouts) artifactContext(shutdown context.Context) (context.Context, context.CancelFunc) {
	var ctx context.Context
	var cancel context.CancelFunc
	if w.job {
		ctx, cancel = context.WithTimeout(context.Background(), w.artifact)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	if shutdown == nil {
		return ctx, cancel
	}
	if shutdown.Err() != nil {
		cancel()
		return ctx, cancel
	}
	stopShutdown := context.AfterFunc(shutdown, cancel)
	return ctx, func() { stopShutdown(); cancel() }
}

func (w finalizationTimeouts) completionContext() (context.Context, context.CancelFunc) {
	if !w.job {
		return context.WithCancel(context.Background())
	}
	return context.WithTimeout(context.Background(), w.completion)
}
```

Update `main()` exactly as follows:

```go
func main() {
	cmd := newAgentCommand(os.Stdout, os.Stderr, func(ctx context.Context, cfg *Config) error {
		return runAgent(ctx, cfg, os.Stdout, os.Stderr)
	})
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(processExitCode(err))
	}
}
```

- [ ] **Step 4: Separate signal cancellation from caller cancellation**

In `runAgent`, replace `signal.NotifyContext(ctx, ...)` with:

```go
shutdownCtx, stopShutdown := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer stopShutdown()
runCtx, cancelRun := context.WithCancel(ctx)
stopForward := context.AfterFunc(shutdownCtx, cancelRun)
defer func() { stopForward(); cancelRun() }()
```

Use `runCtx` for the heartbeat, `runner.Run`, and the existing cancellation
check that populates `result.Error`. Replace the current finalization block with
this shape:

```go
timeouts := newFinalizationTimeouts(cfg)
artifactStarted := time.Now()
fmt.Fprintf(stderr, "artifact finalization started: timeout_ms=%d\n", timeouts.artifact.Milliseconds())
artifactCtx, cancelArtifact := timeouts.artifactContext(shutdownCtx)
if cfg.ArtifactUploadMode == ArtifactUploadDirect || cfg.ArtifactUploadMode == ArtifactUploadStream {
	uploader := NewArtifactUploader(cfg, reporter)
	if uploadErr := uploader.UploadWorkspaceArtifacts(artifactCtx, result); uploadErr != nil {
		fmt.Fprintf(stderr, "artifact finalization failed: duration_ms=%d error=%v\n", time.Since(artifactStarted).Milliseconds(), uploadErr)
		runErr = applyArtifactUploadFailure(result, runErr, uploadErr)
	} else {
		fmt.Fprintf(stderr, "artifact finalization completed: duration_ms=%d\n", time.Since(artifactStarted).Milliseconds())
	}
}
cancelArtifact()

completionStarted := time.Now()
completionCtx, cancelCompletion := timeouts.completionContext()
fmt.Fprintf(stderr, "completion report started: timeout_ms=%d\n", timeouts.completion.Milliseconds())
completeErr := reporter.ReportComplete(completionCtx, result)
cancelCompletion()
if completeErr == nil {
	fmt.Fprintf(stderr, "completion report acknowledged: duration_ms=%d\n", time.Since(completionStarted).Milliseconds())
} else {
	fmt.Fprintf(stderr, "completion report exhausted: duration_ms=%d error=%v\n", time.Since(completionStarted).Milliseconds(), completeErr)
}
```

After encoding `result` to stdout, return
`&completionReportError{err: completeErr}` before evaluating `runErr` whenever
`completeErr != nil`.

Keep `applyArtifactUploadFailure` unchanged.

- [ ] **Step 5: Preserve bootstrap completion failures**

In `agent/job.go`, replace the ignored bootstrap completion error with:

```go
completeErr := reporter.ReportComplete(completeCtx, result)
cancelComplete()
if completeErr != nil {
	return errors.Join(
		fmt.Errorf("bootstrap job: %w", err),
		&completionReportError{err: completeErr},
	)
}
```

Add the `errors` import. Keep the authenticated bootstrap error as the result sent to the Server.

- [ ] **Step 6: Update the slow-hash regression test**

In `agent/artifact_upload_test.go`, update `TestJobArtifactHashCancellationPreservesCompletionReserve` to set:

```go
t.Setenv(jobArtifactTimeoutEnv, "200ms")
t.Setenv(jobCompletionTimeoutEnv, "500ms")
timeouts := newFinalizationTimeouts(&Config{ExecutionID: "execution-1"})
workCtx, cancelWork := timeouts.artifactContext(context.Background())
```

After the slow hash returns `context.DeadlineExceeded`, create `completionCtx := timeouts.completionContext()` and retain the existing assertion that `/api/v1/agent/complete` is reached. Rename the test to `TestJobArtifactHashTimeoutStillGetsFreshCompletionBudget`.

Add `errors` to the imports in `agent/job_test.go`. Update
`TestJobCommandDoesNotRunAfterInvalidBootstrapResponse` to set
`ANBAN_JOB_ARTIFACT_TIMEOUT=40ms` and
`ANBAN_JOB_COMPLETION_TIMEOUT=80ms`. Add:

```go
func TestJobCommandSurfacesBootstrapCompletionFailure(t *testing.T) {
	t.Setenv(jobCompletionTimeoutEnv, "80ms")
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("workload"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/agent/complete" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
			"execution_token": testExecutionToken(t, "execution-1", "task-1", "project-1"),
			"task_id": "task-1", "task_type": "", "project_id": "project-1", "prompt": "write",
			"max_turns": 40, "agent_flag": "", "auto_memory_directory": ".claude/memory",
		}})
	}))
	defer server.Close()
	cmd := newJobCommand(testBootstrapJob, func(context.Context, *Config) error {
		t.Fatal("run must not start after invalid bootstrap")
		return nil
	})
	err := cmd.Run(context.Background(), []string{"job", "--server-url", server.URL, "--execution-id", "execution-1", "--workspace", t.TempDir(), "--workload-token-file", tokenFile})
	var completionErr *completionReportError
	if !errors.As(err, &completionErr) {
		t.Fatalf("error = %v, want completionReportError", err)
	}
}
```

- [ ] **Step 7: Run Go Agent tests**

Run:

```bash
go test ./agent -run 'Test(Finalization|ArtifactContext|ProcessExitCode|JobArtifactHash|JobCommandDoesNotRun)' -count=1
go test ./agent -count=1
```

Expected: targeted and full Agent package tests PASS.

- [ ] **Step 8: Commit the Go runtime change**

```bash
git add agent/main.go agent/job.go agent/main_test.go agent/job_test.go agent/artifact_upload_test.go
git commit -m "fix: isolate Go finalization deadlines"
```

## Task 3: Preserve Completion Failure In Runtime Reconciliation

**Files:**
- Modify: `server/agent/kubernetes_job.go`
- Modify: `server/agent/kubernetes_executor_test.go`
- Modify: `server/agent/runtime_reconciler.go`
- Modify: `server/agent/runtime_reconciler_test.go`
- Modify: `server/config.yaml`
- Modify: `server/config.example.yaml`

- [ ] **Step 1: Write the failing reconciler classification test**

In `TestRuntimeTerminalReasonPrecedence`, declare
`exitCompletion := runtimeCompletionReportFailedExitCode` next to `exit137`,
then add:

```go
{"completion callback not acknowledged", RuntimePhaseFailed, "Error", "completion_report_failed", model.TaskExecutionFailed, &exitCompletion},
```

- [ ] **Step 2: Run the reconciler test and confirm it fails**

Run:

```bash
go test ./server/agent -run TestRuntimeTerminalReason -count=1
```

Expected: FAIL because exit code 2 is currently classified as `runtime_failed`.

- [ ] **Step 3: Map exit code 2 before the generic runtime failure**

In `runtimeTerminalReason` in `server/agent/runtime_reconciler.go`, add this case after timeout and OOM classification and before `default`:

```go
case state.ExitCode != nil && *state.ExitCode == runtimeCompletionReportFailedExitCode:
	return "completion_report_failed", model.TaskExecutionFailed
```

Use a named constant in `server/agent/runtime_reconciler.go`:

```go
const runtimeCompletionReportFailedExitCode int32 = 2
```

Reference the constant in both implementation and test so the cross-process value is explicit.

- [ ] **Step 4: Remove Kubernetes shared-timeout injection**

In `server/agent/kubernetes_job.go`:

- delete `kubernetesFinalizationTimeoutEnv`
- remove the fourth container environment entry
- delete `kubernetesFinalizationTimeoutSeconds`
- remove the now-unused `strconv` import

In `server/agent/kubernetes_executor_test.go`, assert exactly the three trust/runtime-home variables:

```go
if len(c.Env) != 3 ||
	c.Env[0].Name != "HOME" || c.Env[0].Value != kubernetesRuntimeHomePath ||
	c.Env[1].Name != "SSL_CERT_FILE" || c.Env[1].Value != kubernetesServerCAFile ||
	c.Env[2].Name != "NODE_EXTRA_CA_CERTS" || c.Env[2].Value != kubernetesServerCAFile {
	t.Fatalf("environment = %#v, want only HOME and Server CA trust", c.Env)
}
```

Delete `TestKubernetesFinalizationTimeoutIsBoundedByGraceAndDeadline`. Retain the existing assertion that `TerminationGracePeriodSeconds == 30`.

- [ ] **Step 5: Clarify configuration comments without changing schema**

In both `server/config.yaml` and `server/config.example.yaml`, replace the current combined deadline comment with:

```yaml
# Job 总执行截止时间；completion_grace_seconds 仅用于 Pod 终止和 Server 等待终态回调，
# 正常 artifact/completion 阶段预算由 runtime 内部独立控制。
active_deadline_seconds: 3600
```

Do not add YAML keys or change default values.

- [ ] **Step 6: Run Server runtime tests**

Run:

```bash
go test ./server/agent -run 'Test(RuntimeTerminalReason|Kubernetes.*Job|KubernetesDispatcherScope|DockerInspectMapsContainerStates)' -count=1
go test ./server/agent -count=1
```

Expected: reconciler, Kubernetes, and Docker runtime tests PASS. The existing Docker exit-code-2 case proves no Docker dispatcher implementation change is required.

- [ ] **Step 7: Commit Server classification and Kubernetes cleanup**

```bash
git add server/agent/kubernetes_job.go server/agent/kubernetes_executor_test.go server/agent/runtime_reconciler.go server/agent/runtime_reconciler_test.go server/config.yaml server/config.example.yaml
git commit -m "fix: preserve completion report failures"
```

## Task 4: Full Verification And Release Proof

**Files:**
- Verify only; no implementation files should change.

- [ ] **Step 1: Run all TypeScript runtime checks**

```bash
cd agent-ts && bun test
cd agent-ts && bun run typecheck
cd agent-ts && bun run build
```

Expected: all commands exit 0 with no failed tests or TypeScript diagnostics.

- [ ] **Step 2: Run all Go checks serially**

```bash
go test ./... -count=1
go vet ./...
go build -o /tmp/anban ./agent
go build -o /tmp/anban-creator-server ./server
```

Expected: all commands exit 0. If a timing-sensitive test fails during a parallel local run, stop stale processes and rerun the standard suite serially before attribution.

- [ ] **Step 3: Confirm the obsolete contract is gone and scope stayed narrow**

```bash
rg -n "ANBAN_JOB_FINALIZATION_TIMEOUT|jobCompletionReserve|finalizationDeadlines|kubernetesFinalizationTimeoutSeconds" agent agent-ts server
git diff --check
git status --short
```

Expected: `rg` returns no matches; `git diff --check` is silent. `git status` may show only the two pre-existing untracked plans plus intended task commits.

- [ ] **Step 4: Build runtime images on a Docker-capable host**

```bash
make docker-agent-image
make docker-seednote-agent-image
make docker-montage-agent-image
make docker-server-image
```

Expected: all four images build successfully. Record immutable published digests for Article, Seednote, Montage, and Server; do not treat a local Go/TypeScript build as image proof.

- [ ] **Step 5: Deploy in dependency order**

Publish the three runtime images first. Set `ANBAN_AGENT_IMAGE_ARTICLE`, `ANBAN_AGENT_IMAGE_SEEDNOTE`, and `ANBAN_AGENT_IMAGE_MONTAGE` to their immutable digests, then deploy the new Server image and wait for its rollout to finish. Existing executions retain their frozen runtime image and are not production proof for this change.

- [ ] **Step 6: Run one fresh production Seednote acceptance task**

Create a new Seednote task that produces at least the same shape as the incident: Markdown deliverables plus three images and at least 8 MiB total output. Accept only when all of the following are true:

- task status is `completed`
- execution result is successful and Job exit code is 0
- every expected workspace file belongs to one execution manifest
- successful files are `published`
- no upload session for the execution remains stale in `pending`
- neither task nor execution contains `runtime_failed`
- normal finalization has no fixed 120-second or 20-second delay

- [ ] **Step 7: Prove the failure diagnostic in a controlled environment**

Against a non-production Server or intercepted completion endpoint, return retryable 503 responses until the 20-second completion budget expires. Confirm the runtime exits 2 and the reconciler persists `completion_report_failed`. Do not fault-inject the production completion endpoint.

- [ ] **Step 8: Review the final diff and commit history**

```bash
git log -4 --oneline
git diff HEAD~3..HEAD --check
git status --short --branch
```

Expected: three focused implementation commits follow the design/plan commits, the combined diff is clean, and unrelated untracked files remain untouched.
