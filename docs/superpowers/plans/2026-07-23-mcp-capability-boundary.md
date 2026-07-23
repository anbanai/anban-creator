# MCP Capability Boundary Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every MCP handler a thin capability adapter, remove workflow orchestration from image generation, and remove provider/runtime details from Agent-authored artifacts.

**Architecture:** Agents and Skills own workflow sequencing. MCP handlers decode, authorize, invoke one application capability, and encode; application services own atomic domain operations and transactions. Image generation remains generation + durable task-file registration + settlement, while analysis and CDN upload become independent calls.

**Tech Stack:** Go 1.24, Fiber/MCP Go SDK, GORM, repository services, Markdown Agent/Skill assets, Go contract tests.

---

## File Map

- `CLAUDE.md`, `AGENTS.md`: cross-harness MCP ownership rule.
- `server/service/agent_project_profile.go`: project/task profile application query currently assembled in MCP.
- `server/service/article_score.go`: article score calculation currently implemented in MCP.
- `server/service/seednote_export.go`: Seednote export parsing/formatting currently implemented in MCP.
- `server/service/resource_catalog.go`: embedded-resource query and response projection currently implemented in MCP.
- `server/service/task_image.go`: atomic task image generation, idempotency, persistence, and fixed-SKU settlement.
- `server/mcp/tools.go`, `writing_tools.go`, `seednote_format_tools.go`, `resource_tools.go`: thin adapters after extraction.
- `server/mcp/image_tools.go`: thin image adapters and reduced schemas.
- `server/mcp/boundary_contract_test.go`: source-level ownership guard plus public schema checks.
- `server/mcp/*_test.go`, `server/service/*_test.go`: behavior tests for each extracted capability.
- `plugins/agents/*.md`, `plugins/agents/*.toml`, `plugins/skills/**`: callers updated to independent MCP capabilities.
- `plugins/.claude-plugin/plugin.json`, `plugins/.codex-plugin/plugin.json`: synchronized major version.

### Task 1: Establish The Architectural Rule

**Files:**
- Modify: `CLAUDE.md`
- Modify: `AGENTS.md`
- Create: `server/mcp/boundary_contract_test.go`

- [ ] **Step 1: Write the failing documentation contract test**

Add a test that reads both repository instruction files and requires the same normative phrases:

```go
func TestRepositoryInstructionsDefineMCPAsCapabilityTransport(t *testing.T) {
    for _, path := range []string{"../../CLAUDE.md", "../../AGENTS.md"} {
        data, err := os.ReadFile(path)
        if err != nil { t.Fatal(err) }
        text := string(data)
        for _, want := range []string{
            "MCP is a stateless capability transport",
            "Agents and Skills own business workflow orchestration",
            "one application capability",
        } {
            if !strings.Contains(text, want) {
                t.Fatalf("%s missing %q", path, want)
            }
        }
    }
}
```

- [ ] **Step 2: Run the test and verify RED**

Run: `go test ./server/mcp -run TestRepositoryInstructionsDefineMCPAsCapabilityTransport -count=1`

Expected: FAIL because the rule is not yet present.

- [ ] **Step 3: Add the same rule to both instruction files**

Add this rule under the architecture/development constraints in both files:

```markdown
MCP is a stateless capability transport. Agents and Skills own business workflow
orchestration, including sequencing, retries, quality gates, and stop/continue
decisions. An MCP handler may authenticate, validate protocol and security
constraints, invoke one application capability, and encode its result. It must
not compose multiple domain services or conditionally run another capability.
Atomic persistence and settlement belong in the application service.
```

- [ ] **Step 4: Run the test and verify GREEN**

Run: `go test ./server/mcp -run TestRepositoryInstructionsDefineMCPAsCapabilityTransport -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add CLAUDE.md AGENTS.md server/mcp/boundary_contract_test.go
git commit -m "docs: enforce MCP capability boundary"
```

