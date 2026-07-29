# Server 内部大模型边界实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 删除误导性的 Server 文本生成路径，将唯一的同步意图解析配置改为 `server_internal`，把文本推理迁回 Claude Agent，同时保留独立、可计费的图片和视频理解 MCP。

**Architecture:** Server 只持有通用 OpenAI-compatible 客户端、一个仅供 AI 入口使用的 `server_internal` 客户端，以及相互隔离的图片/视频理解客户端。文章渲染保持确定性，爆款分析和直播语义判断进入托管 Agent 生命周期；Seednote 发布追踪只接受明确的外部笔记身份，不再猜测。整个变更按配置、Service、MCP、任务生命周期、Studio、插件契约的顺序原子切换，不保留 `writing` 或已删除工具的兼容入口。

**Tech Stack:** Go 1.x、Fiber v3、GORM、MCP Go SDK、Asynq、React 19、TypeScript、Vite 8、Vitest、Claude Code/Codex 插件清单

---

## 文件结构与职责

- `server/service/model_client.go`：从旧 `writing.go` 拆出的通用 OpenAI-compatible 文本/图片/原生视频客户端与 usage 结果类型。
- `server/service/content_render.go`：只保留 Markdown 转换、主题解析等确定性渲染职责；不持有 LLM、用户模型配置或超时。
- `server/service/render_template.go`：接收 `ContentRenderService`，继续负责确定性模板布局。
- `server/service/task_image_operations.go`：图片理解的授权、输入加载、专用路由调用和 provider cost 记录。
- `server/service/task_video_operations.go`：新增视频来源授权、持久化任务文件解析、原生视频调用和 provider cost 记录。
- `server/mcp/image_tools.go`、`server/mcp/video_understanding_tools.go`：分别暴露原子的 `analyze_image`、`analyze_video`。
- `server/service/ai_entry.go`：唯一使用 `server_internal` 的业务调用方，严格解析意图并记录 provider usage。
- `server/service/live_slice.go`：只保留 TingWu 和确定性切片计划，不再持有文本模型。
- `server/service/viral_analysis.go`：收缩为历史只读查询；新爆款分析由标准 `Task` 生命周期执行。
- `server/service/seednote_tracking.go`：只按明确 `note_id`/`note_url` 绑定并采集，不再发现和猜测。
- `studio/src/components/settings/ModelConfigSection.tsx`：只配置用户图片生成覆盖。
- `plugins/agents/live-slicer.{md,toml}`、`plugins/skills/live-slice/SKILL.md`：由 Agent 直接生成直播语义 JSON。
- `plugins/agents/seednote.{md,toml}`：为 `viral_analysis` 增加只做证据拆解、不继续写笔记的明确分支。

### Task 1: 将 `writing` 配置替换为 Server 内部路由

**Files:**
- Modify: `server/config/config.go`
- Modify: `server/config/model_routes_config_test.go`
- Modify: `server/config/live_slice_config_test.go`
- Modify: `server/config.example.yaml`
- Modify: `server/config.yaml`

- [ ] **Step 1: 先写配置派生与旧键拒绝测试**

在 `server/config/model_routes_config_test.go` 将现有 `TestSemanticModelConfigDerivesRuntimeRoutes` fixture 中的 `writing:` 改成 `server_internal:`，并把最终断言改成：

```go
if cfg.ServerInternal.Model != "kimi-k2.7-code" || cfg.ServerInternal.BaseURL != "https://api.moonshot.cn/v1" {
	t.Fatalf("derived server internal config = %#v", cfg.ServerInternal)
}
```

再新增完整的旧键失败用例（复用同文件现有 `fakePluginDir`）：

```go
func TestSemanticModelConfigRejectsWritingRoute(t *testing.T) {
	dir := t.TempDir()
	pluginDir := fakePluginDir(t, dir)
	cfgPath := filepath.Join(dir, "config.yaml")
	body := []byte(`
server: {}
database:
  dsn: "user:pass@tcp(localhost:3306)/creator"
jwt:
  secret_key: test-secret
model_routes:
  writing:
    provider: moonshot
    model: kimi-k2.7-code
claude:
  plugin_dir: "` + pluginDir + `"
`)
	if err := os.WriteFile(cfgPath, body, 0o644); err != nil { t.Fatal(err) }
	_, err := NewConfig(cfgPath)
	if err == nil || !strings.Contains(err.Error(), "model_routes.writing") {
		t.Fatalf("error = %v, want deprecated model_routes.writing rejection", err)
	}
}
```

在同文件增加 `TestSemanticModelConfigRejectsNonNativeVideoUnderstandingRoute`：fixture 配置非空 `video_understanding` 但 `require_native_video: false`，断言 `NewConfig` 错误固定包含 `model_routes.video_understanding.require_native_video must be true`。这防止新工具在启动时接受一个只支持文本/图片的伪视频路由。

同时把 `server/config/live_slice_config_test.go` 中 `cfg.Writing.Timeout` 的默认值断言删除；直播切片不再借用文本配置。

- [ ] **Step 2: 运行配置测试并确认先失败**

Run: `go test ./server/config -run 'TestSemanticModelConfig(DerivesRuntimeRoutes|RejectsWritingRoute|RejectsNonNativeVideoUnderstandingRoute)' -count=1`

Expected: FAIL，提示 `ServerInternal` 尚不存在或旧 `model_routes.writing` 仍被接受。

- [ ] **Step 3: 实现严格配置切换**

在 `server/config/config.go` 做以下结构替换：

```go
type Config struct {
	Server             ServerConfig                    `yaml:"server"`
	Logging            LoggingConfig                   `yaml:"logging"`
	Database           DatabaseConfig                  `yaml:"database"`
	Redis              RedisConfig                     `yaml:"redis"`
	JWT                JWTConfig                       `yaml:"jwt"`
	WeChat             WeChatConfig                    `yaml:"wechat"`
	Storage            StorageConfig                   `yaml:"storage"`
	MCP                MCPConfig                       `yaml:"mcp"`
	ImageAPI           ImageAPIConfig                  `yaml:"-"`
	Montage            MontageConfig                   `yaml:"montage"`
	ImagePresets       []ImageModelPreset              `yaml:"image_presets"`
	ModelProviders     map[string]ModelProviderConfig  `yaml:"model_providers"`
	ModelRoutes        ModelRoutesConfig               `yaml:"model_routes"`
	BillingRuntime     BillingRuntimeConfig            `yaml:"billing_runtime" json:"billing_runtime"`
	BillingBundle      *serverbilling.Bundle           `yaml:"-" json:"-"`
	ServerInternal     ModelRuntimeConfig              `yaml:"-"`
	ImageUnderstanding UnderstandingRuntimeConfig      `yaml:"-"`
	VideoUnderstanding VideoUnderstandingRuntimeConfig `yaml:"-"`
	TingWu             TingWuConfig                    `yaml:"tingwu"`
	Claude             ClaudeConfig                    `yaml:"claude"`
	CORS               CORSConfig                      `yaml:"cors"`
	Asynq              AsynqConfig                     `yaml:"asynq"`
	Email              EmailConfig                     `yaml:"email"`
	Invitation         InvitationConfig                `yaml:"invitation"`
	Seednote           SeednoteConfig                  `yaml:"seednote"`
	Ilink              IlinkConfig                     `yaml:"ilink"`
	Memory             MemoryConfig                    `yaml:"memory"`
}

type ModelRoutesConfig struct {
	ServerInternal     RouteConfig                   `yaml:"server_internal"`
	ImageUnderstanding UnderstandingRouteConfig      `yaml:"image_understanding"`
	VideoUnderstanding VideoUnderstandingRouteConfig `yaml:"video_understanding"`
	ImageGeneration    ImageGenerationRoutesConfig   `yaml:"image_generation"`
}

type ModelRuntimeConfig struct {
	BaseURL     string
	Key         string
	Model       string
	Provider    string
	ProviderKey string
	Timeout     time.Duration
}
```

删除顶层 `Writing WritingConfig`、`WritingConfig`、已经无调用方的 `Vision VisionConfig` 类型及两者默认值。保留 `ImageAPI` 作为由 `model_routes.image_generation` 派生的内部运行时结构，但将其 YAML tag 改为 `yaml:"-"`；旧顶层 `image_api` 仍由启动校验明确拒绝。`deriveModelRouteRuntimeConfig` 使用同一个 provider resolver 派生：

```go
if route := c.ModelRoutes.ServerInternal; route.Model != "" || route.Provider != "" {
	p, err := provider("model_routes.server_internal", route.Provider)
	if err != nil { return err }
	c.ServerInternal = ModelRuntimeConfig{
		BaseURL: p.BaseURL, Key: p.APIKey, Model: route.Model,
		Provider: providerKind(route.Provider), ProviderKey: route.Provider,
		Timeout: route.Timeout,
	}
}
```

在 `rejectDeprecatedConfigKeys` 的 `model_routes` 检查中显式拒绝 `writing`，错误文案固定包含：

```go
if _, exists := modelRoutes["writing"]; exists {
	return fmt.Errorf("deprecated config key model_routes.writing; use model_routes.server_internal")
}
```

并将允许的语义路由限定为 `server_internal`、`image_understanding`、`video_understanding`、`image_generation`，让拼错的路由同样启动失败。

`Validate` 在 `video_understanding` route 配置了 provider/model 时要求 `RequireNativeVideo == true`；route 完全未配置时仍允许 Server 启动，只让 `analyze_video` 返回专用 route unavailable。

- [ ] **Step 4: 更新两份运行配置**

在 `server/config.example.yaml` 和 `server/config.yaml` 中做同样的替换：

```yaml
model_routes:
  server_internal:
    provider: "moonshot"
    model: "kimi-k2.7-code"
    timeout: 5m
```

保留 `image_understanding`、`video_understanding` 和所有图片生成路由原样。

- [ ] **Step 5: 运行配置包测试**

Run: `go test ./server/config -count=1`

Expected: PASS；测试同时证明旧顶层 `writing` 和旧 `model_routes.writing` 都不可用。

- [ ] **Step 6: 提交配置切换**

```bash
git add server/config/config.go server/config/model_routes_config_test.go server/config/live_slice_config_test.go server/config.example.yaml server/config.yaml
git commit -m "refactor(config): replace writing route with server internal"
```

### Task 2: 拆分通用模型客户端、确定性渲染与 AI 入口

**Files:**
- Create: `server/service/model_client.go`
- Create: `server/service/model_json.go`
- Create: `server/service/model_json_test.go`
- Create: `server/service/content_render.go`
- Create: `server/service/content_render_test.go`
- Delete: `server/service/writing.go`
- Delete: `server/service/writing_test.go`
- Delete: `server/service/writing_convert_test.go`
- Modify: `server/service/render_template.go`
- Modify: `server/service/render_template_test.go`
- Modify: `server/service/ai_entry.go`
- Modify: `server/service/ai_entry_test.go`
- Modify: `server/service/provider_cost.go`
- Modify: `server/service/provider_cost_test.go`
- Modify: `server/mcp/tools.go`
- Create: `server/mcp/content_render_tools.go`
- Create: `server/mcp/content_render_tools_test.go`
- Delete: `server/mcp/writing_tools.go`
- Delete: `server/mcp/writing_tools_test.go`
- Modify: `server/mcp/boundary_contract_test.go`
- Modify: `server/main.go`

