# Server-Controlled Image Generation Timeout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the server own provider-attempt and complete `generate_image` deadlines, propagate cancellation into provider SDK calls, keep MCP transport alive with progress, and return stable timeout classifications.

**Architecture:** Add timeout policy to server semantic configuration, carry provider timeout through the resolved image config, and inject the complete operation timeout into MCP services. The MCP handler creates the outer deadline; `ImageService` creates bounded attempt contexts; `Processor` passes those contexts unchanged to the selected provider.

**Tech Stack:** Go 1.24, official MCP Go SDK, Volcengine/OpenAI/Gemini Go SDKs, Fiber server configuration, table-driven tests with blocking fake providers.

---

### Task 1: Add Server-Owned Timeout Configuration

**Files:**
- Modify: `server/config/config.go`
- Modify: `server/config/model_routes_config_test.go`
- Modify: `server/config.yaml`
- Modify: `server/config.example.yaml`

- [ ] **Step 1: Write failing config parsing/default/validation tests**

In `server/config/model_routes_config_test.go`, add tests that construct or load config and assert:

```go
func TestImageGenerationTimeoutDefaults(t *testing.T) {
	cfg := Config{}
	cfg.applyDefaults()
	if cfg.MCP.ToolTimeouts.GenerateImage != 10*time.Minute {
		t.Fatalf("generate_image timeout = %s, want 10m", cfg.MCP.ToolTimeouts.GenerateImage)
	}
	if cfg.ModelRoutes.ImageGeneration.Cover.Timeout != 5*time.Minute ||
		cfg.ModelRoutes.ImageGeneration.Content.Timeout != 5*time.Minute {
		t.Fatalf("image route timeouts = %s/%s, want 5m/5m",
			cfg.ModelRoutes.ImageGeneration.Cover.Timeout,
			cfg.ModelRoutes.ImageGeneration.Content.Timeout)
	}
}

func TestImageGenerationRouteTimeoutReachesRuntimeConfig(t *testing.T) {
	cfg := Config{
		ModelProviders: map[string]ModelProviderConfig{
			"volcengine_ark": {BaseURL: "https://ark.example.com", APIKey: "key"},
		},
		ModelRoutes: ModelRoutesConfig{
			ImageGeneration: ImageGenerationRoutesConfig{
				Cover: ImageGenerationRouteConfig{
					Provider: "volcengine_ark", Model: "seedream", Timeout: 2 * time.Minute,
				},
			},
		},
	}
	if err := cfg.deriveModelRouteRuntimeConfig(); err != nil {
		t.Fatalf("deriveModelRouteRuntimeConfig() error = %v", err)
	}
	if cfg.ImageAPI.Cover == nil || cfg.ImageAPI.Cover.TimeoutSec != 120 {
		t.Fatalf("cover runtime config = %#v, want timeout_sec 120", cfg.ImageAPI.Cover)
	}
}

func TestValidateRejectsImageTimeoutOutsideOperationBudget(t *testing.T) {
	cfg := baseKubernetesConfigForTest()
	cfg.MCP.ToolTimeouts.GenerateImage = 5 * time.Minute
	cfg.ModelRoutes.ImageGeneration.Cover = ImageGenerationRouteConfig{
		Provider: "volcengine_ark", Model: "seedream", Timeout: 5 * time.Minute,
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "mcp.tool_timeouts.generate_image") {
		t.Fatalf("Validate() error = %v, want timeout relationship error", err)
	}
}
```

- [ ] **Step 2: Run config tests and verify RED**

```bash
go test ./server/config -run 'TestImageGeneration(TimeoutDefaults|RouteTimeoutReachesRuntimeConfig)|TestValidateRejectsImageTimeoutOutsideOperationBudget' -count=1
```

Expected: compile failures for missing timeout fields.

- [ ] **Step 3: Implement config types, defaults, derivation, and validation**

Add:

```go
type MCPToolTimeoutsConfig struct {
	GenerateImage time.Duration `yaml:"generate_image"`
}

type MCPConfig struct {
	APIKey       string                `yaml:"api_key"`
	ToolTimeouts MCPToolTimeoutsConfig `yaml:"tool_timeouts"`
}
```

