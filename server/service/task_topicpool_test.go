package service

import (
	"context"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
)

// setupTaskServiceWithTopicPool wires a real TopicPoolService into the task
// service so CreateFromPlan's claim path is exercised against the sqlite repo.
func setupTaskServiceWithTopicPool(t *testing.T) (*TaskService, *TopicPoolService) {
	t.Helper()
	svc, repo := setupTaskServiceWithEnqueuer(t)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	topicSvc := NewTopicPoolService(repo, &logger)
	svc.SetTopicPoolService(topicSvc)
	return svc, topicSvc
}

// TestCreateFromPlan_ClaimsTopicFromPool_Article: an article-channel plan with
// no Prompt/Title must claim the channel's next unused topic and use it as the
// task prompt.
func TestCreateFromPlan_ClaimsTopicFromPool_Article(t *testing.T) {
	svc, topicSvc := setupTaskServiceWithTopicPool(t)
	ctx := context.Background()
	userID := uuid.New().String()
	channelID := createTestChannel(t, svc.repo, userID, model.PlatformArticle)

	const seeded = "三个月喝懂普洱：从生普到熟普的进阶路线"
	if _, err := topicSvc.Add(ctx, userID, channelID, []string{seeded}); err != nil {
		t.Fatalf("seed topic: %v", err)
	}

	plan := &model.Plan{
		ID:        uuid.New().String(),
		UserID:    userID,
		ChannelID: channelID,
		Type:      model.PlatformArticle,
		Status:    model.PlanStatusActive,
		// Prompt and Title intentionally empty → must claim from pool.
	}

	task, err := svc.CreateFromPlan(ctx, plan)
	if err != nil {
		t.Fatalf("CreateFromPlan: %v", err)
	}
	if task == nil {
		t.Fatal("expected a task, got nil")
	}
	if task.Prompt != seeded {
		t.Fatalf("task.Prompt = %q, want claimed topic %q", task.Prompt, seeded)
	}

	// The claimed topic must be marked used and bound to the spawned task.
	used, _, err := topicSvc.List(ctx, userID, channelID, model.TopicStatusUsed, 0, 10)
	if err != nil {
		t.Fatalf("list used topics: %v", err)
	}
	if len(used) != 1 || used[0].Topic != seeded {
		t.Fatalf("used topics = %v, want 1 used = %q", used, seeded)
	}
	if used[0].TaskID == nil || *used[0].TaskID != task.ID {
		t.Fatalf("claimed topic TaskID = %v, want %q", used[0].TaskID, task.ID)
	}
}

// TestCreateFromPlan_ClaimsTopicFromPool_Seednote: the topic pool is keyed by
// channel_id and is platform-agnostic at the data layer, so a seednote-channel
// plan claims just like an article-channel plan.
func TestCreateFromPlan_ClaimsTopicFromPool_Seednote(t *testing.T) {
	svc, topicSvc := setupTaskServiceWithTopicPool(t)
	ctx := context.Background()
	userID := uuid.New().String()
	channelID := createTestChannel(t, svc.repo, userID, model.PlatformSeednote)

	const seeded = "早C晚A 精华实测：油皮亲测两周真实记录"
	if _, err := topicSvc.Add(ctx, userID, channelID, []string{seeded}); err != nil {
		t.Fatalf("seed topic: %v", err)
	}

	plan := &model.Plan{
		ID:        uuid.New().String(),
		UserID:    userID,
		ChannelID: channelID,
		Type:      model.PlatformSeednote,
		Status:    model.PlanStatusActive,
	}

	task, err := svc.CreateFromPlan(ctx, plan)
	if err != nil {
		t.Fatalf("CreateFromPlan: %v", err)
	}
	if task == nil {
		t.Fatal("expected a task, got nil")
	}
	if task.Prompt != seeded {
		t.Fatalf("task.Prompt = %q, want claimed topic %q", task.Prompt, seeded)
	}
}