- [ ] **Step 1: 写 AI 入口只使用注入客户端的失败测试**

在 `server/service/ai_entry_test.go` 增加 usage-aware fake，并替换所有用户覆盖测试：

```go
type fakeIntentModel struct {
	responses []string
	calls     int
	usage     srvconfig.TokenUsage
	missingUsage bool
}

func (f *fakeIntentModel) CompleteResult(context.Context, string, string) (*LLMResult, error) {
	usage := f.usage
	if !f.missingUsage && usage.TotalTokens == 0 && usage.InputTokens == 0 && usage.OutputTokens == 0 {
		usage = srvconfig.TokenUsage{InputTokens: 10, OutputTokens: 4, TotalTokens: 14}
	}
	result := &LLMResult{
		Text: f.responses[f.calls], Model: "kimi-k2.7-code",
		Usage: usage,
	}
	f.calls++
	return result, nil
}

type fakeProviderTokenCostRecorder struct {
	reconciled   []RecordProviderTokenCostRequest
	unreconciled []RecordProviderTokenUnreconciledRequest
}

func (f *fakeProviderTokenCostRecorder) CatalogID() string { return "catalog" }
func (f *fakeProviderTokenCostRecorder) RecordProviderTokenUsage(_ context.Context, req RecordProviderTokenCostRequest) (*model.BillingProviderCostEvent, error) {
	f.reconciled = append(f.reconciled, req)
	return &model.BillingProviderCostEvent{}, nil
}
func (f *fakeProviderTokenCostRecorder) RecordProviderTokenUnreconciled(_ context.Context, req RecordProviderTokenUnreconciledRequest) (*model.BillingProviderCostEvent, error) {
	f.unreconciled = append(f.unreconciled, req)
	return &model.BillingProviderCostEvent{}, nil
}

func TestAIEntryRequiresServerInternalClientBeforeTaskCreation(t *testing.T) {
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	logger := zerolog.New(io.Discard)
	svc := NewAIEntryService(repo, taskSvc, nil, nil, AIEntryModelConfig{}, &logger)
	result, err := svc.Submit(context.Background(), AIEntrySubmitRequest{
		UserID: userID, ProjectID: projectID, Channel: "studio",
		ExecutionProfile: "cost_effective", Text: "写一篇新品文章",
	})
	if err != nil { t.Fatal(err) }
	if result.Status != AIEntryStatusNeedsConfiguration || result.ActionURL != "/settings" {
		t.Fatalf("result = %#v", result)
	}
	count, err := repo.Tasks().CountByUserID(context.Background(), userID, "", "")
	if err != nil || count != 0 { t.Fatalf("task count = %d, err=%v", count, err) }
}

func TestAIEntryRepairsInvalidJSONWithSameServerInternalClient(t *testing.T) {
	llm := &fakeIntentModel{responses: []string{"not-json", `{"prompt":"整理成文章"}`}}
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	logger := zerolog.New(io.Discard)
	svc := NewAIEntryService(repo, taskSvc, llm, nil, AIEntryModelConfig{}, &logger)
	result, err := svc.Submit(context.Background(), AIEntrySubmitRequest{
		UserID: userID, ProjectID: projectID, Channel: "studio",
		ExecutionProfile: "cost_effective", Text: "整理成文章",
	})
	if err != nil || result.Status != AIEntryStatusCreated { t.Fatalf("result=%#v err=%v", result, err) }
	if llm.calls != 2 { t.Fatalf("calls = %d, want 2", llm.calls) }
}

func TestAIEntryRecordsEverySuccessfulServerInternalResponse(t *testing.T) {
	llm := &fakeIntentModel{responses: []string{"not-json", `{"prompt":"整理成文章"}`}}
	costs := &fakeProviderTokenCostRecorder{}
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	logger := zerolog.New(io.Discard)
	svc := NewAIEntryService(repo, taskSvc, llm, costs, AIEntryModelConfig{ProviderKey: "moonshot", Model: "kimi-k2.7-code"}, &logger)
	_, err := svc.Submit(context.Background(), AIEntrySubmitRequest{
		UserID: userID, ProjectID: projectID, Channel: "studio", ExecutionProfile: "cost_effective", Text: "整理成文章",
	})
	if err != nil { t.Fatal(err) }
	if len(costs.reconciled) != 2 || len(costs.unreconciled) != 0 { t.Fatalf("costs=%#v/%#v", costs.reconciled, costs.unreconciled) }
	for _, req := range costs.reconciled {
		if req.TaskID != "" || req.Provider != "moonshot" || req.Model != "kimi-k2.7-code" || req.Usage.Input != 10 {
			t.Fatalf("cost request = %#v", req)
		}
	}
}
```

删除 `SetModelConfigService`、`llmForUser` 相关断言，并增加缺 usage 用例：

```go
func TestAIEntryRecordsMissingUsageAsUnreconciled(t *testing.T) {
	llm := &fakeIntentModel{responses: []string{`{"prompt":"整理成文章"}`}, missingUsage: true}
	costs := &fakeProviderTokenCostRecorder{}
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	logger := zerolog.New(io.Discard)
	svc := NewAIEntryService(repo, taskSvc, llm, costs, AIEntryModelConfig{ProviderKey: "moonshot", Model: "kimi-k2.7-code"}, &logger)
	_, err := svc.Submit(context.Background(), AIEntrySubmitRequest{
		UserID: userID, ProjectID: projectID, Channel: "studio", ExecutionProfile: "cost_effective", Text: "整理成文章",
	})
	if err != nil { t.Fatal(err) }
	if len(costs.reconciled) != 0 || len(costs.unreconciled) != 1 { t.Fatalf("costs=%#v/%#v", costs.reconciled, costs.unreconciled) }
	req := costs.unreconciled[0]
	if req.TaskID != "" || req.Provider != "moonshot" || req.Model != "kimi-k2.7-code" || req.ReasonCode != model.BillingExecutionCostReasonMissingProviderUsage {
		t.Fatalf("unreconciled request = %#v", req)
	}
}
```

这两组测试共同证明每次成功 provider response 记录 `ProviderKey`、model、usage，且不要求已有 task ID。

- [ ] **Step 2: 运行 AI 入口测试并确认失败**

Run: `go test ./server/service -run '^TestAIEntry(RequiresServerInternalClientBeforeTaskCreation|RepairsInvalidJSONWithSameServerInternalClient|Records)' -count=1`

Expected: FAIL，因为入口仍读取用户 `writing` 覆盖且 `LLMClient` 不暴露 usage。

- [ ] **Step 3: 把模型客户端从 `writing.go` 移到通用文件**

将 `LLMClient`、`LLMResult`、`openaiLLMClient`、图片和原生视频 content-part 实现，以及 `NewOpenAILLMClient` 原样迁到 `server/service/model_client.go`。把注释改成通用模型客户端，并增加可记录 usage 的窄接口：

```go
type LLMClient interface {
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
	CompleteWithImage(ctx context.Context, systemPrompt, userPrompt, imageURL string) (string, error)
}

type ResultLLMClient interface {
	CompleteResult(ctx context.Context, systemPrompt, userPrompt string) (*LLMResult, error)
}
```

不改变供应商协议实现；图片和视频能力仍通过各自的 result interface 做运行时能力检查。把 AI entry、Task 7 前的直播语义解析以及 Task 9 前的 legacy 爆款分析仍在使用的 `extractJSONObject` 及其严格 bracket/string 解析 helper 移到 `server/service/model_json.go`，把对象解析用例移到 `model_json_test.go`。删除没有生产调用方的 `extractJSONArray`、lenient array 测试和 `writing_test.go`；不得把已删除直播生成逻辑需要的数组解析器继续保留为兼容代码。

- [ ] **Step 4: 把确定性渲染改为 `ContentRenderService`**

将 `WritingService` 中的 `resolveEffectiveTheme`、`ConvertMarkdown` 移到 `server/service/content_render.go`，并让 `render_template.go` 的 receiver 同步改名：

```go
type ContentRenderService struct {
	repo   repository.Repository
	logger *zerolog.Logger
}

func NewContentRenderService(repo repository.Repository, logger *zerolog.Logger) *ContentRenderService {
	return &ContentRenderService{repo: repo, logger: logger}
}
```

删除 `llmClient`、`llmTimeout`、`modelConfigSvc`、`writersDir`、`visionClient`、两个 understanding client 及所有 setter。把 `writing_convert_test.go` 重命名为 `content_render_test.go`，删除 `diagnosticLLM`、`callCountOf` 和同步/时间依赖，`setupConvertTest(t)` 只构造 `NewContentRenderService(repo, &logger)`；`render_template_test.go` 同步改用新类型。完成迁移后删除 `server/service/writing.go`；文件名和类型都不再暗示 Server 写作。

将 MCP `Services.WritingSvc` 改为：

```go
ContentRenderSvc *service.ContentRenderService
```

`convertMarkdownHandler` 与 `renderTemplateHandler` 只调用该字段，并同步更新边界表中的 capability 名称。

- [ ] **Step 5: 让 AI 入口固定使用 `server_internal` 并记录 usage**

`AIEntryService` 只保留一个 `ResultLLMClient`，加上 provider cost 依赖与固定路由描述：

```go
type AIEntryModelConfig struct {
	ProviderKey string
	Model       string
}

type AIEntryService struct {
	repo            repository.Repository
	taskSvc         *TaskService
	llm             ResultLLMClient
	costs           ProviderTokenCostRecorder
	config          AIEntryModelConfig
	logger          *zerolog.Logger
	referenceAssets *ReferenceAssetService
}

type ProviderTokenCostRecorder interface {
	CatalogID() string
	RecordProviderTokenUsage(context.Context, RecordProviderTokenCostRequest) (*model.BillingProviderCostEvent, error)
	RecordProviderTokenUnreconciled(context.Context, RecordProviderTokenUnreconciledRequest) (*model.BillingProviderCostEvent, error)
}

func NewAIEntryService(
	repo repository.Repository,
	taskSvc *TaskService,
	llm ResultLLMClient,
	costs ProviderTokenCostRecorder,
	cfg AIEntryModelConfig,
	logger *zerolog.Logger,
) *AIEntryService
```

先在 `server/service/provider_cost.go` 增加非媒体专用的未知 token 成本入口，不能复用 `RecordMediaUnreconciled`：

```go
type RecordProviderTokenUnreconciledRequest struct {
	TaskID, Provider, Model, ProviderRequestID string
	ReasonCode model.BillingExecutionCostReasonCode
}

func (s *ProviderCostService) RecordProviderTokenUnreconciled(
	ctx context.Context,
	req RecordProviderTokenUnreconciledRequest,
) (*model.BillingProviderCostEvent, error)
```

它使用 `kind=token_unreconciled` evidence、active catalog 和 provider-request idempotency，状态为 `unreconciled`；`provider_cost_test.go` 断言空 TaskID 可记录且不会伪造成零成本。