### Task 2: Extract Business Computation From MCP

**Files:**
- Create: `server/service/agent_project_profile.go`
- Create: `server/service/agent_project_profile_test.go`
- Create: `server/service/article_score.go`
- Create: `server/service/article_score_test.go`
- Create: `server/service/seednote_export.go`
- Create: `server/service/seednote_export_test.go`
- Create: `server/service/resource_catalog.go`
- Create: `server/service/resource_catalog_test.go`
- Modify: `server/mcp/tools.go`
- Modify: `server/mcp/writing_tools.go`
- Modify: `server/mcp/seednote_format_tools.go`
- Modify: `server/mcp/resource_tools.go`
- Modify: `server/mcp/tools_test.go`
- Modify: `server/mcp/writing_tools_test.go`
- Modify: `server/mcp/seednote_format_tools_test.go`
- Modify: `server/mcp/resource_tools_test.go`

- [ ] **Step 1: Move existing behavior into tests at the service boundary**

Define typed requests and assert current behavior before moving implementation:

```go
type AgentProjectProfileRequest struct {
    UserID, ProjectID, TaskID, Scope string
}

type ArticleScoreRequest struct {
    ReadCount, LikeCount, ShareCount, CommentCount, CollectCount int64
}

type SeednoteExportRequest struct {
    Format, Markdown, Title, Content string
    Tags []string
}

type ResourceCatalogRequest struct {
    Category, Platform, Name string
    IncludeRaw bool
}
```

Tests must cover the existing profile task override, article score thresholds,
Seednote Markdown parsing, resource category projection, not-found errors, and
raw resource inclusion.

- [ ] **Step 2: Run service tests and verify RED**

Run:

```bash
go test ./server/service -run 'Test(AgentProjectProfile|ArticleScore|SeednoteExport|ResourceCatalog)' -count=1
```

Expected: FAIL because the application capabilities do not exist.

- [ ] **Step 3: Implement the four application capabilities**

Preserve existing semantic outputs while removing image provider, model, model
key, selection reason, and route metadata from `AgentProjectProfile`. Constructors
receive existing dependencies; public methods have one entry point each:

```go
func (s *AgentProjectProfileService) Get(ctx context.Context, req AgentProjectProfileRequest) (*AgentProjectProfile, error)
func (s *ArticleScoreService) Score(req ArticleScoreRequest) (*ArticleScoreResult, error)
func (s *SeednoteExportService) Export(req SeednoteExportRequest) (*SeednoteExportResult, error)
func (s *ResourceCatalogService) Query(req ResourceCatalogRequest) (any, error)
```

Do not leave parsing regexps, score thresholds, image-route selection, or
resource category projections in `server/mcp`.

- [ ] **Step 4: Convert handlers to decode-delegate-encode**

Each affected handler must have this shape:

```go
input, err := decodeRequest(req)
if err != nil { return errorResult(err.Error()), nil }
result, err := svcs.AgentProjectProfileSvc.Get(ctx, input)
if err != nil { return errorResult(err.Error()), nil }
return textResult(result)
```

Wire the four services through `server/services.go`, `server/handlers.go`, and
the MCP `Services` struct.

- [ ] **Step 5: Run extracted and MCP tests**

Run:

```bash
go test ./server/service ./server/mcp -run 'Test(AgentProjectProfile|ArticleScore|SeednoteExport|ResourceCatalog|ProjectProfile|ScoreArticle|ExportSeednote|Resource)' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add server/service server/mcp server/services.go server/handlers.go
git commit -m "refactor(mcp): extract business computations"
```

### Task 3: Build The Atomic Task Image Application Capability

**Files:**
- Create: `server/service/task_image.go`
- Create: `server/service/task_image_test.go`
- Modify: `server/service/image.go`
- Modify: `server/service/task_files.go`
- Modify: `server/services.go`
- Modify: `server/mcp/tools.go`

