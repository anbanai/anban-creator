# Montage Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Montage as an independent Anban platform with its own server input model, Studio flow, agent contract, artifact validation, and submodule-based upgrade path.

**Architecture:** Montage is a peer platform named `montage`, independent from `videocreator` and `videoeditor`. Anban stores a stable `montage_input`, resolves execution target by system policy, runs a dedicated `montage` agent against the `third_party/OpenMontage` submodule, and validates normalized Anban task files.

**Tech Stack:** Go 1.26, Fiber v3, GORM typed JSON columns, React 19, TypeScript, Vite 8, Bun, Vitest, Claude/Codex/OpenClaw plugin assets, git submodule.

---

## File Structure

Create:

- `server/model/montage.go` - Montage input, asset, preferences, defaults, and helper setters.
- `server/config/montage_test.go` - config defaults and validation tests.
- `server/service/montage_execution_target.go` - system-owned execution target resolver.
- `server/service/montage_execution_target_test.go` - resolver behavior tests.
- `server/agent/montage_contract_test.go` - agent/plugin contract tests.
- `studio/src/types/montage.ts` - Studio Montage TypeScript types.
- `studio/src/lib/montage-form.ts` - Studio form defaults and submit normalization.
- `studio/src/lib/montage-form.test.ts` - Studio form normalization tests.
- `studio/src/components/montage/MontageCreationPanel.tsx` - dedicated Montage input panel.
- `studio/src/components/montage/MontageCreationPanel.test.tsx` - panel interaction tests.
- `claudecode/agents/montage.md` - Claude Code Montage agent.
- `codex/agents/montage.toml` - Codex Montage agent.
- `claudecode/skills/montage/SKILL.md` - Claude Code Montage skill.
- `codex/skills/montage/SKILL.md` - Codex Montage skill.
- `openclaw/skills/montage/SKILL.md` - OpenClaw Montage skill.
- `docs/montage-upgrade.md` - submodule update and contract-test procedure.

Modify:

- `server/model/constants.go` - add `ScopeMontage`, `PlatformMontage`, `IsMontagePlatform`.
- `server/model/task.go` - add `MontageInput` typed JSON column and setter.
- `server/model/plan.go` - add `MontageInput` typed JSON column and setter.
- `server/model/project.go` - add Montage project defaults to project snapshot.
- `server/config/config.go` - add `MontageConfig`.
- `server/config.yaml` - add `montage` block.
- `server/service/project.go` - accept Montage as a valid project platform.
- `server/service/task.go` - accept and validate `montage_input`, clamp quantity to one, resolve execution target.
- `server/service/plan.go` - support Montage plans and stored input.
- `server/service/task_execution.go` - validate Montage deliverables.
- `server/service/task_progress_stages.go` - add Montage progress defaults.
- `server/service/credit.go` and related credit tests/config - add base task pricing.
- `server/handler/task.go` - bind, validate, and respond with `montage_input`.
- `server/handler/plan.go` - bind and respond with `montage_input`.
- `server/handler/project.go` - accept Montage project defaults.
- `server/agent/config_builder.go` - map Montage tasks to `montage` agent.
- `server/agent/executor.go` - write `montage-input.json` into task workspace.
- `server/agent/artifacts.go` - artifact validation for local/workdir fallback.
- `studio/src/types/task.ts`, `studio/src/types/project.ts`, `studio/src/types/plan.ts`, `studio/src/types/index.ts` - add Montage types.
- `studio/src/lib/schemas.ts` and `studio/src/lib/schemas.test.ts` - add schema support.
- `studio/src/lib/labels.ts`, `studio/src/lib/pricing.ts`, `studio/src/lib/credit-display.ts`, `studio/src/lib/PlatformIcon.tsx`, `studio/src/lib/command-center.ts` - labels and navigation.
- `studio/src/pages/TasksPage.tsx` - show Montage panel and submit payload.
- `studio/src/pages/PlansPage.tsx` - show Montage plan panel and submit payload.
- `studio/src/components/FilePreview.tsx` - display Montage final video and manifests.
- `codex/install/agents-registration.toml` - register Codex Montage agent.
- `claudecode/.claude-plugin/plugin.json`, `openclaw/openclaw.plugin.json`, `codex/.codex-plugin/plugin.json` - patch version bump and description update.
- `.gitmodules` - add Montage submodule.

## Task 1: Submodule, Config, And Model Foundation

**Files:**
- Create: `server/model/montage.go`
- Create: `server/config/montage_test.go`
- Modify: `.gitmodules`
- Modify: `server/model/constants.go`
- Modify: `server/model/task.go`
- Modify: `server/model/plan.go`
- Modify: `server/model/project.go`
- Modify: `server/config/config.go`
- Modify: `server/config.yaml`
- Test: `server/config/montage_test.go`
- Test: existing `server/service/channel_test.go`

- [ ] **Step 1: Add Montage as a git submodule**

Run:

```bash
git submodule add https://github.com/calesthio/OpenMontage.git third_party/OpenMontage
```

Expected: `.gitmodules` is created or updated with:

```ini
[submodule "third_party/OpenMontage"]
	path = third_party/OpenMontage
	url = https://github.com/calesthio/OpenMontage.git
```

- [ ] **Step 2: Write failing config test**

Create `server/config/montage_test.go`:

```go
package config

import "testing"

func TestMontageConfigDefaults(t *testing.T) {
	cfg := MontageConfig{}
	cfg.ApplyDefaults()

	if !cfg.Enabled {
		t.Fatal("Enabled = false, want true")
	}
	if cfg.SubmodulePath != "third_party/OpenMontage" {
		t.Fatalf("SubmodulePath = %q, want %q", cfg.SubmodulePath, "third_party/OpenMontage")
	}
	if cfg.DefaultPipeline != "default" {
		t.Fatalf("DefaultPipeline = %q, want default", cfg.DefaultPipeline)
	}
	if cfg.MaxDurationSeconds != 600 {
		t.Fatalf("MaxDurationSeconds = %d, want 600", cfg.MaxDurationSeconds)
	}
	if cfg.MaxAssets != 20 {
		t.Fatalf("MaxAssets = %d, want 20", cfg.MaxAssets)
	}
	if cfg.TimeoutMinutes != 90 {
		t.Fatalf("TimeoutMinutes = %d, want 90", cfg.TimeoutMinutes)
	}
	if cfg.DefaultExecutionTarget != "cloud" {
		t.Fatalf("DefaultExecutionTarget = %q, want cloud", cfg.DefaultExecutionTarget)
	}
	if cfg.Runner.CloudImage != "anban/montage-runner:latest" {
		t.Fatalf("CloudImage = %q, want default runner image", cfg.Runner.CloudImage)
	}
}

func TestMontageConfigValidate(t *testing.T) {
	cfg := MontageConfig{
		Enabled:                true,
		SubmodulePath:          "third_party/OpenMontage",
		DefaultPipeline:        "default",
		AllowedPipelines:       []string{"default", "social-short"},
		MaxDurationSeconds:     600,
		MaxAssets:              20,
		TimeoutMinutes:         90,
		ExecutionTargets:       []string{"cloud", "local"},
		DefaultExecutionTarget: "cloud",
		Runner: MontageRunnerConfig{
			CloudImage: "anban/montage-runner:latest",
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate error = %v", err)
	}

	cfg.DefaultPipeline = "missing"
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate succeeded with default pipeline outside allowed list")
	}
}
```

- [ ] **Step 3: Run config test and verify it fails**

Run:

```bash
go test ./server/config -run TestMontageConfig -count=1
```

Expected: FAIL with `undefined: MontageConfig`.

- [ ] **Step 4: Add Montage model types**

Create `server/model/montage.go`:

