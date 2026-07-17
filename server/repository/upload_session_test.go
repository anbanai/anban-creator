package repository

import (
	"context"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
)

func TestUploadSessionFinalizationClaimCAS(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)

	session := &model.UploadSession{
		ID:          "session-1",
		UserID:      "user-1",
		Purpose:     "project_reference",
		StagingKey:  "uploads/staging/user-1/session-1/reference.png",
		FileName:    "reference.png",
		ContentType: "image/png",
		Size:        1024,
		Status:      model.UploadSessionPending,
		ExpiresAt:   now.Add(time.Hour),
	}
	if err := repo.UploadSessions().Create(ctx, session); err != nil {
		t.Fatalf("Create: %v", err)
	}

	claimed, err := repo.UploadSessions().ClaimFinalization(ctx, session.ID, "claim-1", now, now.Add(-5*time.Minute))
	if err != nil {
		t.Fatalf("first ClaimFinalization: %v", err)
	}
	if !claimed {
		t.Fatal("first ClaimFinalization did not claim the pending session")
	}

	claimed, err = repo.UploadSessions().ClaimFinalization(ctx, session.ID, "claim-2", now, now.Add(-5*time.Minute))
	if err != nil {
		t.Fatalf("second ClaimFinalization: %v", err)
	}
	if claimed {
		t.Fatal("second ClaimFinalization claimed a session with a fresh lease")
	}
}

func TestUploadSessionFinalizationLeaseCAS(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 17, 11, 0, 0, 0, time.UTC)
	staleBefore := now.Add(-5 * time.Minute)

	expired := uploadSessionFixture("expired", now)
	expired.ExpiresAt = now
	if err := repo.UploadSessions().Create(ctx, expired); err != nil {
		t.Fatalf("create expired session: %v", err)
	}
	claimed, err := repo.UploadSessions().ClaimFinalization(ctx, expired.ID, "claim-expired", now, staleBefore)
	if err != nil {
		t.Fatalf("claim expired session: %v", err)
	}
	if claimed {
		t.Fatal("session expiring exactly at claim time was claimed for finalization")
	}

	stale := uploadSessionFixture("stale-finalization", now)
	stale.Status = model.UploadSessionFinalizing
	stale.FinalizationToken = "claim-old"
	stale.FinalizationClaimedAt = &staleBefore
	if err := repo.UploadSessions().Create(ctx, stale); err != nil {
		t.Fatalf("create stale finalizing session: %v", err)
	}
	claimed, err = repo.UploadSessions().ClaimFinalization(ctx, stale.ID, "claim-new", now, staleBefore)
	if err != nil {
		t.Fatalf("reclaim stale finalization: %v", err)
	}
	if !claimed {
		t.Fatal("finalization lease exactly at stale boundary was not reclaimed")
	}
	if completed, err := repo.UploadSessions().CompleteFinalization(ctx, stale.ID, "claim-old", "asset-1", now); err != nil || completed {
		t.Fatalf("old finalization token completed after takeover: completed=%v err=%v", completed, err)
	}
	if released, err := repo.UploadSessions().ReleaseFinalization(ctx, stale.ID, "claim-old"); err != nil || released {
		t.Fatalf("old finalization token released after takeover: released=%v err=%v", released, err)
	}
}

func TestUploadSessionCleanupClaimsExpiredStaleFinalizer(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	staleBefore := now.Add(-5 * time.Minute)

	session := uploadSessionFixture("stale-expired-finalization", now)
	session.Status = model.UploadSessionFinalizing
	session.ExpiresAt = now
	session.FinalizationToken = "finalization-old"
	session.FinalizationClaimedAt = &staleBefore
	if err := repo.UploadSessions().Create(ctx, session); err != nil {
		t.Fatalf("create stale expired finalizing session: %v", err)
	}

	candidates, err := repo.UploadSessions().FindForCleanup(ctx, now, staleBefore, 10)
	if err != nil {
		t.Fatalf("FindForCleanup: %v", err)
	}
	if len(candidates) != 1 || candidates[0].ID != session.ID {
		t.Fatalf("cleanup candidates = %+v, want session %q", candidates, session.ID)
	}

	claimed, err := repo.UploadSessions().ClaimExpiration(ctx, session.ID, "cleanup-new", now, staleBefore)
	if err != nil {
		t.Fatalf("ClaimExpiration: %v", err)
	}
	if !claimed {
		t.Fatal("expired stale finalizing session was not claimed for cleanup")
	}

	found, err := repo.UploadSessions().FindByID(ctx, session.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found.Status != model.UploadSessionExpiring || found.CleanupClaimID != "cleanup-new" || found.CleanupClaimedAt == nil {
		t.Fatalf("cleanup claim state = %+v", found)
	}
	if found.FinalizationToken != "" || found.FinalizationClaimedAt != nil {
		t.Fatalf("cleanup takeover retained finalization lease: token=%q claimed_at=%v", found.FinalizationToken, found.FinalizationClaimedAt)
	}
	if completed, err := repo.UploadSessions().CompleteFinalization(ctx, session.ID, "finalization-old", "asset-1", now); err != nil || completed {
		t.Fatalf("old finalization token completed after cleanup takeover: completed=%v err=%v", completed, err)
	}
	if released, err := repo.UploadSessions().ReleaseFinalization(ctx, session.ID, "finalization-old"); err != nil || released {
		t.Fatalf("old finalization token released after cleanup takeover: released=%v err=%v", released, err)
	}
}

func TestUploadSessionCleanupReclaimsStaleLeaseAtBoundary(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 17, 13, 0, 0, 0, time.UTC)
	staleBefore := now.Add(-5 * time.Minute)

	session := uploadSessionFixture("stale-cleanup", now)
	session.Status = model.UploadSessionExpiring
	session.ExpiresAt = now.Add(-time.Hour)
	session.CleanupClaimID = "cleanup-old"
	session.CleanupClaimedAt = &staleBefore
	if err := repo.UploadSessions().Create(ctx, session); err != nil {
		t.Fatalf("create stale expiring session: %v", err)
	}

	claimed, err := repo.UploadSessions().ClaimExpiration(ctx, session.ID, "cleanup-new", now, staleBefore)
	if err != nil {
		t.Fatalf("reclaim stale expiration: %v", err)
	}
	if !claimed {
		t.Fatal("cleanup lease exactly at stale boundary was not reclaimed")
	}
	if completed, err := repo.UploadSessions().CompleteExpiration(ctx, session.ID, "cleanup-old", now); err != nil || completed {
		t.Fatalf("old cleanup claim completed after takeover: completed=%v err=%v", completed, err)
	}
	if reopened, err := repo.UploadSessions().ReopenExpiration(ctx, session.ID, "cleanup-old"); err != nil || reopened {
		t.Fatalf("old cleanup claim reopened after takeover: reopened=%v err=%v", reopened, err)
	}
}

func uploadSessionFixture(id string, now time.Time) *model.UploadSession {
	return &model.UploadSession{
		ID:          id,
		UserID:      "user-1",
		Purpose:     "project_reference",
		StagingKey:  "uploads/staging/user-1/" + id + "/reference.png",
		FileName:    "reference.png",
		ContentType: "image/png",
		Size:        1024,
		Status:      model.UploadSessionPending,
		ExpiresAt:   now.Add(time.Hour),
	}
}