- [ ] **Step 1: Write failing service tests for the new contract**

Define semantic input and a minimal public result:

```go
type GenerateTaskImageRequest struct {
    UserID, ExecutionID, TaskID, ProjectID string
    Prompt, ImageType, OutputPath, Size string
    ReferencePaths []string
    Watermark *bool
}

type TaskImageAsset struct {
    Name        string `json:"name"`
    Role        string `json:"role"`
    DownloadURL string `json:"download_url"`
    FilePath    string `json:"file_path,omitempty"`
}
```

Required tests:

```go
func TestGenerateTaskImagePersistsAndSettlesAtomically(t *testing.T)
func TestGenerateTaskImageReplaysIdenticalSemanticRequest(t *testing.T)
func TestGenerateTaskImageChangedPromptCreatesNewOperation(t *testing.T)
func TestGenerateTaskImageDoesNotAnalyzeOrUpload(t *testing.T)
func TestTaskImageAssetDoesNotExposeProviderMetadata(t *testing.T)
```

- [ ] **Step 2: Run service tests and verify RED**

Run: `go test ./server/service -run 'TestGenerateTaskImage|TestTaskImageAsset' -count=1`

Expected: FAIL because `TaskImageService` does not exist.

- [ ] **Step 3: Implement deterministic server-owned idempotency**

Canonicalize the semantic request and derive both operation identity and
fingerprint on the server:

```go
func taskImageOperationIdentity(req GenerateTaskImageRequest) (string, string, error) {
    canonical, err := json.Marshal(struct {
        ExecutionID, TaskID, ProjectID string
        Prompt, ImageType, OutputPath, Size string
        ReferencePaths []string
        Watermark *bool
    }{req.ExecutionID, req.TaskID, req.ProjectID, req.Prompt, req.ImageType,
      req.OutputPath, req.Size, append([]string(nil), req.ReferencePaths...), req.Watermark})
    if err != nil { return "", "", err }
    sum := sha256.Sum256(canonical)
    fingerprint := hex.EncodeToString(sum[:])
    return "image:" + fingerprint[:32], fingerprint, nil
}
```

Preserve reference order because prompts identify references by position.

- [ ] **Step 4: Move the atomic operation from MCP into `TaskImageService`**

The service must perform, in order: task ownership, model resolution, SKU
resolution, replay lookup, provider generation, durable task-file registration,
and settlement outbox commit. It must not depend on `WritingService` or call
`ImageService.UploadImage`.

Provider/model/revised-prompt details may be logged or stored in the internal
snapshot but must not be fields of `TaskImageAsset`.

- [ ] **Step 5: Run service tests and verify GREEN**

Run: `go test ./server/service -run 'TestGenerateTaskImage|TestTaskImageAsset' -count=1`

Expected: PASS, including one provider call and one settlement for an identical
request replay.

- [ ] **Step 6: Commit**

```bash
git add server/service/task_image.go server/service/task_image_test.go server/service/image.go server/service/task_files.go server/services.go server/mcp/tools.go
git commit -m "refactor(image): add atomic task image capability"
```

### Task 4: Reduce Image MCP Tools To Independent Capabilities

**Files:**
- Modify: `server/mcp/image_tools.go`
- Modify: `server/mcp/image_tools_test.go`
- Modify: `server/mcp/image_gen_diag_test.go`
- Modify: `server/mcp/image_test_helpers_test.go`

- [ ] **Step 1: Replace old schema expectations with failing boundary tests**

```go
func TestGenerateImageSchemaContainsOnlySemanticInputs(t *testing.T) {
    schema := generateImageInputSchema()
    properties := schema["properties"].(map[string]any)
    for _, removed := range []string{
        "operation_id", "verify_with_vision", "verification_prompt", "upload_to_cdn",
    } {
        if _, ok := properties[removed]; ok {
            t.Fatalf("generate_image exposes removed field %q", removed)
        }
    }
}

func TestRegisterRenderedImageDoesNotUpload(t *testing.T)
func TestDownloadImageDoesNotUpload(t *testing.T)
```