```go
package model

import "gorm.io/datatypes"

type MontageInput struct {
	Brief           string                  `json:"brief,omitempty"`
	PipelineKey     string                  `json:"pipeline_key,omitempty"`
	SourceAssets    []MontageAsset      `json:"source_assets,omitempty"`
	Preferences     MontagePreferences  `json:"preferences,omitempty"`
	DeliveryTargets []string                `json:"delivery_targets,omitempty"`
	Advanced        map[string]any          `json:"advanced,omitempty"`
}

type MontageAsset struct {
	Type       string `json:"type"`
	URL        string `json:"url,omitempty"`
	TaskFileID string `json:"task_file_id,omitempty"`
	Text       string `json:"text,omitempty"`
	FileName   string `json:"file_name,omitempty"`
	MimeType   string `json:"mime_type,omitempty"`
	FileSize   int64  `json:"file_size,omitempty"`
}

type MontagePreferences struct {
	AspectRatio     string `json:"aspect_ratio,omitempty"`
	DurationSeconds int64  `json:"duration_seconds,omitempty"`
	Style           string `json:"style,omitempty"`
	MusicPrompt     string `json:"music_prompt,omitempty"`
	SubtitleMode    string `json:"subtitle_mode,omitempty"`
	VoiceoverMode   string `json:"voiceover_mode,omitempty"`
}

type MontageDefaults struct {
	DefaultPipeline  string                 `json:"default_pipeline,omitempty"`
	Preferences      MontagePreferences `json:"preferences,omitempty"`
	AssetGuidance    string                 `json:"asset_guidance,omitempty"`
	DeliveryTargets  []string               `json:"delivery_targets,omitempty"`
}

func (t *Task) SetMontageInput(input MontageInput) {
	t.MontageInput = datatypes.NewJSONType(input)
}

func (p *Plan) SetMontageInput(input MontageInput) {
	p.MontageInput = datatypes.NewJSONType(input)
}

func (p *Project) SetMontageDefaults(defaults MontageDefaults) {
	p.MontageDefaults = datatypes.NewJSONType(defaults)
}
```

- [ ] **Step 5: Add constants and JSON columns**

Modify `server/model/constants.go`:

```go
const (
	ScopeArticle      = "article"
	ScopeSeednote     = "seednote"
	ScopeMoments      = "moments"
	ScopeEcommerce    = "ecommerce"
	ScopeVideoCreator = "videocreator"
	ScopeVideoEditor  = "videoeditor"
	ScopeMontage  = "montage"
)

const (
	PlatformArticle      = "article"
	PlatformSeednote     = "seednote"
	PlatformMoments      = "moments"
	PlatformEcommerce    = "ecommerce"
	PlatformVideoCreator = "videocreator"
	PlatformVideoEditor  = "videoeditor"
	PlatformMontage  = "montage"
)

func IsMontagePlatform(platform string) bool {
	return platform == PlatformMontage
}
```

Modify `server/model/task.go` by adding the typed JSON field near `VideoInput`:

```go
MontageInput datatypes.JSONType[MontageInput] `gorm:"type:json" json:"montage_input"`
```

Modify `server/model/plan.go` by adding the typed JSON field near `VideoInput`:

```go
MontageInput datatypes.JSONType[MontageInput] `gorm:"type:json" json:"montage_input"`
```

Modify `server/model/project.go`:

```go
MontageDefaults datatypes.JSONType[MontageDefaults] `gorm:"type:json" json:"montage_defaults"`
MontageDefaultsSet bool                                 `gorm:"-" json:"-"`
```

Extend `ProjectSnapshot` in `server/model/task.go`:

```go
MontageDefaults MontageDefaults `json:"montage_defaults,omitempty"`
```

- [ ] **Step 6: Implement config structs**

Modify `server/config/config.go`:

```go
type Config struct {
	Server       ServerConfig       `yaml:"server"`
	Logging      LoggingConfig      `yaml:"logging"`
	Database     DatabaseConfig     `yaml:"database"`
	Redis        RedisConfig        `yaml:"redis"`
	JWT          JWTConfig          `yaml:"jwt"`
	WeChat       WeChatConfig       `yaml:"wechat"`
	Storage      StorageConfig      `yaml:"storage"`
	MCP          MCPConfig          `yaml:"mcp"`
	ImageAPI     ImageAPIConfig     `yaml:"image_api"`
	VideoAPI     VideoAPIConfig     `yaml:"video_api"`
	Montage  MontageConfig  `yaml:"montage"`
	ImagePresets []ImageModelPreset `yaml:"image_presets"`
}
```

Keep the existing fields after `ImagePresets`; this snippet only shows the insertion point.

Add structs:

```go
type MontageConfig struct {
	Enabled                bool                      `yaml:"enabled"`
	SubmodulePath          string                    `yaml:"submodule_path"`
	DefaultPipeline         string                    `yaml:"default_pipeline"`
	AllowedPipelines        []string                  `yaml:"allowed_pipelines"`
	MaxDurationSeconds      int64                     `yaml:"max_duration_seconds"`
	MaxAssets               int                       `yaml:"max_assets"`
	TimeoutMinutes          int                       `yaml:"timeout_minutes"`
	ExecutionTargets        []string                  `yaml:"execution_targets"`
	DefaultExecutionTarget  string                    `yaml:"default_execution_target"`
	CreditCost              int                       `yaml:"credit_cost"`
	Runner                  MontageRunnerConfig   `yaml:"runner"`
}

type MontageRunnerConfig struct {
	CloudImage string `yaml:"cloud_image"`
}

func (c *MontageConfig) ApplyDefaults() {
	c.Enabled = true
	if c.SubmodulePath == "" {
		c.SubmodulePath = "third_party/OpenMontage"
	}
	if c.DefaultPipeline == "" {
		c.DefaultPipeline = "default"
	}
	if len(c.AllowedPipelines) == 0 {
		c.AllowedPipelines = []string{c.DefaultPipeline}
	}
	if c.MaxDurationSeconds <= 0 {
		c.MaxDurationSeconds = 600
	}
	if c.MaxAssets <= 0 {
		c.MaxAssets = 20
	}
	if c.TimeoutMinutes <= 0 {
		c.TimeoutMinutes = 90
	}
	if len(c.ExecutionTargets) == 0 {
		c.ExecutionTargets = []string{"cloud", "local"}
	}
	if c.DefaultExecutionTarget == "" {
		c.DefaultExecutionTarget = "cloud"
	}
	if c.CreditCost <= 0 {
		c.CreditCost = 2000
	}
	if c.Runner.CloudImage == "" {
		c.Runner.CloudImage = "anban/montage-runner:latest"
	}
}

func (c MontageConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.SubmodulePath) == "" {
		return fmt.Errorf("montage.submodule_path is required")
	}
	if strings.TrimSpace(c.DefaultPipeline) == "" {
		return fmt.Errorf("montage.default_pipeline is required")
	}
	if c.MaxDurationSeconds <= 0 {
		return fmt.Errorf("montage.max_duration_seconds must be positive")
	}
	if c.MaxAssets <= 0 {
		return fmt.Errorf("montage.max_assets must be positive")
	}
	if c.TimeoutMinutes <= 0 {
		return fmt.Errorf("montage.timeout_minutes must be positive")
	}
	if !stringSliceContains(c.AllowedPipelines, c.DefaultPipeline) {
		return fmt.Errorf("montage.default_pipeline must be in montage.allowed_pipelines")
	}
	if !validMontageTarget(c.DefaultExecutionTarget) {
		return fmt.Errorf("montage.default_execution_target must be cloud or local")
	}
	if !stringSliceContains(c.ExecutionTargets, c.DefaultExecutionTarget) {
		return fmt.Errorf("montage.default_execution_target must be in montage.execution_targets")
	}
	for _, target := range c.ExecutionTargets {
		if !validMontageTarget(target) {
			return fmt.Errorf("montage.execution_targets contains invalid target %q", target)
		}
	}
	return nil
}

func validMontageTarget(target string) bool {
	return target == "cloud" || target == "local"
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
```

If `config.go` already has a helper equivalent to `stringSliceContains`, reuse it and do not duplicate the helper.

- [ ] **Step 7: Wire config defaults and validation into load path**

Find the config load/defaulting function in `server/config/config.go`. Add:

```go
cfg.Montage.ApplyDefaults()
if err := cfg.Montage.Validate(); err != nil {
	return nil, err
}
```

Place it next to other per-section default/validation calls.

- [ ] **Step 8: Add YAML config block**

Modify `server/config.yaml` after `video_generation` or near video settings:

```yaml
montage:
  enabled: true
  submodule_path: "third_party/OpenMontage"
  default_pipeline: "default"
  allowed_pipelines: ["default"]
  max_duration_seconds: 600
  max_assets: 20
  timeout_minutes: 90
  execution_targets: ["cloud", "local"]
  default_execution_target: "cloud"
  credit_cost: 2000
  runner:
    cloud_image: "${ANBAN_MONTAGE_RUNNER_IMAGE:-anban/montage-runner:latest}"
```