`parseIntent` 的首次调用和最多一次 JSON repair 都调用同一个 `CompleteResult`。每次 provider 调用分别使用新的 UUID 生成 `internal:server_internal:<uuid>` 幂等身份并记录 usage；provider 返回成功但 usage 缺失时写 token-unreconciled。请求本身失败时保留原错误，不伪造一次已发生的 usage 响应。若客户端为空，`Submit` 在创建任务前返回稳定结果：

```go
return aiEntryNeedsConfiguration("AI 入口意图解析模型暂不可用。", "/settings"), nil
```

- [ ] **Step 6: 更新 Server wiring**

在 `server/main.go` 构造：

```go
var serverInternalLLMClient service.ResultLLMClient
if route := cfg.ServerInternal; route.BaseURL != "" && route.Key != "" && route.Model != "" {
	client := service.NewOpenAILLMClient(route.BaseURL, route.Key, route.Model, route.Timeout)
	serverInternalLLMClient, _ = client.(service.ResultLLMClient)
}
```

只把它注入 `NewAIEntryService(repo, taskSvc, serverInternalLLMClient, fixedBilling.Cost, service.AIEntryModelConfig{ProviderKey: cfg.ServerInternal.ProviderKey, Model: cfg.ServerInternal.Model}, log)`。所有现有 AI entry 单元测试调用点补上 `nil, AIEntryModelConfig{}`；需要验证计费的用例传 fake recorder。删除 `main.go` 中“goal-mode evaluator 复用 writing LLM”的过期注释；Goal 已由 Agent runtime 的 `/goal` 控制处理。MCP 构造 `ContentRenderService` 不再依赖任何模型配置，并将 `writing_tools.go`/测试重命名为 `content_render_tools.go`/测试。日志使用 `server_internal_model`、`content_render_tools`，删除 `writing_tools` 和 `writing LLM` 文案。

- [ ] **Step 7: 运行相关测试**

Run: `go test ./server/service ./server/mcp -run 'Test(AIEntry|ProviderTokenUnreconciled|Convert|Render|EveryMCPHandler)' -count=1`

Expected: PASS，且 `rg -n 'WritingService|WritingSvc|GetEffectiveWritingConfig' server/service server/mcp server/main.go` 无结果。

- [ ] **Step 8: 提交通用模型与渲染拆分**

```bash
git add server/service/model_client.go server/service/model_json.go server/service/model_json_test.go server/service/content_render.go server/service/content_render_test.go server/service/writing.go server/service/writing_test.go server/service/writing_convert_test.go server/service/render_template.go server/service/render_template_test.go server/service/ai_entry.go server/service/ai_entry_test.go server/service/provider_cost.go server/service/provider_cost_test.go server/mcp/tools.go server/mcp/content_render_tools.go server/mcp/content_render_tools_test.go server/mcp/writing_tools.go server/mcp/writing_tools_test.go server/mcp/boundary_contract_test.go server/main.go
git commit -m "refactor(server): isolate internal llm from content rendering"
```

### Task 3: 删除用户文本模型配置并保留图片覆盖

**Files:**
- Modify: `server/model/model_config.go`
- Modify: `server/repository/model_config.go`
- Modify: `server/service/model_config.go`
- Modify: `server/service/model_config_test.go`
- Modify: `server/service/model_config_resolve_test.go`
- Modify: `server/handler/model_config.go`
- Modify: `server/handler/model_config_test.go`
- Create: `server/migrations/20260729_remove_user_text_model_config.sql`
- Modify: `server/migrations/migrations_test.go`

- [ ] **Step 1: 写只接受图片配置的 API 与迁移契约测试**

将 Service/Handler 用例改为只断言 `image`，并新增：

```go
func TestModelConfigUpdateRejectsRemovedTextConfig(t *testing.T) {
	app := setupModelConfigHandlerTest(t)
	resp, err := app.Test(httptestJSON(http.MethodPut, "/model-config", `{"text":{"model":"gpt-5"}}`))
	if err != nil { t.Fatal(err) }
	if resp.StatusCode != http.StatusBadRequest { t.Fatalf("status = %d", resp.StatusCode) }
}
```

在 `server/migrations/migrations_test.go` 新增：

```go
func TestRemoveUserTextModelConfigMigration(t *testing.T) {
	raw, err := os.ReadFile("20260729_remove_user_text_model_config.sql")
	if err != nil { t.Fatal(err) }
	sql := strings.ToLower(string(raw))
	if !strings.Contains(sql, "drop column text_config_json") {
		t.Fatalf("migration must drop user_model_configs.text_config_json")
	}
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `go test ./server/handler ./server/service ./server/migrations -run 'Test(ModelConfig|RemoveUserText)' -count=1`

Expected: FAIL，当前 DTO 仍接受 `text` 且迁移不存在。

- [ ] **Step 3: 删除后端文本配置面**

在 `server/model/model_config.go` 删除 `TextUserConfig`、它的 `(*TextUserConfig).HasConfig` 方法和 `TextConfigJSON`；保留 `ImageUserConfig.HasConfig`/`HasCompleteConfig`。DTO 只剩：

```go
type ModelConfigResponse struct {
	Image *ImageConfigDTO `json:"image,omitempty"`
}

type UpdateModelConfigRequest struct {
	Image *ImageConfigDTO `json:"image,omitempty"`
}
```

删除 `GetTextConfig`、`HasTextOverride`、`GetEffectiveWritingConfig`、`loadTextConfig`、`loadUserTextConfig`。Repository upsert 的更新列只保留：

```go
DoUpdates: clause.AssignmentColumns([]string{"image_config_json", "updated_at"})
```

Handler 使用严格的顶层 JSON 字段检查拒绝 `text`，并在 `Image == nil` 时返回 `no image config provided`。图片 provider、endpoint、key、model、proxy 的现有验证不变。

- [ ] **Step 4: 添加前向 SQL**

`server/migrations/20260729_remove_user_text_model_config.sql` 内容固定为：

```sql
ALTER TABLE user_model_configs
  DROP COLUMN text_config_json;
```

不删除 `user_model_configs` 表，不迁移或清空 `image_config_json`。

- [ ] **Step 5: 运行模型配置和迁移测试**

Run: `go test ./server/handler ./server/service ./server/migrations -run 'Test(ModelConfig|RemoveUserText)' -count=1`

Expected: PASS。

- [ ] **Step 6: 提交用户文本配置清理**

```bash
git add server/model/model_config.go server/repository/model_config.go server/service/model_config.go server/service/model_config_test.go server/service/model_config_resolve_test.go server/handler/model_config.go server/handler/model_config_test.go server/migrations/20260729_remove_user_text_model_config.sql server/migrations/migrations_test.go
git commit -m "refactor(models): remove user text model overrides"
```

### Task 4: 清理 Studio 文本模型配置和错误的用户覆盖门槛

**Files:**
- Modify: `studio/src/lib/api/model-config.ts`
- Modify: `studio/src/components/settings/ModelConfigSection.tsx`
- Create: `studio/src/components/settings/ModelConfigSection.test.tsx`
- Modify: `studio/src/lib/command-center.ts`
- Modify: `studio/src/lib/command-center.test.ts`
- Modify: `studio/src/lib/studio-ux.ts`
- Modify: `studio/src/lib/studio-ux.test.ts`
- Modify: `studio/src/pages/SettingsPage.tsx`
- Modify: `studio/src/pages/DashboardPage.tsx`
- Modify: `studio/src/components/GlobalCommandPalette.tsx`

- [ ] **Step 1: 写用户模型覆盖不再是任务 readiness 条件的测试**

在 `studio/src/lib/command-center.test.ts` 删除 `hasUsableModelConfig` 用例，并把 readiness 断言改成不存在 `checks.modelConfig`：

```ts
it('does not treat optional user model overrides as task readiness', () => {
  const signals = buildCommandCenterSignals({
    projects: [project()],
    apiKeysReady: true,
    localExecutorReady: true,
  })
  expect(signals.readiness.checks).not.toHaveProperty('modelConfig')
})
```

新建 `studio/src/components/settings/ModelConfigSection.test.tsx`，使用现有 QueryClient 测试模式和 API mock：

```tsx
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, expect, it, vi } from 'vitest'

import ModelConfigSection from './ModelConfigSection'
import { api } from '@/lib/api'

vi.mock('@/lib/api', () => ({
  api: { modelConfig: { get: vi.fn(), update: vi.fn(), clear: vi.fn() } },
}))

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.modelConfig.get).mockResolvedValue({
    image: { provider: 'openai', endpoint: 'https://images.example.com/v1', api_key: '****', model: 'gpt-image-1' },
  })
  vi.mocked(api.modelConfig.update).mockResolvedValue({ code: 0, msg: 'ok' })
})

it('only renders and saves the image model override', async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><ModelConfigSection /></QueryClientProvider>)

  expect(await screen.findByText('图片生成模型覆盖')).toBeInTheDocument()
  expect(screen.queryByText('文本模型')).not.toBeInTheDocument()
  expect(screen.queryByText('写作模型')).not.toBeInTheDocument()
  expect(screen.queryByText('MCP 模型配置')).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '图片生成模型覆盖' }))
  fireEvent.click(screen.getByRole('button', { name: '保存图片模型配置' }))
  await waitFor(() => expect(api.modelConfig.update).toHaveBeenCalledWith({
    image: { provider: 'openai', endpoint: 'https://images.example.com/v1', api_key: '****', model: 'gpt-image-1', proxy: '' },
  }))
})
```

- [ ] **Step 2: 运行 Studio 定向测试并确认失败**

Run: `cd studio && bun run test -- src/lib/command-center.test.ts src/components/settings/ModelConfigSection.test.tsx src/lib/studio-ux.test.ts`

Expected: FAIL，因为 API 类型和表单仍暴露 `text`，command center 仍把用户覆盖当作 readiness。

- [ ] **Step 3: 收缩 API 类型和设置表单**

`studio/src/lib/api/model-config.ts` 改为：

```ts
export interface ModelConfigResponse {
  image?: ImageConfigDTO | null
}