Also assert the serialized generation response lacks `provider`, `model`,
`selection_reason`, `response_type`, `revised_prompt`, `output_mime`,
`verification`, `wechat_url`, `media_id`, and billing details.

- [ ] **Step 2: Run MCP image tests and verify RED**

Run:

```bash
go test ./server/mcp -run 'Test(GenerateImageSchemaContainsOnlySemanticInputs|RegisterRenderedImageDoesNotUpload|DownloadImageDoesNotUpload|GenerateImagePublicResult)' -count=1
```

Expected: FAIL because compound fields and branches still exist.

- [ ] **Step 3: Make `generateImageHandler` a thin adapter**

Decode semantic fields, obtain user/execution identity, invoke exactly one
method, and return `TaskImageAsset`:

```go
asset, err := svcs.TaskImageSvc.Generate(ctx, service.GenerateTaskImageRequest{
    UserID: getUserID(ctx), ExecutionID: getExecutionID(ctx),
    TaskID: taskID, ProjectID: projectID, Prompt: prompt,
    ImageType: imageType, OutputPath: outputPath, Size: size,
    ReferencePaths: refPaths, Watermark: watermark,
})
```

Delete MCP-owned model resolution, billing, replay, verification parsing,
conditional upload, task-file settlement, and technical result projection.

- [ ] **Step 4: Split other compound image options**

Remove `upload_to_cdn` from `register_rendered_image`; registration returns the
task asset only. Remove `upload` from `download_image`; downloading returns the
local/durable image only. Keep `upload_image` and `analyze_image` independently
callable.

- [ ] **Step 5: Run all MCP image tests and verify GREEN**

Run: `go test ./server/mcp -run 'Test.*Image' -count=1`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add server/mcp/image_tools.go server/mcp/image_tools_test.go server/mcp/image_gen_diag_test.go server/mcp/image_test_helpers_test.go
git commit -m "refactor(mcp): make image tools independent"
```

### Task 5: Update All Agent And Skill Callers

**Files:**
- Modify: `plugins/agents/article.md`
- Modify: `plugins/agents/article.toml`
- Modify: `plugins/agents/ecommerce.md`
- Modify: `plugins/agents/ecommerce.toml`
- Modify: `plugins/agents/seednote.md`
- Modify: `plugins/agents/seednote.toml`
- Modify: `plugins/skills/article-cover-design/SKILL.md`
- Modify: `plugins/skills/article-publishing/SKILL.md`
- Modify: `plugins/skills/article-visual-design/SKILL.md`
- Modify: `plugins/skills/article-visual-design/references/content.md`
- Modify: `plugins/skills/article/SKILL.md`
- Modify: `plugins/skills/ecommerce-visual-design/SKILL.md`
- Modify: `plugins/skills/ecommerce/SKILL.md`
- Modify: `plugins/skills/line-art-coloring/SKILL.md`
- Modify: `plugins/skills/portrait-pose-variants/SKILL.md`
- Modify: `plugins/skills/seednote-visual-design/SKILL.md`
- Modify: `plugins/skills/short-video-cover/SKILL.md`
- Modify: `plugins/.claude-plugin/plugin.json`
- Modify: `plugins/.codex-plugin/plugin.json`
- Modify: `server/agent/seednote_skill_contract_test.go`
- Modify: `server/mcp/tools_test.go`

- [ ] **Step 1: Write failing plugin contract tests**

Add recursive scans over runtime plugin Markdown/TOML:

```go
var removedImageContractTerms = []string{
    "verify_with_vision", "verification_prompt", "upload_to_cdn", "operation_id",
}

