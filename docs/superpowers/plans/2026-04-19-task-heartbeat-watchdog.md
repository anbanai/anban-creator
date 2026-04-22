# Task Heartbeat + Watchdog Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add heartbeat mechanism to detect stuck tasks within 5 minutes instead of 35.

**Architecture:** Add `last_heartbeat_at` column to tasks. DockerExecutor polls every 500ms and updates heartbeat via callback. Reaper checks heartbeat staleness (5 min threshold) instead of `started_at` (35 min threshold).

**Tech Stack:** Go, GORM, Docker SDK, Asynq

---

### Task 1: Add `last_heartbeat_at` to Task model

**Files:**
- Modify: `server/model/task.go:6-25`

- [ ] **Step 1: Add field to Task struct**

Add `LastHeartbeatAt` field after `CleanedUpAt` on line 19:

```go
LastHeartbeatAt    *time.Time `gorm:"index" json:"last_heartbeat_at,omitempty"`
```

Full context — the struct becomes:

```go
type Task struct {
	ID           string     `gorm:"type:char(36);primaryKey" json:"id"`
	UserID       string     `gorm:"type:char(36);index:idx_user_status,priority:1;index:idx_user_created,priority:1;not null" json:"user_id"`
	ChannelID    string     `gorm:"type:char(36);index" json:"channel_id"`
	PlanID       *uint      `gorm:"index" json:"plan_id"`
	Type         string     `gorm:"type:varchar(20);not null" json:"type"`
	Status       string     `gorm:"type:varchar(20);default:pending;index:idx_user_status,priority:2" json:"status"`
	Topic        string     `gorm:"type:varchar(500)" json:"topic"`
	ProgressLog  string     `gorm:"type:longtext" json:"progress_log,omitempty"`
	Result       *string    `gorm:"type:json" json:"result,omitempty"`
	ErrorMessage string     `gorm:"type:text" json:"error_message,omitempty"`
	StartedAt    *time.Time `gorm:"index" json:"started_at"`
	CompletedAt  *time.Time `gorm:"index" json:"completed_at"`
	CleanedUpAt  *time.Time `gorm:"index" json:"cleaned_up_at"`
	LastHeartbeatAt *time.Time `gorm:"index" json:"last_heartbeat_at,omitempty"`
	RetryCount         int        `gorm:"default:0" json:"retry_count"`
	MaxRetries         int        `gorm:"default:3" json:"max_retries"`
	RateLimitRetryCount int       `gorm:"default:0" json:"rate_limit_retry_count"`
	CreatedAt    time.Time  `gorm:"index:idx_user_created,priority:2" json:"created_at"`
	Plan         *Plan      `gorm:"foreignKey:PlanID" json:"plan,omitempty"`
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./server/...`
Expected: success (GORM AutoMigrate adds the column on next server start)

- [ ] **Step 3: Commit**

```bash
git add server/model/task.go
git commit -m "feat(task): add last_heartbeat_at column to task model"
```

---

### Task 2: Add `UpdateHeartbeat` to repository

**Files:**
- Modify: `server/repository/repository.go:62-85` (interface)
- Modify: `server/repository/task.go` (implementation)

- [ ] **Step 1: Add method to TaskRepository interface**

In `server/repository/repository.go`, add after `SetCompletedAt` (line 80):

```go
UpdateHeartbeat(ctx context.Context, id string) error
```

- [ ] **Step 2: Implement in taskRepository**

In `server/repository/task.go`, add after `SetCompletedAt` (after line 169):

```go
func (r *taskRepository) UpdateHeartbeat(ctx context.Context, id string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.Task{}).Where("id = ?", id).Update("last_heartbeat_at", now).Error
}
```

- [ ] **Step 3: Verify build**

Run: `go build ./server/...`
Expected: success

- [ ] **Step 4: Commit**

```bash
git add server/repository/repository.go server/repository/task.go
git commit -m "feat(task): add UpdateHeartbeat repository method"
```

---

### Task 3: Add heartbeat callback to ExecutionOptions and wire it in DockerExecutor

**Files:**
- Modify: `server/agent/executor.go:59-66` (ExecutionOptions)
- Modify: `server/agent/docker_executor.go:300-333` (polling loop)
- Modify: `server/service/task_execution.go:78-87` (caller wiring)

- [ ] **Step 1: Add HeartbeatFunc to ExecutionOptions**

In `server/agent/executor.go`, add field to `ExecutionOptions` after `OnProgress` (line 64):

```go
HeartbeatFunc func(taskID string) // periodic heartbeat callback for stuck-task detection
```

Full struct:

```go
type ExecutionOptions struct {
	Task          *model.Task
	Channel       *model.Channel
	Model         string
	MaxTurns      int
	OnProgress    func(taskID string, message string) // callback for SSE
	HeartbeatFunc func(taskID string)                 // periodic heartbeat callback for stuck-task detection
	LogWriter     *TaskLogWriter                      // optional per-task log file writer; nil = no log file
}
```

- [ ] **Step 2: Call HeartbeatFunc in executeViaExec polling loop**

In `server/agent/docker_executor.go`, in `executeViaExec`, add heartbeat call at the start of each polling iteration. After the `default:` branch on line 307, before the `inspect` call on line 310, add:

```go
			// Update heartbeat each poll iteration for stuck-task detection.
			if opts.HeartbeatFunc != nil {
				opts.HeartbeatFunc(opts.Task.ID)
			}
```

