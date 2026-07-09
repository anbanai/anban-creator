# Montage Integration Design

## Context

Anban already has two video-related task surfaces:

- `videocreator`: AI video generation through Anban's existing video model and
  MCP flow.
- `videoeditor`: video post-production/editing flow with its own source media
  requirements and artifact contract.

Montage must be integrated as a separate production system, not as an
extension of either existing video flow. The integration should follow the same
business completeness expected from Seednote-style workflows: project setup,
Studio input, server validation and configuration, plans, agent execution,
artifact registration, previews, billing, and repeatable updates.

Montage should remain easy to update when upstream changes. Anban should
own an adapter layer, while Montage itself stays in its upstream shape.

## Goals

- Add Montage as an independent Anban platform and task type.
- Keep Montage independent from existing `videocreator` and `videoeditor`
  inputs, agents, MCP video generation tools, and artifact validation.
- Use a git submodule for upstream Montage so updates are commit-based and
  reviewable.
- Let Anban orchestrate Montage through a stable adapter manifest.
- Support both cloud and local execution through the same business contract.
- Decide cloud or local execution by system policy and runtime capability, not
  by user-authored creative parameters.
- Keep Studio input stable and business-oriented: user intent, source assets,
  and a small set of preferences.
- Preserve complete task and plan flows.
- Add contract tests so upstream Montage updates can be verified before
  release.

## Non-Goals

- Do not merge Montage into Anban's existing video generation service.
- Do not reuse `video_creator_input`, `video_editor_input`, `VideoCreationPanel`,
  or Seedance-oriented MCP tools for Montage.
- Do not expose raw Montage internal pipeline arguments as stable Studio or
  server API fields.
- Do not require users to choose cloud versus local execution as a creative
  setting.
- Do not modify upstream Montage core files inside the submodule as part of
  normal Anban feature work.

## Recommended Approach

Add a new `montage` platform. Anban stores a compact `montage_input`
snapshot, writes an adapter manifest for the selected Montage pipeline, runs
Montage through a dedicated agent, and registers normalized Anban task
files.

High-level flow:

```text
Studio Montage project/task/plan
  -> server validates and snapshots montage_input
  -> scheduler/enqueuer dispatches an montage task
  -> system resolves cloud or local runner
  -> anban:montage agent prepares adapter manifest
  -> Montage submodule pipeline runs
  -> agent collects outputs and uploads/registers task files
  -> server validates final_video + delivery_manifest
  -> Studio previews files from task_files
```

The Montage submodule should live under a dependency path such as:

```text
third_party/OpenMontage
```

The exact path is configurable so container builds and local developer setups can
override it when needed.

## Alternatives Considered

### A. Anban adapter orchestrates Montage

Montage remains a submodule. Anban owns only the product-facing platform,
input schema, execution adapter, and artifact normalization.

This is recommended because it keeps the integration independent and gives a
clean upgrade path.

### B. Montage sidecar API

Run a long-lived Montage service and have Anban call it over HTTP. This gives
strong process isolation, but introduces another product API, lifecycle, auth,
and observability surface. It also does not fit the existing agentic task model
as naturally.

Keep this as a future option if Montage itself grows a stable server API.

### C. Fold Montage into existing video flows

Reuse `videocreator` or `videoeditor` fields and MCP tools. This reduces the
number of UI choices, but violates the independence requirement and makes future
Montage upstream updates harder.

Rejected.

## Server Model

Add platform and scope constants:

```go
const (
    ScopeMontage    = "montage"
    PlatformMontage = "montage"
)
```

`montage` should be treated as its own task platform everywhere task types
are enumerated:

- project platform validation
- task creation
- plan creation
- credit pricing
- timeline and usage labels
- agent mapping
- artifact validation
- Studio TypeScript unions

Add a dedicated input model:

```go
type MontageInput struct {
    Brief           string                 `json:"brief,omitempty"`
    PipelineKey     string                 `json:"pipeline_key,omitempty"`
    SourceAssets    []MontageAsset     `json:"source_assets,omitempty"`
    Preferences     MontagePreferences `json:"preferences,omitempty"`
    DeliveryTargets []string               `json:"delivery_targets,omitempty"`
    Advanced        map[string]any         `json:"advanced,omitempty"`
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
```

The stable contract is Anban intent and assets. The adapter translates this into
the current Montage pipeline format. If a pipeline needs extra information,
the agent fails with a clear `failure_diagnosis` instead of silently inventing
values.

Prefer explicit JSON columns on tasks and plans:

```text
tasks.montage_input
plans.montage_input
```

This keeps Montage independent from `video_input`, `video_creator_input`, and
generic overrides.

## Server Configuration

Add an Montage-specific config block rather than extending `video_api`:

```yaml
montage:
  enabled: true
  submodule_path: third_party/OpenMontage
  default_pipeline: default
  allowed_pipelines:
    - default
  max_duration_seconds: 600
  max_assets: 20
  timeout_minutes: 90
  execution_targets:
    - cloud
    - local
  default_execution_target: cloud
  credit_cost: 2000
```

Validation rules:

- `enabled=false` hides Studio creation entry points and rejects new task/plan
  creation for `montage`.
- `submodule_path` must exist in environments that execute Montage.
- `default_pipeline` must be present in `allowed_pipelines` when the allowed list
  is not empty.
- `max_duration_seconds` and `max_assets` must be positive.
- `execution_targets` may include `cloud`, `local`, or both.
- `default_execution_target` must be one of the allowed targets.

## Execution Target Resolution

Execution target is system-selected. It is not a user form field.

The resolver considers:

- server deployment mode and `montage.execution_targets`
- whether a local executor is online and capable
- whether the task originated from a scheduled plan
- whether source assets are cloud-accessible
- project or tenant policy
- queue capacity and timeout policy

Default behavior:

- Scheduled Montage plans run in cloud unless an explicit system policy says
  otherwise.
- Manual tasks prefer the configured default target.
- If the default target is unavailable, the resolver may fall back to another
  allowed target only when assets and capabilities are compatible.
- If no target is valid, task creation fails with an actionable message.

Cloud and local execution both consume the same `montage_input` and must
produce the same artifact contract.

## Studio Experience

Add `montage` as a project and task platform alongside existing platforms.

Project form:

- project name
- project positioning/instructions
- default Montage pipeline
- optional default preferences such as aspect ratio, duration, style, subtitle
  mode, and voiceover mode
- asset guidance for the project

Task form:

- `brief`: required creative intent
- `pipeline_key`: optional; default comes from project or server
- `source_assets`: required only when the selected or default pipeline needs
  assets
- preferences: optional and compact
- delivery targets: optional
- advanced: collapsed adapter passthrough for non-stable parameters

Plan form:

- same `montage_input` as task creation
- cron schedule
- no user-facing cloud/local selector

Task detail:

- final video preview from registered task files
- delivery manifest
- source manifest
- timeline/storyboard
- subtitles and audio when produced
- run log
- failure diagnosis when failed

Studio should not assume all Montage pipelines need the same fields. Pipeline
metadata should come from server configuration or adapter discovery.

## Agent And Adapter

Add a dedicated Montage agent:

```text
claudecode/agents/montage.md
codex/agents/montage.toml
```

Add a dedicated Montage skill in every distribution that needs parity:

```text
claudecode/skills/montage/SKILL.md
openclaw/skills/montage/SKILL.md
codex/skills/montage/SKILL.md
```

The agent's responsibilities:

1. Read `$TASK_ID`, `$PROJECT_ID`, and `montage-input.json`.
2. Fetch project profile and task asset metadata through Anban MCP tools.
3. Resolve the pipeline from task input, project defaults, and server defaults.
4. Write an adapter manifest, such as `montage-project.json`.
5. Run Montage from the configured submodule path or prepared working copy.
6. Collect Montage outputs.
7. Write `delivery-manifest.json`.
8. Upload or register task files through Anban MCP tools.
9. Write `failure-diagnosis.md` when execution cannot complete.
10. Submit agent feedback.