- [ ] **Step 9: Run model/config tests**

Run:

```bash
go test ./server/config -run TestMontageConfig -count=1
go test ./server/model -count=1
```

Expected: PASS.

- [ ] **Step 10: Commit foundation**

Run:

```bash
git add .gitmodules third_party/OpenMontage server/model/montage.go server/model/constants.go server/model/task.go server/model/plan.go server/model/project.go server/config/config.go server/config.yaml server/config/montage_test.go
git commit -m "feat: add montage model and config"
```

## Task 2: Server Task Creation And Plan Flow

**Files:**
- Modify: `server/handler/task.go`
- Modify: `server/handler/plan.go`
- Modify: `server/service/task.go`
- Modify: `server/service/plan.go`
- Modify: `server/service/task_retry.go`
- Modify: `server/service/credit.go`
- Modify: `server/service/project.go`
- Test: `server/service/task_test.go`
- Test: `server/service/plan_test.go`
- Test: `server/handler/task_test.go`
- Test: `server/handler/plan_test.go`

- [ ] **Step 1: Write failing task-service test**

Append to `server/service/task_test.go`:

```go
func TestTaskServiceCreateManualMontageStoresInputAndClampsQuantity(t *testing.T) {
	repo := setupTestRepo(t)
	userID := "user-om"
	projectID := createTestProject(t, repo, userID, model.PlatformMontage)
	svc := newTestTaskService(repo)

	tasks, err := svc.CreateManual(context.Background(), CreateManualParams{
		UserID:    userID,
		ProjectID: projectID,
		Quantity:  3,
		MontageInput: &model.MontageInput{
			Brief:       "做一条新品发布短片",
			PipelineKey: "default",
			Preferences: model.MontagePreferences{
				AspectRatio:     "9:16",
				DurationSeconds: 30,
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateManual error = %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("len(tasks) = %d, want 1", len(tasks))
	}
	got := tasks[0].MontageInput.Data()
	if got.Brief != "做一条新品发布短片" || got.PipelineKey != "default" {
		t.Fatalf("montage input = %#v", got)
	}
	if got.Preferences.AspectRatio != "9:16" || got.Preferences.DurationSeconds != 30 {
		t.Fatalf("preferences = %#v", got.Preferences)
	}
}

func TestTaskServiceCreateManualRejectsMontageInputForOtherPlatforms(t *testing.T) {
	repo := setupTestRepo(t)
	userID := "user-om-reject"
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	svc := newTestTaskService(repo)

	_, err := svc.CreateManual(context.Background(), CreateManualParams{
		UserID:    userID,
		ProjectID: projectID,
		Prompt:    "春季穿搭",
		MontageInput: &model.MontageInput{
			Brief: "错误平台",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "montage_input can only be set on montage tasks") {
		t.Fatalf("CreateManual error = %v, want montage input rejection", err)
	}
}
```

- [ ] **Step 2: Run task-service tests and verify failure**

Run:

```bash
go test ./server/service -run 'TestTaskServiceCreateManualMontage' -count=1
```

Expected: FAIL with `unknown field MontageInput`.

- [ ] **Step 3: Add service params and validation**

Modify `server/service/task.go` `CreateManualParams`:

```go
MontageInput *model.MontageInput
```

In `CreateManual`, after resolving `taskType`, add:

```go
if p.MontageInput != nil && !model.IsMontagePlatform(taskType) {
	return nil, fmt.Errorf("%w: montage_input can only be set on montage tasks", ErrVideoTaskInput)
}
if model.IsMontagePlatform(taskType) {
	quantity = 1
	if p.MontageInput == nil || strings.TrimSpace(p.MontageInput.Brief) == "" {
		return nil, fmt.Errorf("montage task requires brief")
	}
}
```

In task construction, after `SetVideoInput` block, add:

```go
if model.IsMontagePlatform(taskType) && p.MontageInput != nil {
	task.SetMontageInput(*p.MontageInput)
}
```

- [ ] **Step 4: Add handler request binding**

Modify `server/handler/task.go` `createTaskRequest`:

```go
MontageInput *model.MontageInput `json:"montage_input,omitempty"`
```

Pass it into `service.CreateManualParams`:

```go
MontageInput: req.MontageInput,
```

Add request guard after video field validation:

```go
if req.MontageInput != nil && strings.TrimSpace(req.MontageInput.Brief) == "" {
	return Error(c, fiber.StatusBadRequest, "montage_input.brief is required")
}
```

- [ ] **Step 5: Add task API response field**

Find `taskAPIResponse` in `server/handler/task.go`. Add:

```go
if model.IsMontagePlatform(task.Type) {
	resp["montage_input"] = task.MontageInput.Data()
}
```

- [ ] **Step 6: Write failing plan-service test**

Append to `server/service/plan_test.go`:

```go
func TestPlanServiceCreateMontagePlanStoresInput(t *testing.T) {
	repo := setupTestRepo(t)
	userID := "plan-om-user"
	projectID := createTestProject(t, repo, userID, model.PlatformMontage)
	svc := NewPlanService(repo, testLogger())

	plan, err := svc.Create(context.Background(), CreatePlanParams{
		UserID:    userID,
		ProjectID: projectID,
		CronExpr:  "0 10 * * *",
		MontageInput: &model.MontageInput{
			Brief:       "每天做一条新品短片",
			PipelineKey: "default",
		},
	})
	if err != nil {
		t.Fatalf("Create error = %v", err)
	}
	if plan.Type != model.PlatformMontage {
		t.Fatalf("plan.Type = %q, want montage", plan.Type)
	}
	got := plan.MontageInput.Data()
	if got.Brief != "每天做一条新品短片" || got.PipelineKey != "default" {
		t.Fatalf("montage input = %#v", got)
	}
}
```

- [ ] **Step 7: Run plan test and verify failure**

Run:

```bash
go test ./server/service -run TestPlanServiceCreateMontagePlanStoresInput -count=1
```

Expected: FAIL with `unknown field MontageInput`.

- [ ] **Step 8: Add plan support**

Modify `server/service/plan.go`:

```go
type CreatePlanParams struct {
	UserID             string
	ProjectID          string
	CronExpr           string
	Prompt             string
	ImageModelKey      string
	SkipReferenceImage *bool
	ReferenceImageURL  string
	Watermark          *bool
	Goal               string
	GoalMode           bool
	HasContentImage    *bool
	HasTailImage       *bool
	ArticleWithCover   *bool
	ArticleWithContentImages *bool
	VideoCreatorConfig *model.VideoTaskConfig
	VideoCreatorInput  *model.VideoInput
	MontageInput   *model.MontageInput
}
```

Add validation:

```go
if p.MontageInput != nil && !model.IsMontagePlatform(project.Platform) {
	return nil, fmt.Errorf("%w: montage_input can only be set on montage plans", ErrVideoTaskInput)
}
if model.IsMontagePlatform(project.Platform) && (p.MontageInput == nil || strings.TrimSpace(p.MontageInput.Brief) == "") {
	return nil, fmt.Errorf("montage plan requires montage_input.brief")
}
```

Store input:

```go
if model.IsMontagePlatform(project.Platform) && p.MontageInput != nil {
	plan.SetMontageInput(*p.MontageInput)
}
```

Ensure the existing unsupported-platform switch does not reject `PlatformMontage`.

- [ ] **Step 9: Bind Montage input in plan handler**

Modify `server/handler/plan.go` `createPlanRequest` and `updatePlanRequest`:

```go
MontageInput *model.MontageInput `json:"montage_input,omitempty"`
```

Pass it to service create/update params:

```go
MontageInput: req.MontageInput,
```

Add API response:

```go
if model.IsMontagePlatform(plan.Type) {
	resp["montage_input"] = plan.MontageInput.Data()
}
```

- [ ] **Step 10: Add plan trigger propagation**

Find `CreateFromPlan` in `server/service/task.go`. Add:

```go
if model.IsMontagePlatform(plan.Type) {
	input := plan.MontageInput.Data()
	params.MontageInput = &input
}
```

Place it next to existing video plan propagation.

- [ ] **Step 11: Add retry/clone propagation**

Modify `server/service/task_retry.go` so Montage retries preserve input:

```go
if model.IsMontagePlatform(src.Type) {
	input := src.MontageInput.Data()
	params.MontageInput = &input
}
```

- [ ] **Step 12: Run service and handler tests**