Add `Timeout time.Duration` to `ImageGenerationRouteConfig`. In `applyDefaults`, set zero image route timeouts to 5 minutes for cover, content, and every designer route, and set `MCP.ToolTimeouts.GenerateImage` to 10 minutes.

In `imageAPIFromRoute`, propagate:

```go
TimeoutSec: int(route.Timeout / time.Second),
```

Add `Timeout time.Duration` to the internal `ImageModelPreset` contract. In
`resolveImagePresetRoutes`, copy the resolved designer route timeout alongside
provider/model/capabilities:

```go
preset.Timeout = route.Timeout
```

In `presetToImageAPIConfig`, preserve the selected preset timeout when the
runtime cover/content configs are built:

```go
dst.TimeoutSec = int(p.Timeout / time.Second)
```

In `Validate`, reject non-positive image route timeouts and require:

```go
MCP.ToolTimeouts.GenerateImage > route.Timeout + ModelRoutes.ImageUnderstanding.Timeout
```

for every configured cover, content, and designer route. This guarantees one full provider attempt plus configured vision verification fits inside the operation budget.

- [ ] **Step 4: Update checked-in server configuration**

Add to both YAML files:

```yaml
mcp:
  api_key: "${ANBAN_MCP_API_KEY}"
  tool_timeouts:
    generate_image: 10m
```

Add `timeout: 5m` to cover, content, and each designer image-generation route.

- [ ] **Step 5: Run config tests and verify GREEN**

```bash
go test ./server/config -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit configuration policy**

```bash
git add server/config/config.go server/config/model_routes_config_test.go server/config.yaml server/config.example.yaml
git commit -m "feat(server): own image generation timeouts"
```

### Task 2: Propagate Context Through Processor

**Files:**
- Modify: `app/image/processor.go`
- Modify: `app/image/processor_test.go`
- Modify: `server/service/image.go`
- Modify: `server/service/image_test.go`

- [ ] **Step 1: Write a failing Processor cancellation test**

Add a provider that records the received context:

```go
type blockingContextProvider struct{}