Adapter manifest shape:

```json
{
  "task_id": "task-id",
  "project_id": "project-id",
  "brief": "user intent",
  "pipeline_key": "default",
  "assets": [],
  "preferences": {},
  "limits": {},
  "output_dir": "output/montage/task-id"
}
```

The adapter layer owns translation into the actual Montage pipeline format.
The server and Studio own only the stable Anban-side schema.

## Artifact Contract

An Montage task is complete only when the server sees:

- `final_video`: final MP4 or supported video output
- `delivery_manifest`: normalized output manifest

Recommended additional roles:

- `source_manifest`
- `timeline`
- `subtitles`
- `audio`
- `run_log`
- `failure_diagnosis`

Server validation:

- Completed Montage tasks must have `final_video` and
  `delivery_manifest`.
- A plain video file without `delivery_manifest` is not enough.
- Failed Montage tasks should surface `failure_diagnosis` first when present.
- Studio previews files from `task_files`, not from Montage working
  directories.

## Plans

Montage plans are supported.

The plan stores `montage_input` and creates `montage` tasks when due.
Plan-triggered tasks use the same validation and artifact contract as manual
tasks. The system chooses execution target using runtime policy; the plan editor
does not expose that choice to users.

## Billing

Add a base task cost for `montage`, configured independently from
`videocreator` and `videoeditor`.

Initial billing behavior:

- deduct the base Montage task service fee on task creation
- do not charge `video_gen` for Montage work
- introduce a future `montage_operation` or provider-specific operation type
  only if Montage adapter calls paid external APIs that must be metered

This keeps Montage economics independent from Anban's existing video model
pricing.

## Upgrade Strategy

Montage is pinned by submodule commit.

Update flow:

```bash
git submodule update --remote third_party/OpenMontage
go test ./server/agent -run Montage -count=1
go test ./server/service -run Montage -count=1
cd studio && bun run test -- montage
```

Add `docs/montage-upgrade.md` with the repeatable procedure once the
submodule exists.

Contract tests should verify:

- submodule path exists when Montage is enabled
- adapter can discover or read allowed pipeline metadata
- Anban `montage_input` schema remains stable
- required artifact roles remain stable
- agent mapping resolves `montage -> montage`
- plugin distribution files exist where required
- plugin manifest versions are bumped when distribution assets change

## Testing Plan

Go tests:

- platform constants and `IsMontagePlatform`
- project creation accepts `montage`
- task creation accepts `montage_input` only for Montage projects
- task creation rejects Montage when the feature is disabled
- execution target resolver chooses valid system targets
- Montage task creation clamps quantity to one
- plan creation accepts `montage_input`
- plan trigger creates Montage tasks
- artifact validation requires `final_video` and `delivery_manifest`
- agent mapping returns `montage`
- credit pricing contains Montage

Studio tests:

- schema accepts `montage`
- task payload contains `montage_input`
- plan payload contains `montage_input`
- UI does not show a user-facing cloud/local selector
- labels, icons, pricing, and command center include Montage
- task detail previews final video and manifest roles

Agent/plugin tests:

- Montage agent files exist for supported distributions
- Montage skill files exist for supported distributions
- skill text does not call existing videocreator/videoeditor flows
- agent writes the required manifest names
- manifest version bump is enforced for changed plugin assets

## Implementation Decisions

- Use `third_party/OpenMontage` as the default submodule path.
- The adapter must read pipeline metadata from the checked-in Montage
  submodule during implementation. It must not hardcode unverified upstream
  pipeline arguments into Studio or server schemas.
- Local dispatch remains disabled by resolver policy until capability checks
  report the configured Montage path, ffmpeg, browser runtime, and required
  language runtimes as available.
- Cloud dispatch remains the production default and uses the shared Agent image
  selected by `claude.docker.image` or `claude.kubernetes.agent_image`.