func TestPluginAssetsDoNotUseRemovedGenerateImageWorkflowFields(t *testing.T)
func TestSeednoteImagePromptsContainOnlyCreativeContent(t *testing.T)
```

The Seednote test must reject instructions requiring `provider`, `model`,
`selection_reason`, `response_type`, `output_mime`, physical dimensions,
generation attempts, or verification objects in `image-prompts.md`.
The project-profile test must reject `image_model.provider`,
`image_model.model`, `image_model.key`, and `selection_reason` in the public
profile while preserving semantic style and reference-asset fields.

- [ ] **Step 2: Run contract tests and verify RED**

Run:

```bash
go test ./server/agent ./server/mcp -run 'Test(PluginAssetsDoNotUseRemoved|SeednoteImagePrompts|Seednote.*Skill|Designer.*Contract|.*SkillFiles)' -count=1
```

Expected: FAIL with current plugin references.

- [ ] **Step 3: Rewrite callers around independent capabilities**

Use this workflow wording where quality analysis or CDN upload is needed:

```text
Call generate_image once for the planned asset. If the workflow requires a
content-quality review, call analyze_image separately and decide in the Agent
whether to revise the creative prompt. If publishing requires a CDN asset, call
upload_image separately after accepting the image.
```

Do not make analysis availability a prerequisite for generating later planned
images. Do not record provider/runtime metadata in creative artifacts.
Remove provider-specific reference branching from ecommerce and line-art
instructions. Agents pass the semantic reference set in stable prompt order;
server-side image routing owns provider limits and capability selection.

- [ ] **Step 4: Simplify Seednote artifacts**

Define `image-prompts.md` as:

```markdown
## cover.png

用途：封面

