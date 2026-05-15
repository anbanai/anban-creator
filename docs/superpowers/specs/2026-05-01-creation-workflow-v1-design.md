# Creation Workflow v1 Design

## Purpose

AnbanWriter already has strong individual capabilities: channel profiles, scheduled/manual tasks, Claude/OpenClaw agent execution, MCP writing tools, AI image generation, WeChat draft publishing, file upload, progress logs, credits, and Web Studio management views. The next product step is to turn those capabilities from a toolbox into a repeatable creation workflow that users can trust every day.

Creation Workflow v1 turns one content request into a staged, inspectable pipeline:

1. Topic direction
2. Outline
3. Draft
4. Humanization
5. SEO/title optimization
6. Visual assets
7. Platform packaging
8. Optional draft publishing
9. Review report

The goal is not to rebuild the platform. The goal is to wrap existing AI, agent, publishing, storage, and task systems in a clearer business flow with saved artifacts, visible stage status, and quality feedback.

## User Value

The current task model tells users whether an agent completed. That is useful, but not enough for serious creators. They need to understand what was produced, why it fits their account, what still needs attention, and what can be reused later.

Creation Workflow v1 should make a task feel like a managed editorial process:

- The user can see which stage is running and what it produced.
- The user can preview intermediate outputs instead of only final files.
- The final output includes a review report, not just article/images.
- The platform can later learn from stage outputs and publishing results.

The first release should target creators publishing to WeChat article and Xiaolvshu/XLS. Seednote can reuse parts of the flow later, but v1 should avoid expanding scope into direct Seednote publishing.

## Scope

In scope:

- Add a canonical workflow stage model for generated tasks.
- Persist stage artifacts as task files and lightweight metadata.
- Add a quality review artifact generated from final content and channel profile.
- Surface stage progress and artifacts in Studio task detail.
- Reuse existing agent/MCP tools where practical.
- Keep current task creation API compatible.
- Support manual tasks and scheduled plan tasks.

Out of scope for v1:

- Full collaborative editor.
- Direct Seednote publishing.
- Automatic collection of post-publish analytics.
- New billing products.
- A complete replacement for existing agent definitions.
- Multi-user approval workflows.

## Product Flow

### 1. Task Creation

Users still create a task from Studio by selecting a channel, entering an optional prompt, choosing quantity, image ratio, and video option where supported.

For v1, the server derives a workflow template from the channel platform:

- `article`: topic, outline, draft, humanize, seo, cover, html, draft_package, publish_optional, review
- `xls`: topic, content_script, visual_plan, images, xls_package, publish_optional, review

The task can keep its current `pending/running/completed/failed/cancelled` status. Stage status is additive.

### 2. Stage Execution

The existing agent execution remains the core runtime. The agent should be instructed to emit known artifact names into `output/`, such as:

- `01-topic.json`
- `02-outline.md`
- `03-draft.md`
- `04-final.md`
- `05-article.html`
- `cover.png`
- `images.json`
- `draft.json`
- `review.json`

The server already uploads missing task files from `output/`; v1 should formalize these filenames and roles so Studio can render them consistently.

### 3. Review Report

Every completed workflow should include a `review.json` artifact. For articles, it should evaluate:

- account fit
- title strength
- opening hook
- information density
- human voice
- platform formatting
- visual consistency
- publishing readiness

For image posts, it should evaluate:

- topic clarity
- cover attractiveness
- slide continuity
- text density
- visual consistency
- platform fit
- publishing readiness

The review report should contain numeric scores, short reasons, and concrete next actions. It should not block task completion in v1; if review generation fails, the task can complete with a warning.

## Data Model

### Minimal v1 Model

Avoid a heavy migration if possible. Use existing `task_files` for durable artifacts and add one optional JSON metadata field to `tasks` only if needed.

Preferred addition:

```go
WorkflowStatus *string `gorm:"type:json" json:"workflow_status,omitempty"`
```

The JSON shape:

```json
{
  "version": "creation_workflow_v1",
  "current_stage": "review",
  "stages": [
    {
      "key": "draft",
      "label": "初稿",
      "status": "completed",
      "artifact_paths": ["03-draft.md"],
      "started_at": "2026-05-01T00:00:00Z",
      "completed_at": "2026-05-01T00:02:00Z",
      "error": ""
    }
  ],
  "warnings": []
}
```

If adding a task column is too costly for v1, stage status can initially be inferred from uploaded artifact filenames plus progress logs. The column is still recommended because it gives Studio a reliable source of truth.

### Artifact Roles

Extend task file role detection or add a normalized artifact classifier:

- `topic`
- `outline`
- `draft`
- `final_markdown`
- `html`
- `cover`
- `image`
- `draft_package`
- `review`
- `video`
- `other`

This can be computed from file name, extension, and existing MIME type.

## Backend Design

### Task Service

`TaskService.HandleExecution` already manages execution, upload, title extraction, auto-publish, retries, refunds, and cleanup. v1 should add a post-processing step after workspace upload:

1. Scan uploaded task files.
2. Classify workflow artifacts.
3. Parse `review.json` if present.
4. Persist `WorkflowStatus`.
5. Publish a progress event for stage summary.

This should happen before marking the task completed so the final task detail response can include workflow metadata immediately.

### Agent Contract

Update agent instructions for `wechatarticle` and `wechatxls` to produce the canonical artifacts. The agent should treat artifact creation as required output, not a nice-to-have.

Minimum contract:

- Always write into `output/`.
- Always create `review.json`.
- Use stable filenames.
- Include enough metadata for Studio previews.
- Do not publish directly unless the channel has publishing enabled or the task asks for it.

### MCP Tools

Existing writing tools can remain. v1 should add one new tool only if needed:

- `review_content`: accepts channel context, content/artifact paths, platform, and returns structured review JSON.

If the agent can generate `review.json` without a server MCP tool, defer this tool. The server-side tool is useful later for retries and manual re-review from Studio.

### Publishing

Current auto-publish can publish article and XLS drafts when enabled. v1 should keep publishing optional and non-blocking. Publishing failures should appear in workflow warnings.

Important fix: make publish detection more explicit than searching log text for tool names. If possible, prefer `draft.json` or a structured publish result artifact.

## Frontend Design

### Task Detail Page

Add a workflow panel above the file gallery:

- Horizontal or vertical stage list.
- Status per stage: pending, running, completed, warning, failed.
- Click a completed stage to preview its artifact.
- Show warning badges for review or publishing issues.

Add a review summary section when `review.json` exists:

- Overall readiness score.
- Top strengths.
- Top risks.
- Suggested next actions.

Keep current logs and file previews; do not hide them. Logs remain useful for debugging, while the workflow panel is the user-facing editorial view.

### Task List

Show a small readiness indicator for completed tasks when review data exists:

- `可发布`
- `建议修改`
- `需重做`

This helps users triage completed work quickly.

### Create Task Dialog

Keep the current dialog. Do not add a large wizard in v1. The workflow should work automatically from channel/platform context.

## AI Design

### Review Prompt

The review prompt should include:

- channel platform
- channel positioning
- keywords
- writing or visual style
- task prompt
- final article or package summary
- artifact list

The output must be strict JSON. Suggested schema:

```json
{
  "overall_score": 82,
  "readiness": "ready_with_minor_edits",
  "scores": {
    "account_fit": 8,
    "hook": 7,
    "density": 8,
    "human_voice": 9,
    "platform_fit": 8,
    "visual_consistency": 7
  },
  "strengths": ["..."],
  "risks": ["..."],
  "next_actions": ["..."]
}
```

### Prompt Versioning

Each generated review should include:

- `workflow_version`
- `review_prompt_version`
- `model`
- `generated_at`

This makes future quality evaluation possible.

## Error Handling

- Missing optional artifacts should become workflow warnings.
- Missing final content should fail the task, matching existing no-output behavior.
- Review generation failure should not fail the task in v1.
- Publishing failure should not fail the task, but should be visible.
- Invalid `review.json` should be uploaded as a file and surfaced as a parse warning.

## Testing

Backend tests:

- Classify canonical artifact filenames.
- Build workflow status from a set of task files.
- Handle missing review file.
- Handle invalid review JSON.
- Ensure completed task still completes when review parse fails.
- Ensure plan-created tasks get the same workflow template as manual tasks.

Frontend tests:

- Task detail renders stages from workflow metadata.
- Review summary renders readiness state.
- Missing review does not break existing task detail.
- Task list shows readiness badge when available.

Agent contract tests can start as fixture-based checks:

- Given sample output directory, server recognizes all expected artifacts.
- Given article workflow fixture, review JSON parses into expected fields.

## Rollout

1. Add artifact classification and workflow status builder.
2. Add task workflow metadata response.
3. Update agents to emit canonical output files.
4. Add review artifact generation.
5. Add Studio workflow panel and review summary.
6. Add tests and fixtures.
7. Update README and product docs to describe Studio-first workflow.

## Success Criteria

Creation Workflow v1 is successful when:

- A completed article task shows staged artifacts and review summary in Studio.
- A completed XLS task shows staged artifacts and review summary in Studio.
- Existing task creation, execution, upload, download, retry, cancel, and auto-publish behavior continues to work.
- At least one backend test fixture proves artifact recognition.
- At least one frontend test proves review summary rendering.
- README no longer presents the project primarily as a deprecated CLI surface.

## Open Decisions

1. Whether to store workflow status in a new `tasks.workflow_status` JSON column or infer it from files in v1.
2. Whether review generation should be agent-owned initially or server-owned through a new MCP tool.
3. Whether stage progress should be persisted only at completion or updated live during execution.

Recommendation:

Use a `tasks.workflow_status` JSON column, let the agent generate `review.json` for v1, and update stage status at completion first. This is the smallest useful product step and leaves a clean path to live stage updates later.
