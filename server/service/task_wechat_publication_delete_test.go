package service

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type wechatDeleteFixture struct {
	db             *gorm.DB
	repo           repository.Repository
	taskService    *TaskService
	projectService *ProjectService
	userID         string
	projectID      string
	taskID         string
	publicationID  string
	trackingID     string
}

func newWechatDeleteFixture(t *testing.T, status string) *wechatDeleteFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	t.Cleanup(func() { _ = repo.Close() })
	logger := zerolog.New(io.Discard)
	f := &wechatDeleteFixture{
		db: db, repo: repo,
		taskService:    newTestTaskService(repo, &mockEnqueuer{}, nil, &logger, "", nil, nil),
		projectService: NewProjectService(repo, &logger),
		userID:         uuid.NewString(), projectID: uuid.NewString(), taskID: uuid.NewString(),
		publicationID: uuid.NewString(), trackingID: uuid.NewString(),
	}
	ctx := context.Background()
	if err := repo.Users().Create(ctx, &model.User{ID: f.userID, Email: f.userID + "@delete.test", Password: "x", InviteCode: f.userID}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: f.projectID, UserID: f.userID, Name: "WeChat", Platform: model.PlatformArticle, Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{ID: f.taskID, UserID: f.userID, ProjectID: f.projectID, Type: model.PlatformArticle, Status: model.TaskStatusCompleted}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	publication := &model.WechatPublication{ID: f.publicationID, TaskID: f.taskID, UserID: f.userID, ProjectID: f.projectID, Source: model.WechatPublicationSourceAnbanAPI, Status: status}
	if status == model.WechatPublicationStatusPublished {
		publication.ArticleID, publication.ArticleURL, publication.PublishedAt = "article-1", "https://mp.weixin.qq.com/s/article-1", &now
	}
	if err := repo.WechatPublications().Create(ctx, publication); err != nil {
		t.Fatal(err)
	}
	if status == model.WechatPublicationStatusPublished {
		if err := db.Create(&model.WechatPublicationBinding{ProjectID: f.projectID, ArticleID: "article-1", PublicationID: f.publicationID}).Error; err != nil {
			t.Fatal(err)
		}
		if err := repo.WechatTrackings().Create(ctx, &model.WechatArticleTracking{ID: f.trackingID, TaskID: f.taskID, UserID: f.userID, ProjectID: f.projectID, PublicationID: f.publicationID, Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatTrackingStatusTracking, ArticleID: "article-1", PublishedAt: now, ExpiresAt: now.Add(model.WechatTrackingWindow)}); err != nil {
			t.Fatal(err)
		}
		if err := repo.WechatMetricSnapshots().Create(ctx, &model.WechatMetricSnapshot{ID: uuid.NewString(), TrackingID: f.trackingID, TaskID: f.taskID, StatDate: "2026-08-31", CapturedAt: now, RawResponse: []byte(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f *wechatDeleteFixture) lifecycleCounts(t *testing.T) (snapshots, trackings, bindings, publications int64) {
	t.Helper()
	for table, count := range map[any]*int64{
		&model.WechatMetricSnapshot{}:     &snapshots,
		&model.WechatArticleTracking{}:    &trackings,
		&model.WechatPublicationBinding{}: &bindings,
		&model.WechatPublication{}:        &publications,
	} {
		if err := f.db.Model(table).Count(count).Error; err != nil {
			t.Fatal(err)
		}
	}
	return
}

func TestTaskDeleteRemovesWechatLifecycleBeforeProjectDelete(t *testing.T) {
	for _, status := range []string{model.WechatPublicationStatusDrafting, model.WechatPublicationStatusPublished} {
		t.Run(status, func(t *testing.T) {
			f := newWechatDeleteFixture(t, status)
			if err := f.taskService.Delete(context.Background(), f.taskID); err != nil {
				t.Fatal(err)
			}
			if snapshots, trackings, bindings, publications := f.lifecycleCounts(t); snapshots != 0 || trackings != 0 || bindings != 0 || publications != 0 {
				t.Fatalf("orphan lifecycle rows: snapshots=%d trackings=%d bindings=%d publications=%d", snapshots, trackings, bindings, publications)
			}
			if _, err := f.repo.Tasks().FindByID(context.Background(), f.taskID); !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("task still exists: %v", err)
			}
			if err := f.projectService.Delete(context.Background(), f.userID, f.projectID); err != nil {
				t.Fatalf("delete project after task lifecycle cleanup: %v", err)
			}
		})
	}
}

func TestTaskDeleteRollsBackWechatCleanupWhenTaskRowDeleteFails(t *testing.T) {
	f := newWechatDeleteFixture(t, model.WechatPublicationStatusPublished)
	trigger := `CREATE TRIGGER fail_selected_task_delete BEFORE DELETE ON tasks
		WHEN OLD.id = '` + f.taskID + `'
		BEGIN SELECT RAISE(FAIL, 'forced task delete failure'); END;`
	if err := f.db.Exec(trigger).Error; err != nil {
		t.Fatal(err)
	}

	if err := f.taskService.Delete(context.Background(), f.taskID); err == nil {
		t.Fatal("Delete error = nil, want forced final-row failure")
	}
	if snapshots, trackings, bindings, publications := f.lifecycleCounts(t); snapshots != 1 || trackings != 1 || bindings != 1 || publications != 1 {
		t.Fatalf("cleanup escaped failed transaction: snapshots=%d trackings=%d bindings=%d publications=%d", snapshots, trackings, bindings, publications)
	}
}

func TestDeletedWechatLifecycleMakesQueuedRecoveryWorkANoop(t *testing.T) {
	f := newWechatDeleteFixture(t, model.WechatPublicationStatusPublished)
	providerCalls := 0
	logger := zerolog.New(io.Discard)
	publicationService := NewWechatPublicationService(f.repo, func(*model.Project) (WechatPublicationAPI, error) {
		providerCalls++
		return nil, errors.New("provider must not be called for deleted lifecycle work")
	}, &logger)

	if err := f.taskService.Delete(context.Background(), f.taskID); err != nil {
		t.Fatal(err)
	}
	if err := publicationService.ProcessPoll(context.Background(), f.publicationID); err != nil {
		t.Fatalf("orphan poll work returned error: %v", err)
	}
	if err := f.projectService.Delete(context.Background(), f.userID, f.projectID); err != nil {
		t.Fatal(err)
	}
	if err := publicationService.ReconcileProject(context.Background(), f.projectID); err != nil {
		t.Fatalf("orphan project reconciliation returned error: %v", err)
	}
	if providerCalls != 0 {
		t.Fatalf("deleted lifecycle reached provider %d times", providerCalls)
	}
}