提示词：
<最终创作提示词>
```

`image-review.md` may contain only visible content-quality observations. Provider
errors and timeouts go to `failure-state.json` and server observability.

- [ ] **Step 5: Bump both manifests to `4.0.0`**

Update both JSON files in the same patch and assert equality in the existing
plugin contract test.

- [ ] **Step 6: Run plugin contract tests and verify GREEN**

Run:

```bash
go test ./server/agent ./server/mcp -run 'Test(PluginAssetsDoNotUseRemoved|SeednoteImagePrompts|Seednote.*Skill|Designer.*Contract|.*SkillFiles)' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add plugins server/agent server/mcp
git commit -m "refactor(plugins): move workflows out of MCP"
```

### Task 6: Finish The Repository-Wide MCP Handler Audit

**Files:**
- Modify: `server/mcp/agent_feedback_tools.go`
- Modify: `server/mcp/topic_pool_tools.go`
- Modify: `server/mcp/seednote_tools.go`
- Modify: `server/mcp/file_upload_tools.go`
- Modify: `server/mcp/media_pipeline_tools.go`
- Modify: `server/mcp/live_slice_tools.go`
- Modify: `server/mcp/progress_tools.go`
- Modify: `server/mcp/publishing_tools.go`
- Modify: `server/mcp/template_tools.go`
- Modify: `server/mcp/workspace_tools.go`
- Create: `server/service/seednote_capability.go`
- Create: `server/service/seednote_capability_test.go`
- Create: `server/service/media_pipeline.go`
- Create: `server/service/media_pipeline_test.go`
- Modify: `server/service/topic_pool.go`
- Modify: `server/service/topic_pool_test.go`
- Modify: `server/mcp/mcp_test.go`
- Modify: `server/mcp/seednote_tools_test.go`
- Modify: `server/mcp/live_slice_tools_test.go`
- Modify: `server/mcp/template_tools_test.go`
- Modify: `server/mcp/tools_test.go`

- [ ] **Step 1: Generate a complete handler inventory test**

Use `go/parser` to enumerate every function ending in `Handler` in production
MCP files. Maintain an explicit reviewed map:

```go
var reviewedMCPHandlers = map[string]string{
    "accountInfoHandler": "AgentProjectProfileService.Get",
    "addTopicHandler": "TopicPoolService.Add",
    "agentFeedbackSubmitHandler": "AgentFeedbackService.Create",
    "analyzeImageHandler": "WritingService.AnalyzeImageDetailed",
    "buildLiveClipManifestHandler": "LiveSliceService.BuildLiveClipManifest",
    "buildLiveClipPlanHandler": "LiveSliceService.BuildLiveClipPlan",
    "buildLiveSubjectClipPlanHandler": "LiveSliceService.BuildLiveSubjectClipPlan",
    "checkSeednoteLoginStatusHandler": "SeednoteCapabilityService.LoginStatus",
    "claimTopicHandler": "TopicPoolService.ClaimTopic",
    "completeLiveSubjectHandler": "LiveSliceService.CompleteLiveSubject",
    "compressImageHandler": "ImageService.CompressImage",
    "convertMarkdownHandler": "WritingService.ConvertMarkdown",
    "createLiveAnalysisTaskHandler": "LiveSliceService.CreateLiveAnalysisTask",
    "downloadImageHandler": "ImageService.DownloadImage",
    "exportSeednoteHandler": "SeednoteExportService.Export",
    "generateImageHandler": "TaskImageService.Generate",
    "getMediaPipelineStatusHandler": "MediaPipelineService.Status",
    "getResourceHandler": "ResourceCatalogService.Query",
    "getSeednoteFeedDetailHandler": "SeednoteCapabilityService.FeedDetail",
    "getSeednoteLoginQRCodeHandler": "SeednoteCapabilityService.LoginQRCode",
    "getSeednoteUserProfileHandler": "SeednoteCapabilityService.UserProfile",
    "getTemplateHandler": "TemplateService.GetByID",
    "listDraftsHandler": "PublishingService.ListDrafts",
    "listPublishedHandler": "PublishingService.ListPublished",
    "listResourcesHandler": "ResourceCatalogService.Query",
    "listTemplatesHandler": "TemplateService.List",
    "listTopicsHandler": "TopicPoolService.List",
    "planCreateHandler": "PlanService.Create",
    "planListHandler": "PlanService.List",
    "prepareFileUploadHandler": "FileUploadService.Prepare",
    "prepareWorkspaceHandler": "WorkspaceService.Prepare",
    "progressUpdateHandler": "TaskService.UpdateProgress",
    "projectGetHandler": "ProjectService.Get",
    "projectListHandler": "ProjectService.List",
    "publishDraftHandler": "PublishingService.PublishDraft",
    "queryLiveAnalysisTaskHandler": "LiveSliceService.QueryLiveAnalysisTask",
    "recognizeLiveInvalidSentencesHandler": "LiveSliceService.RecognizeLiveInvalidSentences",
    "recognizeLiveSegmentsHandler": "LiveSliceService.RecognizeLiveSegments",
    "recognizeLiveSubjectsHandler": "LiveSliceService.RecognizeLiveSubjects",
    "registerRenderedImageHandler": "TaskService.RegisterRenderedImage",
    "renderTemplateHandler": "WritingService.RenderTemplate",
    "saveTemplateHandler": "TemplateService.SaveGlobal",
    "scoreArticleHandler": "ArticleScoreService.Score",
    "searchSeednoteFeedsHandler": "SeednoteCapabilityService.SearchFeeds",
    "taskCancelHandler": "TaskService.CancelForUser",
    "taskFilesHandler": "TaskService.GetVisibleFiles",
    "taskGetHandler": "TaskService.GetByID",
    "taskListHandler": "TaskService.List",
    "titleFinalizeHandler": "TaskService.FinalizeTitle",
    "titleListHandler": "TaskService.ListTitles",
    "uploadImageHandler": "ImageService.UploadImage",
    "uploadLiveAudioHandler": "LiveSliceService.UploadLiveAudio",
}
```

Fail when a handler is missing from the inventory. For each handler, use AST
selectors to reject more than one direct application/service capability call
after protocol helper calls are excluded.

- [ ] **Step 2: Run the inventory test and verify RED**

Run: `go test ./server/mcp -run TestEveryMCPHandlerHasReviewedCapabilityBoundary -count=1`

Expected: FAIL with unreviewed and multi-capability handlers.

- [ ] **Step 3: Extract every remaining conditional business decision**

- move `claim_topic`'s task/no-task branch into one `TopicPoolService.ClaimTopic`
  request;
- wrap Seednote readiness plus remote client calls in one Seednote capability
  service method per tool;
- wrap file-upload preparation and media-pipeline status in one application
  capability each;
- keep live-slice parsing as protocol decoding but ensure each handler calls one
  `LiveSliceService` method;
- keep publishing, template, workspace, progress, and task commands when already
  one-service delegates.

Use typed requests rather than passing MCP argument maps into services.

- [ ] **Step 4: Remove workflow directives from every MCP tool description**

Descriptions state inputs, atomic effect, and result only. Remove instructions
such as “query until completed”, “retry upload”, “call X next”, quality gates,
fallback selection, or stop/continue behavior.

- [ ] **Step 5: Complete the explicit handler inventory and verify GREEN**

Run:

```bash
go test ./server/mcp -run 'TestEveryMCPHandlerHasReviewedCapabilityBoundary|TestMCPToolDescriptionsContainNoWorkflowDirectives' -count=1
```

Expected: PASS with every handler named in the inventory.

- [ ] **Step 6: Run package tests**

Run: `go test ./server/service ./server/mcp -count=1`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add server/mcp server/service server/services.go server/handlers.go
git commit -m "refactor(mcp): complete capability boundary audit"
```

