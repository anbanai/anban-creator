package repository

import (
	"context"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAssistantSessionLeaseIsDurableAndRenewable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AssistantSession{}, &model.AssistantSessionLease{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	repo := New(db).AssistantSessions()
	now := time.Now().UTC().Truncate(time.Millisecond)
	newSession := func(id string, expiry time.Time) (*model.AssistantSession, *model.AssistantSessionLease) {
		return &model.AssistantSession{ID: id, UserID: "user-1", ModeID: model.AssistantModeCreation, ProjectID: "project-1", Status: "starting", ExpiresAt: expiry, CreatedAt: now, UpdatedAt: now},
			&model.AssistantSessionLease{Scope: "scope-1", SessionID: id, UserID: "user-1", ModeID: model.AssistantModeCreation, ProjectID: "project-1", AcquiredAt: now, ExpiresAt: expiry}
	}

	expiresAt := now.Add(time.Minute)
	first, firstLease := newSession("session-1", expiresAt)
	if won, err := repo.CreateWithLease(context.Background(), first, firstLease, now); err != nil || !won {
		t.Fatalf("CreateWithLease() = %v, %v; want true, nil", won, err)
	}
	second, secondLease := newSession("session-2", expiresAt)
	if won, err := repo.CreateWithLease(context.Background(), second, secondLease, now); err != nil || won {
		t.Fatalf("competing CreateWithLease() = %v, %v; want false, nil", won, err)
	}
	var sessions int64
	if err := db.Model(&model.AssistantSession{}).Count(&sessions).Error; err != nil || sessions != 1 {
		t.Fatalf("session rows = %d, %v; losing session must roll back", sessions, err)
	}

	newExpiry := now.Add(2 * time.Minute)
	if renewed, err := repo.RenewLease(context.Background(), "session-1", now, newExpiry); err != nil || !renewed {
		t.Fatalf("RenewLease() = %v, %v; want true, nil", renewed, err)
	}
	var lease model.AssistantSessionLease
	if err := db.First(&lease, "session_id = ?", "session-1").Error; err != nil || !lease.ExpiresAt.Equal(newExpiry) {
		t.Fatalf("renewed lease = %#v, %v", lease, err)
	}
	if err := repo.Close(context.Background(), "session-1", "closed", now); err != nil {
		t.Fatal(err)
	}
	if renewed, err := repo.RenewLease(context.Background(), "session-1", now, newExpiry); err != nil || renewed {
		t.Fatalf("renew after close = %v, %v; want false, nil", renewed, err)
	}
	third, thirdLease := newSession("session-3", expiresAt)
	if won, err := repo.CreateWithLease(context.Background(), third, thirdLease, now); err != nil || !won {
		t.Fatalf("CreateWithLease() after close = %v, %v; want true, nil", won, err)
	}
}

func TestAssistantSessionLeaseDoesNotRenewWhenSessionIsClosed(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AssistantSession{}, &model.AssistantSessionLease{}); err != nil {
		t.Fatal(err)
	}
	repo := New(db).AssistantSessions()
	now := time.Now().UTC().Truncate(time.Millisecond)
	expiresAt := now.Add(time.Minute)
	session := &model.AssistantSession{ID: "closed-session", UserID: "user-1", ModeID: model.AssistantModeCreation, Status: "closed", ExpiresAt: expiresAt, ClosedAt: &now, CreatedAt: now, UpdatedAt: now}
	lease := &model.AssistantSessionLease{Scope: "closed-scope", SessionID: session.ID, UserID: session.UserID, ModeID: session.ModeID, AcquiredAt: now, ExpiresAt: expiresAt}
	if err := db.Create(session).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(lease).Error; err != nil {
		t.Fatal(err)
	}

	newExpiry := now.Add(2 * time.Minute)
	if renewed, err := repo.RenewLease(context.Background(), session.ID, now, newExpiry); err != nil || renewed {
		t.Fatalf("RenewLease() = %v, %v; want false, nil", renewed, err)
	}
	var persisted model.AssistantSessionLease
	if err := db.First(&persisted, "session_id = ?", session.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !persisted.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("lease expiry = %s; want unchanged expiry %s", persisted.ExpiresAt, expiresAt)
	}
}

func TestAssistantSessionReapsExpiredLeaseAndMarksSession(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AssistantSession{}, &model.AssistantSessionLease{}); err != nil {
		t.Fatal(err)
	}
	repo := New(db).AssistantSessions()
	now := time.Now().UTC().Truncate(time.Millisecond)
	session := &model.AssistantSession{ID: "expired-session", UserID: "user-1", ModeID: model.AssistantModeReview, Status: "running", ExpiresAt: now.Add(time.Minute), CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute)}
	lease := &model.AssistantSessionLease{Scope: "scope-expired", SessionID: session.ID, UserID: session.UserID, ModeID: session.ModeID, AcquiredAt: session.CreatedAt, ExpiresAt: session.ExpiresAt}
	if _, err := repo.CreateWithLease(context.Background(), session, lease, now); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReapExpired(context.Background(), now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	var persisted model.AssistantSession
	if err := db.First(&persisted, "id = ?", session.ID).Error; err != nil || persisted.Status != "expired" || persisted.ClosedAt == nil {
		t.Fatalf("reaped session = %#v, %v", persisted, err)
	}
	var leases int64
	if err := db.Model(&model.AssistantSessionLease{}).Count(&leases).Error; err != nil || leases != 0 {
		t.Fatalf("remaining leases = %d, %v", leases, err)
	}
}