Run:

```bash
go test ./server/service -run 'Montage|CreateFromPlan|Retry' -count=1
go test ./server/handler -run 'Montage|Task|Plan' -count=1
```

Expected: PASS for Montage tests. Existing unrelated handler tests may also run; fix any compile errors caused by new fields.

- [ ] **Step 13: Commit server creation flow**

Run:

```bash
git add server/handler/task.go server/handler/plan.go server/service/task.go server/service/plan.go server/service/task_retry.go server/service/task_test.go server/service/plan_test.go
git commit -m "feat: support montage task and plan input"
```

## Task 3: Execution Target Resolver, Billing, And Progress

**Files:**
- Create: `server/service/montage_execution_target.go`
- Create: `server/service/montage_execution_target_test.go`
- Modify: `server/service/credit.go`
- Modify: `server/service/credit_test.go`
- Modify: `server/service/task_progress_stages.go`
- Modify: `server/config.yaml`

- [ ] **Step 1: Write failing resolver test**

Create `server/service/montage_execution_target_test.go`:

```go
package service

import (
	"testing"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

func TestResolveMontageExecutionTargetDefaultsCloud(t *testing.T) {
	got, err := ResolveMontageExecutionTarget(MontageExecutionTargetRequest{
		Config: srvconfig.MontageConfig{
			Enabled:                true,
			ExecutionTargets:       []string{"cloud", "local"},
			DefaultExecutionTarget: "cloud",
		},
		TaskType: model.PlatformMontage,
	})
	if err != nil {
		t.Fatalf("ResolveMontageExecutionTarget error = %v", err)
	}
	if got != model.ExecutionTargetCloud {
		t.Fatalf("target = %q, want cloud empty target", got)
	}
}

func TestResolveMontageExecutionTargetRejectsWhenDisabled(t *testing.T) {
	_, err := ResolveMontageExecutionTarget(MontageExecutionTargetRequest{
		Config: srvconfig.MontageConfig{Enabled: false},
		TaskType: model.PlatformMontage,
	})
	if err == nil {
		t.Fatal("ResolveMontageExecutionTarget succeeded when disabled")
	}
}

func TestResolveMontageExecutionTargetKeepsLocalDisabledWithoutCapability(t *testing.T) {
	_, err := ResolveMontageExecutionTarget(MontageExecutionTargetRequest{
		Config: srvconfig.MontageConfig{
			Enabled:                true,
			ExecutionTargets:       []string{"local"},
			DefaultExecutionTarget: "local",
		},
		TaskType:       model.PlatformMontage,
		LocalAvailable: false,
	})
	if err == nil {
		t.Fatal("ResolveMontageExecutionTarget succeeded without local capability")
	}
}
```

- [ ] **Step 2: Run resolver test and verify failure**

Run:

```bash
go test ./server/service -run TestResolveMontageExecutionTarget -count=1
```

Expected: FAIL with `undefined: ResolveMontageExecutionTarget`.

- [ ] **Step 3: Implement resolver**

Create `server/service/montage_execution_target.go`:

```go
package service

import (
	"fmt"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

type MontageExecutionTargetRequest struct {
	Config          srvconfig.MontageConfig
	TaskType        string
	FromPlan        bool
	LocalAvailable  bool
	CloudAvailable  bool
	AssetsCloudSafe bool
}

func ResolveMontageExecutionTarget(req MontageExecutionTargetRequest) (string, error) {
	if !model.IsMontagePlatform(req.TaskType) {
		return model.ExecutionTargetCloud, nil
	}
	cfg := req.Config
	cfg.ApplyDefaults()
	if !cfg.Enabled {
		return "", fmt.Errorf("montage is disabled")
	}
	if req.FromPlan {
		if containsMontageTarget(cfg.ExecutionTargets, "cloud") {
			return model.ExecutionTargetCloud, nil
		}
		return "", fmt.Errorf("montage plans require an available cloud execution target")
	}
	target := cfg.DefaultExecutionTarget
	if target == "cloud" {
		return model.ExecutionTargetCloud, nil
	}
	if target == "local" {
		if req.LocalAvailable {
			return model.ExecutionTargetLocal, nil
		}
		if containsMontageTarget(cfg.ExecutionTargets, "cloud") {
			return model.ExecutionTargetCloud, nil
		}
		return "", fmt.Errorf("montage local execution is unavailable")
	}
	return "", fmt.Errorf("invalid montage execution target %q", target)
}

func containsMontageTarget(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Wire resolver into task creation**

Add `montageCfg srvconfig.MontageConfig` field to `TaskService` and a setter:

```go
func (s *TaskService) SetMontageConfig(cfg srvconfig.MontageConfig) {
	if s == nil {
		return
	}
	cfg.ApplyDefaults()
	s.montageCfg = cfg
}
```

In `CreateManual`, when task type is Montage:

```go
target, err := ResolveMontageExecutionTarget(MontageExecutionTargetRequest{
	Config:          s.montageCfg,
	TaskType:        taskType,
	LocalAvailable:  false,
	CloudAvailable:  true,
	AssetsCloudSafe: true,
})
if err != nil {
	return nil, err
}
p.ExecutionTarget = target
```

This intentionally does not expose target choice to Studio. Later local capability work can flip `LocalAvailable`.

- [ ] **Step 5: Add credit cost**

Modify credit task-cost defaults in `server/service/credit.go`:

```go
case model.ScopeMontage:
	return 2000, true
```

If the code uses a map, add:

```go
model.PlatformMontage: 2000,
```

Append to `server/service/credit_test.go`:

```go
func TestCreditServiceIncludesMontageTaskCost(t *testing.T) {
	svc := NewCreditService(nil, nil)
	cost, ok := svc.TaskCost(model.PlatformMontage)
	if !ok {
		t.Fatal("TaskCost(montage) ok = false")
	}
	if cost != 2000 {
		t.Fatalf("TaskCost(montage) = %d, want 2000", cost)
	}
}
```

- [ ] **Step 6: Add progress defaults**

Modify `server/service/task_progress_stages.go`:

```go
var montageStagePercent = map[string]int{
	"prepare":   10,
	"assets":    20,
	"pipeline":  35,
	"render":    70,
	"delivery":  90,
	"completed": 100,
}
```

In `defaultPercentForStage`:

```go
case model.PlatformMontage:
	return montageStagePercent[stage]
```

- [ ] **Step 7: Run tests**

Run:

```bash
go test ./server/service -run 'Montage|CreditServiceIncludesMontage|Progress' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit resolver and billing**

Run:

```bash
git add server/service/montage_execution_target.go server/service/montage_execution_target_test.go server/service/task.go server/service/credit.go server/service/credit_test.go server/service/task_progress_stages.go
git commit -m "feat: resolve montage execution and billing"
```

## Task 4: Agent Workspace, Mapping, And Artifact Validation

**Files:**
- Create: `server/agent/montage_contract_test.go`
- Modify: `server/agent/config_builder.go`
- Modify: `server/agent/executor.go`
- Modify: `server/agent/artifacts.go`
- Modify: `server/service/task_execution.go`
- Test: `server/agent/montage_contract_test.go`
- Test: `server/service/task_test.go`

- [ ] **Step 1: Write failing agent mapping test**

Create `server/agent/montage_contract_test.go`:

```go
package agent

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestMontageTaskMapsToDedicatedAgent(t *testing.T) {
	if got := TaskTypeToAgent(model.PlatformMontage); got != "montage" {
		t.Fatalf("TaskTypeToAgent(montage) = %q, want montage", got)
	}
}

func TestMontagePluginContractsExist(t *testing.T) {
	root := repoRoot(t)
	paths := []string{
		filepath.Join(root, "claudecode", "agents", "montage.md"),
		filepath.Join(root, "codex", "agents", "montage.toml"),
		filepath.Join(root, "claudecode", "skills", "montage", "SKILL.md"),
		filepath.Join(root, "codex", "skills", "montage", "SKILL.md"),
		filepath.Join(root, "openclaw", "skills", "montage", "SKILL.md"),
	}
	for _, path := range paths {
		text := readRepoFile(t, path)
		for _, want := range []string{"Montage", "montage-input.json", "delivery-manifest.json", "final_video"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q", path, want)
			}
		}
		for _, forbidden := range []string{"create_video_generation_job", "seedance-20", "video-use"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("%s must not reuse existing video flow %q", path, forbidden)
			}
		}
	}
}
```

