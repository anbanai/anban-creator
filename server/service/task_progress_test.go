package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"gorm.io/datatypes"

	"github.com/anbanai/anban-creator/server/agentpack"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func setupProgressFromAgentTest(t *testing.T, withPubSub bool, mutateExecution func(*model.TaskExecution)) (*TaskService, repository.Repository, *model.Task, *model.TaskExecution, *ProgressSubscriber, *miniredis.Miniredis) {
	t.Helper()
	ctx := context.Background()
	repo := repository.New(setupTaskTestDB(t))
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformMoments)
	pack, ok := agentpack.Default().Pack("moments")
	if !ok {
		t.Fatal("embedded moments Pack missing")
	}
	executionID := uuid.NewString()
	task := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformMoments,
		Status: model.TaskStatusRunning, CurrentExecutionID: &executionID,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	execution := &model.TaskExecution{
		ID: executionID, TaskID: task.ID, Attempt: 1, Target: "kubernetes", Status: model.TaskExecutionRunning,
		AgentPackID: pack.ID, AgentPackVersion: pack.Version, AgentPackDigest: pack.Digest,
		RuntimeAdapter: pack.Runtime.Adapter, RuntimeProfile: pack.Runtime.Profile,
	}
	progressContract, err := json.Marshal(pack.Progress)
	if err != nil {
		t.Fatalf("marshal progress contract: %v", err)
	}
	execution.AgentPackProgressContract = datatypes.JSON(progressContract)
	if mutateExecution != nil {
		mutateExecution(execution)
	}
	if err := repo.TaskExecutions().Create(ctx, execution); err != nil {
		t.Fatalf("create execution: %v", err)
	}

	logger := zerolog.New(io.Discard)
	var pubsub *RedisPubSub
	var subscriber *ProgressSubscriber
	var miniRedis *miniredis.Miniredis
	if withPubSub {
		miniRedis = miniredis.RunT(t)
		rdb := redis.NewClient(&redis.Options{Addr: miniRedis.Addr()})
		t.Cleanup(func() { _ = rdb.Close() })
		pubsub = NewRedisPubSub(rdb, &logger)
		subscriber = pubsub.SubscribeProgress(ctx, task.ID)
		t.Cleanup(func() { _ = subscriber.Close() })
		channel := progressChannelPrefix + task.ID
		deadline := time.Now().Add(time.Second)
		for miniRedis.PubSubNumSub(channel)[channel] != 1 {
			if time.Now().After(deadline) {
				t.Fatal("timed out establishing progress subscription")
			}
		}
	}
	return newTestTaskService(repo, nil, nil, &logger, "", pubsub, nil), repo, task, execution, subscriber, miniRedis
}