export interface UpdateModelConfigRequest {
  image: ImageConfigDTO
}
```

`ModelConfigSection` 删除文本 endpoint/key/model/proxy 的 state、校验和 JSX，只显示“图片生成模型覆盖”，并继续用 `****` 保留已有图片 key。折叠按钮的可访问名称固定为“图片生成模型覆盖”，保存按钮固定为“保存图片模型配置”，使组件测试不依赖描述文字。保存请求只发送 `{ image }`。

删除 `ModelConfigLike`、`hasUsableModelConfig`、`modelConfigKnown`、`modelConfigReady` 和 `checks.modelConfig`。Dashboard 与 GlobalCommandPalette 不再为了创建 readiness 查询或传递用户模型配置；`studio-ux.ts` 删除“模型配置未就绪，先选择可用模型”的阻断分支。用户没有图片覆盖时仍使用 Server 的系统图片路由，不能被标成 not ready。Settings 的说明改成“自定义图片生成模型覆盖”，不能再暗示文章写作依赖用户模型配置。

- [ ] **Step 4: 运行 Studio 测试和类型构建**

Run: `cd studio && bun run test -- src/lib/command-center.test.ts src/components/settings/ModelConfigSection.test.tsx src/lib/studio-ux.test.ts`

Expected: PASS。

Run: `cd studio && bun run build`

Expected: PASS，无残留 `text` 类型引用，也没有把可选图片覆盖当作任务创建门槛。

- [ ] **Step 5: 提交 Studio 配置清理**

```bash
git add studio/src/lib/api/model-config.ts studio/src/components/settings/ModelConfigSection.tsx studio/src/components/settings/ModelConfigSection.test.tsx studio/src/lib/command-center.ts studio/src/lib/command-center.test.ts studio/src/lib/studio-ux.ts studio/src/lib/studio-ux.test.ts studio/src/pages/SettingsPage.tsx studio/src/pages/DashboardPage.tsx studio/src/components/GlobalCommandPalette.tsx
git commit -m "refactor(studio): keep only image model overrides"
```

### Task 5: 删除 Seednote 画像补全并固定项目图片理解路由

**Files:**
- Modify: `server/handler/project.go`
- Modify: `server/handler/channel_test.go`
- Modify: `server/main.go`

- [ ] **Step 1: 将测试改成供应商字段原样保留和 vision-only**

删除 `TestProjectFetchProfileAIAnalysis*`。新增端到端 Handler 用例：通过 `httptest.NewServer` 返回 `seednote.APIResponse[seednote.UserProfile]`，给 Handler 注入 `seednote.NewClient(upstream.URL, time.Second)`，然后调用真实 `FetchProfile`：

```go
func TestProjectFetchProfilePreservesProviderFieldsWithoutLLMEnrichment(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/user/profile" { http.NotFound(w, r); return }
		_ = json.NewEncoder(w).Encode(seednote.APIResponse[seednote.UserProfile]{
			Success: true,
			Data: seednote.UserProfile{UserBasicInfo: seednote.UserBasicInfo{
				Nickname: "原始昵称", Desc: "原始简介", Avatar: "https://cdn.example.com/avatar.png", RedID: "red-1",
			}},
		})
	}))
	defer upstream.Close()

	h := NewProjectHandler(nil, testProjectLogger(t))
	h.SetSeednoteClient(seednote.NewClient(upstream.URL, time.Second))
	app := fiber.New()
	app.Post("/fetch", func(c fiber.Ctx) error { c.Locals("user_id", "user-1"); return h.FetchProfile(c) })
	resp, err := app.Test(httptestJSON("POST", "/fetch", `{"platform":"seednote","profile_url":"https://www.xiaohongshu.com/user/profile/user-1"}`))
	if err != nil { t.Fatal(err) }
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK { t.Fatalf("status = %d", resp.StatusCode) }
	var envelope struct { Data platform.PlatformProfile `json:"data"` }
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil { t.Fatal(err) }
	if envelope.Data.Name != "原始昵称" || envelope.Data.RawData["desc"] != "原始简介" {
		t.Fatalf("provider profile changed: %#v", envelope.Data)
	}
	if _, exists := envelope.Data.RawData["analysis"]; exists { t.Fatal("hidden LLM analysis remains") }
}

func TestAnalyzeImageReturns503WithoutImageUnderstandingClient(t *testing.T) {
	h := NewProjectHandler(nil, testProjectLogger(t))
	app := fiber.New()
	app.Post("/analyze", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.AnalyzeImage(c)
	})
	resp, err := app.Test(httptestJSON("POST", "/analyze", `{"image_url":"https://cdn.example.com/image.png"}`))
	if err != nil { t.Fatal(err) }
	if resp.StatusCode != fiber.StatusServiceUnavailable { t.Fatalf("status=%d", resp.StatusCode) }
}
```

保留并调整所有 OSS、staging、跨用户授权测试，使其只通过 `SetVisionClient` 注入 fake；删除“回退到 writing”的测试，新增断言未配置 vision 时任何文本 fake 都不会被调用。

- [ ] **Step 2: 运行 handler 测试并确认失败**

Run: `go test ./server/handler -run 'Test(ProjectFetchProfilePreserves|AnalyzeImage)' -count=1`

Expected: FAIL，当前仍做画像 LLM 补全并允许 writing fallback。

- [ ] **Step 3: 删除 ProjectHandler 的文本模型状态**

删除以下字段和符号：

```text
ProjectHandler.llm
ProjectHandler.llmTimeout
ProjectHandler.modelConfigSvc
SetLLMClient
SetModelConfigService
getLLMClient
seednoteProfileAnalysis
enrichSeednoteProfileWithAI
buildSeednoteProfileAnalysisPrompt
buildSeednoteProfileExtractionPrompt
parseSeednoteProfileAnalysis
```

`FetchProfile` 在 provider 成功后直接 `Success(c, profile)`。`AnalyzeImage` 只取 `h.visionClient`；为空时返回：

```go
return Error(c, fiber.StatusServiceUnavailable, "image understanding model is not configured")
```

保持原有图片字节、MIME、大小和所有权校验不变。

- [ ] **Step 4: 清理 wiring 并运行测试**

从 `server/main.go` 删除 ProjectHandler 的文本模型和 ModelConfig 注入，只保留：

```go
if imageUnderstandingClient != nil {
	projectHandler.SetVisionClient(imageUnderstandingClient)
}
```

Run: `go test ./server/handler -run 'Test(ProjectFetchProfilePreserves|AnalyzeImage)' -count=1`

Expected: PASS。

- [ ] **Step 5: 提交项目调用方清理**

```bash
git add server/handler/project.go server/handler/channel_test.go server/main.go
git commit -m "refactor(projects): remove hidden profile llm enrichment"
```

### Task 6: 独立图片理解并新增原子 `analyze_video`

**Files:**
- Modify: `server/service/task_image_operations.go`
- Modify: `server/service/task_image_operations_test.go`
- Create: `server/service/task_video_operations.go`
- Create: `server/service/task_video_operations_test.go`
- Modify: `server/mcp/image_tools.go`
- Modify: `server/mcp/image_tools_test.go`
- Create: `server/mcp/video_understanding_tools.go`
- Create: `server/mcp/video_understanding_tools_test.go`
- Modify: `server/mcp/tools.go`
- Modify: `server/mcp/tools_test.go`
- Modify: `server/mcp/boundary_contract_test.go`
- Modify: `server/agent/runtime_policy.go`
- Modify: `server/agent/runtime_policy_test.go`
- Modify: `server/main.go`

- [ ] **Step 1: 写视频 Service 的输入与授权失败测试**

在 `server/service/task_video_operations_test.go` 使用 table-driven 测试覆盖：缺 `project_id`、缺 prompt、同时缺/同时给 `video_url` 与 `task_file_id`、外部非 HTTPS、外部 URL 带 userinfo、task 非本用户、task 与 project 不匹配、task file 不属于 task、task file 非 video MIME、专用 client 缺失。

核心成功用例：

```go
func TestTaskVideoOperationsAnalyzeTaskFileWithNativeVideoRoute(t *testing.T) {
	svc, fixture := setupTaskVideoOperations(t)
	result, err := svc.Analyze(context.Background(), AnalyzeTaskVideoRequest{
		UserID: fixture.userID, ProjectID: fixture.projectID, TaskID: fixture.taskID,
		TaskFileID: fixture.videoFileID, Prompt: "总结镜头和动作",
	})
	if err != nil { t.Fatal(err) }
	if result.Analysis != "完整视频分析" { t.Fatalf("analysis=%q", result.Analysis) }
	if fixture.nativeClient.videoURL == "" { t.Fatal("native video URL was not sent") }
	if !strings.Contains(fixture.cost.lastProviderRequestID, model.OperationVideoUnderstanding) {
		t.Fatalf("provider request id=%q", fixture.cost.lastProviderRequestID)
	}
}
```

同一测试文件实现 `setupTaskVideoOperations` fixture：创建 user、Seednote project、task、`video/mp4` TaskFile 和 fake storage；fake native client 记录 `videoURL`，fake cost recorder 记录 `ProviderRequestID`。该 helper 不访问网络。

再写一个 client 只实现文本/图片、不实现 `CompleteWithVideoURLResult` 的测试，稳定期待 `ErrVideoUnderstandingNativeUnsupported`。

- [ ] **Step 2: 写 MCP schema 和注册失败测试**

在 `server/mcp/video_understanding_tools_test.go` 注册完整工具集并断言：

```go
func TestAnalyzeVideoToolIsRegisteredWithoutLegacyAlias(t *testing.T) {
	names := listToolNames(t, registerVideoUnderstandingTools)
	if !names["analyze_video"] { t.Fatal("analyze_video is not registered") }
	if names["analyze_video_reference"] { t.Fatal("legacy alias must not be registered") }
}
```

在 `server/mcp/tools_test.go` 添加共享 helper，视频与后续直播测试都直接调用它：

```go
func listToolNames(t *testing.T, register func(*mcp.Server)) map[string]bool {
	t.Helper()
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "1.0.0"}, nil)
	register(server)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil { t.Fatal(err) }
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil { t.Fatal(err) }
	t.Cleanup(func() { _ = clientSession.Close() })
	result, err := clientSession.ListTools(ctx, nil)
	if err != nil { t.Fatal(err) }
	names := make(map[string]bool, len(result.Tools))
	for _, tool := range result.Tools { names[tool.Name] = true }
	return names
}
```

handler 测试断言只调用 `svcs.TaskVideoOperationsSvc.Analyze`，并将 `project_id`、`prompt`、可选 `task_id`、二选一来源原样传入。

- [ ] **Step 3: 运行新测试并确认失败**

Run: `go test ./server/service ./server/mcp -run 'Test(TaskVideoOperations|AnalyzeVideoTool|AnalyzeVideoHandler)' -count=1`

Expected: FAIL，新 Service 和工具尚不存在。

- [ ] **Step 4: 收紧图片理解命名和错误**

`TaskImageOperationsService` 不再接收 `WritingService`，改收窄接口：

```go
type ImageUnderstandingClient interface {
	CompleteWithImageResult(ctx context.Context, systemPrompt, userPrompt, imageURL string) (*LLMResult, error)
}
```

把私有 `taskImageOperationsCost` 重命名为图片/视频共用的 `UnderstandingCostRecorder`，保留 `CatalogID`、`RecordProviderTokenUsage`、`RecordMediaUnreconciled` 三个方法。字段改为 `understanding ImageUnderstandingClient`。`Analyze` 用固定 system prompt 和请求 prompt/image source 直接调用 `CompleteWithImageResult`，trim `result.Text` 后返回。缺配置返回导出的稳定错误 `ErrImageUnderstandingUnavailable`；不再存在 legacy vision 或文本 fallback。成本仍使用 `OperationImageUnderstanding`。

- [ ] **Step 5: 实现 `TaskVideoOperationsService`**

请求与结果定义：

```go
type AnalyzeTaskVideoRequest struct {
	UserID, ProjectID, TaskID, TaskFileID, VideoURL, Prompt string
}

type AnalyzeTaskVideoResult struct {
	Analysis string                  `json:"analysis"`
	Usage    srvconfig.TokenUsage    `json:"usage"`
}

