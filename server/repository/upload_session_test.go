package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
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

func TestUploadSessionCleanupRescheduleUsesNotBeforeWithoutChangingExpiry(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	expiresAt := now.Add(-time.Hour)
	session := uploadSessionFixture("rescheduled-cleanup", now)
	session.ExpiresAt = expiresAt
	if err := repo.UploadSessions().Create(ctx, session); err != nil {
		t.Fatalf("create upload session: %v", err)
	}
	found, err := repo.UploadSessions().FindByID(ctx, session.ID)
	if err != nil || found.NextCleanupAt != nil {
		t.Fatalf("new upload session next cleanup = %v, %v; want nil", found.NextCleanupAt, err)
	}
	claimed, err := repo.UploadSessions().ClaimExpiration(ctx, session.ID, "cleanup-1", now, now.Add(-5*time.Minute))
	if err != nil || !claimed {
		t.Fatalf("ClaimExpiration = %v, %v", claimed, err)
	}
	nextCleanupAt := now.Add(30 * time.Minute)
	rescheduled, err := repo.UploadSessions().RescheduleExpiration(ctx, session.ID, "cleanup-1", nextCleanupAt)
	if err != nil || !rescheduled {
		t.Fatalf("RescheduleExpiration = %v, %v", rescheduled, err)
	}
	found, err = repo.UploadSessions().FindByID(ctx, session.ID)
	if err != nil || found.Status != model.UploadSessionPending || found.NextCleanupAt == nil || !found.NextCleanupAt.Equal(nextCleanupAt) || !found.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("rescheduled upload session = %#v, err=%v", found, err)
	}
	if claimed, err := repo.UploadSessions().ClaimExpiration(ctx, session.ID, "cleanup-early", now, now.Add(-5*time.Minute)); err != nil || claimed {
		t.Fatalf("early ClaimExpiration = %v, %v; want false, nil", claimed, err)
	}
	candidates, err := repo.UploadSessions().FindForCleanup(ctx, now, now.Add(-5*time.Minute), 10)
	if err != nil || len(candidates) != 0 {
		t.Fatalf("early cleanup candidates = %#v, %v", candidates, err)
	}
	candidates, err = repo.UploadSessions().FindForCleanup(ctx, nextCleanupAt, nextCleanupAt.Add(-5*time.Minute), 10)
	if err != nil || len(candidates) != 1 || candidates[0].ID != session.ID {
		t.Fatalf("due cleanup candidates = %#v, %v", candidates, err)
	}
}