func (blockingContextProvider) Name() string { return "blocking" }
func (blockingContextProvider) Capabilities() *ProviderCapabilities { return &ProviderCapabilities{} }
func (blockingContextProvider) Generate(ctx context.Context, _ string, _ *GenerateOptions) (*GenerateResult, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestProcessorGenerateRawPropagatesCancellation(t *testing.T) {
	p := newTestProcessor(&config.ImageAPI{Provider: "test", Key: "test-key"})
	p.provider = blockingContextProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.GenerateRaw(ctx, "prompt")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}
```

Update the existing metadata test call to `p.GenerateRaw(context.Background(), "春日饮茶")` so the intended API is explicit.

- [ ] **Step 2: Run the test and verify RED**

```bash
go test ./app/image -run 'TestProcessorGenerateRawPropagatesCancellation|TestProcessor_GenerateRawIncludesProviderMetadata' -count=1
```

Expected: compile failure because `GenerateRaw` does not accept context.

- [ ] **Step 3: Make Processor generation context-aware**

Change signatures and remove both `context.Background()` calls:

```go
func (p *Processor) GenerateRaw(ctx context.Context, prompt string) (*GenerateRawResult, error)
func (p *Processor) GenerateRawWithSize(ctx context.Context, prompt, size string) (*GenerateRawResult, error)
```

Pass `ctx` directly to `Provider.Generate`. Update `server/service/image.go` closures:

```go
rawResult, err := s.generateWithRetry(ctx, providerAttemptTimeout(resolved, imageType), func(attemptCtx context.Context) (*image.GenerateRawResult, error) {
	if size != "" {
		return processor.GenerateRawWithSize(attemptCtx, prompt, size)
	}
	return processor.GenerateRaw(attemptCtx, prompt)
}, imageType)
```

In the same change, update every existing `generateWithRetry` test closure in
`server/service/image_test.go` from `func()` to `func(context.Context)` and pass
the attempt timeout argument. This keeps the repository compiling at the Task 2
checkpoint:

```go
got, err := svc.generateWithRetry(context.Background(), 5*time.Minute, gen, "cover")
```

- [ ] **Step 4: Run image package tests and verify GREEN**

```bash
go test ./app/image ./server/service -run 'TestProcessor|TestImage' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit context propagation**

```bash
git add app/image/processor.go app/image/processor_test.go server/service/image.go
git commit -m "fix(image): propagate generation cancellation"
```

### Task 3: Bound Provider Attempts and Retry Budget

**Files:**
- Modify: `server/service/image.go`
- Modify: `server/service/image_test.go`

- [ ] **Step 1: Write failing retry/deadline tests**

Add these tests around `generateWithRetry` using millisecond deadlines:

```go
func TestGenerateWithRetryStopsOnCallerCancellation(t *testing.T) {
	logger := zerolog.Nop()
	svc := &ImageService{logger: &logger, imageRetry: &imageRetryConfig{
		MaxAttempts: 3,
		Backoffs: []time.Duration{0, time.Millisecond, time.Millisecond},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	_, err := svc.generateWithRetry(ctx, 20*time.Millisecond, func(context.Context) (*image.GenerateRawResult, error) {
		calls++
		return nil, context.Canceled
	}, "cover")
	if !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("error/calls = %v/%d, want canceled/0", err, calls)
	}
}

func TestGenerateWithRetryDoesNotStartAttemptWithoutBudget(t *testing.T) {
	logger := zerolog.Nop()
	svc := &ImageService{logger: &logger, imageRetry: &imageRetryConfig{
		MaxAttempts: 3,
		Backoffs: []time.Duration{0, 10 * time.Millisecond},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	calls := 0
	_, _ = svc.generateWithRetry(ctx, 20*time.Millisecond, func(context.Context) (*image.GenerateRawResult, error) {
		calls++
		return nil, &image.GenerateError{Code: "network_error", Message: "temporary"}
	}, "cover")
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}
```

- [ ] **Step 2: Run tests and verify RED**

```bash
go test ./server/service -run 'TestGenerateWithRetry(StopsOnCallerCancellation|DoesNotStartAttemptWithoutBudget)' -count=1
```

Expected: compile failure for the new callback/timeout signature.

- [ ] **Step 3: Implement bounded attempts**

Change `generateWithRetry` to accept `attemptTimeout time.Duration` and `gen func(context.Context)`. Before each retry, calculate whether `backoff + attemptTimeout` fits the outer deadline. For each call:

```go
attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
result, err = gen(attemptCtx)
attemptErr := attemptCtx.Err()
cancel()
if attemptErr != nil && ctx.Err() == nil {
	err = fmt.Errorf("provider attempt %d timed out after %s: %w", attempt+1, attemptTimeout, attemptErr)
}
```

Return `context.Canceled` immediately without retry. Keep same-provider transient retry behavior for fast network/rate-limit failures while budget remains.

Update `Test_isTransientImageError` so `context.Canceled` expects `false` while
`context.DeadlineExceeded` remains transient. Cancellation is terminal and must
never enter retry policy.

Add `providerAttemptTimeout(resolved, imageType)` that reads `TimeoutSec` from `resolved.Config.Cover` or `.Content` and falls back to 5 minutes only for defensive nil/legacy test descriptors.

- [ ] **Step 4: Run service tests and verify GREEN**

```bash
go test ./server/service -run 'TestGenerateWithRetry|TestImageService' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit retry budgeting**

```bash
git add server/service/image.go server/service/image_test.go
git commit -m "fix(image): bound provider retry budget"
```

### Task 4: Enforce the MCP Operation Deadline

**Files:**
- Modify: `server/mcp/tools.go`
- Modify: `server/mcp/image_tools.go`
- Modify: `server/mcp/image_tools_test.go`
- Modify: `server/mcp/image_gen_diag_test.go`
- Modify: `server/main.go`

- [ ] **Step 1: Make the fake generator observe context**

Extend `fakeImageGenerator` with `waitForContext bool` and `ctxErr error`. Name the first parameter `ctx` and add:

```go
if f.waitForContext {
	<-ctx.Done()
	f.ctxErr = ctx.Err()
	return nil, ctx.Err()
}
```

- [ ] **Step 2: Write failing operation timeout and cancellation tests**

Add this helper and handler tests with real task/project rows and `Services.GenerateImageTimeout = 20 * time.Millisecond`:

```go
func setupTimedGenerateImageHandlerTest(t *testing.T) (context.Context, string, string, *mcp.CallToolRequest, *fakeImageModelResolver, *service.TaskService) {
	t.Helper()
	db := repositoryTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	logger := zerolog.New(io.Discard)
	userID := "user-image-timeout"
	projectID := "project-image-timeout"
	taskID := "task-image-timeout"
	if err := repo.Projects().Create(ctx, &model.Project{
		ID: projectID, UserID: userID, Platform: model.PlatformSeednote, Name: "Seednote",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID: taskID, UserID: userID, ProjectID: projectID,
		Type: model.PlatformSeednote, Status: model.TaskStatusRunning,
	}); err != nil {
		t.Fatal(err)
	}
	resolver := &fakeImageModelResolver{resolved: &service.ResolvedImageModel{
		Provider: "volcengine", Model: "seedream", SelectionReason: "preferred",
	}}
	request := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(fmt.Sprintf(`{
		"project_id": %q,
		"task_id": %q,
		"prompt": "cover",
		"output_path": "output/cover.png",
		"image_type": "cover"
	}`, projectID, taskID))}}
	return ctx, userID, taskID, request, resolver,
		service.NewTaskService(repo, nil, nil, nil, nil, &logger, "", nil, "", nil, nil)
}