// TestCreateFromPlan_DoesNotClaimWhenPromptSet: when the plan carries an
// explicit Prompt (or Title), the topic pool must NOT be consumed — the user's
// explicit topic wins.
func TestCreateFromPlan_DoesNotClaimWhenPromptSet(t *testing.T) {
	svc, topicSvc := setupTaskServiceWithTopicPool(t)
	ctx := context.Background()
	userID := uuid.New().String()
	channelID := createTestChannel(t, svc.repo, userID, model.PlatformArticle)

	// Seed a topic that must remain UNUSED.
	const seeded = "不应被消费的选题"
	if _, err := topicSvc.Add(ctx, userID, channelID, []string{seeded}); err != nil {
		t.Fatalf("seed topic: %v", err)
	}

	const explicit = "用户显式指定的主题"
	plan := &model.Plan{
		ID:        uuid.New().String(),
		UserID:    userID,
		ChannelID: channelID,
		Type:      model.PlatformArticle,
		Prompt:    explicit,
		Status:    model.PlanStatusActive,
	}

	task, err := svc.CreateFromPlan(ctx, plan)
	if err != nil {
		t.Fatalf("CreateFromPlan: %v", err)
	}
	if task == nil {
		t.Fatal("expected a task, got nil")
	}
	if task.Prompt != explicit {
		t.Fatalf("task.Prompt = %q, want explicit %q", task.Prompt, explicit)
	}
	used, _, err := topicSvc.List(ctx, userID, channelID, model.TopicStatusUsed, 0, 10)
	if err != nil {
		t.Fatalf("list used topics: %v", err)
	}
	if len(used) != 0 {
		t.Fatalf("topic pool was consumed (%d used) even though plan had an explicit prompt", len(used))
	}
}

// TestCreateFromPlan_TitleAlsoBlocksClaim: a non-empty Title (with empty Prompt)
// must also prevent claiming, since CreateFromPlan falls back to plan.Title.
func TestCreateFromPlan_TitleAlsoBlocksClaim(t *testing.T) {
	svc, topicSvc := setupTaskServiceWithTopicPool(t)
	ctx := context.Background()
	userID := uuid.New().String()
	channelID := createTestChannel(t, svc.repo, userID, model.PlatformArticle)

	if _, err := topicSvc.Add(ctx, userID, channelID, []string{"不应被消费"}); err != nil {
		t.Fatalf("seed topic: %v", err)
	}

	const titleTopic = "标题即主题"
	plan := &model.Plan{
		ID:        uuid.New().String(),
		UserID:    userID,
		ChannelID: channelID,
		Type:      model.PlatformArticle,
		Title:     titleTopic,
		Status:    model.PlanStatusActive,
	}

	task, err := svc.CreateFromPlan(ctx, plan)
	if err != nil {
		t.Fatalf("CreateFromPlan: %v", err)
	}
	if task == nil {
		t.Fatal("expected a task, got nil")
	}
	if task.Prompt != titleTopic {
		t.Fatalf("task.Prompt = %q, want title %q", task.Prompt, titleTopic)
	}
}

// TestCreateFromPlan_EmptyPoolFallsBack: when no prompt AND the pool is empty,
// CreateFromPlan must still succeed with an empty prompt (agent auto-research),
// not error.
func TestCreateFromPlan_EmptyPoolFallsBack(t *testing.T) {
	svc, _ := setupTaskServiceWithTopicPool(t)
	ctx := context.Background()
	userID := uuid.New().String()
	channelID := createTestChannel(t, svc.repo, userID, model.PlatformArticle)

	// No topic seeded → pool empty.
	plan := &model.Plan{
		ID:        uuid.New().String(),
		UserID:    userID,
		ChannelID: channelID,
		Type:      model.PlatformArticle,
		Status:    model.PlanStatusActive,
	}

	task, err := svc.CreateFromPlan(ctx, plan)
	if err != nil {
		t.Fatalf("CreateFromPlan on empty pool: %v", err)
	}
	if task == nil {
		t.Fatal("expected a task, got nil")
	}
	if task.Prompt != "" {
		t.Fatalf("task.Prompt = %q, want empty (auto-research fallback)", task.Prompt)
	}
}
