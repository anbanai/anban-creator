package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
)

func TestWechatProjectReconcileLeaseHasOneConcurrentWinnerAndExpires(t *testing.T) {
	db := setupTestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	repos := []Repository{New(db), New(db)}
	start := make(chan struct{})
	results := make(chan bool, len(repos))
	errs := make(chan error, len(repos))
	var wg sync.WaitGroup
	for _, repo := range repos {
		wg.Add(1)
		go func(repo Repository) {
			defer wg.Done()
			<-start
			won, err := repo.WechatPublications().ClaimProjectReconcile(context.Background(), "project-1", time.Minute)
			results <- won
			errs <- err
		}(repo)
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	winners := 0
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for won := range results {
		if won {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("lease winners=%d, want 1", winners)
	}
	if err := db.Model(&model.WechatProjectReconcileLease{}).Where("project_id = ?", "project-1").Update("lease_until_micros", int64(0)).Error; err != nil {
		t.Fatal(err)
	}
	if won, err := repos[0].WechatPublications().ClaimProjectReconcile(context.Background(), "project-1", time.Minute); err != nil || !won {
		t.Fatalf("expired lease claim=%v, %v", won, err)
	}
}

func TestWechatArticleBindingHasOneConcurrentWinnerPerProject(t *testing.T) {
	db := setupTestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	repoA, repoB := New(db), New(db)
	publications := []*model.WechatPublication{
		{ID: uuid.NewString(), TaskID: uuid.NewString(), UserID: "user-1", ProjectID: "project-1", Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusDrafted},
		{ID: uuid.NewString(), TaskID: uuid.NewString(), UserID: "user-1", ProjectID: "project-1", Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusDrafted},
		{ID: uuid.NewString(), TaskID: uuid.NewString(), UserID: "user-1", ProjectID: "project-2", Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusDrafted},
	}
	for _, publication := range publications {
		if err := repoA.WechatPublications().Create(context.Background(), publication); err != nil {
			t.Fatal(err)
		}
	}

	start := make(chan struct{})
	results := make(chan bool, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for index, repo := range []Repository{repoA, repoB} {
		wg.Add(1)
		go func(repo Repository, publicationID string) {
			defer wg.Done()
			<-start
			won, err := repo.WechatPublications().ClaimArticleBinding(context.Background(), "project-1", "article-1", publicationID)
			results <- won
			errs <- err
		}(repo, publications[index].ID)
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	winners := 0
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for won := range results {
		if won {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("binding winners=%d, want 1", winners)
	}
	bound, err := repoA.WechatPublications().FindByArticleID(context.Background(), "project-1", "article-1")
	if err != nil || (bound.ID != publications[0].ID && bound.ID != publications[1].ID) {
		t.Fatalf("bound publication=%#v, %v", bound, err)
	}
	if won, err := repoA.WechatPublications().ClaimArticleBinding(context.Background(), "project-2", "article-1", publications[2].ID); err != nil || !won {
		t.Fatalf("cross-project binding=%v, %v", won, err)
	}
}