- [ ] **Step 2: Run mapping test and verify failure**

Run:

```bash
go test ./server/agent -run TestMontage -count=1
```

Expected: FAIL because `TaskTypeToAgent(montage)` returns the fallback or plugin files are missing.

- [ ] **Step 3: Add agent mapping**

Modify `server/agent/config_builder.go`:

```go
case model.ScopeMontage:
	return "montage"
```

- [ ] **Step 4: Write workspace input file**

Modify `server/agent/executor.go` where task-specific JSON files are prepared. Add:

```go
if model.IsMontagePlatform(task.Type) {
	input := task.MontageInput.Data()
	data, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal montage input: %w", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "montage-input.json"), data, 0o644); err != nil {
		return fmt.Errorf("write montage-input.json: %w", err)
	}
}
```

Use existing `workDir`, `task`, and imports. If `executor.go` has a helper for task JSON files, place this logic there.

- [ ] **Step 5: Add workdir artifact validation**

Modify `server/agent/artifacts.go` `validateTaskArtifacts` before the generic non-seednote path:

```go
if task != nil && model.IsMontagePlatform(task.Type) {
	missing := []string{}
	if !files["final.mp4"] && !files["final_video.mp4"] && !files["final-video.mp4"] {
		missing = append(missing, "final_video")
	}
	if !files["delivery-manifest.json"] {
		missing = append(missing, "delivery-manifest.json")
	}
	if len(missing) > 0 {
		result.Missing = missing
		result.Reason = "montage missing required deliverables: " + strings.Join(missing, ", ")
		return result
	}
	result.Valid = true
	return result
}
```

- [ ] **Step 6: Add task-file validation in service**

Append to `server/service/task_test.go`:

```go
func TestTaskServiceHandleExecutionRejectsMontageWithoutDeliveryManifest(t *testing.T) {
	repo := setupTestRepo(t)
	userID := "om-artifacts"
	projectID := createTestProject(t, repo, userID, model.PlatformMontage)
	task := &model.Task{
		ID:        generateTaskID(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformMontage,
		Status:    model.TaskStatusRunning,
	}
	task.SetMontageInput(model.MontageInput{Brief: "做短片"})
	if err := repo.Tasks().Create(context.Background(), task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repo.TaskFiles().Create(context.Background(), &model.TaskFile{
		ID:       uuid.New().String(),
		TaskID:   task.ID,
		Role:     model.FileRoleVideo,
		FileName: "final.mp4",
		FilePath: "output/montage/final.mp4",
	}); err != nil {
		t.Fatalf("create task file: %v", err)
	}

	svc := newTestTaskService(repo)
	err := svc.HandleExecutionResult(context.Background(), task.ID, &agent.ExecutionResult{Success: true})
	if err == nil {
		t.Fatal("HandleExecutionResult succeeded without delivery manifest")
	}
}
```

Use the existing helper name for execution completion if it differs from `HandleExecutionResult`; keep the assertion and fixture shape.

- [ ] **Step 7: Implement service artifact validation**

Modify `server/service/task_execution.go`:

```go
if model.IsMontagePlatform(task.Type) {
	return validateMontageCompletionArtifacts(files), nil
}
```

Add helper:

```go
func validateMontageCompletionArtifacts(files []*model.TaskFile) agent.ArtifactValidation {
	hasFinal := false
	hasManifest := false
	for _, file := range files {
		if file == nil {
			continue
		}
		role := strings.TrimSpace(file.Role)
		name := strings.ToLower(strings.TrimSpace(file.FileName))
		path := strings.ToLower(strings.TrimSpace(file.FilePath))
		if role == "final_video" || name == "final.mp4" || name == "final_video.mp4" || strings.HasSuffix(path, "/final.mp4") || strings.HasSuffix(path, "/final_video.mp4") {
			hasFinal = true
		}
		if role == "delivery_manifest" || name == "delivery-manifest.json" || strings.HasSuffix(path, "/delivery-manifest.json") {
			hasManifest = true
		}
	}
	missing := []string{}
	if !hasFinal {
		missing = append(missing, "final_video")
	}
	if !hasManifest {
		missing = append(missing, "delivery_manifest")
	}
	if len(missing) > 0 {
		return agent.ArtifactValidation{Reason: "montage missing required deliverables: " + strings.Join(missing, ", "), Missing: missing}
	}
	return agent.ArtifactValidation{Valid: true, MeaningfulFileCount: 2}
}
```

- [ ] **Step 8: Run agent and service tests**

Run:

```bash
go test ./server/agent -run TestMontage -count=1
go test ./server/service -run 'Montage.*Artifact|HandleExecution' -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit agent mapping and validation**

Run:

```bash
git add server/agent/montage_contract_test.go server/agent/config_builder.go server/agent/executor.go server/agent/artifacts.go server/service/task_execution.go server/service/task_test.go
git commit -m "feat: validate montage agent delivery"
```

## Task 5: Plugin Agents, Skills, And Manifest Versions

**Files:**
- Create: `claudecode/agents/montage.md`
- Create: `codex/agents/montage.toml`
- Create: `claudecode/skills/montage/SKILL.md`
- Create: `codex/skills/montage/SKILL.md`
- Create: `openclaw/skills/montage/SKILL.md`
- Modify: `codex/install/agents-registration.toml`
- Modify: `claudecode/.claude-plugin/plugin.json`
- Modify: `openclaw/openclaw.plugin.json`
- Modify: `codex/.codex-plugin/plugin.json`
- Test: `server/agent/montage_contract_test.go`

- [ ] **Step 1: Create Claude Code agent**

Create `claudecode/agents/montage.md`:

```markdown
---
name: montage
description: Montage 视频生产专用 agent。读取 Anban 的 montage-input.json，准备 Montage adapter manifest，运行上游 Montage pipeline，并交付 final_video 与 delivery-manifest.json。
model: inherit
memory: project
skills:
  - montage
maxTurns: 180
---

# Montage

## 角色

你是 Anban Creator 的 Montage agent。你只处理 `montage` 平台任务，负责把 Anban 的业务输入转换为 Montage 项目 manifest，运行 Montage，并把结果登记回 Anban。

## 硬边界

- 禁止调用 Claude `Agent` 工具来执行本次主工作流；必须在当前 montage 上下文内完成。
- 不得调用现有 `videocreator`、`videoeditor`、Seedance、Dreamina 或 video-use 主链路。
- 不得自写 provider HTTP 客户端绕过 Anban MCP。
- 不得修改 `third_party/OpenMontage` 上游源码；需要临时项目文件时复制到任务工作目录。

## 必需产物

- `montage-input.json`
- `montage-project.json`
- `delivery-manifest.json`
- `final.mp4` 或等价的 `final_video` task file
- 失败时写 `failure-diagnosis.md`

## 工作流

1. 获取 `$TASK_ID` 与 `$PROJECT_ID`。
2. 调用 `prepare_workspace(content_type="montage", task_id=$TASK_ID)` 并进入返回的目录。
3. 读取工作区根目录的 `montage-input.json`。
4. 调用 `get_project_profile(project_id=$PROJECT_ID, task_id=$TASK_ID)` 获取项目定位和 Montage 默认值。
5. 解析 pipeline：优先任务 `pipeline_key`，其次项目默认，最后服务端默认。
6. 写入 `montage-project.json`，包含 task_id、project_id、brief、pipeline_key、assets、preferences、limits 和 output_dir。
7. 从配置的 Montage submodule 或 runner 环境运行 Montage，不修改上游源码。
8. 收集 Montage 输出，写 `delivery-manifest.json`。
9. 使用 Anban MCP 上传并登记最终视频、manifest、timeline、subtitles、audio、run log 和 failure diagnosis。
10. 完成前确认 `final_video` 与 `delivery-manifest.json` 已登记为 task files。
11. 调用 `submit_agent_feedback(agent_name="montage", status="completed", summary="Montage delivery registered")`。
```

- [ ] **Step 2: Create shared skill in three distributions**

Create the same `SKILL.md` content at:

- `claudecode/skills/montage/SKILL.md`
- `codex/skills/montage/SKILL.md`
- `openclaw/skills/montage/SKILL.md`

Content:

```markdown
---
name: montage
description: Use for Anban Montage tasks. Converts montage-input.json into an Montage adapter manifest, runs the upstream Montage pipeline, and registers normalized Anban deliverables.
---

