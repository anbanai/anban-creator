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
