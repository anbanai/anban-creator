# Agent Runtime Billing and Failure Artifact Collection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (- [ ]) syntax for tracking.

**Goal:** Charge the fixed task fee plus actual Claude Code usage, and retain every eligible failed-execution artifact without contaminating the successful delivery set.

**Architecture:** Runtime settlement becomes an idempotent post-execution agent_runtime:<task_id> charge that prefers Claude Code's reported USD cost and falls back to configured token pricing. Cloud manifests gain a collected terminal state: successful attempts publish and replace the delivery set, while failed attempts remain user-visible and downloadable through a separate read path.

**Tech Stack:** Go 1.24, GORM, SQLite/MySQL migrations, Fiber v3, Claude Agent SDK result usage, React 19, TypeScript, TanStack Query, Vitest.

---

### Task 1: Restore Actual Claude Code Runtime Settlement

**Files:**
- Modify: server/service/agent_runtime_billing_test.go
- Modify: server/service/credit.go

- [ ] **Step 1: Write failing actual-usage tests**

Add this price configuration to runtimeBillingTestConfig:

~~~go
ModelPrices: srvconfig.ModelPricesConfig{
    CurrencyRates: map[string]srvconfig.CurrencyRate{
        "USD": {ToCNY: srvconfig.FlexibleFloat(7.2)},
    },
    TokenModels: map[string]srvconfig.TokenModelPrice{
        "anthropic/claude-test": {
            Currency: "USD", Unit: 1_000_000,
            Input: srvconfig.FlexibleFloat(3),
            Output: srvconfig.FlexibleFloat(15),
            CachedInput: srvconfig.FlexibleFloat(0.30),
            CacheReadInput: srvconfig.FlexibleFloat(0.30),
            CacheCreationInput: srvconfig.FlexibleFloat(3.75),
        },
    },
},
~~~

Replace the platform-paid settlement assertion with a table-driven success/failed result test:

~~~go
result := &serveragent.ExecutionResult{
    Success: success, Model: "claude-test", SessionID: "sess-runtime",
    NumTurns: 7, DurationAPIMs: 12345, TotalCostUSD: ptrFloat64(1.00),
    TokenUsage: &serveragent.TokenUsage{
        InputTokens: 1000, OutputTokens: 500,
        CacheReadTokens: 300, CacheCreationTokens: 200,
    },
}
// 9000 starting - 4000 fixed - 7200 runtime = -2200.
// Assert one agent_runtime transaction with amount -7200 and complete metadata.
~~~

Add these tests with real repository state:

~~~go
func TestSettleAgentRuntimeFallsBackToTokenUsage(t *testing.T) // expect 159 credits
func TestSettleAgentRuntimeIsIdempotent(t *testing.T) // call twice, one transaction
func TestSettleAgentRuntimeMissingUsageDoesNotFabricateCharge(t *testing.T) // settled, no runtime tx
~~~

Decode CreditTransaction.Metadata into model.CreditTransactionMetadata and assert model, all token categories, USD cost, price snapshot, multipliers, and final credits.

- [ ] **Step 2: Verify RED**

~~~bash
go test ./server/service -run 'Test(SettleAgentRuntime|DeductForTaskCreation|DeductBatchForTaskCreation)' -count=1
~~~

Expected: FAIL because current settlement leaves the balance unchanged and creates no runtime transaction.

- [ ] **Step 3: Implement exact idempotent settlement**

Extend CreditService:

~~~go
type CreditService struct {
    repo repository.Repository
    cfg *config.CreditsConfig
    fullCfg *config.Config
    logger *zerolog.Logger
}

func (s *CreditService) SetFullConfig(cfg *config.Config) {
    if s == nil || cfg == nil { return }
    s.fullCfg = cfg
    s.cfg = normalizeCreditsConfig(&cfg.Credits)
}
~~~

Add:

~~~go
func agentRuntimeResultHasUsage(result *serveragent.ExecutionResult) bool
func (s *CreditService) calculateAgentRuntimeCredits(ctx context.Context, task *model.Task, result *serveragent.ExecutionResult) (int, model.CreditTransactionMetadata, error)
func (s *CreditService) calculateUSDRunCredits(costUSD float64, tier string, userMultiplier float64) (int, int, float64, map[string]any, error)
~~~