# Montage Skill

Use this skill only for Anban `montage` tasks.

## Inputs

- `$TASK_ID`
- `$PROJECT_ID`
- `montage-input.json`
- project profile from Anban MCP
- configured Montage submodule or runner path

## Required Files

- `montage-project.json`: adapter manifest sent to Montage
- `delivery-manifest.json`: normalized Anban delivery manifest
- `final.mp4`: final video when the pipeline succeeds
- `failure-diagnosis.md`: required when the pipeline cannot complete

## Rules

- Keep Montage independent from existing Anban video generation and video editing flows.
- Do not call `create_video_generation_job`, `validate_video_delivery`, Seedance skills, Dreamina skills, or `video-use`.
- Do not expose raw Montage pipeline internals as Anban stable schema.
- Do not modify files under `third_party/OpenMontage`.
- Use Anban MCP tools for project profile, workspace preparation, progress, uploads, task files, and feedback.

## Adapter Manifest

Write `montage-project.json` with:

```json
{
  "task_id": "$TASK_ID",
  "project_id": "$PROJECT_ID",
  "brief": "",
  "pipeline_key": "default",
  "assets": [],
  "preferences": {},
  "limits": {},
  "output_dir": "output/montage/$TASK_ID"
}
```

The adapter maps this stable manifest into the current upstream Montage project format.
```

- [ ] **Step 3: Create Codex agent**

Create `codex/agents/montage.toml`:

```toml
name = "montage"
description = "Montage 视频生产专用 agent：读取 montage-input.json，准备 Montage adapter manifest，运行上游 Montage pipeline，并登记 final_video 与 delivery-manifest.json。"
nickname_candidates = ["Montage", "Montage", "视频生产"]
model_reasoning_effort = "medium"
sandbox_mode = "workspace-write"
developer_instructions = """
# Montage

你是 Anban Creator 的 Montage agent。只处理 montage 平台任务。

硬边界：
- 禁止调用嵌套 Agent 工具执行主工作流。
- 不得调用 videocreator、videoeditor、Seedance、Dreamina 或 video-use 主链路。
- 不得修改 third_party/OpenMontage 上游源码。
- 所有服务端交互必须走 Anban MCP。

必需产物：
- montage-input.json
- montage-project.json
- delivery-manifest.json
- final.mp4 或 final_video task file
- 失败时 failure-diagnosis.md

工作流：
1. 读取 TASK_ID、PROJECT_ID 和 montage-input.json。
2. 调用 prepare_workspace(content_type=\"montage\", task_id=$TASK_ID)。
3. 调用 get_project_profile(project_id=$PROJECT_ID, task_id=$TASK_ID)。
4. 写 montage-project.json。
5. 在配置的 Montage submodule/runner 环境中运行上游 pipeline。
6. 写 delivery-manifest.json，登记 final_video 和 manifest task files。
7. 调用 submit_agent_feedback(agent_name=\"montage\", status=\"completed\", summary=\"Montage delivery registered\")。
"""

[mcp_servers.creator]
url = "${ANBAN_API_URL:-https://api.creator.anbanai.com}/mcp"
bearer_token_env_var = "ANBAN_API_KEY"

[[skills.config]]
path = "__PLUGIN_ROOT__/skills/montage/SKILL.md"
enabled = true
```

- [ ] **Step 4: Register Codex agent**

Modify `codex/install/agents-registration.toml`:

```toml
[agents.montage]
description = "Montage 视频生产引擎（业务 brief + 素材 → Montage pipeline → 成片交付）"
config_file = "~/.codex/agents/montage.toml"
nickname_candidates = ["Montage", "Montage", "视频生产"]
```

Also update the comment and `[agents] max_threads` if the file tracks agent count explicitly.

- [ ] **Step 5: Patch bump plugin manifests**

Modify versions:

```json
// claudecode/.claude-plugin/plugin.json
"version": "2.10.52"
```

```json
// openclaw/openclaw.plugin.json
"version": "2.7.38"
```

```json
// codex/.codex-plugin/plugin.json
"version": "2.10.46"
```

Update descriptions to include `Montage`.

- [ ] **Step 6: Run plugin contract tests**

Run:

```bash
go test ./server/agent -run 'Montage|NamingContract' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit plugin assets**

Run:

```bash
git add claudecode/agents/montage.md codex/agents/montage.toml claudecode/skills/montage/SKILL.md codex/skills/montage/SKILL.md openclaw/skills/montage/SKILL.md codex/install/agents-registration.toml claudecode/.claude-plugin/plugin.json openclaw/openclaw.plugin.json codex/.codex-plugin/plugin.json server/agent/montage_contract_test.go
git commit -m "feat: add montage agent contracts"
```

## Task 6: Studio Types, Schemas, And Form Panel

**Files:**
- Create: `studio/src/types/montage.ts`
- Create: `studio/src/lib/montage-form.ts`
- Create: `studio/src/lib/montage-form.test.ts`
- Create: `studio/src/components/montage/MontageCreationPanel.tsx`
- Create: `studio/src/components/montage/MontageCreationPanel.test.tsx`
- Modify: `studio/src/types/index.ts`
- Modify: `studio/src/types/task.ts`
- Modify: `studio/src/types/project.ts`
- Modify: `studio/src/types/plan.ts`
- Modify: `studio/src/lib/schemas.ts`
- Modify: `studio/src/lib/schemas.test.ts`
- Modify: `studio/src/lib/labels.ts`
- Modify: `studio/src/lib/pricing.ts`
- Modify: `studio/src/lib/credit-display.ts`
- Modify: `studio/src/lib/PlatformIcon.tsx`
- Modify: `studio/src/lib/command-center.ts`

- [ ] **Step 1: Write failing schema tests**

Append to `studio/src/lib/schemas.test.ts`:

```ts
it('accepts montage task input without exposing execution target choice', () => {
  const result = createTaskSchema.safeParse({
    project_id: 'project-montage',
    type: 'montage',
    montage_input: {
      brief: '做一条新品发布短片',
      pipeline_key: 'default',
      preferences: {
        aspect_ratio: '9:16',
        duration_seconds: 30,
      },
    },
  })

  expect(result.success).toBe(true)
  if (result.success) {
    expect(result.data).not.toHaveProperty('execution_target')
  }
})

it('requires brief for montage tasks', () => {
  const result = createTaskSchema.safeParse({
    project_id: 'project-montage',
    type: 'montage',
    montage_input: {
      brief: '',
    },
  })

  expect(result.success).toBe(false)
})

it('accepts montage plans', () => {
  const result = planSchema.safeParse({
    project_id: 'project-montage',
    type: 'montage',
    cron_expr: '0 10 * * *',
    montage_input: {
      brief: '每天生成一条品牌短片',
    },
  })

  expect(result.success).toBe(true)
})
```

- [ ] **Step 2: Run schema tests and verify failure**

Run:

```bash
cd studio && bun run test -- src/lib/schemas.test.ts
```

Expected: FAIL because `montage` is not in enums and `montage_input` is unknown.

- [ ] **Step 3: Add Studio Montage types**

Create `studio/src/types/montage.ts`:

```ts
export type MontageAssetType = 'text' | 'image_url' | 'video_url' | 'audio_url' | 'document_url'

export interface MontageAsset {
  type: MontageAssetType
  url?: string
  task_file_id?: string
  text?: string
  file_name?: string
  mime_type?: string
  file_size?: number
}

export interface MontagePreferences {
  aspect_ratio?: string
  duration_seconds?: number
  style?: string
  music_prompt?: string
  subtitle_mode?: string
  voiceover_mode?: string
}

export interface MontageInput {
  brief?: string
  pipeline_key?: string
  source_assets?: MontageAsset[]
  preferences?: MontagePreferences
  delivery_targets?: string[]
  advanced?: Record<string, unknown>
}
```

Export it from `studio/src/types/index.ts`:

```ts
export type {
  MontageAsset,
  MontageAssetType,
  MontageInput,
  MontagePreferences,
} from './montage'
```

- [ ] **Step 4: Extend task/project/plan unions**

Modify `studio/src/types/task.ts`:

```ts
import type { MontageInput } from './montage'

export type TaskType = 'seednote' | 'article' | 'moments' | 'viral_analysis' | 'ecommerce' | 'videocreator' | 'videoeditor' | 'montage'

export interface Task {
  montage_input?: MontageInput
}

export interface CreateTaskRequest {
  montage_input?: MontageInput
}
```

