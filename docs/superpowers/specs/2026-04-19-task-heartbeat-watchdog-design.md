# Task Heartbeat + Watchdog Design

## Problem

Tasks in the DockerExecutor can get stuck in "running" status forever. The system has no way to distinguish "actively working" from "stuck" — it only knows "executor hasn't returned" vs "executor returned". When the Docker exec process doesn't respond to SIGTERM, `executeViaExec` blocks on `<-done` indefinitely, and no code path transitions the task out of "running".

## Design

Add a heartbeat mechanism so the system can detect stuck tasks quickly and accurately.

### Data Model

Add `last_heartbeat_at` column to `tasks` table:

```go
// server/model/task.go
LastHeartbeatAt *time.Time `gorm:"column:last_heartbeat_at" json:"last_heartbeat_at,omitempty"`
```

GORM AutoMigrate handles the column addition. No manual SQL migration needed.

### Heartbeat Reporting

In `executeViaExec`'s polling loop (existing 500ms interval), update heartbeat each iteration:

```go
// server/repository/task.go
func (r *taskRepository) UpdateHeartbeat(ctx context.Context, taskID string) error {
    now := time.Now()
    return r.db.WithContext(ctx).Model(&Task{}).
        Where("id = ?", taskID).
        Update("last_heartbeat_at", now).Error
}
```

Called from `executeViaExec` in the polling loop, after the `select default` branch. Errors are logged as warnings only — heartbeat failure must not crash the executor.

`executeInNewContainer` path does NOT update heartbeat (no polling loop). Acceptable because that path has `ContainerWait` with context timeout.

### Watchdog (Reaper)

Replace `reapStuckTasks` in `plan_checker.go` with heartbeat-based detection:

**Detection rules:**
- `started_at` set AND `last_heartbeat_at` IS NULL AND `started_at` > 5 min ago → executor never entered polling loop
- `last_heartbeat_at` < 5 min ago → heartbeat stopped, task is stuck

**Actions on stuck task:**
1. Set status to "failed" with descriptive error message
2. Set `completed_at`
3. Refund credits via `TaskService.RefundForTask`
4. Dispatch pending tasks for the affected channel

**Threshold choice:** 5 minutes. Normal heartbeat interval is 500ms, so a healthy task updates every <1s. 5 minutes gives ample tolerance for DB blips, garbage collection pauses, and network jitter.

### Keep: SIGKILL Fallback

The `forceKillExecProcess` + `waitForDone` (10s timeout) added in the previous fix remains. It handles the Docker-layer process kill when SIGTERM fails. This is orthogonal to the heartbeat — heartbeat detects the problem, SIGKILL ensures cleanup.

### Remove: started_at-based 35-minute threshold

The original `reapStuckTasks` used `started_at` + 35 minutes. Replace with heartbeat-based 5-minute detection. The 35-minute threshold was too long (tasks could be stuck for 35 minutes before being caught) and too blunt (could not distinguish active long-running tasks from stuck ones).

## Files to Modify

| File | Change |
|------|--------|
| `server/model/task.go` | Add `LastHeartbeatAt` field |
| `server/repository/task.go` | Add `UpdateHeartbeat` method |
| `server/agent/docker_executor.go` | Call `UpdateHeartbeat` in polling loop |
| `server/scheduler/plan_checker.go` | Replace `reapStuckTasks` with heartbeat-based detection |

## Verification

1. `make server-build` — compiles without errors
2. `go test ./server/...` — all existing tests pass
3. Manual test: create a task, verify `last_heartbeat_at` updates every ~500ms while running
4. Manual test: kill the Docker exec process manually, verify task is reaped within 5 minutes
