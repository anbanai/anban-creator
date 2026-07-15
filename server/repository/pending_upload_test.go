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

	expirationClaimed, err := repo.PendingUploads().ClaimPendingUploadExpiration(ctx, "expired", "claim-1", now, now.Add(-5*time.Minute))
	if err != nil {
		t.Fatalf("claim expiration: %v", err)
	}
	if !expirationClaimed {
		t.Fatal("expired pending upload was not claimed")
	}
	if claimed, err := repo.PendingUploads().FinalizePendingUploads(ctx, []string{"expired"}, now); err != nil || claimed != 0 {
		t.Fatalf("finalize expiration winner = %d, %v; want 0, nil", claimed, err)
	}
	if claimed, err := repo.PendingUploads().ClaimPendingUploadExpiration(ctx, "fresh", "claim-2", now, now.Add(-5*time.Minute)); err != nil || claimed {
		t.Fatalf("expiration claimed finalized row = %v, %v", claimed, err)
	}

	reopened, err := repo.PendingUploads().ReopenPendingUploadExpiration(ctx, "expired", "claim-1")
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

func TestPendingUploadRepositoryFinalizesVerifiedSetAllOrNoneAndReusesFinalized(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 11, 0, 0, 0, time.UTC)
	for _, id := range []string{"first", "second"} {
		if err := repo.PendingUploads().CreatePendingUpload(ctx, &model.PendingUpload{
			ID: id, UserID: "user-1", Purpose: "ai_entry_attachment",
			Key: "uploads/pending/user-1/" + id + "/input.png", PublicURL: "https://cdn/" + id,
			Status: model.PendingUploadStatusPending, ExpiresAt: now.Add(time.Hour),
		}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}

	claims := []model.PendingUploadClaim{
		{UploadID: "first", UserID: "user-1", Key: "uploads/pending/user-1/first/input.png", FinalizedKey: "uploads/finalized/user-1/first/input.png", AllowedPurposes: []string{"ai_entry_attachment"}},
		{UploadID: "second", UserID: "user-1", Key: "uploads/pending/user-1/second/forged.png", FinalizedKey: "uploads/finalized/user-1/second/input.png", AllowedPurposes: []string{"ai_entry_attachment"}},
	}
	if err := repo.PendingUploads().FinalizePendingUploadClaims(ctx, claims, now); err == nil {
		t.Fatal("partially mismatched verified set finalized")
	}
	for _, id := range []string{"first", "second"} {
		found, err := repo.PendingUploads().FindPendingUploadByID(ctx, id)
		if err != nil {
			t.Fatalf("find %s: %v", id, err)
		}
		if found.Status != model.PendingUploadStatusPending {
			t.Fatalf("%s status = %q, want transaction rollback to pending", id, found.Status)
		}
	}

	claims[1].Key = "uploads/pending/user-1/second/input.png"
	if err := repo.PendingUploads().FinalizePendingUploadClaims(ctx, claims, now); err != nil {
		t.Fatalf("finalize exact set: %v", err)
	}
	if err := repo.PendingUploads().FinalizePendingUploadClaims(ctx, claims, now.Add(time.Minute)); err != nil {
		t.Fatalf("reuse exact finalized set: %v", err)
	}
	for _, claim := range claims {
		found, err := repo.PendingUploads().FindPendingUploadByID(ctx, claim.UploadID)
		if err != nil {
			t.Fatalf("find finalized %s: %v", claim.UploadID, err)
		}
		if found.Key != claim.Key || found.FinalizedKey != claim.FinalizedKey || found.Status != model.PendingUploadStatusFinalized {
			t.Fatalf("finalized identity = %#v, want source=%q final=%q", found, claim.Key, claim.FinalizedKey)
		}
	}
	claims[1].FinalizedKey = "uploads/finalized/user-1/second/other.png"
	if err := repo.PendingUploads().FinalizePendingUploadClaims(ctx, claims, now.Add(2*time.Minute)); err == nil {
		t.Fatal("reused finalized row with a different final key")
	}
}