Add the fields to the existing interfaces rather than creating duplicate interface declarations.

Modify `studio/src/types/project.ts`:

```ts
import type { MontageInput, MontagePreferences } from './montage'

export type ProjectPlatform = 'article' | 'seednote' | 'moments' | 'ecommerce' | 'videocreator' | 'videoeditor' | 'montage'

export interface MontageProjectDefaults {
  default_pipeline?: string
  preferences?: MontagePreferences
  asset_guidance?: string
  delivery_targets?: string[]
}
```

Add `montage_defaults?: MontageProjectDefaults` to `Project` and `CreateProjectRequest`.

Modify `studio/src/types/plan.ts`:

```ts
import type { MontageInput } from './montage'

export type PlanType = 'seednote' | 'article' | 'videocreator' | 'montage'
```

Add `montage_input?: MontageInput` to `Plan`, `CreatePlanRequest`, and `UpdatePlanRequest`.

- [ ] **Step 5: Add schemas**

Modify `studio/src/lib/schemas.ts`:

```ts
const montageAssetSchema = z.object({
  type: z.enum(['text', 'image_url', 'video_url', 'audio_url', 'document_url']),
  url: z.string().optional(),
  task_file_id: z.string().optional(),
  text: z.string().optional(),
  file_name: z.string().optional(),
  mime_type: z.string().optional(),
  file_size: z.number().optional(),
})

const montageInputSchema = z.object({
  brief: promptSchema.optional(),
  pipeline_key: z.string().max(100).optional(),
  source_assets: z.array(montageAssetSchema).default([]),
  preferences: z.object({
    aspect_ratio: z.string().optional(),
    duration_seconds: z.number().int().min(1).max(600).optional(),
    style: z.string().max(1000).optional(),
    music_prompt: z.string().max(1000).optional(),
    subtitle_mode: z.string().optional(),
    voiceover_mode: z.string().optional(),
  }).optional(),
  delivery_targets: z.array(z.string()).default([]),
  advanced: z.record(z.string(), z.unknown()).optional(),
}).optional()
```

Extend enums:

```ts
type: z.enum(["seednote", "article", "moments", "viral_analysis", "ecommerce", "videocreator", "videoeditor", "montage"])
```

Plan enum:

```ts
type: z.enum(["seednote", "article", "videocreator", "montage"])
```

Project enum:

```ts
platform: z.enum(["seednote", "article", "moments", "ecommerce", "videocreator", "videoeditor", "montage"])
```

Add fields:

```ts
montage_input: montageInputSchema,
```

In `superRefine`:

```ts
if (data.type === "montage") {
  const brief = data.montage_input?.brief?.trim() || ""
  if (!brief) {
    ctx.addIssue({
      code: "custom",
      message: "请填写 Montage 视频 brief",
      path: ["montage_input", "brief"],
    })
  }
}
```

- [ ] **Step 6: Add form helpers**

Create `studio/src/lib/montage-form.ts`:

```ts
import type { MontageInput } from '@/types'

export function initialMontageInput(brief = '', input?: MontageInput): MontageInput {
  return {
    brief: input?.brief ?? brief,
    pipeline_key: input?.pipeline_key ?? '',
    source_assets: input?.source_assets ?? [],
    preferences: {
      aspect_ratio: input?.preferences?.aspect_ratio ?? '9:16',
      duration_seconds: input?.preferences?.duration_seconds ?? 30,
      style: input?.preferences?.style ?? '',
      music_prompt: input?.preferences?.music_prompt ?? '',
      subtitle_mode: input?.preferences?.subtitle_mode ?? '',
      voiceover_mode: input?.preferences?.voiceover_mode ?? '',
    },
    delivery_targets: input?.delivery_targets ?? [],
    advanced: input?.advanced ?? undefined,
  }
}

export function buildMontageInputForSubmit(brief: string | undefined, input?: MontageInput): MontageInput {
  const next = initialMontageInput(brief ?? '', input)
  return {
    ...next,
    brief: (next.brief ?? '').trim(),
    pipeline_key: next.pipeline_key?.trim() || undefined,
    source_assets: next.source_assets ?? [],
    preferences: {
      aspect_ratio: next.preferences?.aspect_ratio || undefined,
      duration_seconds: next.preferences?.duration_seconds,
      style: next.preferences?.style?.trim() || undefined,
      music_prompt: next.preferences?.music_prompt?.trim() || undefined,
      subtitle_mode: next.preferences?.subtitle_mode || undefined,
      voiceover_mode: next.preferences?.voiceover_mode || undefined,
    },
    delivery_targets: next.delivery_targets ?? [],
    advanced: next.advanced,
  }
}
```

- [ ] **Step 7: Add helper tests**

Create `studio/src/lib/montage-form.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import { buildMontageInputForSubmit, initialMontageInput } from './montage-form'

describe('montage form helpers', () => {
  it('creates stable defaults', () => {
    expect(initialMontageInput('新品短片')).toMatchObject({
      brief: '新品短片',
      pipeline_key: '',
      source_assets: [],
      preferences: {
        aspect_ratio: '9:16',
        duration_seconds: 30,
      },
    })
  })

  it('trims submit fields without adding execution target', () => {
    const result = buildMontageInputForSubmit('', {
      brief: '  新品短片  ',
      pipeline_key: '  default  ',
      source_assets: [],
      preferences: {
        style: '  干净高级  ',
      },
    })

    expect(result.brief).toBe('新品短片')
    expect(result.pipeline_key).toBe('default')
    expect(result.preferences?.style).toBe('干净高级')
    expect(result).not.toHaveProperty('execution_target')
  })
})
```

- [ ] **Step 8: Add dedicated panel**

Create `studio/src/components/montage/MontageCreationPanel.tsx`:

```tsx
import { Controller, type Control } from 'react-hook-form'
import { Textarea } from '@/components/ui/textarea'
import { Input } from '@/components/ui/input'
import type { CreateTaskFormValues, PlanFormValues } from '@/lib/schemas'

type FormValues = CreateTaskFormValues | PlanFormValues

interface MontageCreationPanelProps {
  control: Control<FormValues>
  fieldRoot: 'montage_input'
}

export function MontageCreationPanel({ control, fieldRoot }: MontageCreationPanelProps) {
  return (
    <div className="space-y-4">
      <Controller
        control={control}
        name={`${fieldRoot}.brief` as never}
        render={({ field }) => (
          <Textarea
            {...field}
            value={field.value ?? ''}
            placeholder="描述这次要生产的视频内容、素材用途、节奏和交付目标"
            rows={5}
          />
        )}
      />
      <div className="grid gap-3 md:grid-cols-3">
        <Controller
          control={control}
          name={`${fieldRoot}.pipeline_key` as never}
          render={({ field }) => (
            <Input {...field} value={field.value ?? ''} placeholder="pipeline（可选）" />
          )}
        />
        <Controller
          control={control}
          name={`${fieldRoot}.preferences.aspect_ratio` as never}
          render={({ field }) => (
            <Input {...field} value={field.value ?? ''} placeholder="画幅，如 9:16" />
          )}
        />
        <Controller
          control={control}
          name={`${fieldRoot}.preferences.duration_seconds` as never}
          render={({ field }) => (
            <Input
              type="number"
              min={1}
              max={600}
              value={field.value ?? ''}
              onChange={(event) => field.onChange(event.target.value ? Number(event.target.value) : undefined)}
              placeholder="时长（秒）"
            />
          )}
        />
      </div>
      <Controller
        control={control}
        name={`${fieldRoot}.preferences.style` as never}
        render={({ field }) => (
          <Textarea {...field} value={field.value ?? ''} placeholder="风格偏好（可选）" rows={3} />
        )}
      />
    </div>
  )
}
```

- [ ] **Step 9: Add labels and icon**

Modify `studio/src/lib/labels.ts`:

```ts
montage: 'Montage',
```

Add to `contentTypeOptions`:

```ts
{ value: 'montage', label: 'Montage' },
```

Modify `studio/src/lib/PlatformIcon.tsx`:

```tsx
import { Clapperboard } from 'lucide-react'

montage: Clapperboard,
```

Add color entries:

```ts
montage: 'text-[#9333EA]',
montage: 'border-l-[#9333EA]',
montage: 'hover:border-l-[#9333EA]/50',
montage: 'outline',
montage: 'bg-[#9333EA]/10',
```