type NativeVideoUnderstandingClient interface {
	CompleteWithVideoURLResult(ctx context.Context, systemPrompt, userPrompt, videoURL string) (*LLMResult, error)
}

type TaskVideoOperationsConfig struct {
	UnderstandingProvider string
	UnderstandingModel    string
}
```

构造函数接收 `repository.Repository`、`storage.Provider`、基础 `LLMClient`、与图片理解相同的 provider cost recorder、`TaskVideoOperationsConfig` 和 logger。Service 内部保留该 client，但 `Analyze` 调用前必须断言它实现 `NativeVideoUnderstandingClient`；断言失败返回 `ErrVideoUnderstandingNativeUnsupported`，绝不调用它的文本或图片方法。之后按顺序完成：严格字段校验并查询 `project_id`，确认项目属于 MCP 用户且处于 active；若使用 `task_file_id`，先通过 repository `TaskFiles().FindByID` 取得 published/collected 文件，使用文件自身的 `TaskID` 查任务并校验 user/project，若请求同时给了 `task_id` 则还必须与文件一致，再校验 `video/` MIME，把 `OSSKey`/持久 URL 交给 `ResolveMediaSource`；若使用 `video_url`，可选 `task_id` 非空时校验 task user/project，并通过 `net/url` 要求 HTTPS、非空 host、无 userinfo，对 owned URL 重新签名；最后用固定视频理解 system prompt、请求 prompt 和解析后的 URL 仅调用 `CompleteWithVideoURLResult`。这样 `task_id` 保持可选，同时 task file 永远不能跨任务或跨项目使用。

使用同 `TaskImageOperationsService.recordAnalysisCost` 的共享 helper，media kind 为 `video`、operation 为 `OperationVideoUnderstanding`。usage 缺失写 unreconciled，调用失败也记录未知成本证据；绝不抽帧、转录或调用图片/文本模型。

- [ ] **Step 6: 注册 MCP 与 runtime policy**

`server/mcp/video_understanding_tools.go` 的唯一 schema：

```go
server.AddTool(&mcp.Tool{
	Name: "analyze_video",
	Description: "Analyze one authorized complete video with the configured native video-understanding route. No frame, audio, or text fallback is performed.",
	InputSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"project_id": map[string]any{"type": "string"},
			"prompt": map[string]any{"type": "string"},
			"task_id": map[string]any{"type": "string"},
			"video_url": map[string]any{"type": "string"},
			"task_file_id": map[string]any{"type": "string"},
		},
		"required": []any{"project_id", "prompt"},
	},
}, analyzeVideoHandler)
```

在 `RegisterTools` 调用 `registerVideoUnderstandingTools`，`Services` 增加 `TaskVideoOperationsSvc`，边界表增加 `analyzeVideoHandler -> svcs.TaskVideoOperationsSvc.Analyze`。把 `analyze_video` 加到 Montage 和 live-slicer 的 managed required tools；保留 Seednote 的 `analyze_image`。

- [ ] **Step 7: 更新 wiring 并运行多模态测试**

`server/main.go` 先构造两条独立的基础 OpenAI-compatible client：image route 必须实现 `service.ImageUnderstandingClient` 才能注入 `TaskImageOperationsService`；video route 将其独立基础 client 注入 `TaskVideoOperationsService`，由 Service 在每次调用时检查 `NativeVideoUnderstandingClient`，以保留稳定的 native-unsupported 错误。ProjectHandler 可继续接收同一个 image 基础 client，但两个 MCP operations service 都不能经过 content renderer。日志独立报告两条路由是否配置；provider 对 `video_url` content part 的真实支持仍由受控 smoke 验证。

Run: `go test ./server/service ./server/mcp ./server/agent -run 'Test(TaskImageOperations|TaskVideoOperations|AnalyzeImage|AnalyzeVideo|EveryMCPHandler|ManagedRequiredMCPTools)' -count=1`

Expected: PASS，并且 `rg -n 'analyze_video_reference' server plugins` 无结果。

- [ ] **Step 8: 提交多模态理解能力**

```bash
git add server/service/task_image_operations.go server/service/task_image_operations_test.go server/service/task_video_operations.go server/service/task_video_operations_test.go server/mcp/image_tools.go server/mcp/image_tools_test.go server/mcp/video_understanding_tools.go server/mcp/video_understanding_tools_test.go server/mcp/tools.go server/mcp/tools_test.go server/mcp/boundary_contract_test.go server/agent/runtime_policy.go server/agent/runtime_policy_test.go server/main.go
git commit -m "feat(mcp): add atomic native video understanding"
```

### Task 7: 删除直播切片的四个生成式 MCP

**Files:**
- Modify: `server/service/live_slice.go`
- Modify: `server/service/live_slice_test.go`
- Modify: `server/mcp/live_slice_tools.go`
- Modify: `server/mcp/live_slice_tools_test.go`
- Modify: `server/mcp/live_slice_skill_test.go`
- Modify: `server/mcp/boundary_contract_test.go`
- Modify: `server/agent/runtime_policy.go`
- Modify: `server/agent/runtime_policy_test.go`
- Modify: `server/main.go`

- [ ] **Step 1: 先把工具存在性测试改成删除契约**

在 `server/mcp/live_slice_tools_test.go` 增加：

```go
func TestLiveSliceToolsExcludeGenerativeSemanticTools(t *testing.T) {
	names := listToolNames(t, registerLiveSliceTools)
	for _, removed := range []string{
		"recognize_live_subjects", "recognize_live_invalid_sentences",
		"recognize_live_segments", "complete_live_subject",
	} {
		if names[removed] { t.Errorf("removed tool %s is still registered", removed) }
	}
	for _, kept := range []string{"build_live_clip_plan", "build_live_subject_clip_plan", "build_live_clip_manifest"} {
		if !names[kept] { t.Errorf("deterministic tool %s is missing", kept) }
	}
}
```

复用 Task 6 已放在 `server/mcp/tools_test.go` 的通用 `listToolNames` helper，使视频与直播测试共享同一个真实 MCP 注册读取实现。

runtime policy expected list只保留 TingWu、上传、状态查询、三个确定性计划/manifest 和进度/反馈工具，另含 Task 6 新增的 `analyze_video`。

- [ ] **Step 2: 运行删除契约并确认失败**

Run: `go test ./server/mcp ./server/agent -run 'Test(LiveSliceToolsExclude|ManagedRequiredMCPTools)' -count=1`

Expected: FAIL，四个工具仍注册。

- [ ] **Step 3: 删除 Service 生成式方法和状态**

从 `LiveSliceService` 删除 `llm LLMClient`，构造函数改为：

```go
func NewLiveSliceService(cfg config.TingWuConfig, store storage.Provider, logger *zerolog.Logger) (*LiveSliceService, error)
func NewLiveSliceServiceWithClients(tw TingWuClient, store storage.Provider, logger *zerolog.Logger) *LiveSliceService
```

删除 `RecognizeLiveSubjects`、`RecognizeLiveInvalidSentences`、`RecognizeLiveSegments`、`CompleteLiveSubject` 及只由它们使用的 prompt、LLM JSON 清理/解析 helper。保留 `LiveSentence`、`LiveInvalid`、`LiveSegment`、`LiveSubjectCompletion` 等输入类型，因为确定性计划工具仍消费 Agent 生成的结构。

删除对应 LLM parsing 测试；保留并补充 plan 校验测试，覆盖不存在的 sentence index、倒序范围、重复范围和错误来源 ID 必须直接失败。

- [ ] **Step 4: 删除 MCP handler/schema 和边界记录**

从 `registerLiveSliceTools` 删除四个 `AddTool`，删除四个 handler 及只服务于它们的 schema。同步删除 `boundary_contract_test.go` 的四项 reviewed capability。

`server/main.go` 初始化 live slice 只依赖 TingWu 或 storage：

```go
if cfg.TingWu.Complete() || store != nil {
	liveSliceSvc, err = service.NewLiveSliceService(cfg.TingWu, store, log)
}
```

- [ ] **Step 5: 运行直播切片测试**

Run: `go test ./server/service ./server/mcp ./server/agent -run 'Test(Live|BuildLive|ManagedRequiredMCPTools|EveryMCPHandler)' -count=1`

Expected: PASS，`rg -n 'RecognizeLive|CompleteLiveSubject|recognize_live_|complete_live_subject' server --glob '*.go'` 无生产代码结果。

- [ ] **Step 6: 提交 Server 直播语义清理**

```bash
git add server/service/live_slice.go server/service/live_slice_test.go server/mcp/live_slice_tools.go server/mcp/live_slice_tools_test.go server/mcp/live_slice_skill_test.go server/mcp/boundary_contract_test.go server/agent/runtime_policy.go server/agent/runtime_policy_test.go server/main.go
git commit -m "refactor(live-slice): move semantic decisions to agent"
```

### Task 8: 更新 Live Slicer 与 Seednote 插件契约

**Files:**
- Modify: `plugins/agents/live-slicer.md`
- Modify: `plugins/agents/live-slicer.toml`
- Modify: `plugins/skills/live-slice/SKILL.md`
- Modify: `plugins/agents/seednote.md`
- Modify: `plugins/agents/seednote.toml`
- Modify: `plugins/docs/plugin-development.md`
- Modify: `plugins/CODEX.md`
- Modify: `plugins/.claude-plugin/plugin.json`
- Modify: `plugins/.codex-plugin/plugin.json`
- Modify: `server/mcp/live_slice_skill_test.go`
- Modify: `server/agent/server_workspace_contract_test.go`
- Modify: `server/agent/seednote_skill_contract_test.go`

- [ ] **Step 1: 写插件文本契约测试**

`server/mcp/live_slice_skill_test.go` 对三个 canonical 文件同时断言：

```go
removed := []string{"recognize_live_subjects", "recognize_live_invalid_sentences", "recognize_live_segments", "complete_live_subject"}
required := []string{
	"output/invalid-sentences.json", "output/segments.json",
	"output/subjects.json", "output/subject-completions.json",
	"build_live_clip_plan", "build_live_subject_clip_plan",
}
```

并要求文字明确说明：Agent 直接从 `analysis.json` 生成这些 JSON、先自检 index/range/source，再交给确定性 MCP；MCP 拒绝时在当前 Agent 修正循环内修改文件。

Seednote 契约测试要求 `viral_analysis` 分支只生成 `source-analysis.md`、`viral-template.json`、`template-meta.json`，不得继续执行写作、出图、发布步骤。

- [ ] **Step 2: 运行插件契约并确认失败**

Run: `go test ./server/mcp ./server/agent -run 'Test(LiveSliceSkill|Seednote.*Viral|ServerWorkspace)' -count=1`

Expected: FAIL，插件仍调用已删除的工具且没有独立爆款任务停止分支。

- [ ] **Step 3: 改写 Live Slicer Agent/Skill**

将原调用步骤替换为以下契约（Markdown 与 TOML instructions 保持语义一致）：

```text
1. 读取 output/analysis.json 的 sentences。
2. 直接判断不可用句，按固定结构写 output/invalid-sentences.json，例如：{"invalid":[{"index":3,"reason":"与直播主题无关的广告口播"}]}。
3. 过滤后按固定结构写 output/segments.json，例如：{"segments":[{"title":"核心演示","description":"展示产品的主要操作流程","thoughts":"保留连续操作和结果反馈","start":4,"end":18}]}。
4. 主题模式可生成 output/subjects.json 与 output/subject-completions.json；completion 的每个 sentence index 必须来自 analysis.json。
5. 自检所有 index 唯一、存在、范围单调且 start <= end，再调用 build_live_clip_plan 或 build_live_subject_clip_plan。
6. MCP 校验失败时修改当前 JSON 并重试；不得寻找另一个模型工具修复。
```

若需要理解完整画面，可单独调用 `analyze_video(project_id, task_id, task_file_id|video_url, prompt)`；该结果只作 Agent 判断输入，不替代 TingWu transcript 或确定性计划。

- [ ] **Step 4: 增加 Seednote `viral_analysis` 任务分支**

在两个 Seednote Agent 定义的最前面读取任务类型。当类型为 `viral_analysis` 时：获取链接指向的源笔记、按 `seednote-viral-analysis` 生成三个固定产物、更新进度、提交 feedback 后结束。明确禁止进入 `seednote-writing`、视觉生成、模板保存和发布步骤。

- [ ] **Step 5: 更新插件文档和双清单版本**

文档工具列表删除四个 live semantic MCP，加入 `analyze_video`，并说明 `analyze_image`/`analyze_video` 是专用单次理解能力。

将两个 manifest 从 `4.0.7` 同步 patch bump 到：

```json
"version": "4.0.8"
```

- [ ] **Step 6: 运行插件与 manifest 验证**

Run: `go test ./server/mcp ./server/agent -run 'Test(LiveSliceSkill|Seednote.*Viral|ServerWorkspace|Plugin)' -count=1`

Expected: PASS。

Run: `claude plugin validate plugins`

Expected: PASS。

- [ ] **Step 7: 在 plugins 子模块提交并发布 child commit**

```bash
git -C plugins add agents/live-slicer.md agents/live-slicer.toml skills/live-slice/SKILL.md agents/seednote.md agents/seednote.toml docs/plugin-development.md CODEX.md .claude-plugin/plugin.json .codex-plugin/plugin.json
git -C plugins commit -m "refactor: move semantic analysis into agents"
git -C plugins push origin HEAD:main
```

Expected: child `origin/main` 包含该提交；记录 SHA，后续父仓库提交 gitlink。

- [ ] **Step 8: 提交父仓库测试和 gitlink**

```bash
git add plugins server/mcp/live_slice_skill_test.go server/agent/server_workspace_contract_test.go server/agent/seednote_skill_contract_test.go
git commit -m "chore(plugin): align agent model boundaries"
```

### Task 9: 将爆款分析切到标准托管任务

**Files:**
- Modify: `server/model/constants.go`
- Modify: `server/config/config.go`
- Modify: `server/config/kubernetes_config_test.go`
- Modify: `server/billing/products.yaml`
- Modify: `server/billing/catalog_contract_test.go`
- Modify: `server/agent/config_builder.go`
- Modify: `server/agent/executor.go`
- Modify: `server/agent/executor_test.go`
- Modify: `server/agent/artifacts.go`
- Modify: `server/agent/artifacts_test.go`
- Modify: `server/service/billing_catalog.go`
- Modify: `server/service/billing_catalog_test.go`
- Modify: `server/service/billing_wallet_test.go`
- Modify: `server/service/task.go`
- Modify: `server/service/task_test.go`
- Modify: `server/service/workflow_status.go`
- Modify: `server/service/workflow_status_test.go`
- Modify: `server/handler/task.go`
- Modify: `server/handler/task_test.go`
- Modify: `server/service/viral_analysis.go`
- Modify: `server/service/viral_analysis_test.go`
- Modify: `server/handler/viral_analysis.go`
- Modify: `server/router/router.go`
- Modify: `server/scheduler/scheduler.go`
- Modify: `server/scheduler/scheduler_test.go`
- Modify: `server/main.go`

- [ ] **Step 1: 写显式 Agent 路由与任务类型测试**

在 `server/model/constants.go` 使用常量 `TaskTypeViralAnalysis = "viral_analysis"`。先在 `server/agent/executor_test.go` 加入：

```go
{model.TaskTypeViralAnalysis, "seednote"},
```

并新增一个测试确保未知类型仍保持现有 fallback，但爆款分析不是通过 fallback 命中的。`DefaultMaxTurns` 测试还要证明 `viral_analysis` 复用 `max_turns.seednote`，而不是未知类型默认值。

在 `server/config/kubernetes_config_test.go::TestRuntimeImageForTaskUsesCanonicalProfileMap` 加入 `{taskType: model.TaskTypeViralAnalysis, profile: "seednote", image: "creator-agent-seednote:latest"}`，防止任务虽然选择 Seednote Agent 却落到 Article runtime image。同步在 `server/service/billing_catalog_test.go` 增加 `CreateTaskQuote` 用例，分别以 `cost_effective`、`balanced`、`maximum_quality` 请求 `TaskTypeViralAnalysis`，断言都能选到当前 catalog 中 execution-profile 精确匹配的 SKU；空 execution profile 必须继续失败。

在 `server/agent/artifacts_test.go` 增加表格用例：三份 `source-analysis.md`、`viral-template.json`、`template-meta.json` 齐全时通过，分别缺任一文件时失败。`server/service/workflow_status_test.go` 断言 viral analysis 只显示“源笔记”“证据分析”“模板产物”三个阶段，且不会出现“排版 HTML”“发布草稿”等文章阶段。

- [ ] **Step 2: 写标准任务创建测试**

在 handler/service 测试构造一个 active Seednote project，并使用该 fixture 的真实 ID 发送：

```go
body := fmt.Sprintf(`{
  "project_id": %q,
  "type": "viral_analysis",
  "prompt": "https://www.xiaohongshu.com/explore/note-1",
  "execution_profile": "cost_effective",
  "quantity": 1
}`, project.ID)
```

断言创建出的 `Task.Type == "viral_analysis"`、`Task.ExecutionProfile` 与 snapshot 已冻结、存在 `task_execution`、队列 payload 是标准 content generate，并且 task admission 使用 `task.viral_analysis` 和所选 execution-profile SKU。另测非 Seednote project 请求 `viral_analysis` 返回 400，普通任务请求 type 与 project platform 不一致也返回 400。

- [ ] **Step 3: 运行任务测试并确认失败**

Run: `go test ./server/agent ./server/service ./server/handler -run 'Test(TaskTypeToAgent|.*ViralAnalysis.*Task)' -count=1`

Expected: FAIL，Server 当前忽略 request `type` 并把任务类型取成 project platform。

- [ ] **Step 4: 实现受控的非平台任务类型**

`createTaskRequest` 增加：

```go
Type string `json:"type"`
```

`CreateManualParams` 增加内部已校验字段：

```go
RequestedTaskType string
```

Handler 校验规则：空 type 使用 project platform；`viral_analysis` 只允许 active Seednote project；其他非空值必须等于 project platform。Service 再做 defense-in-depth：

```go
taskType := project.Platform
if p.RequestedTaskType == model.TaskTypeViralAnalysis {
	if project.Platform != model.PlatformSeednote {
		return nil, ErrViralAnalysisRequiresSeednoteProject
	}
	taskType = model.TaskTypeViralAnalysis
}
if p.FrozenTaskType != "" { taskType = p.FrozenTaskType }
```

`TaskTypeToAgent` 增加显式 case 返回 `seednote`；`DefaultMaxTurns` 在查 map 前把 `viral_analysis` 归一为 `seednote`；`config.canonicalRuntimeProfile` 同样把 `model.TaskTypeViralAnalysis` 映射为 `model.PlatformSeednote`。`agentTaskOperation` 增加 `model.TaskTypeViralAnalysis -> "task.viral_analysis"`，使 `CreateTaskQuote` 走标准 Agent task 校验。

`ValidateTaskArtifactsFromTaskFiles` 为 viral analysis 增加专用分支，精确要求 `source-analysis.md`、`viral-template.json`、`template-meta.json`。`BuildWorkflowStatus` 为该类型返回三个文件驱动的阶段：`source_note`、`evidence_analysis`、`template_artifacts`；其他任务的现有阶段不变。这样 SKU 的 `viral_analysis_report_verified` delivery 与真实终态校验一致，Studio 也不会展示不相关的文章工作流。

将 `server/billing/products.yaml` 的 catalog ID 从 `retail-2026-07-28-v5` 升到 `retail-2026-07-29-v6`，把旧的 profileless `task.viral-analysis.standard.v1` 替换为三个 SKU：

```yaml
  - id: "task.viral-analysis.cost-effective.v2"
    operation: "task.viral_analysis"
    execution_profile: "cost_effective"
    charge_policy: "task_admission"
    price_credits: 1200
    tier_prices: { free: 1200, pro: 1080, enterprise: 960 }
    delivery: "viral_analysis_report_verified"
  - id: "task.viral-analysis.balanced.v2"
    operation: "task.viral_analysis"
    execution_profile: "balanced"
    charge_policy: "task_admission"
    price_credits: 1200
    tier_prices: { free: 1200, pro: 1080, enterprise: 960 }
    delivery: "viral_analysis_report_verified"
  - id: "task.viral-analysis.maximum-quality.v2"
    operation: "task.viral_analysis"
    execution_profile: "maximum_quality"
    charge_policy: "task_admission"
    price_credits: 1200
    tier_prices: { free: 1200, pro: 1080, enterprise: 960 }
    delivery: "viral_analysis_report_verified"