Prefer positive TotalCostUSD:

~~~go
baseCredits = ceil(totalCostUSD * usdToCNY * creditsPerCNY)
finalCredits = ceil(baseCredits * tierMultiplier * userMultiplier)
~~~

Otherwise use all reported token categories with Config.CalculateTokenModelCredits("anthropic", modelName, usage, tier, userMultiplier), falling back to provider key "claude" only when needed.

Implement SettleAgentRuntime in one repository transaction:

1. If usage is absent, emit a structured warning, create no charge, and mark billing settled.
2. Check agent_runtime:<task_id>; if it exists, only mark billing settled.
3. Calculate credits and marshal metadata.
4. Call Users().AdjustBalance(ctx, userID, -credits), allowing overdraft.
5. Create one agent_runtime transaction with task ID, operation ID, negative amount, balance, description, and datatypes.JSON metadata.
6. Mark billing settled.

Do not restore runtime reserve creation or payment-required shortfall charging.

- [ ] **Step 4: Verify GREEN**

~~~bash
go test ./server/service -run 'Test(SettleAgentRuntime|DeductForTaskCreation|DeductBatchForTaskCreation)' -count=1
~~~

Expected: PASS.

- [ ] **Step 5: Commit**

~~~bash
git add server/service/credit.go server/service/agent_runtime_billing_test.go
git commit -m "fix(credits): charge actual Claude runtime usage"
~~~

### Task 2: Add Collected Artifact Schema and Repository Transition

**Files:**
- Modify: server/model/task_file.go
- Modify: server/model/task_execution.go
- Modify: server/model/task_file_migrate.go
- Modify: server/model/model.go
- Modify: server/model/task_file_execution_test.go
- Modify: server/repository/repository.go
- Modify: server/repository/task_file.go
- Modify: server/repository/task_file_execution_test.go

- [ ] **Step 1: Write failing migration and repository tests**

Create a legacy-schema migration test that calls AutoMigrate, then inserts:

~~~go
&TaskFile{TaskID: "t1", ExecutionID: "e1", State: "collected",
    FilePath: "output/failure-state.json", FileName: "failure-state.json", Role: FileRoleOther}
&TaskExecution{ID: "e1", TaskID: "t1", Attempt: 1, Target: "kubernetes",
    Status: TaskExecutionFailed, ManifestStatus: "collected"}
~~~

Add:

~~~go
func TestTaskFileRepositoryCollectsFailedExecutionWithoutReplacingPublishedSet(t *testing.T)
func TestTaskFileRepositoryCollectCurrentExecutionIsIdempotent(t *testing.T)
func TestTaskFileRepositoryCollectRejectsStaleExecution(t *testing.T)
~~~

The first test seeds an older published file and a pending failure-state.json. After marking the current execution failed, collection must preserve the published file, change the pending file to collected, and set manifest status collected.

- [ ] **Step 2: Verify RED**

~~~bash
go test ./server/model ./server/repository -run 'Test.*(Collected|Collect)' -count=1
~~~

Expected: FAIL because old constraints reject collected and CollectCurrentExecution does not exist.

- [ ] **Step 3: Add collected states and migration support**

Update tags/constants:

~~~go
State string `gorm:"type:varchar(20);index;not null;default:published;check:chk_task_file_state,state IN ('pending','published','collected','superseded')" json:"state"`
const TaskFileStateCollected = "collected"

ManifestStatus string `gorm:"type:varchar(20);default:'';check:chk_task_execution_manifest_status,manifest_status IN ('','pending','published','collected','discarded','rejected')" json:"manifest_status,omitempty"`
const TaskExecutionManifestCollected = "collected"
~~~

Before AutoMigrate, drop the named legacy check constraints when present so GORM recreates them:

~~~go
func MigrateTaskArtifactCollectionSchema(db *gorm.DB) error {
    for _, item := range []struct{ model any; name string }{
        {&TaskFile{}, "chk_task_file_state"},
        {&TaskExecution{}, "chk_task_execution_manifest_status"},
    } {
        if db.Migrator().HasConstraint(item.model, item.name) {
            if err := db.Migrator().DropConstraint(item.model, item.name); err != nil { return err }
        }
    }
    return nil
}
~~~