func TestGenerateImageHandlerReturnsOperationTimeout(t *testing.T) {
	oldSvcs := svcs
	t.Cleanup(func() { svcs = oldSvcs })
	ctx, userID, _, request, resolver, taskSvc := setupTimedGenerateImageHandlerTest(t)
	generator := &fakeImageGenerator{waitForContext: true}
	svcs = &Services{
		TaskSvc: taskSvc, ImageSvc: &service.ImageService{},
		ImageModelResolver: resolver, ImageGenerationBiller: &fakeImageGenerationBiller{},
		ImageGenerator: generator, GenerateImageTimeout: 20 * time.Millisecond,
	}
	res, err := generateImageHandler(withMCPUserID(ctx, userID), request)
	if err != nil || !res.IsError || !strings.Contains(callToolText(res), `"code":"operation_timeout"`) {
		t.Fatalf("result/error = %#v/%v text=%s", res, err, callToolText(res))
	}
	if !errors.Is(generator.ctxErr, context.DeadlineExceeded) {
		t.Fatalf("generator context error = %v", generator.ctxErr)
	}
}

func TestGenerateImageHandlerClassifiesCallerCancellation(t *testing.T) {
	oldSvcs := svcs
	t.Cleanup(func() { svcs = oldSvcs })
	_, userID, _, request, resolver, taskSvc := setupTimedGenerateImageHandlerTest(t)
	generator := &fakeImageGenerator{waitForContext: true}
	svcs = &Services{
		TaskSvc: taskSvc, ImageSvc: &service.ImageService{},
		ImageModelResolver: resolver, ImageGenerationBiller: &fakeImageGenerationBiller{},
		ImageGenerator: generator, GenerateImageTimeout: time.Minute,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := generateImageHandler(withMCPUserID(ctx, userID), request)
	if err != nil || !res.IsError || !strings.Contains(callToolText(res), `"code":"request_cancelled"`) {
		t.Fatalf("result/error = %#v/%v text=%s", res, err, callToolText(res))
	}
}
```

Add a focused diagnostic test proving a provider deadline while the outer
context remains live maps to `provider_timeout`:

```go
func TestClassifyImageToolFailureProviderTimeout(t *testing.T) {
	parentCtx := context.Background()
	operationCtx, cancel := context.WithTimeout(parentCtx, time.Minute)
	defer cancel()
	failure := classifyImageToolFailure(parentCtx, operationCtx, context.DeadlineExceeded, "generate", "volcengine", "seedream", time.Minute, false)
	if failure.Code != "provider_timeout" {
		t.Fatalf("failure code = %q, want provider_timeout", failure.Code)
	}
}
```

- [ ] **Step 3: Run MCP tests and verify RED**

```bash
go test ./server/mcp -run 'TestGenerateImageHandler(ReturnsOperationTimeout|ClassifiesCallerCancellation)' -count=1
```

Expected: compile failure for missing `GenerateImageTimeout`, then incorrect unstructured error until implementation.

- [ ] **Step 4: Wire server timeout into MCP services**

Add to `Services`:

```go
GenerateImageTimeout time.Duration
```

Set it in `server/main.go`:

```go
GenerateImageTimeout: cfg.MCP.ToolTimeouts.GenerateImage,
```

- [ ] **Step 5: Add operation context, heartbeat, and stable error payloads**

At the beginning of `generateImageHandler`, derive the operation context and start progress:

```go
parentCtx := ctx
ctx, cancel := context.WithTimeout(ctx, svcs.GenerateImageTimeout)
defer cancel()
stopHeartbeat := startProgressHeartbeat(ctx, req.Session, req.Params.GetProgressToken(), "generate_image", 15*time.Second)
defer stopHeartbeat()
```

Guard nil `req`/`req.Params` before reading the token. Add the structured
failure type and helpers:

```go
type imageToolFailure struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Stage      string `json:"stage"`
	Provider   string `json:"provider,omitempty"`
	Model      string `json:"model,omitempty"`
	TimeoutMS  int64  `json:"timeout_ms,omitempty"`
	Durable    bool   `json:"durable_task_file"`
}

