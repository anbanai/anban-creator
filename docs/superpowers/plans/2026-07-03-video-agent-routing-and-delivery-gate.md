# Video Agent Routing And Delivery Gate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ensure video generation tasks run the `video` agent directly and cannot complete without a downloaded, registered generated-video task file.

**Architecture:** Route `platform="video"` tasks directly to the `video` Claude agent. In the server completion path, distinguish generated/billed video tasks from other video-agent workflows: generated tasks must have a `video_generations.task_file_ids.generated_video` entry pointing to a real non-empty video task file, while non-generation workflows such as cover or pose variants continue to use the existing meaningful-artifact validation. Add Claude plugin guardrails so the video agent executes the main workflow inline and a SubagentStop hook blocks incomplete delivery manifests.

**Tech Stack:** Go, GORM-backed task/task-file/video-generation models, Claude Code agent definitions, Claude Code hooks, Bash/Python hook script.

---

## Implementation Tasks

- [x] **Route video tasks to the video agent**
  - Update `server/agent/config_builder.go` so `model.ScopeVideo` maps to `video`.
  - Add `TestTaskTypeToAgent` coverage for `model.ScopeVideo`.

- [x] **Require generated-video closure for billed video generation**
  - Add service-level completion validation for `model.PlatformVideo` tasks with `VideoGenerationID`, `VideoEstimatedCredits`, or `VideoCreditsCharged`.
  - Require the related `VideoGeneration.TaskFileIDs` JSON to contain `generated_video`.
  - Require that task file ID to exist on the task and be a non-empty video file (`video/*`, `.mp4`, `.mov`, `.webm`, or `.m4v`).
  - Reuse this validation in both cloud execution completion and local executor completion.

- [x] **Avoid misclassifying input references as delivery**
  - Add tests proving a generated video task with only an input video reference task file fails.
  - Add tests proving a generated video task with a `generated_video` task file passes.
  - Add tests proving non-generation video workflows with a meaningful non-video artifact, such as `cover.png`, are not broken by the generated-video gate.

- [x] **Harden Claude plugin workflow discipline**
  - Update `claudecode/agents/video.md` to forbid using the Claude `Agent` tool for the main video workflow.
  - Add `claudecode/hooks/video-quality-gate.sh` to block incomplete `dreamina-video`, `video-use`, cover, pose, and CapCut draft deliveries.
  - Register the hook before the existing video summary prompt in `claudecode/hooks/hooks.json`.
  - Patch-bump `claudecode/.claude-plugin/plugin.json` and update the server-side plugin version contract test.

- [x] **Block managed-runtime delegation escapes**
  - Add a shared Go runtime policy that passes `--disallowed-tools Agent,ScheduleWakeup` through the Claude Code SDK.
  - Apply it to both the server SDK executor and the local/Docker agent runner.
  - Keep MCP inheritance intact by avoiding agent frontmatter `tools:` allowlists.

## Verification

- `python3 -m json.tool claudecode/hooks/hooks.json`
- `python3 -m json.tool claudecode/.claude-plugin/plugin.json`
- `bash -n claudecode/hooks/video-quality-gate.sh`
- Manual hook check for missing `dreamina-video` delivery files returns `decision:block`.
- Manual hook check with submit/result/delivery manifest exits cleanly.
- `go test ./server/agent -count=1`
- `go test ./agent -count=1`
- `go test ./server/service -run 'TestTaskServiceHandleExecution.*Video|TestCompleteLocalTask_NestedAgentOnlyFails' -count=1`
- `go test ./...`
- `go build -o /tmp/anban-creator-server ./server`
- `go build -o /tmp/anban ./agent`
