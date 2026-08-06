package repository

import (
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestProjectStatsIncludeUnusedTopics(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	ctx := t.Context()

	projects := []*model.Project{
		{ID: "project-stats-1", UserID: "user-1", Platform: model.PlatformArticle, Name: "one", Status: model.ProjectStatusActive},
		{ID: "project-stats-2", UserID: "user-1", Platform: model.PlatformArticle, Name: "two", Status: model.ProjectStatusActive},
		{ID: "project-stats-3", UserID: "user-1", Platform: model.PlatformArticle, Name: "three", Status: model.ProjectStatusActive},
	}
	for _, project := range projects {
		if err := repo.Projects().Create(ctx, project); err != nil {
			t.Fatalf("create project %s: %v", project.ID, err)
		}
	}

	for _, task := range []*model.Task{
		{ID: "task-stats-1", UserID: "user-1", ProjectID: projects[0].ID, Type: model.PlatformArticle, Status: model.TaskStatusCompleted},
		{ID: "task-stats-2", UserID: "user-1", ProjectID: projects[0].ID, Type: model.PlatformArticle, Status: model.TaskStatusPending},
	} {
		if err := repo.Tasks().Create(ctx, task); err != nil {
			t.Fatalf("create task %s: %v", task.ID, err)
		}
	}

	usedTaskID := "task-stats-1"
	if err := repo.TopicPools().CreateBatch(ctx, []*model.TopicPool{
		{UserID: "user-1", ProjectID: projects[0].ID, Topic: "unused one", Status: model.TopicStatusUnused},
		{UserID: "user-1", ProjectID: projects[0].ID, Topic: "unused two", Status: model.TopicStatusUnused},
		{UserID: "user-1", ProjectID: projects[0].ID, Topic: "used", Status: model.TopicStatusUsed, TaskID: &usedTaskID},
		{UserID: "user-1", ProjectID: projects[1].ID, Topic: "unused other", Status: model.TopicStatusUnused},
	}); err != nil {
		t.Fatalf("create topics: %v", err)
	}

	assertStats := func(t *testing.T, stats *ProjectStats, wantTasks, wantUnused int64) {
		t.Helper()
		if stats.TotalTasks != wantTasks {
			t.Fatalf("total_tasks = %d, want %d", stats.TotalTasks, wantTasks)
		}
		if stats.UnusedTopics != wantUnused {
			t.Fatalf("unused_topics = %d, want %d", stats.UnusedTopics, wantUnused)
		}
	}

	single, err := repo.Projects().GetStats(ctx, projects[0].ID)
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}
	assertStats(t, single, 2, 2)

	batch, err := repo.Projects().GetStatsByProjectIDs(ctx, []string{projects[0].ID, projects[1].ID, projects[2].ID})
	if err != nil {
		t.Fatalf("get batch stats: %v", err)
	}
	assertStats(t, batch[projects[0].ID], 2, 2)
	assertStats(t, batch[projects[1].ID], 0, 1)
	assertStats(t, batch[projects[2].ID], 0, 0)
}

func TestProjectUpdateIfReferenceImageAssetID(t *testing.T) {
	repo := New(setupTestDB(t))
	project := &model.Project{
		ID: "project-cas", UserID: "user-1", Platform: model.PlatformArticle,
		Name: "before", ReferenceImageAssetID: "asset-a", Status: model.ProjectStatusActive,
	}
	if err := repo.Projects().Create(t.Context(), project); err != nil {
		t.Fatal(err)
	}

	matched := *project
	matched.Name = "matched"
	won, err := repo.Projects().UpdateIfReferenceImageAssetID(t.Context(), &matched, "asset-a")
	if err != nil {
		t.Fatal(err)
	}
	if !won {
		t.Fatal("matching reference CAS did not update")
	}
	got, err := repo.Projects().FindByID(t.Context(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "matched" || got.ReferenceImageAssetID != "asset-a" {
		t.Fatalf("matched project = name %q reference %q", got.Name, got.ReferenceImageAssetID)
	}

	mismatch := *got
	mismatch.Name = "must-not-write"
	mismatch.ReferenceImageAssetID = "asset-stale"
	won, err = repo.Projects().UpdateIfReferenceImageAssetID(t.Context(), &mismatch, "asset-b")
	if err != nil {
		t.Fatal(err)
	}
	if won {
		t.Fatal("mismatching reference CAS updated")
	}
	got, err = repo.Projects().FindByID(t.Context(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "matched" || got.ReferenceImageAssetID != "asset-a" {
		t.Fatalf("mismatch modified project = name %q reference %q", got.Name, got.ReferenceImageAssetID)
	}
}

func TestProjectUpdateIfReferenceImageAssetIDMatchesEmptyAndNull(t *testing.T) {
	for _, tt := range []struct {
		name    string
		id      string
		setNull bool
	}{
		{name: "empty string", id: "project-empty"},
		{name: "null", id: "project-null", setNull: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db := setupTestDB(t)
			repo := New(db)
			project := &model.Project{
				ID: tt.id, UserID: "user-1", Platform: model.PlatformArticle,
				Name: "before", Status: model.ProjectStatusActive,
			}
			if err := repo.Projects().Create(t.Context(), project); err != nil {
				t.Fatal(err)
			}
			if tt.setNull {
				if err := db.Exec("UPDATE projects SET reference_image_asset_id = NULL WHERE id = ?", project.ID).Error; err != nil {
					t.Fatal(err)
				}
			}
			loaded, err := repo.Projects().FindByID(t.Context(), project.ID)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.ReferenceImageAssetID != "" {
				t.Fatalf("loaded reference = %q, want empty", loaded.ReferenceImageAssetID)
			}
			if tt.setNull {
				wrong := *loaded
				wrong.Name = "must-not-write"
				won, err := repo.Projects().UpdateIfReferenceImageAssetID(t.Context(), &wrong, "asset-nonempty")
				if err != nil {
					t.Fatal(err)
				}
				if won {
					t.Fatal("nonempty expected reference matched null")
				}
			}
			loaded.Name = "after"
			won, err := repo.Projects().UpdateIfReferenceImageAssetID(t.Context(), loaded, "")
			if err != nil {
				t.Fatal(err)
			}
			if !won {
				t.Fatal("empty expected reference did not match")
			}
			persisted, err := repo.Projects().FindByID(t.Context(), project.ID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.Name != "after" || persisted.ReferenceImageAssetID != "" {
				t.Fatalf("persisted project name=%q reference=%q", persisted.Name, persisted.ReferenceImageAssetID)
			}
		})
	}
}
