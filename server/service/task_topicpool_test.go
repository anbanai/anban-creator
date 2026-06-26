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

// TestCreateFromPlan_ClaimsTopicFromPool_Article: an article-project plan with
// no Prompt/Title must claim the project's next unused topic and use it as the
// task prompt.
func TestCreateFromPlan_ClaimsTopicFromPool_Article(t *testing.T) {
	svc, topicSvc := setupTaskServiceWithTopicPool(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, svc.repo, userID, model.PlatformArticle)

	const seeded = "三个月喝懂普洱：从生普到熟普的进阶路线"
	if _, err := topicSvc.Add(ctx, userID, projectID, []string{seeded}); err != nil {
		t.Fatalf("seed topic: %v", err)
	}

	plan := &model.Plan{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
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
	used, _, err := topicSvc.List(ctx, userID, projectID, model.TopicStatusUsed, 0, 10)
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
// project_id and is platform-agnostic at the data layer, so a seednote-project
// plan claims just like an article-project plan.
func TestCreateFromPlan_ClaimsTopicFromPool_Seednote(t *testing.T) {
	svc, topicSvc := setupTaskServiceWithTopicPool(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, svc.repo, userID, model.PlatformSeednote)

	const seeded = "早C晚A 精华实测：油皮亲测两周真实记录"
	if _, err := topicSvc.Add(ctx, userID, projectID, []string{seeded}); err != nil {
		t.Fatalf("seed topic: %v", err)
	}

	plan := &model.Plan{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
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
	projectID := createTestProject(t, svc.repo, userID, model.PlatformArticle)

	// Seed a topic that must remain UNUSED.
	const seeded = "不应被消费的选题"
	if _, err := topicSvc.Add(ctx, userID, projectID, []string{seeded}); err != nil {
		t.Fatalf("seed topic: %v", err)
	}

	const explicit = "用户显式指定的主题"
	plan := &model.Plan{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
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
	used, _, err := topicSvc.List(ctx, userID, projectID, model.TopicStatusUsed, 0, 10)
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
	projectID := createTestProject(t, svc.repo, userID, model.PlatformArticle)

	if _, err := topicSvc.Add(ctx, userID, projectID, []string{"不应被消费"}); err != nil {
		t.Fatalf("seed topic: %v", err)
	}

	const titleTopic = "标题即主题"
	plan := &model.Plan{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
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
	projectID := createTestProject(t, svc.repo, userID, model.PlatformArticle)

	// No topic seeded → pool empty.
	plan := &model.Plan{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
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

// TestCreateManual_ClaimsTopicFromPool_Article: a manual article task with no
// prompt must claim the project's next unused topic and use it as the task
// prompt (mirroring CreateFromPlan), marking the topic used and bound to the task.
func TestCreateManual_ClaimsTopicFromPool_Article(t *testing.T) {
	svc, topicSvc := setupTaskServiceWithTopicPool(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, svc.repo, userID, model.PlatformArticle)

	const seeded = "三个月喝懂普洱：从生普到熟普的进阶路线"
	if _, err := topicSvc.Add(ctx, userID, projectID, []string{seeded}); err != nil {
		t.Fatalf("seed topic: %v", err)
	}

	tasks, err := svc.CreateManual(ctx, CreateManualParams{
		UserID:    userID,
		ProjectID: projectID,
		Quantity:  1,
		// Prompt intentionally empty → must claim from pool.
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Prompt != seeded {
		t.Fatalf("task.Prompt = %q, want claimed topic %q", tasks[0].Prompt, seeded)
	}

	// The claimed topic must be marked used and bound to the spawned task.
	used, _, err := topicSvc.List(ctx, userID, projectID, model.TopicStatusUsed, 0, 10)
	if err != nil {
		t.Fatalf("list used topics: %v", err)
	}
	if len(used) != 1 || used[0].Topic != seeded {
		t.Fatalf("used topics = %v, want 1 used = %q", used, seeded)
	}
	if used[0].TaskID == nil || *used[0].TaskID != tasks[0].ID {
		t.Fatalf("claimed topic TaskID = %v, want %q", used[0].TaskID, tasks[0].ID)
	}
}

// TestCreateManual_DoesNotClaimWhenPromptSet: an explicit prompt must win — the
// pool is not consumed.
func TestCreateManual_DoesNotClaimWhenPromptSet(t *testing.T) {
	svc, topicSvc := setupTaskServiceWithTopicPool(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, svc.repo, userID, model.PlatformArticle)

	const seeded = "不应被消费的选题"
	if _, err := topicSvc.Add(ctx, userID, projectID, []string{seeded}); err != nil {
		t.Fatalf("seed topic: %v", err)
	}

	const explicit = "用户显式指定的主题"
	tasks, err := svc.CreateManual(ctx, CreateManualParams{
		UserID:    userID,
		ProjectID: projectID,
		Prompt:    explicit,
		Quantity:  1,
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Prompt != explicit {
		t.Fatalf("task.Prompt = %q, want explicit %q", tasks[0].Prompt, explicit)
	}
	used, _, err := topicSvc.List(ctx, userID, projectID, model.TopicStatusUsed, 0, 10)
	if err != nil {
		t.Fatalf("list used topics: %v", err)
	}
	if len(used) != 0 {
		t.Fatalf("topic pool consumed (%d used) despite explicit prompt", len(used))
	}
}

// TestCreateManual_DoesNotClaimForEcommerce: ecommerce tasks have no topic
// semantics, so the pool must not be consumed even with an empty prompt.
func TestCreateManual_DoesNotClaimForEcommerce(t *testing.T) {
	svc, topicSvc := setupTaskServiceWithTopicPool(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, svc.repo, userID, model.PlatformEcommerce)

	const seeded = "电商项目不该消费的选题"
	if _, err := topicSvc.Add(ctx, userID, projectID, []string{seeded}); err != nil {
		t.Fatalf("seed topic: %v", err)
	}

	tasks, err := svc.CreateManual(ctx, CreateManualParams{
		UserID:    userID,
		ProjectID: projectID,
		Quantity:  1,
		// Prompt empty, platform=ecommerce → must NOT claim.
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Prompt != "" {
		t.Fatalf("ecommerce task.Prompt = %q, want empty (no claim)", tasks[0].Prompt)
	}
	used, _, err := topicSvc.List(ctx, userID, projectID, model.TopicStatusUsed, 0, 10)
	if err != nil {
		t.Fatalf("list used topics: %v", err)
	}
	if len(used) != 0 {
		t.Fatalf("ecommerce consumed topic pool (%d used); want 0", len(used))
	}
}

// TestCreateManual_ClaimsOnePerTask: with quantity greater than the pool size,
// each task claims its own topic FIFO; once the pool is exhausted, remaining
// tasks get an empty prompt (agent auto-research) without error.
func TestCreateManual_ClaimsOnePerTask(t *testing.T) {
	svc, topicSvc := setupTaskServiceWithTopicPool(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, svc.repo, userID, model.PlatformArticle)

	const t1, t2 = "选题一：慢炖锅选购指南", "选题二：通勤背包横评"
	if _, err := topicSvc.Add(ctx, userID, projectID, []string{t1, t2}); err != nil {
		t.Fatalf("seed topics: %v", err)
	}

	tasks, err := svc.CreateManual(ctx, CreateManualParams{
		UserID:    userID,
		ProjectID: projectID,
		Quantity:  3,
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}

	// Exactly 2 tasks claim the 2 seeded topics (set-equal, FIFO order not
	// asserted to avoid created_at tie flakes); the 3rd must be empty since the
	// pool is exhausted.
	claimed := map[string]bool{}
	empty := 0
	for _, tk := range tasks {
		switch tk.Prompt {
		case "":
			empty++
		default:
			claimed[tk.Prompt] = true
		}
	}
	if empty != 1 {
		t.Fatalf("expected 1 task with empty prompt, got %d", empty)
	}
	if len(claimed) != 2 || !claimed[t1] || !claimed[t2] {
		t.Fatalf("claimed topics = %v, want {%q, %q}", claimed, t1, t2)
	}

	used, _, err := topicSvc.List(ctx, userID, projectID, model.TopicStatusUsed, 0, 10)
	if err != nil {
		t.Fatalf("list used topics: %v", err)
	}
	if len(used) != 2 {
		t.Fatalf("expected 2 used topics, got %d", len(used))
	}
}