- [ ] **Step 10: Run Studio tests**

Run:

```bash
cd studio && bun run test -- src/lib/schemas.test.ts src/lib/montage-form.test.ts
```

Expected: PASS.

- [ ] **Step 11: Commit Studio foundation**

Run:

```bash
git add studio/src/types/montage.ts studio/src/types/index.ts studio/src/types/task.ts studio/src/types/project.ts studio/src/types/plan.ts studio/src/lib/schemas.ts studio/src/lib/schemas.test.ts studio/src/lib/montage-form.ts studio/src/lib/montage-form.test.ts studio/src/components/montage/MontageCreationPanel.tsx studio/src/lib/labels.ts studio/src/lib/pricing.ts studio/src/lib/credit-display.ts studio/src/lib/PlatformIcon.tsx studio/src/lib/command-center.ts
git commit -m "feat: add montage studio schema"
```

## Task 7: Studio Task And Plan Page Integration

**Files:**
- Modify: `studio/src/pages/TasksPage.tsx`
- Modify: `studio/src/pages/PlansPage.tsx`
- Modify: `studio/src/pages/VideoGenerationUx.contract.test.ts`
- Modify: `studio/src/components/FilePreview.tsx`
- Test: `studio/src/pages/MontageUx.contract.test.ts`

- [ ] **Step 1: Add UX contract test**

Create `studio/src/pages/MontageUx.contract.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'

describe('montage UX contracts', () => {
  it('tasks page uses dedicated montage input and no user execution target selector', () => {
    const source = readFileSync('src/pages/TasksPage.tsx', 'utf8')
    expect(source).toContain('montage_input')
    expect(source).toContain('MontageCreationPanel')
    expect(source).not.toContain('MontageExecutionTarget')
  })

  it('plans page supports montage input', () => {
    const source = readFileSync('src/pages/PlansPage.tsx', 'utf8')
    expect(source).toContain('montage_input')
    expect(source).toContain('MontageCreationPanel')
  })
})
```

- [ ] **Step 2: Run UX test and verify failure**

Run:

```bash
cd studio && bun run test -- src/pages/MontageUx.contract.test.ts
```

Expected: FAIL because pages do not contain Montage fields.

- [ ] **Step 3: Integrate TasksPage**

Modify `studio/src/pages/TasksPage.tsx` imports:

```tsx
import { MontageCreationPanel } from '@/components/montage/MontageCreationPanel'
import { buildMontageInputForSubmit, initialMontageInput } from '@/lib/montage-form'
```

Add helpers:

```ts
const isMontageTask = watchedType === 'montage'
```

Default values:

```ts
montage_input: defaultType === 'montage' ? initialMontageInput('') : undefined,
```

On platform/project type change:

```ts
form.setValue('montage_input', defaults.type === 'montage' ? initialMontageInput(form.getValues('prompt') || '') : undefined, { shouldDirty: false })
```

Submit payload:

```ts
montage_input: values.type === 'montage'
  ? buildMontageInputForSubmit(values.prompt, values.montage_input)
  : undefined,
```

Render panel:

```tsx
{isMontageTask && (
  <MontageCreationPanel
    control={form.control}
    fieldRoot="montage_input"
  />
)}
```

Ensure Montage does not render `VideoCreationPanel`.

- [ ] **Step 4: Integrate PlansPage**

Modify `studio/src/pages/PlansPage.tsx` similarly:

```tsx
import { MontageCreationPanel } from '@/components/montage/MontageCreationPanel'
import { buildMontageInputForSubmit, initialMontageInput } from '@/lib/montage-form'
```

Extend `planTypeOptions`:

```ts
{ value: 'montage', label: 'Montage' },
```

In `planToFormValues`:

```ts
montage_input: plan.type === 'montage' ? initialMontageInput(plan.prompt || '', plan.montage_input) : undefined,
```

Submit payload:

```ts
montage_input: values.type === 'montage'
  ? buildMontageInputForSubmit(values.prompt, values.montage_input)
  : undefined,
```

Render panel:

```tsx
{watchedType === 'montage' && (
  <MontageCreationPanel control={form.control} fieldRoot="montage_input" />
)}
```

- [ ] **Step 5: Add task file preview labels**

Find the existing file preview component. Add Montage role labels:

```ts
const montageRoleLabel: Record<string, string> = {
  final_video: '最终视频',
  delivery_manifest: '交付清单',
  source_manifest: '素材清单',
  timeline: '时间线',
  subtitles: '字幕',
  audio: '音频',
  run_log: '运行日志',
  failure_diagnosis: '失败诊断',
}
```

Use the labels when `task.type === 'montage'`.

- [ ] **Step 6: Run Studio page tests**

Run:

```bash
cd studio && bun run test -- src/pages/MontageUx.contract.test.ts src/lib/schemas.test.ts
```

Expected: PASS.

- [ ] **Step 7: Commit Studio page integration**

Run:

```bash
git add studio/src/pages/TasksPage.tsx studio/src/pages/PlansPage.tsx studio/src/pages/MontageUx.contract.test.ts studio/src/components/FilePreview.tsx
git commit -m "feat: wire montage studio workflow"
```

## Task 8: Upgrade Documentation And Full Verification

**Files:**
- Create: `docs/montage-upgrade.md`
- Modify: `server/agent/montage_contract_test.go`
- Test: `server/agent/montage_contract_test.go`

- [ ] **Step 1: Add upgrade docs**

Create `docs/montage-upgrade.md`:

```markdown
# Montage Upgrade Procedure

Montage is integrated as a git submodule at `third_party/OpenMontage`.
Anban owns the adapter contract and does not modify upstream Montage source
files during normal feature work.

## Update

```bash
git submodule update --init --recursive
git -C third_party/OpenMontage fetch origin
git -C third_party/OpenMontage checkout origin/main
```

Review the submodule diff:

```bash
git diff --submodule=log
```

## Verify

```bash
go test ./server/agent -run Montage -count=1
go test ./server/service -run Montage -count=1
cd studio && bun run test -- src/lib/montage-form.test.ts src/pages/MontageUx.contract.test.ts
```

## Adapter Rule

If upstream pipeline metadata changes, update only Anban's Montage adapter
mapping and tests. Do not copy Montage internals into Studio schemas.
```

- [ ] **Step 2: Add submodule contract test**

Append to `server/agent/montage_contract_test.go`:

```go
func TestMontageSubmodulePathIsDeclared(t *testing.T) {
	root := repoRoot(t)
	gitmodules := readRepoFile(t, filepath.Join(root, ".gitmodules"))
	if !strings.Contains(gitmodules, "third_party/OpenMontage") {
		t.Fatal(".gitmodules missing third_party/OpenMontage submodule")
	}
	if !strings.Contains(gitmodules, "https://github.com/calesthio/OpenMontage.git") {
		t.Fatal(".gitmodules missing Montage upstream URL")
	}
}
```

- [ ] **Step 3: Run targeted verification**

Run:

```bash
go test ./server/agent -run Montage -count=1
go test ./server/service -run Montage -count=1
go test ./server/config -run Montage -count=1
cd studio && bun run test -- src/lib/montage-form.test.ts src/pages/MontageUx.contract.test.ts src/lib/schemas.test.ts
```

Expected: PASS.

- [ ] **Step 4: Run surface-level builds**

Run:

```bash
go test ./...
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
cd studio && bun run test
cd studio && bun run build
```

Expected: PASS. If unrelated pre-existing tests fail, capture the exact failing package/test and continue only after confirming whether the failure is related.

- [ ] **Step 5: Commit docs and verification contracts**

Run:

```bash
git add docs/montage-upgrade.md server/agent/montage_contract_test.go
git commit -m "docs: add montage upgrade procedure"
```

## Self-Review Checklist

- [ ] Spec coverage: platform, config, model, Studio input, task creation, plans, agent, artifacts, billing, submodule upgrade, and tests are covered.
- [ ] No user-facing cloud/local selector is introduced.
- [ ] Montage does not reuse `video_creator_input`, `video_editor_input`, `VideoCreationPanel`, `create_video_generation_job`, Seedance skills, or `video-use`.
- [ ] All plugin distribution asset changes include manifest patch version bumps.
- [ ] All production code tasks start with failing tests.
- [ ] Final verification includes Go tests/builds and Studio tests/build.