Call it after the uniqueness migration and before db.AutoMigrate.

- [ ] **Step 4: Implement atomic collection**

Extend TaskFileRepository:

~~~go
CollectCurrentExecution(ctx context.Context, taskID, executionID string) error
FindCollectedByTaskID(ctx context.Context, taskID string) ([]*model.TaskFile, error)
~~~

Implement collection with the current task-then-execution lock order. Require the current execution to be failed, cancelled, or timed out; update pending rows to collected; set manifest status collected; accept zero pending rows; and return success when already collected. Keep FindByTaskID published-only and accept collected in validateTaskFileMutation.

- [ ] **Step 5: Verify GREEN**

~~~bash
go test ./server/model ./server/repository -run 'Test.*(TaskFile|Collected|Collect)' -count=1
~~~

Expected: PASS, including migration, idempotency, stale identity, and published-set preservation.

- [ ] **Step 6: Commit**

~~~bash
git add server/model server/repository
git commit -m "feat(server): retain failed execution artifacts"
~~~

### Task 3: Collect Failed Cloud Manifests and Expose Safe Read APIs

**Files:**
- Modify: agent/artifact_upload_test.go
- Modify: server/service/task_execution_complete.go
- Modify: server/service/task_execution_complete_test.go
- Modify: server/service/task.go
- Modify: server/service/task_files.go
- Modify: server/handler/task.go
- Modify: server/handler/task_test.go
- Modify: server/mcp/tools.go
- Modify: server/mcp/tools_test.go

- [ ] **Step 1: Write failing finalization/read-boundary tests**

First add a characterization test for the existing Runner hook:

~~~go
func TestArtifactUploaderUploadsFailureArtifactsForUnsuccessfulResult(t *testing.T) {
    // Write output/failure-state.json, call UploadWorkspaceArtifacts with
    // ExecutionResult{Success: false}, and assert prepare + manifest contain it.
}
~~~

This test should already pass; it locks the verified upload behavior while the
server-side retention tests below drive the production change RED.

Seed pending output/failure-state.json, complete a Seednote execution that fails artifact validation, and assert:

~~~go
execution.Status == model.TaskExecutionFailed
execution.ManifestStatus == model.TaskExecutionManifestCollected
failureState.State == model.TaskFileStateCollected
~~~

Add tests proving:

~~~text
TaskService.GetFiles returns published only.
TaskService.GetVisibleFiles returns published plus collected.
GET /api/v1/tasks/:id/files returns both states.
Single-file download accepts an owned collected file.
Delivery ZIP still contains only published files.
MCP list_task_files returns collected files for diagnosis.
~~~

- [ ] **Step 2: Verify RED**

~~~bash
go test ./agent ./server/service ./server/handler ./server/mcp -run 'Test.*(FailureArtifacts|Failed.*Artifact|Collected|VisibleFiles|ListTaskFiles)' -count=1
~~~

Expected: FAIL because finalization discards the manifest and no visible-file query exists.

- [ ] **Step 3: Route failed finalization to collection**

~~~go
case model.TaskExecutionFinalizationTerminal:
    return model.TaskExecutionFinalizationArtifacts, func(ctx context.Context) error {
        if execution.Status == model.TaskExecutionSucceeded {
            return s.repo.TaskFiles().PublishCurrentExecution(ctx, task.ID, execution.ID)
        }
        return s.repo.TaskFiles().CollectCurrentExecution(ctx, task.ID, execution.ID)
    }, nil
~~~

Keep explicit discard behavior for stale/replaced attempts.

- [ ] **Step 4: Add the safe visible-file boundary**

Keep TaskService.GetFiles published-only. Add GetVisibleFiles that reads FindByTaskID plus FindCollectedByTaskID, puts published rows first, enriches URLs, and returns the combined list. Use it only in the task files HTTP handler and MCP list_task_files.