func TestAssistantSessionRecoveryInterruptsActiveSessions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AssistantCheckpoint{}, &model.AssistantSession{}, &model.AssistantSessionLease{}); err != nil {
		t.Fatal(err)
	}
	repo := New(db).AssistantSessions()
	now := time.Now().UTC().Truncate(time.Millisecond)

	create := func(id string) {
		t.Helper()
		expiresAt := now.Add(time.Hour)
		session := &model.AssistantSession{ID: id, UserID: "user-1", ModeID: model.AssistantModeCreation, Status: "running", ExpiresAt: expiresAt, CreatedAt: now, UpdatedAt: now}
		lease := &model.AssistantSessionLease{Scope: "scope-" + id, SessionID: id, UserID: session.UserID, ModeID: session.ModeID, AcquiredAt: now, ExpiresAt: expiresAt}
		if won, err := repo.CreateWithLease(context.Background(), session, lease, now); err != nil || !won {
			t.Fatalf("CreateWithLease(%s) = %v, %v", id, won, err)
		}
	}
	create("session-old-1")
	create("session-old-2")
	checkpoint := &model.AssistantCheckpoint{
		ID: "checkpoint-1", UserID: "user-1", ModeID: model.AssistantModeCreation,
		SchemaVersion: "1.0", Payload: []byte(`{"plan":{"goal":"write"}}`),
		Status: model.AssistantCheckpointReady, SourceSessionID: "session-old-1",
		ExpiresAt: now.Add(24 * time.Hour), CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(checkpoint).Error; err != nil {
		t.Fatal(err)
	}

	if err := repo.InterruptActive(context.Background(), now.Add(time.Second)); err != nil {
		t.Fatalf("InterruptActive() error = %v", err)
	}

	var sessions []model.AssistantSession
	if err := db.Order("id").Find(&sessions).Error; err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("session count = %d; want 2", len(sessions))
	}
	for _, session := range sessions {
		if session.Status != "interrupted" || session.ClosedAt == nil {
			t.Errorf("recovered session = %#v; want interrupted and closed", session)
		}
	}
	var leaseCount int64
	if err := db.Model(&model.AssistantSessionLease{}).Count(&leaseCount).Error; err != nil || leaseCount != 0 {
		t.Fatalf("remaining lease count = %d, %v; want no remaining leases", leaseCount, err)
	}
	var persistedCheckpoint model.AssistantCheckpoint
	if err := db.First(&persistedCheckpoint, "id = ?", checkpoint.ID).Error; err != nil || persistedCheckpoint.Status != model.AssistantCheckpointReady {
		t.Fatalf("recovery checkpoint = %#v, %v; want ready checkpoint retained", persistedCheckpoint, err)
	}
}

func TestAssistantCheckpointUpsertDoesNotReopenCommittedCheckpoint(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AssistantCheckpoint{}); err != nil {
		t.Fatal(err)
	}
	repo := New(db).AssistantCheckpoints()
	now := time.Now().UTC().Truncate(time.Millisecond)
	committed := &model.AssistantCheckpoint{
		ID: "checkpoint-committed", UserID: "user-1", ModeID: model.AssistantModeCreation,
		SchemaVersion: "1.0", Payload: []byte(`{"plan":{"goal":"confirmed"}}`), Summary: "confirmed",
		Status: model.AssistantCheckpointCommitted, SourceSessionID: "session-committed",
		ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.UpsertBySourceSession(context.Background(), committed); err != nil {
		t.Fatal(err)
	}
	stale := &model.AssistantCheckpoint{
		ID: committed.ID, UserID: committed.UserID, ModeID: committed.ModeID,
		SchemaVersion: "1.0", Payload: []byte(`{"plan":{"goal":"stale"}}`), Summary: "stale",
		Status: model.AssistantCheckpointDraft, SourceSessionID: committed.SourceSessionID,
		ExpiresAt: now.Add(2 * time.Hour), CreatedAt: now, UpdatedAt: now.Add(time.Minute),
	}
	if err := repo.UpsertBySourceSession(context.Background(), stale); err != nil {
		t.Fatal(err)
	}
	persisted, err := repo.FindByIDForUser(context.Background(), committed.UserID, committed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != model.AssistantCheckpointCommitted || persisted.Summary != "confirmed" || string(persisted.Payload) != string(committed.Payload) {
		t.Fatalf("committed checkpoint was reopened: %#v", persisted)
	}
}

func TestAssistantCheckpointExpiryPreservesCommittedReceipt(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AssistantCheckpoint{}); err != nil {
		t.Fatal(err)
	}
	repo := New(db).AssistantCheckpoints()
	now := time.Now().UTC()
	for _, status := range []string{model.AssistantCheckpointDraft, model.AssistantCheckpointCommitted} {
		row := &model.AssistantCheckpoint{ID: status, UserID: "user-1", SourceSessionID: status, ModeID: model.AssistantModeCreation, SchemaVersion: "1.0", Payload: []byte(`{}`), Status: status, ExpiresAt: now.Add(-time.Hour), CommitResultJSON: []byte(`{"status":"committed"}`)}
		if err := repo.UpsertBySourceSession(context.Background(), row); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := repo.DeleteExpired(context.Background(), now); err != nil || n != 1 {
		t.Fatalf("DeleteExpired = %d,%v; want one draft", n, err)
	}
	if row, err := repo.FindByIDForUser(context.Background(), "user-1", model.AssistantCheckpointCommitted); err != nil || len(row.CommitResultJSON) == 0 {
		t.Fatalf("committed receipt lost: %v", err)
	}
}
