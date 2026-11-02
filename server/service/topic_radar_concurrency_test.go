package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestTopicRadarConcurrentManualRunsReturnOneActiveDigest(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared&_busy_timeout=10000"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	subscription := seedTopicRadarSubscription(t, repo)
	service := NewTopicRadarService(repo, nil, time.Now)

	const runs = 4
	results := make([]*model.TopicDigest, runs)
	errs := make([]error, runs)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < runs; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = service.Run(context.Background(), RunTopicDigestParams{
				UserID: subscription.UserID, SubscriptionID: subscription.ID, RequestKey: fmt.Sprintf("manual-%d", i),
			})
		}(i)
	}
	close(start)
	wg.Wait()

	var activeID string
	for i, err := range errs {
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if results[i] == nil {
			t.Fatalf("run %d returned no digest", i)
		}
		if activeID == "" {
			activeID = results[i].ID
		} else if results[i].ID != activeID {
			t.Fatalf("run %d returned %q, want shared active digest %q", i, results[i].ID, activeID)
		}
	}
	digests, err := repo.TopicDigests().ListBySubscription(context.Background(), subscription.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(digests) != 1 {
		t.Fatalf("digests = %d, want single active digest", len(digests))
	}
}
