package repository

import (
	"context"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
)

func TestPendingUploadRepositoryClaimsFinalizationAndExpirationAtomically(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)

	for _, upload := range []*model.PendingUpload{
		{ID: "fresh", UserID: "user-1", Purpose: "attachment", Key: "uploads/pending/user-1/fresh/a.png", PublicURL: "https://cdn/fresh", Status: model.PendingUploadStatusPending, ExpiresAt: now.Add(time.Hour)},
		{ID: "expired", UserID: "user-1", Purpose: "attachment", Key: "uploads/pending/user-1/expired/a.png", PublicURL: "https://cdn/expired", Status: model.PendingUploadStatusPending, ExpiresAt: now},
		{ID: "finalized", UserID: "user-1", Purpose: "attachment", Key: "uploads/pending/user-1/finalized/a.png", PublicURL: "https://cdn/finalized", Status: model.PendingUploadStatusFinalized, ExpiresAt: now.Add(time.Hour)},
	} {
		if err := repo.PendingUploads().CreatePendingUpload(ctx, upload); err != nil {
			t.Fatalf("create %s: %v", upload.ID, err)
		}
	}

	claimed, err := repo.PendingUploads().FinalizePendingUploads(ctx, []string{"fresh", "expired", "finalized"}, now)
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if claimed != 1 {
		t.Fatalf("finalized claims = %d, want 1", claimed)
	}

	expirationClaimed, err := repo.PendingUploads().ClaimPendingUploadExpiration(ctx, "expired", now)
	if err != nil {
		t.Fatalf("claim expiration: %v", err)
	}
	if !expirationClaimed {
		t.Fatal("expired pending upload was not claimed")
	}
	if claimed, err := repo.PendingUploads().FinalizePendingUploads(ctx, []string{"expired"}, now); err != nil || claimed != 0 {
		t.Fatalf("finalize expiration winner = %d, %v; want 0, nil", claimed, err)
	}
	if claimed, err := repo.PendingUploads().ClaimPendingUploadExpiration(ctx, "fresh", now); err != nil || claimed {
		t.Fatalf("expiration claimed finalized row = %v, %v", claimed, err)
	}

	reopened, err := repo.PendingUploads().ReopenPendingUploadExpiration(ctx, "expired")
	if err != nil {
		t.Fatalf("reopen expiration: %v", err)
	}
	if !reopened {
		t.Fatal("expired cleanup claim was not reopened")
	}
	found, err := repo.PendingUploads().FindPendingUploadByID(ctx, "expired")
	if err != nil {
		t.Fatalf("find reopened upload: %v", err)
	}
	if found.Status != model.PendingUploadStatusPending || found.ExpiredAt != nil {
		t.Fatalf("reopened upload = %#v", found)
	}
}