Full loop context:

```go
	for {
		select {
		case <-ctx.Done():
			res.err = fmt.Errorf("task cancelled")
			e.killExecProcess(execCreate.ID, containerName)
			e.waitForDone(done, execCreate.ID, containerName)
			return res
		default:
		}

		// Update heartbeat each poll iteration for stuck-task detection.
		if opts.HeartbeatFunc != nil {
			opts.HeartbeatFunc(opts.Task.ID)
		}

		inspect, err := e.dockerCLI.ContainerExecInspect(ctx, execCreate.ID)
		// ... rest unchanged
```

- [ ] **Step 3: Wire HeartbeatFunc in HandleExecution**

In `server/service/task_execution.go`, in the `executor.Execute` call (around line 88), add the `HeartbeatFunc` to the `ExecutionOptions`:

```go
	result, execErr := s.executor.Execute(ctx, &agent.ExecutionOptions{
		Task:      task,
		Channel:   channel,
		LogWriter: taskLogWriter,
		OnProgress: func(id string, message string) {
			if err := s.AppendProgressLog(ctx, id, message); err != nil {
				s.logger.Error().Err(err).Str("task_id", id).Msg("failed to update progress log")
			}
		},
		HeartbeatFunc: func(id string) {
			if err := s.repo.Tasks().UpdateHeartbeat(ctx, id); err != nil {
				s.logger.Warn().Err(err).Str("task_id", id).Msg("failed to update task heartbeat")
			}
		},
	})
```

Note: errors are logged as `Warn`, not `Error` — heartbeat failure must not crash the executor.

- [ ] **Step 4: Verify build**

Run: `go build ./server/...`
Expected: success

- [ ] **Step 5: Commit**

```bash
git add server/agent/executor.go server/agent/docker_executor.go server/service/task_execution.go
git commit -m "feat(task): wire heartbeat callback in DockerExecutor polling loop"
```

---

### Task 4: Replace reaper with heartbeat-based detection

**Files:**
- Modify: `server/scheduler/plan_checker.go:72-119`

- [ ] **Step 1: Replace reapStuckTasks function**

Replace the entire `reapStuckTasks` function (lines 72-119) with heartbeat-based detection:

```go
// stuckTaskThreshold is how long without a heartbeat before a task is considered stuck.
const stuckTaskThreshold = 5 * time.Minute

// reapStuckTasks finds running tasks whose heartbeat has stopped and marks them
// as failed. Uses last_heartbeat_at to distinguish "actively working" from "stuck".
func reapStuckTasks(ctx context.Context, repo repository.Repository, taskSvc *service.TaskService, logger *zerolog.Logger) {
	tasks, err := repo.Tasks().FindRunning(ctx)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to list running tasks for stuck reaper")
		return
	}

	now := time.Now()
	reaped := 0
	for _, t := range tasks {
		stuck := false
		var staleDuration time.Duration

		if t.LastHeartbeatAt != nil {
			// Heartbeat was set but is stale.
			staleDuration = now.Sub(*t.LastHeartbeatAt)
			if staleDuration > stuckTaskThreshold {
				stuck = true
			}
		} else if t.StartedAt != nil {
			// Task is running but never sent a heartbeat (executor never entered polling).
			staleDuration = now.Sub(*t.StartedAt)
			if staleDuration > stuckTaskThreshold {
				stuck = true
			}
		}

		if !stuck {
			continue
		}

		errMsg := fmt.Sprintf("stuck task reaped: no heartbeat for %s (threshold %s)", staleDuration.Round(time.Second), stuckTaskThreshold)
		logger.Warn().
			Str("task_id", t.ID).
			Str("user_id", t.UserID).
			Str("channel_id", t.ChannelID).
			Str("stale_duration", staleDuration.Round(time.Second).String()).
			Msg(errMsg)

		if err := repo.Tasks().UpdateStatusAndError(ctx, t.ID, model.TaskStatusFailed, errMsg); err != nil {
			logger.Error().Err(err).Str("task_id", t.ID).Msg("failed to mark stuck task as failed")
			continue
		}
		if err := repo.Tasks().SetCompletedAt(ctx, t.ID); err != nil {
			logger.Error().Err(err).Str("task_id", t.ID).Msg("failed to set completed_at on reaped task")
		}

		if err := taskSvc.RefundForTask(ctx, t.ID); err != nil {
			logger.Error().Err(err).Str("task_id", t.ID).Msg("failed to refund credits for reaped task")
		}

		if t.ChannelID != "" {
			if err := taskSvc.DispatchPendingTasks(ctx, t.ChannelID); err != nil {
				logger.Warn().Err(err).Str("channel_id", t.ChannelID).Msg("failed to dispatch pending tasks after reaping stuck task")
			}
		}
		reaped++
	}
	if reaped > 0 {
		logger.Info().Int("count", reaped).Msg("reaped stuck tasks")
	}
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./server/...`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add server/scheduler/plan_checker.go
git commit -m "feat(task): use heartbeat-based stuck task detection (5min threshold)"
```

---

### Task 5: Run tests and verify

- [ ] **Step 1: Run all server tests**

Run: `go test ./server/... -v 2>&1 | tail -30`
Expected: all tests pass

- [ ] **Step 2: Run build**

Run: `make server-build`
Expected: success

- [ ] **Step 3: Final commit (if any test fixes needed)**

Only if test adjustments were needed.
