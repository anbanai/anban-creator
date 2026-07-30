package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func setupViralAnalysisHistoryRepo(t *testing.T) repository.Repository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ViralAnalysis{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	return repository.New(db)
}

func TestViralAnalysisHistoryGetByIDEnforcesOwnership(t *testing.T) {
	repo := setupViralAnalysisHistoryRepo(t)
	analysis := &model.ViralAnalysis{ID: uuid.NewString(), UserID: uuid.NewString(), SourceType: model.ViralAnalysisSourceNote, SourceURL: "https://example.com/note/1", Status: model.ViralAnalysisStatusCompleted, BillingQuoteID: uuid.NewString(), BillingCatalogID: "legacy", BillingSKUID: "legacy", BillingChargeID: stringPointer(uuid.NewString())}
	if err := repo.ViralAnalyses().Create(context.Background(), analysis); err != nil {
		t.Fatal(err)
	}
	svc := NewViralAnalysisHistoryService(repo)
	if got, err := svc.GetByID(context.Background(), analysis.ID, analysis.UserID); err != nil || got.ID != analysis.ID {
		t.Fatalf("owned history = %#v, %v", got, err)
	}
	if got, err := svc.GetByID(context.Background(), analysis.ID, uuid.NewString()); err == nil || got != nil {
		t.Fatalf("foreign history = %#v, %v; want ownership failure", got, err)
	}
}

func TestViralAnalysisHistoryListIsUserScoped(t *testing.T) {
	repo := setupViralAnalysisHistoryRepo(t)
	userID := uuid.NewString()
	for _, owner := range []string{userID, userID, uuid.NewString()} {
		chargeID := uuid.NewString()
		analysis := &model.ViralAnalysis{ID: uuid.NewString(), UserID: owner, SourceType: model.ViralAnalysisSourceNote, SourceURL: "https://example.com/note", Status: model.ViralAnalysisStatusCompleted, BillingQuoteID: uuid.NewString(), BillingCatalogID: "legacy", BillingSKUID: "legacy", BillingChargeID: &chargeID}
		if err := repo.ViralAnalyses().Create(context.Background(), analysis); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewViralAnalysisHistoryService(repo)
	items, total, err := svc.ListByUserID(context.Background(), userID, 0, 20)
	if err != nil || total != 2 || len(items) != 2 {
		t.Fatalf("history list = %#v total=%d err=%v", items, total, err)
	}
}