func TestUpdateProgressFromAgentUsesFrozenExecutionPackAndPublishesSSE(t *testing.T) {
	ctx := context.Background()
	svc, repo, task, execution, subscriber, miniRedis := setupProgressFromAgentTest(t, true, nil)

	if err := svc.UpdateProgressFromAgent(ctx, task.ID, execution.ID, "writing", "complete", "朋友圈正文", "正文已生成", 55); err != nil {
		t.Fatalf("UpdateProgressFromAgent: %v", err)
	}
	persisted, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	latest := persisted.LatestProgress.Data()
	if persisted.Progress != 55 || latest.Stage != "writing" || latest.State != "complete" || latest.Title != "朋友圈正文" || latest.Description != "正文已生成" || latest.Percent != 55 {
		t.Fatalf("persisted progress = %d/%#v", persisted.Progress, latest)
	}
	if !strings.Contains(persisted.ProgressLog, `"stage":"writing"`) {
		t.Fatalf("progress log = %q", persisted.ProgressLog)
	}

	select {
	case message := <-subscriber.Events():
		var event ProgressEvent
		if err := json.Unmarshal([]byte(message.Payload), &event); err != nil {
			t.Fatal(err)
		}
		if event.TaskID != task.ID || event.Stage != "writing" || event.State != "complete" || event.Title != "朋友圈正文" || event.Description != "正文已生成" || event.Percent != 55 {
			t.Fatalf("progress event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for structured progress event")
	}
	commandsBeforeStale := miniRedis.CommandCount()
	if err := svc.UpdateProgressFromAgent(ctx, task.ID, execution.ID, "writing", "active", "朋友圈正文", "迟到的 active 事件", 35); err != nil {
		t.Fatalf("stale UpdateProgressFromAgent: %v", err)
	}
	if commandsAfterStale := miniRedis.CommandCount(); commandsAfterStale != commandsBeforeStale {
		t.Fatalf("stale progress published Redis command: before=%d after=%d", commandsBeforeStale, commandsAfterStale)
	}
}

func TestUpdateProgressFromAgentRejectsStaleExecutionOrTerminalTaskWithoutPublishingSSE(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(context.Context, repository.Repository, *model.Task) error
	}{
		{
			name: "current execution replaced",
			mutate: func(ctx context.Context, repo repository.Repository, task *model.Task) error {
				_, err := repo.Tasks().SetCurrentExecution(ctx, task.ID, uuid.NewString())
				return err
			},
		},
		{name: model.TaskStatusCancelled, mutate: func(ctx context.Context, repo repository.Repository, task *model.Task) error {
			return repo.Tasks().UpdateStatus(ctx, task.ID, model.TaskStatusCancelled)
		}},
		{name: model.TaskStatusCompleted, mutate: func(ctx context.Context, repo repository.Repository, task *model.Task) error {
			return repo.Tasks().UpdateStatus(ctx, task.ID, model.TaskStatusCompleted)
		}},
		{name: model.TaskStatusFailed, mutate: func(ctx context.Context, repo repository.Repository, task *model.Task) error {
			return repo.Tasks().UpdateStatus(ctx, task.ID, model.TaskStatusFailed)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			svc, repo, task, execution, subscriber, _ := setupProgressFromAgentTest(t, true, nil)
			if err := tt.mutate(ctx, repo, task); err != nil {
				t.Fatal(err)
			}

			if err := svc.UpdateProgressFromAgent(ctx, task.ID, execution.ID, "writing", "complete", "朋友圈正文", "must be rejected", 55); err != nil {
				t.Fatalf("UpdateProgressFromAgent: %v", err)
			}
			persisted, err := repo.Tasks().FindByID(ctx, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.ProgressSequence != 0 || persisted.Progress != 0 || persisted.LatestProgress.Data() != (model.ProgressPayload{}) || persisted.ProgressLog != "" {
				t.Fatalf("rejected progress mutated task: sequence=%d progress=%d latest=%#v log=%q",
					persisted.ProgressSequence, persisted.Progress, persisted.LatestProgress.Data(), persisted.ProgressLog)
			}
			select {
			case message := <-subscriber.Events():
				t.Fatalf("rejected progress published structured SSE payload: %s", message.Payload)
			case <-time.After(50 * time.Millisecond):
			}
		})
	}
}

func TestUpdateProgressFromAgentTreatsClientPercentOnlyAsStateHint(t *testing.T) {
	ctx := context.Background()
	svc, repo, task, execution, _, _ := setupProgressFromAgentTest(t, false, nil)

	if err := svc.UpdateProgressFromAgent(ctx, task.ID, execution.ID, "writing", "active", "朋友圈正文", "开始写作", 35); err != nil {
		t.Fatalf("UpdateProgressFromAgent active: %v", err)
	}
	persisted, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Progress != 35 || persisted.LatestProgress.Data().Percent != 35 {
		t.Fatalf("active Pack percent not persisted: progress=%d latest=%#v", persisted.Progress, persisted.LatestProgress.Data())
	}
	if err := svc.UpdateProgressFromAgent(ctx, task.ID, execution.ID, "writing", "active", "朋友圈正文", "任意值", 987); !errors.Is(err, ErrAgentProgressPercentMismatch) {
		t.Fatalf("arbitrary percent error = %v, want ErrAgentProgressPercentMismatch", err)
	}
	persisted, err = repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Progress != 35 || persisted.LatestProgress.Data().Description != "开始写作" {
		t.Fatalf("rejected percent changed progress: progress=%d latest=%#v", persisted.Progress, persisted.LatestProgress.Data())
	}
}

func TestUpdateProgressFromAgentUsesFrozenPackInsteadOfLegacyStageFallback(t *testing.T) {
	ctx := context.Background()
	frozen := datatypes.JSON(`[{"id":"writing","title":"Frozen Writing","active_percent":41,"complete_percent":73}]`)
	svc, repo, task, execution, _, _ := setupProgressFromAgentTest(t, false, func(execution *model.TaskExecution) {
		execution.AgentPackProgressContract = frozen
	})

	if err := svc.UpdateProgressFromAgent(ctx, task.ID, execution.ID, "writing", "complete", "Frozen Writing", "frozen contract", 73); err != nil {
		t.Fatalf("UpdateProgressFromAgent: %v", err)
	}
	persisted, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Progress != 73 || persisted.LatestProgress.Data().Percent != 73 {
		t.Fatalf("managed progress = %d/%#v, want frozen Pack percent 73 instead of legacy moments fallback 55", persisted.Progress, persisted.LatestProgress.Data())
	}
}

func TestUpdateProgressFromAgentRejectsInvalidExecutionPackContract(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name   string
		mutate func(*model.TaskExecution)
		stage  string
		state  string
		title  string
		want   error
	}{
		{name: "unknown stage", stage: "not_declared", state: "complete", title: "未知", want: ErrAgentProgressUnknownStage},
		{name: "invalid state", stage: "writing", state: "paused", title: "朋友圈正文", want: ErrAgentProgressStateMismatch},
		{name: "title mismatch", stage: "writing", state: "complete", title: "别的标题", want: ErrAgentProgressTitleMismatch},
		{name: "execution task mismatch", stage: "writing", state: "complete", title: "朋友圈正文", mutate: func(execution *model.TaskExecution) { execution.TaskID = uuid.NewString() }, want: ErrAgentProgressExecutionMismatch},
		{name: "missing pack id", stage: "writing", state: "complete", title: "朋友圈正文", mutate: func(execution *model.TaskExecution) { execution.AgentPackID = "" }, want: ErrAgentProgressPackMismatch},
		{name: "missing pack version", stage: "writing", state: "complete", title: "朋友圈正文", mutate: func(execution *model.TaskExecution) { execution.AgentPackVersion = "" }, want: ErrAgentProgressPackMismatch},
		{name: "missing pack digest", stage: "writing", state: "complete", title: "朋友圈正文", mutate: func(execution *model.TaskExecution) { execution.AgentPackDigest = "" }, want: ErrAgentProgressPackMismatch},
		{name: "missing progress snapshot", stage: "writing", state: "complete", title: "朋友圈正文", mutate: func(execution *model.TaskExecution) { execution.AgentPackProgressContract = nil }, want: ErrAgentProgressPackMismatch},
		{name: "invalid progress snapshot", stage: "writing", state: "complete", title: "朋友圈正文", mutate: func(execution *model.TaskExecution) {
			execution.AgentPackProgressContract = datatypes.JSON(`{"bad":true}`)
		}, want: ErrAgentProgressPackMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, task, execution, _, _ := setupProgressFromAgentTest(t, false, func(execution *model.TaskExecution) {
				if tt.mutate != nil {
					tt.mutate(execution)
				}
			})
			err := svc.UpdateProgressFromAgent(ctx, task.ID, execution.ID, tt.stage, tt.state, tt.title, "", 55)
			if !errors.Is(err, tt.want) {
				t.Fatalf("UpdateProgressFromAgent error = %v, want %v", err, tt.want)
			}
			persisted, findErr := repo.Tasks().FindByID(ctx, task.ID)
			if findErr != nil {
				t.Fatal(findErr)
			}
			if persisted.Progress != 0 || persisted.ProgressLog != "" || persisted.LatestProgress.Data().Stage != "" {
				t.Fatalf("rejected event changed task: %#v", persisted)
			}
		})
	}
}