```

更新 catalog snapshot、billing service 和 wallet fixtures，删除 `task.viral_analysis` 可以无 profile 报价的旧特例。三个 SKU 暂时保持现有爆款分析的 1200 credits 基准；本次架构迁移不顺带改变产品定价。标准 TaskService 原有 admission、execution、terminal usage 结算不增加特例。

- [ ] **Step 5: 将旧爆款接口降为历史只读**

保留 `server/model/viral_analysis.go`、repository 和 GET `/viral-analyses`、GET `/viral-analyses/:id`，以便历史记录继续读取。删除：

```text
POST /viral-analyses
ViralAnalysisService.Create
ViralAnalysisService.ExecuteAnalysis
ViralAnalysisService.analyzeWithLLM 及所有 prompt/解析/计费 helper
ViralAnalysisService.StartAnalysis/CompleteAnalysis/FailAnalysis
ViralAnalysisService.CleanupOldCompleted
scheduler.TypeViralAnalysis
ViralAnalysisHandler scheduler 参数和注册
```

把剩余 Service 改名为 `ViralAnalysisHistoryService`，只实现 ownership-aware `GetByID`、`ListByUserID`。旧表不再有新写入、异步任务或自动清理。

- [ ] **Step 6: 运行 Server 爆款任务和 scheduler 测试**

Run: `go test ./server/billing ./server/agent ./server/service ./server/handler ./server/router ./server/scheduler -run 'Test(TaskTypeToAgent|CreateTaskQuote.*Viral|.*ViralAnalysis|TaskProcessor|RetailCatalog)' -count=1`

Expected: PASS；`rg -n 'viral:analyze|ExecuteAnalysis|analyzeWithLLM' server --glob '*.go'` 无结果，router 只保留历史 GET。

- [ ] **Step 7: 提交爆款任务切换**

```bash
git add server/model/constants.go server/config/config.go server/config/kubernetes_config_test.go server/billing/products.yaml server/billing/catalog_contract_test.go server/agent/config_builder.go server/agent/executor.go server/agent/executor_test.go server/agent/artifacts.go server/agent/artifacts_test.go server/service/billing_catalog.go server/service/billing_catalog_test.go server/service/billing_wallet_test.go server/service/task.go server/service/task_test.go server/service/workflow_status.go server/service/workflow_status_test.go server/handler/task.go server/handler/task_test.go server/service/viral_analysis.go server/service/viral_analysis_test.go server/handler/viral_analysis.go server/router/router.go server/scheduler/scheduler.go server/scheduler/scheduler_test.go server/main.go
git commit -m "refactor(viral): use managed seednote task lifecycle"
```

### Task 10: 对齐 Studio 爆款任务创建与历史 API

**Files:**
- Modify: `studio/src/lib/schemas.ts`
- Modify: `studio/src/lib/schemas.test.ts`
- Modify: `studio/src/lib/task-form.ts`
- Modify: `studio/src/lib/task-form.test.ts`
- Modify: `studio/src/components/tasks/TaskFormDialog.tsx`
- Modify: `studio/src/components/tasks/TaskFormDialog.test.tsx`
- Modify: `studio/src/lib/api/viral-analyses.ts`
- Modify: `studio/src/lib/api/index.ts`
- Modify: `studio/src/types/viral-analysis.ts`
- Modify: `studio/src/types/index.ts`
- Modify: `studio/src/test/mocks/handlers.ts`

- [ ] **Step 1: 写 viral analysis 必须选择 Seednote 项目的表单测试**

新增 schema/form 测试：

```ts
it('keeps viral_analysis when selecting a seednote project', () => {
  const seednoteProject = project({ id: 'seednote-project', platform: 'seednote' })
  const current = {
    ...createTaskFormDefaults(seednoteProject),
    type: 'viral_analysis' as const,
    prompt: 'https://www.xiaohongshu.com/explore/note-1',
  }
  const result = switchTaskFormDefaults(current, seednoteProject)
  expect(result.type).toBe('viral_analysis')
  expect(result.project_id).toBe(seednoteProject.id)
})