func TestUploadSessionCleanupQueryIsPortableAcrossSQLiteAndMySQL(t *testing.T) {
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	dialectors := map[string]gorm.Dialector{
		"sqlite": sqlite.Open(":memory:"),
		"mysql": mysql.New(mysql.Config{
			DSN:                       "unused:unused@tcp(localhost:3306)/unused?parseTime=true",
			SkipInitializeWithVersion: true,
		}),
	}
	for name, dialector := range dialectors {
		t.Run(name, func(t *testing.T) {
			db, err := gorm.Open(dialector, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
			if err != nil {
				t.Fatalf("open dry-run database: %v", err)
			}
			var sessions []*model.UploadSession
			stmt := buildUploadSessionCleanupQuery(db.Model(&model.UploadSession{}), now, now.Add(-5*time.Minute)).Limit(100).Find(&sessions).Statement
			sql := stmt.SQL.String()
			for _, fragment := range []string{"next_cleanup_at IS NULL OR next_cleanup_at <=", "COALESCE(next_cleanup_at, expires_at) ASC", "expires_at ASC", "id ASC"} {
				if !strings.Contains(sql, fragment) {
					t.Fatalf("%s cleanup SQL = %s; missing %q", name, sql, fragment)
				}
			}
		})
	}
}

func TestUploadSessionRecordsFinalizationETagWithLeaseToken(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	session := uploadSessionFixture("record-finalization-etag", now)
	session.Status = model.UploadSessionFinalizing
	session.FinalizationToken = "lease-current"
	session.FinalizationClaimedAt = &now
	if err := repo.UploadSessions().Create(ctx, session); err != nil {
		t.Fatalf("create upload session: %v", err)
	}

	if recorded, err := repo.UploadSessions().RecordPromotionSourceETag(ctx, session.ID, "lease-old", "etag-source"); err != nil || recorded {
		t.Fatalf("stale token source record = %v, %v; want false, nil", recorded, err)
	}
	if recorded, err := repo.UploadSessions().RecordPromotionSourceETag(ctx, session.ID, "lease-current", "etag-source"); err != nil || !recorded {
		t.Fatalf("current token source record = %v, %v; want true, nil", recorded, err)
	}
	if recorded, err := repo.UploadSessions().RecordFinalizationETag(ctx, session.ID, "lease-old", "etag-target"); err != nil || recorded {
		t.Fatalf("stale token record = %v, %v; want false, nil", recorded, err)
	}
	if recorded, err := repo.UploadSessions().RecordFinalizationETag(ctx, session.ID, "lease-current", "etag-target"); err != nil || !recorded {
		t.Fatalf("current token record = %v, %v; want true, nil", recorded, err)
	}
	if released, err := repo.UploadSessions().ReleaseFinalization(ctx, session.ID, "lease-current"); err != nil || !released {
		t.Fatalf("release finalization = %v, %v", released, err)
	}
	found, err := repo.UploadSessions().FindByID(ctx, session.ID)
	if err != nil {
		t.Fatalf("find upload session: %v", err)
	}
	if found.Status != model.UploadSessionPending || found.PromotionSourceETag != "etag-source" || found.FinalizationETag != "etag-target" {
		t.Fatalf("released session = %#v; want pending with retained fingerprint", found)
	}
}

func TestUploadSessionFinalizationRecoveryClaimCAS(t *testing.T) {
	now := time.Date(2026, 7, 18, 9, 30, 0, 0, time.UTC)
	staleBefore := now.Add(-time.Minute)
	tests := []struct {
		name      string
		mutate    func(*model.UploadSession)
		wantClaim bool
	}{
		{name: "expired status", mutate: func(s *model.UploadSession) {
			s.Status = model.UploadSessionExpired
			s.ExpiredAt = ptrTime(now.Add(-time.Hour))
		}, wantClaim: true},
		{name: "pending expiry boundary", mutate: func(s *model.UploadSession) {
			s.ExpiresAt = now
		}, wantClaim: true},
		{name: "unexpired pending uses normal claim", mutate: func(s *model.UploadSession) {
			s.ExpiresAt = now.Add(time.Nanosecond)
		}},
		{name: "stale finalizing boundary", mutate: func(s *model.UploadSession) {
			s.Status = model.UploadSessionFinalizing
			s.FinalizationToken = "lease-old"
			s.FinalizationClaimedAt = ptrTime(staleBefore)
		}, wantClaim: true},
		{name: "active finalizing lease", mutate: func(s *model.UploadSession) {
			s.Status = model.UploadSessionFinalizing
			s.FinalizationToken = "lease-active"
			s.FinalizationClaimedAt = ptrTime(staleBefore.Add(time.Nanosecond))
		}},
		{name: "missing fingerprint", mutate: func(s *model.UploadSession) {
			s.ExpiresAt = now
			s.FinalizationETag = ""
		}},
		{name: "source fingerprint before target fingerprint", mutate: func(s *model.UploadSession) {
			s.ExpiresAt = now
			s.FinalizationETag = ""
			s.PromotionSourceETag = "etag-source"
		}, wantClaim: true},
		{name: "active expiring cleanup lease", mutate: func(s *model.UploadSession) {
			s.Status = model.UploadSessionExpiring
			s.CleanupClaimID = "cleanup-active"
			s.CleanupClaimedAt = ptrTime(now)
		}},
		{name: "stale expiring cleanup lease", mutate: func(s *model.UploadSession) {
			s.Status = model.UploadSessionExpiring
			s.CleanupClaimID = "cleanup-stale"
			s.CleanupClaimedAt = ptrTime(staleBefore)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupTestDB(t)
			repo := New(db)
			session := uploadSessionFixture("recovery", now)
			session.FinalizationETag = "etag-verified"
			tt.mutate(session)
			if err := repo.UploadSessions().Create(t.Context(), session); err != nil {
				t.Fatalf("create upload session: %v", err)
			}

			claimed, err := repo.UploadSessions().ClaimFinalizationRecovery(t.Context(), session.ID, "lease-recovery", now, staleBefore)
			if err != nil || claimed != tt.wantClaim {
				t.Fatalf("ClaimFinalizationRecovery = %v, %v; want %v, nil", claimed, err, tt.wantClaim)
			}
			found, err := repo.UploadSessions().FindByID(t.Context(), session.ID)
			if err != nil {
				t.Fatalf("find upload session: %v", err)
			}
			if !tt.wantClaim {
				if found.Status != session.Status || found.FinalizationToken != session.FinalizationToken || found.CleanupClaimID != session.CleanupClaimID {
					t.Fatalf("rejected recovery mutated session: before=%#v after=%#v", session, found)
				}
				return
			}
			if found.Status != model.UploadSessionFinalizing || found.FinalizationToken != "lease-recovery" || found.FinalizationClaimedAt == nil || !found.FinalizationClaimedAt.Equal(now) || found.ExpiredAt != nil || found.CleanupClaimID != "" || found.CleanupClaimedAt != nil {
				t.Fatalf("recovery claim state = %#v", found)
			}
			if completed, err := repo.UploadSessions().CompleteFinalization(t.Context(), session.ID, "lease-old", session.ID, now); err != nil || completed {
				t.Fatalf("old token completed recovery claim: %v, %v", completed, err)
			}
		})
	}
}

func ptrTime(value time.Time) *time.Time { return &value }

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
