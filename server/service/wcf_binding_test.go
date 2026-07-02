package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/wcf"
)

// bindFakeRepo embeds the nil interface and overrides only the methods the
// binding service touches; any other call panics (test hygiene).
type bindFakeRepo struct {
	repository.WCFBindingRepository
	binding     *model.WCFBinding
	updateCalls int // number of Update invocations
	lastUpdate  *model.WCFBinding
}

func (r *bindFakeRepo) FindByUserID(_ context.Context, _ string) (*model.WCFBinding, error) {
	return r.binding, nil
}
func (r *bindFakeRepo) FindByWCFAccountID(_ context.Context, _ string) (*model.WCFBinding, error) {
	return nil, nil // no conflict
}
func (r *bindFakeRepo) Update(_ context.Context, b *model.WCFBinding) error {
	r.updateCalls++
	snap := *b
	r.lastUpdate = &snap
	if r.binding != nil {
		// Mirror the persisted fields so a later FindByUserID re-read sees them.
		r.binding.WCFAccountID = b.WCFAccountID
		r.binding.Status = b.Status
		r.binding.LoginSessionID = b.LoginSessionID
	}
	return nil
}
func (r *bindFakeRepo) UpdateStatus(_ context.Context, _ string, status string) error {
	if r.binding != nil {
		r.binding.Status = status
	}
	return nil
}

// bindStubRepo wraps a binding sub-repo as a full Repository (embedding the nil
// interface and overriding only WCFBindings). Mirrors pollerStubRepo.
type bindStubRepo struct {
	repository.Repository
	bindings repository.WCFBindingRepository
}

func (r *bindStubRepo) WCFBindings() repository.WCFBindingRepository { return r.bindings }

// newBindTestClient points a real *wcf.Client at an httptest server that reports
// a confirmed login, and returns a counter of login-status hits so a test can
// prove the sidecar was (or was not) queried.
func newBindTestClient(t *testing.T, confirmed bool) (*wcf.Client, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/accounts/login/status" {
			atomic.AddInt32(&hits, 1)
		}
		resp := map[string]any{"session_id": "login_1", "status": "wait"}
		if confirmed {
			resp["status"] = "confirmed"
			resp["account_id"] = "acc_xyz@im.bot"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return wcf.NewClient(srv.URL, 5*time.Second), &hits
}

// TestPollBindStatus_RejectsSessionIDNotOwnedByUser is the L2 regression: wcfLink
// session ids are sequential ("login_<unixnano>"), so without server-side
// ownership an authenticated attacker who approximates a victim's bind-start time
// could poll the victim's login and bind the victim's WeChat to their own
// account. The binding row stores the id StartBind minted for the owner; a
// different supplied id is rejected BEFORE the sidecar is queried.
func TestPollBindStatus_RejectsSessionIDNotOwnedByUser(t *testing.T) {
	client, hits := newBindTestClient(t, true)
	bindings := &bindFakeRepo{binding: &model.WCFBinding{
		UserID:         "u1",
		LoginSessionID: "login_owner", // the id StartBind stored for u1
		Status:         model.WCFBindingStatusPending,
	}}
	svc := NewWCFBindingService(&bindStubRepo{bindings: bindings}, client, nil)

	_, err := svc.PollBindStatus(context.Background(), "u1", "login_attacker_guess")
	if !errors.Is(err, ErrLoginNotPending) {
		t.Fatalf("expected ErrLoginNotPending for a foreign session id, got %v", err)
	}
	if atomic.LoadInt32(hits) != 0 {
		t.Fatalf("wcfLink status must NOT be queried for a foreign session id; got %d hits", atomic.LoadInt32(hits))
	}
}