it('rejects viral analysis without a seednote project', () => {
  const parsed = createTaskSchema.safeParse({
    project_id: '',
    type: 'viral_analysis',
    prompt: 'https://www.xiaohongshu.com/explore/note-1',
    execution_profile: 'cost_effective',
  })
  expect(parsed.success).toBe(false)
})
```

TaskFormDialog 测试断言 project picker 只展示 Seednote projects，提交走 `api.tasks.create`，payload 同时带 `type: viral_analysis`、Seednote `project_id` 和 `execution_profile`。

- [ ] **Step 2: 运行定向测试并确认失败**

Run: `cd studio && bun run test -- src/lib/schemas.test.ts src/lib/task-form.test.ts src/components/tasks/TaskFormDialog.test.tsx`

Expected: FAIL，切换项目当前会把类型改回 `seednote`。

- [ ] **Step 3: 实现 Studio 任务表单约束**

`viral_analysis` 分支要求 `project_id` 非空，TaskFormDialog 的 project list 过滤为 `platform === 'seednote'`。`switchTaskFormDefaults` 特判保留 `viral_analysis`，但复用所选 Seednote project 的 project ID 和默认上下文；该类型隐藏图片比例、图片模型、参考图、水印和数量，只保留执行配置、Seednote 项目和源笔记链接提示框。

`taskFormValuesToRequest` 必须继续发送 `type`，且 viral analysis 不发送图片字段。

- [ ] **Step 4: 将 legacy API 改为只读**

`studio/src/lib/api/viral-analyses.ts` 删除 `create`，保留 `get`、`list`；删除 `CreateViralAnalysisRequest` 类型及 re-export。MSW 删除 POST handler。新建爆款分析已经只通过 `api.tasks.create`。

- [ ] **Step 5: 运行 Studio 测试和 build**

Run: `cd studio && bun run test -- src/lib/schemas.test.ts src/lib/task-form.test.ts src/components/tasks/TaskFormDialog.test.tsx`

Expected: PASS。

Run: `cd studio && bun run build`

Expected: PASS。

- [ ] **Step 6: 提交 Studio 爆款任务切换**

```bash
git add studio/src/lib/schemas.ts studio/src/lib/schemas.test.ts studio/src/lib/task-form.ts studio/src/lib/task-form.test.ts studio/src/components/tasks/TaskFormDialog.tsx studio/src/components/tasks/TaskFormDialog.test.tsx studio/src/lib/api/viral-analyses.ts studio/src/lib/api/index.ts studio/src/types/viral-analysis.ts studio/src/types/index.ts studio/src/test/mocks/handlers.ts
git commit -m "refactor(studio): create viral analyses as managed tasks"
```

### Task 11: 将 Seednote 发布追踪改为确定性身份绑定

**Files:**
- Modify: `server/model/seednote_tracking.go`
- Modify: `server/service/task.go`
- Modify: `server/service/task_test.go`
- Modify: `server/service/seednote_tracking.go`
- Modify: `server/service/seednote_tracking_test.go`
- Modify: `server/handler/task.go`
- Modify: `server/handler/task_test.go`
- Modify: `server/handler/seednote_analytics_test.go`
- Modify: `server/router/router_test.go`
- Modify: `server/scheduler/scheduler.go`
- Modify: `server/scheduler/scheduler_test.go`
- Modify: `server/main.go`
- Modify: `studio/src/lib/api/tasks.ts`
- Modify: `studio/src/types/seednote-analytics.ts`
- Modify: `studio/src/components/tasks/SeednoteAnalyticsPanel.tsx`
- Modify: `studio/src/components/tasks/SeednoteAnalyticsPanel.test.tsx`

- [ ] **Step 1: 写直接绑定、冲突和 unresolved 测试**

将 `PublishedTrackingService` 改为接收身份对象，并先更新 fake：

```go
type SeednotePublicationIdentity struct {
	NoteID  string `json:"note_id,omitempty"`
	NoteURL string `json:"note_url,omitempty"`
}

type PublishedTrackingService interface {
	EnsureTrackingForPublishedTask(ctx context.Context, userID, taskID string, identity SeednotePublicationIdentity) error
}
```

新增 table-driven 测试：

```go
func TestEnsureTrackingForPublishedTaskUsesDeterministicIdentity(t *testing.T) {
	tests := []struct {
		name, noteID, noteURL, wantID, wantStatus string
		wantErr, wantEnqueue                    bool
	}{
		{name: "url only", noteURL: "https://www.xiaohongshu.com/explore/note-1", wantID: "note-1", wantStatus: model.SeednoteTrackingStatusTracking, wantEnqueue: true},
		{name: "id only", noteID: "note-1", wantID: "note-1", wantStatus: model.SeednoteTrackingStatusTracking, wantEnqueue: true},
		{name: "matching id and url", noteID: "note-1", noteURL: "https://www.xiaohongshu.com/explore/note-1", wantID: "note-1", wantStatus: model.SeednoteTrackingStatusTracking, wantEnqueue: true},
		{name: "conflicting identity", noteID: "note-2", noteURL: "https://www.xiaohongshu.com/explore/note-1", wantErr: true},
		{name: "foreign host", noteURL: "https://example.com/explore/note-1", wantErr: true},
		{name: "missing identity", wantStatus: model.SeednoteTrackingStatusUnresolved},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, enqueuer := setupSeednoteTrackingServiceTest(t)
			userID, _, taskID := createSeednoteTrackingFixtures(t, repo)
			err := svc.EnsureTrackingForPublishedTask(context.Background(), userID, taskID, SeednotePublicationIdentity{NoteID: tt.noteID, NoteURL: tt.noteURL})
			if tt.wantErr { if err == nil { t.Fatal("expected identity error") }; return }
			if err != nil { t.Fatal(err) }
			tracking, err := repo.SeednoteTrackings().FindByTaskID(context.Background(), taskID)
			if err != nil { t.Fatal(err) }
			if tracking.NoteID != tt.wantID || tracking.Status != tt.wantStatus { t.Fatalf("tracking = %#v", tracking) }
			calls := len(enqueuer.now) + len(enqueuer.delayed)
			if (calls > 0) != tt.wantEnqueue { t.Fatalf("enqueue calls = %d", calls) }
		})
	}
}
```

断言有 identity 时状态立即为 `tracking` 并 enqueue capture（不是 discover）；无 identity 时状态为 `unresolved`、`NextRunAt == nil`，不会调用 profile posts 或 LLM。

- [ ] **Step 2: 运行追踪测试并确认失败**

Run: `go test ./server/service ./server/handler -run 'Test(EnsureTrackingForPublishedTask|TaskService_SetPublished|TaskHandler.*Published)' -count=1`

Expected: FAIL，当前接口没有 identity 且会排队 discovery。

- [ ] **Step 3: 实现确定性追踪状态**

在 model 增加：

```go
const SeednoteTrackingStatusUnresolved = "unresolved"
```

`EnsureTrackingForPublishedTask` 先 trim 身份。显式 ID 必须满足现有 Seednote note ID 的长度和安全字符约束；URL 必须是合法 HTTPS Seednote URL，并通过 `platform.ExtractSeednoteNoteID` 取 ID；若显式 ID 与 URL ID 不同，返回 `ErrSeednotePublicationIdentityMismatch`。只有 ID 时生成 canonical `https://www.xiaohongshu.com/explore/<url.PathEscape(noteID)>`，有 URL 时保留原 URL（包括采集所需的 `xsec_token`）。有 ID 时直接写 `tracking`、`NoteID`、`NoteURL`、`TrackingStartedAt` 并 enqueue/call capture；无 ID 时写 `unresolved`，`LastError` 使用稳定用户可见文案“缺少公开笔记 ID 或链接，尚未建立追踪关联”。

删除：

```text
SeednoteTrackingService.llm
seednoteAIMatch
DiscoverPublishedNote
matchPublishedNote
findMatchedCandidate
validPublishedNoteMatch
recordDiscoveryMiss
enqueueDiscover
```

构造函数同步收缩为：

```go
func NewSeednoteTrackingService(
	repo repository.Repository,
	platform SeednotePublicPlatform,
	enqueuer TaskEnqueuer,
	logger *zerolog.Logger,
) *SeednoteTrackingService
```

更新 `server/main.go`、`server/service/seednote_tracking_test.go`、`server/handler/seednote_analytics_test.go` 和 `server/router/router_test.go` 的全部调用点，彻底删除原 `llm LLMClient` 参数，不保留无用途占位参数。

保留历史 `waiting_discovery` 常量以读取旧记录，但 scheduler 不再注册 discovery worker；旧 waiting rows 不再自动猜测。

- [ ] **Step 4: 扩展发布 API**

`TaskService.SetPublished` 改为接收 identity。先调用纯函数完成 trim、Seednote host、ID 提取及 ID/URL 一致性校验：