func imageFailureResult(failure imageToolFailure) *mcp.CallToolResult {
	payload, err := json.Marshal(failure)
	if err != nil {
		return errorResult(failure.Message)
	}
	return errorResult(string(payload))
}

func classifyImageToolFailure(parentCtx, operationCtx context.Context, err error, stage, provider, model string, timeout time.Duration, durable bool) imageToolFailure {
	code := categorizeImageGenFailure(err, "")
	switch {
	case errors.Is(parentCtx.Err(), context.Canceled):
		code = "request_cancelled"
	case errors.Is(operationCtx.Err(), context.DeadlineExceeded):
		code = "operation_timeout"
	case errors.Is(err, context.DeadlineExceeded):
		code = "provider_timeout"
	}
	return imageToolFailure{
		Code: code, Message: err.Error(), Stage: stage,
		Provider: provider, Model: model,
		TimeoutMS: timeout.Milliseconds(), Durable: durable,
	}
}
```

Classify in this order:

```go
switch {
case errors.Is(parentCtx.Err(), context.Canceled):
	code = "request_cancelled"
case errors.Is(ctx.Err(), context.DeadlineExceeded):
	code = "operation_timeout"
case errors.Is(err, context.DeadlineExceeded):
	code = "provider_timeout"
default:
	code = categorizeImageGenFailure(err, refPathForService)
}
```

Before registration, settlement, verification, and upload, check `ctx.Err()` and return the structured terminal result. Track `durableTaskFile = true` immediately after successful task-file registration so a later timeout tells the Agent to reuse the asset rather than regenerate it.

- [ ] **Step 6: Run MCP tests and verify GREEN**

```bash
go test ./server/mcp -run 'TestGenerateImage|TestCategorizeImageGenFailure|TestStartProgressHeartbeat' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit MCP enforcement**

```bash
git add server/mcp/tools.go server/mcp/image_tools.go server/mcp/image_tools_test.go server/mcp/image_gen_diag_test.go server/main.go
git commit -m "fix(mcp): enforce server image deadlines"
```

### Task 5: Verify End to End

**Files:**
- Verify all modified Go and YAML files

- [ ] **Step 1: Format modified Go files**

```bash
gofmt -w app/image/processor.go app/image/processor_test.go server/config/config.go server/config/model_routes_config_test.go server/service/image.go server/service/image_test.go server/mcp/tools.go server/mcp/image_tools.go server/mcp/image_tools_test.go server/mcp/image_gen_diag_test.go server/main.go
```

- [ ] **Step 2: Run targeted packages**

```bash
go test ./app/image ./server/config ./server/service ./server/mcp -count=1
```

Expected: PASS.

- [ ] **Step 3: Run repository-wide verification**

```bash
go test ./...
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
```

Expected: all commands exit 0.

- [ ] **Step 4: Audit the final diff**

```bash
git status --short
git diff origin/main...HEAD -- app/image server/config server/service/image.go server/mcp server/main.go
```

Confirm:

- no `MCP_TOOL_TIMEOUT` or business timeout was added to Agent/client configuration;
- no `context.Background()` remains in Processor generation paths;
- every configured image route has a server timeout;
- heartbeat only preserves transport liveness;
- timeout results use `provider_timeout`, `operation_timeout`, or `request_cancelled`;
- unrelated `claudecode` and Studio changes are untouched.