func TestPollBindStatus_AcceptsOwnedSessionID(t *testing.T) {
	// The owner polling their own stored session id proceeds normally: wcfLink
	// confirms, the account id is captured, the binding activates.
	client, hits := newBindTestClient(t, true)
	bindings := &bindFakeRepo{binding: &model.WCFBinding{
		UserID:         "u1",
		LoginSessionID: "login_owner",
		Status:         model.WCFBindingStatusPending,
	}}
	svc := NewWCFBindingService(&bindStubRepo{bindings: bindings}, client, nil)

	res, err := svc.PollBindStatus(context.Background(), "u1", "login_owner")
	if err != nil {
		t.Fatalf("owner's own session id should be accepted: %v", err)
	}
	if res.Status != model.WCFBindingStatusActive {
		t.Fatalf("status = %q, want active", res.Status)
	}
	if atomic.LoadInt32(hits) != 1 {
		t.Fatalf("wcfLink status should be queried exactly once for the owner; got %d", atomic.LoadInt32(hits))
	}
}

// newStartBindTestClient serves the two endpoints StartBind touches — /start
// (mints the session id) and /qr (returns QR bytes) — so a StartBind call runs
// end-to-end against an in-process server. The assertion is on the persisted
// binding, so no hit counter is needed.
func newStartBindTestClient(t *testing.T, sessionID string) *wcf.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/accounts/login/start", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session_id":  sessionID,
			"status":      "wait",
			"qr_code_url": "http://example.test/qr",
		})
	})
	mux.HandleFunc("/api/accounts/login/qr", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte{0x89, 'P', 'N', 'G'}) // bytes are only base64-wrapped
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return wcf.NewClient(srv.URL, 5*time.Second)
}

// TestStartBind_PersistsSessionIDAtomically pins the B2 fix: re-binding a stale
// pending row flips it to pending AND stamps the new login session id in a SINGLE
// Update — never UpdateStatus then a separately-failing session write (which
// would leave the row pending under the previous id and lock the user out).
func TestStartBind_PersistsSessionIDAtomically(t *testing.T) {
	const sid = "login_start_atomic"
	client := newStartBindTestClient(t, sid)
	bindings := &bindFakeRepo{binding: &model.WCFBinding{
		UserID:         "u1",
		Status:         model.WCFBindingStatusPending, // stale pending row from a prior attempt
		LoginSessionID: "",                            // intentionally empty (stale)
	}}
	svc := NewWCFBindingService(&bindStubRepo{bindings: bindings}, client, nil)

	res, err := svc.StartBind(context.Background(), "u1")
	if err != nil {
		t.Fatalf("StartBind: unexpected error: %v", err)
	}
	if res.SessionID != sid {
		t.Fatalf("returned session id = %q, want %q", res.SessionID, sid)
	}
	if bindings.updateCalls != 1 {
		t.Fatalf("expected exactly 1 atomic Update, got %d", bindings.updateCalls)
	}
	if bindings.lastUpdate.Status != model.WCFBindingStatusPending {
		t.Fatalf("persisted status = %q, want pending", bindings.lastUpdate.Status)
	}
	if bindings.lastUpdate.LoginSessionID != sid {
		t.Fatalf("persisted login_session_id = %q, want %q", bindings.lastUpdate.LoginSessionID, sid)
	}
}

// TestPollBindStatus_LegacyRowWithoutSessionIDIsRejected pins the B3 fix: a
// pre-fix pending row (empty LoginSessionID) must NOT let an arbitrary session id
// through. Strict equality — no lenient "" branch — so a guessed id can't poll a
// legacy row, and the sidecar is never queried on rejection.
func TestPollBindStatus_LegacyRowWithoutSessionIDIsRejected(t *testing.T) {
	client, hits := newBindTestClient(t, true)
	bindings := &bindFakeRepo{binding: &model.WCFBinding{
		UserID:         "u1",
		LoginSessionID: "", // legacy pre-fix row
		Status:         model.WCFBindingStatusPending,
	}}
	svc := NewWCFBindingService(&bindStubRepo{bindings: bindings}, client, nil)

	_, err := svc.PollBindStatus(context.Background(), "u1", "login_attacker_guess")
	if !errors.Is(err, ErrLoginNotPending) {
		t.Fatalf("expected ErrLoginNotPending for a legacy empty-id row, got %v", err)
	}
	if atomic.LoadInt32(hits) != 0 {
		t.Fatalf("wcfLink status must NOT be queried when the stored id is empty; got %d hits", atomic.LoadInt32(hits))
	}
}