func TestUpdateProgressFromAgentPreservesMonotonicProgress(t *testing.T) {
	ctx := context.Background()
	svc, repo, task, execution, _, _ := setupProgressFromAgentTest(t, false, nil)
	if err := svc.UpdateProgressFromAgent(ctx, task.ID, execution.ID, "quality_review", "complete", "质量复盘", "完成复盘", 88); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateProgressFromAgent(ctx, task.ID, execution.ID, "writing", "complete", "朋友圈正文", "迟到的写作事件", 55); err != nil {
		t.Fatal(err)
	}
	persisted, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Progress != 88 {
		t.Fatalf("progress rolled backward to %d", persisted.Progress)
	}
	latest := persisted.LatestProgress.Data()
	if latest.Stage != "quality_review" || latest.Percent != 88 || latest.Description != "完成复盘" {
		t.Fatalf("latest progress rolled backward: %#v", latest)
	}
	if strings.Contains(persisted.ProgressLog, "迟到的写作事件") {
		t.Fatalf("stale progress appended to log: %q", persisted.ProgressLog)
	}
}

func TestUpdateProgressFromAgentOrdersZeroAndEqualPercentStates(t *testing.T) {
	ctx := context.Background()
	contract := datatypes.JSON(`[
		{"id":"first","title":"First","active_percent":0,"complete_percent":0},
		{"id":"second","title":"Second","active_percent":0,"complete_percent":0}
	]`)
	svc, repo, task, execution, _, _ := setupProgressFromAgentTest(t, false, func(execution *model.TaskExecution) {
		execution.AgentPackProgressContract = contract
	})
	events := []struct {
		stage, state, title string
		wantSequence        int
	}{
		{stage: "first", state: "active", title: "First", wantSequence: 1},
		{stage: "first", state: "active", title: "First", wantSequence: 1},
		{stage: "first", state: "complete", title: "First", wantSequence: 2},
		{stage: "second", state: "active", title: "Second", wantSequence: 3},
	}
	for _, event := range events {
		if err := svc.UpdateProgressFromAgent(ctx, task.ID, execution.ID, event.stage, event.state, event.title, event.state, 0); err != nil {
			t.Fatalf("%s/%s: %v", event.stage, event.state, err)
		}
		persisted, err := repo.Tasks().FindByID(ctx, task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if persisted.ProgressSequence != event.wantSequence || persisted.Progress != 0 {
			t.Fatalf("%s/%s progress = sequence:%d percent:%d", event.stage, event.state, persisted.ProgressSequence, persisted.Progress)
		}
	}
	persisted, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	latest := persisted.LatestProgress.Data()
	if latest.Stage != "second" || latest.State != "active" || latest.Percent != 0 {
		t.Fatalf("equal-percent latest = %#v", latest)
	}
	if strings.Count(persisted.ProgressLog, `"stage":"first","state":"active"`) != 1 || strings.Count(persisted.ProgressLog, `"state":`) != 3 {
		t.Fatalf("equal-percent progress log = %q", persisted.ProgressLog)
	}
}
