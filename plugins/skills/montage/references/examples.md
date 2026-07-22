# Montage Examples

### Case 1: Social Short From Brand Brief

Input: `montage-input.json` contains a launch brief, `pipeline_key="social-short"`, vertical aspect ratio, and several product image assets.

Process: write `montage-project.json` with task/project IDs, normalized assets, selected pipeline, preferences, limits, and `output_dir`. Run Montage through the configured submodule or runner, then register `final_video` and `delivery-manifest.json`.

### Case 2: Local Runner With Existing Media

Input: the task was system-routed to local execution and includes uploaded task files for source footage plus a short creative direction.

Process: use Anban MCP workspace and task-file tools to resolve source assets, copy only needed adapter files into the workdir, leave `third_party/OpenMontage` untouched, and require non-empty final video plus manifest before completion.

### Case 3: Pipeline Failure

Input: Montage exits without a renderable final video, or the runner returns partial timeline/subtitle artifacts only.

Process: write `failure-diagnosis.md`, upload run logs and partial artifacts, register no successful `final_video`, and report failure through `submit_agent_feedback` with the failing stage and next action.
