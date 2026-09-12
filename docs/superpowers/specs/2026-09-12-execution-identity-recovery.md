# Execution Identity Recovery Specification

## Problem

An article task reached `image_generation`, but every fixed-SKU image operation failed with an execution-identity error. Completion metadata contained empty `task_id` / `execution_id`, and the runtime later surfaced `completion_report_failed`. Existing text artifacts were preserved, but the task could not resume safely.

The failure is an execution-contract problem, not an image prompt, aspect-ratio, provider-route, or content-quality problem. The current architecture requires a short-lived execution JWT from Bootstrap for MCP calls. The runtime also receives the execution ID as a required command argument, while Hook metadata receives non-secret identity fields through its child-process environment.

## Required Behavior

1. Server dispatch creates exactly one current execution identity for the task.
2. Docker and Kubernetes start the runtime with that execution ID and a workload token.
3. Bootstrap verifies the workload, confirms the execution is current/running, and returns the canonical `execution_id`, `task_id`, `project_id`, and execution JWT.
4. The TypeScript runtime rejects missing or mismatched identity before starting Claude, exposes only non-secret IDs to Claude/Hook, and uses the JWT for MCP and Agent HTTP requests.
5. MCP image, image-analysis, image-upload, and completion-metadata operations fail closed when the request is not execution-scoped; they must not charge or call a provider.
6. Runtime/server contract mismatch is rejected at Bootstrap with an upgrade-required response before content work begins.
7. Identity failures during image generation are deterministic, preserve existing artifacts, write a structured recoverable failure with `resume_from=image_generation`, and do not consume creative retry budgets.
8. `completion_report_failed` remains a delivery-layer status and must not replace the original root failure.
9. Studio presents a short Chinese recoverable message while retaining the machine code and detailed diagnostics for operators.

## Non-Goals

- Do not make API keys valid substitutes for execution JWTs.
- Do not put credentials in environment dumps, failure artifacts, or user-facing copy.
- Do not change provider retry policy for genuine provider or quality failures.
- Do not delete or rewrite already-produced task artifacts during recovery.