```go
var (
	ErrSeednotePublicationIDInvalid         = errors.New("invalid seednote publication note id")
	ErrSeednotePublicationURLInvalid        = errors.New("invalid seednote publication note url")
	ErrSeednotePublicationIdentityMismatch  = errors.New("seednote publication identity mismatch")
)

func NormalizeSeednotePublicationIdentity(identity SeednotePublicationIdentity) (SeednotePublicationIdentity, error)
```

只有 `NormalizeSeednotePublicationIdentity` 成功后才能调用 repository `SetPublished`，从而保证 400 不会留下 `published=true`。Handler request：

```go
var body struct {
	Published bool   `json:"published"`
	NoteID    string `json:"note_id,omitempty"`
	NoteURL   string `json:"note_url,omitempty"`
}
```

只在 `published=true` 且 task 是 Seednote 时传给 tracking；非 Seednote 任务忽略 note 字段。缺失身份是合法请求并产生 `unresolved`，冲突或非法身份返回 400 且不得改变 published flag。同步更新所有 `SetPublished` 单元测试调用点，显式传 `SeednotePublicationIdentity{}` 或所需身份。

为非法 ID、非法 URL 和 ID/URL 冲突分别使用可 `errors.Is` 的稳定 sentinel error；`TaskHandler.MarkPublished` 将这三类错误映射为 `fiber.StatusBadRequest`，其余持久化或 enqueue 错误继续返回 500。Handler 测试必须同时断言 400 响应和 repository 中 `published` 仍为 false。

- [ ] **Step 5: 更新 Studio API 和状态文案**

`tasks.markPublished` 改为：

```ts
markPublished: (id: string, published: boolean, identity?: { note_id?: string; note_url?: string }) =>
  unwrap<{ published: boolean }>(http.patch(`/tasks/${id}/published`, { published, ...identity }))
```

类型增加 `'unresolved'`。Analytics 面板新增：

```ts
unresolved: '尚未关联公开笔记，请补充笔记链接或 ID',
```

保留 `waiting_discovery` 文案用于历史记录，但不再描述“明天自动识别”；改成“历史记录等待关联”。`unresolved` 状态在面板内显示“公开笔记链接或 ID”输入和“关联公开笔记”按钮；提交时调用：

```ts
api.tasks.markPublished(taskId, true, value.startsWith('http') ? { note_url: value } : { note_id: value })
```

成功后刷新 task 和 `queryKeys.tasks.seednoteAnalytics(taskId)`。组件测试给 `getByTask` 返回 unresolved tracking，输入 `https://www.xiaohongshu.com/explore/note-1` 并提交，断言 `markPublished` 收到明确 identity；同时证明 unresolved 不显示加载中或自动识别承诺。这样现有 Tasks/TaskDetail 的首次“标记发布”仍可在没有身份时产生 unresolved，用户随后可在同一详情页完成确定性绑定。

- [ ] **Step 6: 运行追踪、scheduler 和 Studio 测试**

Run: `go test ./server/service ./server/handler ./server/scheduler -run 'Test(SeednoteTracking|TaskService_SetPublished|TaskHandler.*Published|TaskProcessor)' -count=1`

Expected: PASS，`rg -n 'DiscoverPublishedNote|matchPublishedNote|TypeSeednoteDiscover' server --glob '*.go'` 无生产代码结果。

Run: `cd studio && bun run test -- src/components/tasks/SeednoteAnalyticsPanel.test.tsx`

Expected: PASS。

- [ ] **Step 7: 提交确定性发布追踪**

```bash
git add server/model/seednote_tracking.go server/service/task.go server/service/task_test.go server/service/seednote_tracking.go server/service/seednote_tracking_test.go server/handler/task.go server/handler/task_test.go server/handler/seednote_analytics_test.go server/router/router_test.go server/scheduler/scheduler.go server/scheduler/scheduler_test.go server/main.go studio/src/lib/api/tasks.ts studio/src/types/seednote-analytics.ts studio/src/components/tasks/SeednoteAnalyticsPanel.tsx studio/src/components/tasks/SeednoteAnalyticsPanel.test.tsx
git commit -m "refactor(seednote): track publications by explicit identity"
```

### Task 12: 全量边界检查、构建与发布前证明

**Files:**
- Modify only if verification exposes a scoped defect in files already listed above.

- [ ] **Step 1: 扫描所有禁止残留**

Run:

```bash
rg -n 'model_routes\.writing|cfg\.Writing|WritingConfig|WritingService|WritingSvc|GetEffectiveWritingConfig|TextUserConfig|TextConfigJSON|text_config_json' server studio/src plugins --glob '!**/node_modules/**'
rg -n 'recognize_live_subjects|recognize_live_invalid_sentences|recognize_live_segments|complete_live_subject|analyze_video_reference' server studio/src plugins --glob '!**/node_modules/**'
rg -n 'viral:analyze|ExecuteAnalysis|analyzeWithLLM|DiscoverPublishedNote|matchPublishedNote' server --glob '*.go'
```

Expected: 第一组只允许 migration SQL 中的 `text_config_json` 和明确的旧键拒绝测试；第二组只允许删除契约测试中的字符串；第三组只允许历史兼容说明测试，不得有生产调用。

- [ ] **Step 2: 验证保留能力和配置**

Run:

```bash
rg -n 'server_internal|image_understanding|video_understanding|analyze_image|analyze_video' server/config.yaml server/config.example.yaml server plugins
```

Expected: `server_internal` 只连到 AI entry；`analyze_image` 与 `analyze_video` 分别连到独立 operations service；不存在跨路由 fallback。

- [ ] **Step 3: 运行格式检查**

Run:

```bash
gofmt -w \
  server/config/config.go server/config/model_routes_config_test.go server/config/live_slice_config_test.go server/config/kubernetes_config_test.go \
  server/billing/catalog_contract_test.go \
  server/migrations/migrations_test.go \
  server/model/constants.go server/model/model_config.go server/model/seednote_tracking.go \
  server/repository/model_config.go \
  server/service/model_client.go server/service/model_json.go server/service/model_json_test.go \
  server/service/content_render.go server/service/content_render_test.go server/service/render_template.go \
  server/service/render_template_test.go server/service/ai_entry.go server/service/ai_entry_test.go \
  server/service/provider_cost.go server/service/provider_cost_test.go server/service/model_config.go \
  server/service/model_config_test.go server/service/model_config_resolve_test.go \
  server/service/billing_catalog.go server/service/billing_catalog_test.go server/service/billing_wallet_test.go \
  server/service/task_image_operations.go server/service/task_image_operations_test.go \
  server/service/task_video_operations.go server/service/task_video_operations_test.go \
  server/service/live_slice.go server/service/live_slice_test.go server/service/task.go server/service/task_test.go \
  server/service/workflow_status.go server/service/workflow_status_test.go \
  server/service/viral_analysis.go server/service/viral_analysis_test.go \
  server/service/seednote_tracking.go server/service/seednote_tracking_test.go \
  server/handler/model_config.go server/handler/model_config_test.go server/handler/project.go \
  server/handler/channel_test.go server/handler/task.go server/handler/task_test.go server/handler/seednote_analytics_test.go \
  server/handler/viral_analysis.go server/router/router.go server/scheduler/scheduler.go \
  server/router/router_test.go server/scheduler/scheduler_test.go server/mcp/tools.go server/mcp/tools_test.go server/mcp/content_render_tools.go \
  server/mcp/content_render_tools_test.go server/mcp/image_tools.go server/mcp/image_tools_test.go \
  server/mcp/video_understanding_tools.go server/mcp/video_understanding_tools_test.go \
  server/mcp/live_slice_tools.go server/mcp/live_slice_tools_test.go server/mcp/live_slice_skill_test.go \
  server/mcp/boundary_contract_test.go server/agent/config_builder.go server/agent/executor.go server/agent/executor_test.go \
  server/agent/artifacts.go server/agent/artifacts_test.go \
  server/agent/runtime_policy.go server/agent/runtime_policy_test.go server/agent/server_workspace_contract_test.go \
  server/agent/seednote_skill_contract_test.go server/main.go
```

Expected: 无格式差异之外的代码变化。

Run: `git diff --check`

Expected: 无 trailing whitespace 或冲突标记。

- [ ] **Step 4: 运行完整 Go 验证**

Run: `go test ./...`

Expected: PASS。

Run: `go build -o /tmp/anban-creator-server ./server && go build -o /tmp/anban ./agent`

Expected: 两个 binary 构建成功。

- [ ] **Step 5: 运行完整 Studio 验证**

Run: `cd studio && bun run test`

Expected: PASS。

Run: `cd studio && bun run build`

Expected: `tsc -b && vite build` PASS。

- [ ] **Step 6: 验证插件 child/parent SHA**

Run:

```bash
git -C plugins status --short --branch
git -C plugins rev-parse HEAD
git -C plugins rev-parse origin/main
git ls-tree HEAD plugins
```

Expected: child 工作区干净，child HEAD 等于 child `origin/main`，父仓库 gitlink 指向同一 SHA。

- [ ] **Step 7: 处理验证失败**

若验证失败，回到拥有该文件的 Task，先补失败测试、再修实现、重跑该 Task 的定向命令，并使用该 Task 已列出的精确 `git add` 文件表提交；不得用 broad add 混入用户的其他改动。

- [ ] **Step 8: 记录部署顺序**

实施完成后的部署顺序必须是：先执行 `20260729_remove_user_text_model_config.sql`，准备 `server_internal` 配置和 `retail-2026-07-29-v6` billing catalog，再部署新 Server 与 `4.0.8` plugin runtime images。旧 Server 不能读取新配置，新 Server 也会故意拒绝旧 `model_routes.writing`，因此不支持混合版本滚动；需采用维护窗口或先准备配置与迁移、再一次性替换 Server Pod。启动日志必须证明 v6 catalog 已发布且三条 viral-analysis profile SKU 可解析。`analyze_video` 只有在所选 provider 真正支持原生视频 content part 时才可用，代码测试不能替代一次受控 provider smoke。

---

## 规格自检结果

- `server_internal` 唯一调用方、旧 `writing` 启动失败、无用户覆盖：Task 1-3。
- 文章正文继续由 Claude Agent 生成，Server 只保留确定性渲染：Task 2。
- Seednote 画像不再隐藏补全，项目图片分析只用专用路由：Task 5。
- `analyze_image` 保留，`analyze_video` 原子恢复且没有旧别名、抽帧或文本 fallback：Task 6。
- 四个直播语义 MCP 删除，Agent 直接生成并自检 JSON：Task 7-8。
- 爆款分析显式映射 Seednote Agent、冻结 execution profile、标准 task execution 和终态 usage 结算，旧记录只读：Task 8-10。
- Seednote 发布追踪只接受明确 ID/URL，缺失时 unresolved，不再猜测：Task 11。
- 用户图片模型配置保留、文本字段和数据库列删除：Task 3-4。
- 双插件清单同步 bump，child 先发布再更新 parent gitlink：Task 8、12。
- provider cost：AI 入口、图片理解、视频理解分别记录；Agent 文本推理只走终态 usage，不再双计：Task 2、6、9。