Allow owned collected files through FindByID and ExistsByTaskIDAndID by selecting states in published,collected. Keep task ownership checks before download. Preview, workflow reconstruction, video production, publishing, ZIP, and bulk export remain published-only.

- [ ] **Step 5: Verify GREEN**

~~~bash
go test ./agent ./server/service ./server/handler ./server/mcp -run 'Test.*(FailureArtifacts|Failed.*Artifact|Collected|VisibleFiles|ListTaskFiles|DownloadFile)' -count=1
~~~

Expected: PASS.

- [ ] **Step 6: Commit**

~~~bash
git add agent/artifact_upload_test.go server/service server/handler server/mcp
git commit -m "fix(server): expose collected failure artifacts"
~~~

### Task 4: Separate Failed-Attempt Files in Studio

**Files:**
- Modify: studio/src/types/task.ts
- Modify: studio/src/pages/TaskDetailPage.tsx
- Modify: studio/src/pages/TaskDetailPage.test.tsx

- [ ] **Step 1: Write a failing Studio test**

Add a published content.md and:

~~~ts
const collectedFile: TaskFile = {
  id: 'collected-1', task_id: task.id, execution_id: 'execution-failed',
  state: 'collected', role: 'other', file_name: 'failure-state.json',
  mime_type: 'application/json', file_size: 96,
  url: '/failure-state.json', created_at: now,
}
~~~

Assert content.md stays in generated files and failure-state.json appears under a separate 失败执行文件 section.

- [ ] **Step 2: Verify RED**

~~~bash
cd studio && bun run test -- src/pages/TaskDetailPage.test.tsx
~~~

Expected: FAIL because the type and separate section do not exist.

- [ ] **Step 3: Add state-aware rendering**

Extend TaskFile:

~~~ts
execution_id?: string
state?: 'published' | 'collected'
~~~

Partition once:

~~~ts
const publishedFiles = files?.filter((file) => file.state !== 'collected') ?? []
const collectedFiles = files?.filter((file) => file.state === 'collected') ?? []
~~~

Render existing ecommerce/image/general delivery UI with publishedFiles. Render collectedFiles in a separate un-nested section headed 失败执行文件, with concise text that they are diagnostic or partial outputs excluded from successful delivery ZIPs. Reuse FilePreviewGallery.

- [ ] **Step 4: Verify GREEN**

~~~bash
cd studio && bun run test -- src/pages/TaskDetailPage.test.tsx
~~~

Expected: PASS.

- [ ] **Step 5: Commit**

~~~bash
git add studio/src/types/task.ts studio/src/pages/TaskDetailPage.tsx studio/src/pages/TaskDetailPage.test.tsx
git commit -m "fix(studio): separate failed execution files"
~~~

### Task 5: Full Verification

- [ ] **Step 1: Format changed Go files**

~~~bash
gofmt -w server/service/credit.go server/service/agent_runtime_billing_test.go   server/model/task_file.go server/model/task_execution.go server/model/task_file_migrate.go server/model/model.go   server/model/task_file_execution_test.go server/repository/repository.go server/repository/task_file.go   server/repository/task_file_execution_test.go server/service/task_execution_complete.go   server/service/task_execution_complete_test.go server/service/task.go server/service/task_files.go   server/handler/task.go server/handler/task_test.go server/mcp/tools.go server/mcp/tools_test.go
~~~

- [ ] **Step 2: Run focused Go suites**

~~~bash
go test ./agent ./server/model ./server/repository ./server/service ./server/handler ./server/mcp -count=1
~~~

Expected: PASS.

- [ ] **Step 3: Run all Go tests**

~~~bash
go test ./... -count=1
~~~

Expected: PASS.

- [ ] **Step 4: Build both binaries**

~~~bash
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
~~~

Expected: both commands exit 0 without creating root-level binaries.

- [ ] **Step 5: Run full Studio verification**

~~~bash
cd studio && bun run test
cd studio && bun run build
~~~

Expected: all Vitest tests pass and tsc -b plus vite build exits 0.

- [ ] **Step 6: Inspect final state**

~~~bash
git diff --check
git status --short
git log --oneline --decorate -6
~~~

Expected: no whitespace errors, only intentional changes, and four implementation commits after the design/plan commits.