### Task 7: Full Verification And Merge Readiness

**Files:**
- Verify all changed files
- Do not modify unrelated `docs/superpowers/plans/2026-07-22-managed-mcp-request-timeout.md`

- [ ] **Step 1: Format and check the diff**

Run:

```bash
changed_go_files=$(git diff --name-only --diff-filter=ACM -- '*.go')
test -z "$changed_go_files" || gofmt -w $changed_go_files
git diff --check
git status --short
```

Expected: no formatting errors; only scoped files plus the pre-existing
untracked plan appear.

- [ ] **Step 2: Run targeted packages**

Run:

```bash
go test ./server/service ./server/mcp ./server/agent -count=1
```

Expected: PASS.

- [ ] **Step 3: Run the complete Go suite**

Run: `go test ./... -count=1`

Expected: PASS.

- [ ] **Step 4: Build both Go binaries**

Run:

```bash
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
```

Expected: both commands exit 0.

- [ ] **Step 5: Prove removed contracts are absent**

Run:

```bash
rg -n 'verify_with_vision|verification_prompt|upload_to_cdn' plugins server/mcp
rg -n 'generate_image[^\n]*(operation_id)|operation_id[^\n]*generate_image' plugins server/mcp
rg -n 'selection_reason|response_type|output_mime|generation_attempts|width/height' plugins/agents/seednote.md plugins/skills/seednote-visual-design
```

Expected: no runtime plugin or MCP schema/handler references. Internal migration
notes or historical design docs are excluded from this assertion.

- [ ] **Step 6: Review public and transactional behavior**

Inspect `git diff --stat`, `git diff`, and the commits. Confirm:

- no tool unrelated to the boundary audit was removed;
- image task files and settlement remain atomic;
- duplicate semantic requests replay;
- all plugin callers use the new independent capabilities;
- both manifests are exactly `4.0.0`;
- no unrelated worktree changes were included.

- [ ] **Step 7: Run review-before-merge checks and integrate**

Review the branch against the merge base, resolve findings, rerun affected
tests, and merge into local `main` only after all required checks pass. Verify
the resulting local `main` contains every implementation commit and report
exactly which server/agent images require rebuilding from the final diff.
