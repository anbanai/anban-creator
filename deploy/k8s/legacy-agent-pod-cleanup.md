# Legacy Agent Pod Cleanup

This explicit post-rollout tool is the sole bulk migration cleanup for pods that still use the retired `anban-agent` identity. Run it only after all task intake is quiesced, every running task is drained, and the Server rollout that understands the `creator-agent` runtime identity is complete. Do not run or rely on legacy pod cleanup during Server startup or task execution because either path can overlap active agent executions.

## Required Sequence

1. Quiesce all task intake. Prevent API clients, schedules, and operators from starting new tasks.
2. Drain every running task and verify that no agent execution remains active.
3. Roll out the creator-agent-aware Server version and wait for the target Deployment rollout to complete.
4. Run a dry-run and review every listed pod name and UID:

   ```bash
   go run ./cmd/cleanup-legacy-agent-pods \
     --namespace <namespace> \
     --deployment <server-deployment>
   ```

5. After confirming task intake is still quiesced, all tasks are still drained, and the dry-run list is correct, execute cleanup:

   ```bash
   go run ./cmd/cleanup-legacy-agent-pods \
     --namespace <namespace> \
     --deployment <server-deployment> \
     --execute \
     --confirm-drained
   ```

   Type the exact confirmation token shown by the command. For controlled automation, add `--yes` to skip the interactive token.

Optional kubeconfig selection is available through `--kubeconfig <path>` and `--context <name>`.

## Safety Constraints

- Do not start a concurrent Server rollout or rollback while the cleanup command is running. The command snapshots the Deployment UID and generation and fails closed if either changes or rollout completeness is lost.
- `--execute` is rejected without `--confirm-drained`. That flag is an operator attestation that task intake has been quiesced and all running tasks have drained.
- The command lists pods once, accepts only pods matching the retired label and deterministic legacy identity, then re-gets every candidate before deletion.
- Every delete carries the candidate's snapshotted Kubernetes UID and last-observed resource version as preconditions. A replacement pod with the same name or a pod changed after final validation is preserved.
- The default mode is dry-run and performs no deletes.

Keep task intake quiesced until the command exits and any reported per-pod errors have been investigated. Re-run the dry-run before retrying cleanup.