func TestPendingUploadRepositoryRejectsLegacyFinalizedRowWithoutFinalKey(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 11, 30, 0, 0, time.UTC)
	upload := &model.PendingUpload{
		ID: "legacy", UserID: "user-1", Purpose: "ai_entry_attachment",
		Key: "uploads/pending/user-1/legacy/input.png", PublicURL: "https://cdn/legacy",
		Status: model.PendingUploadStatusFinalized, ExpiresAt: now.Add(time.Hour),
	}
	if err := repo.PendingUploads().CreatePendingUpload(ctx, upload); err != nil {
		t.Fatalf("create legacy row: %v", err)
	}
	claim := model.PendingUploadClaim{
		UploadID: upload.ID, UserID: upload.UserID, Key: upload.Key,
		FinalizedKey: "uploads/finalized/user-1/legacy/input.png", AllowedPurposes: []string{upload.Purpose},
	}
	if err := repo.PendingUploads().FinalizePendingUploadClaims(ctx, []model.PendingUploadClaim{claim}, now); err == nil {
		t.Fatal("legacy finalized row without FinalizedKey was reused")
	}
}

func TestPendingUploadRepositoryReclaimsAbandonedCleanupLease(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	oldClaim := now.Add(-10 * time.Minute)
	upload := &model.PendingUpload{
		ID: "abandoned", UserID: "user-1", Purpose: "ai_entry_attachment",
		Key: "uploads/pending/user-1/abandoned/input.png", PublicURL: "https://cdn/abandoned",
		Status: model.PendingUploadStatusExpiring, ExpiresAt: now.Add(-time.Hour), CleanupClaimID: "old-claim", CleanupClaimedAt: &oldClaim,
	}
	if err := repo.PendingUploads().CreatePendingUpload(ctx, upload); err != nil {
		t.Fatalf("create abandoned upload: %v", err)
	}

	candidates, err := repo.PendingUploads().FindPendingUploadsForCleanup(ctx, now, now.Add(-5*time.Minute), 10)
	if err != nil {
		t.Fatalf("find cleanup candidates: %v", err)
	}
	if len(candidates) != 1 || candidates[0].ID != upload.ID {
		t.Fatalf("cleanup candidates = %#v", candidates)
	}
	claimed, err := repo.PendingUploads().ClaimPendingUploadExpiration(ctx, upload.ID, "new-claim", now, now.Add(-5*time.Minute))
	if err != nil || !claimed {
		t.Fatalf("reclaim abandoned lease = %v, %v", claimed, err)
	}
	if completed, err := repo.PendingUploads().CompletePendingUploadExpiration(ctx, upload.ID, "old-claim", now); err != nil || completed {
		t.Fatalf("old lease completed reclaimed upload = %v, %v", completed, err)
	}
	claim := model.PendingUploadClaim{UploadID: upload.ID, UserID: upload.UserID, Key: upload.Key, FinalizedKey: "uploads/finalized/user-1/abandoned/input.png", AllowedPurposes: []string{upload.Purpose}}
	if err := repo.PendingUploads().FinalizePendingUploadClaims(ctx, []model.PendingUploadClaim{claim}, now); err == nil {
		t.Fatal("finalization won while cleanup lease was active")
	}
	completed, err := repo.PendingUploads().CompletePendingUploadExpiration(ctx, upload.ID, "new-claim", now)
	if err != nil || !completed {
		t.Fatalf("complete active cleanup lease = %v, %v", completed, err)
	}
	found, err := repo.PendingUploads().FindPendingUploadByID(ctx, upload.ID)
	if err != nil {
		t.Fatalf("find completed upload: %v", err)
	}
	if found.Status != model.PendingUploadStatusExpired || found.CleanupClaimID != "" || found.CleanupClaimedAt != nil || found.ExpiredAt == nil {
		t.Fatalf("completed upload = %#v", found)
	}
}
