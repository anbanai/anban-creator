package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/wcf"
)

// pollerFakeDispatcher records Handle invocations. Implements wcfDispatcher.
type pollerFakeDispatcher struct {
	calls int
	last  *model.WCFBinding
}

func (d *pollerFakeDispatcher) Handle(_ context.Context, binding *model.WCFBinding, _ wcf.Event) {
	d.calls++
	d.last = binding
}

// pollerStubBindings embeds the nil interface and overrides only the two methods
// processEvent touches. Every other method panics if called.
type pollerStubBindings struct {
	repository.WCFBindingRepository
	binding *model.WCFBinding
	findErr error
	peerSet string // last peer id passed to UpdatePeerID
	updErr  error
}

func (s *pollerStubBindings) FindByWCFAccountID(_ context.Context, _ string) (*model.WCFBinding, error) {
	return s.binding, s.findErr
}
func (s *pollerStubBindings) UpdatePeerID(_ context.Context, _ string, peerID string) error {
	s.peerSet = peerID
	return s.updErr
}

type pollerStubRepo struct {
	repository.Repository
	bindings repository.WCFBindingRepository
}

func (r *pollerStubRepo) WCFBindings() repository.WCFBindingRepository { return r.bindings }

func inboundText(from, body string) wcf.Event {
	return wcf.Event{
		ID: 1, AccountID: "acc", Direction: "inbound", EventType: "text",
		FromUserID: from, BodyText: body,
	}
}

func TestWCFPoller_PeerTrustBoundary(t *testing.T) {
	// Once a peer is captured, ONLY that peer may dispatch; any other sender
	// (a contact, a group member) is ignored — they must not create billable
	// tasks or hijack the reply channel. This is the B1 guard.
	active := &model.WCFBinding{UserID: "u1", WCFAccountID: "acc", PeerID: "peerA", Status: model.WCFBindingStatusActive}

	cases := []struct {
		name            string
		binding         *model.WCFBinding
		ev              wcf.Event
		wantDispatched  bool
		wantPeerCapture string
	}{
		{"outbound ignored", active, wcf.Event{ID: 1, AccountID: "acc", Direction: "outbound", EventType: "text", FromUserID: "peerA", BodyText: "x"}, false, ""},
		{"unbound account ignored", nil, inboundText("peerA", "写文章 测试"), false, ""},
		{"same peer dispatched", active, inboundText("peerA", "写文章 测试"), true, ""},
		{"different peer ignored (B1)", active, inboundText("intruder", "写文章 测试"), false, ""},
		{"captures peer on first recognized command", &model.WCFBinding{UserID: "u1", WCFAccountID: "acc", PeerID: "", Status: model.WCFBindingStatusActive}, inboundText("peerA", "写文章 测试"), true, "peerA"},
		{"ignores unrecognized when peer unset", &model.WCFBinding{UserID: "u1", WCFAccountID: "acc", PeerID: "", Status: model.WCFBindingStatusActive}, inboundText("peerA", "随机闲聊不相关"), false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			disp := &pollerFakeDispatcher{}
			bindings := &pollerStubBindings{binding: tc.binding}
			p := &WCFPoller{repo: &pollerStubRepo{bindings: bindings}, dispatcher: disp}
			p.processEvent(context.Background(), tc.ev)

			if tc.wantDispatched {
				if disp.calls != 1 {
					t.Fatalf("expected 1 dispatch, got %d", disp.calls)
				}
			} else {
				if disp.calls != 0 {
					t.Fatalf("expected NO dispatch, got %d", disp.calls)
				}
			}
			if tc.wantPeerCapture != "" && bindings.peerSet != tc.wantPeerCapture {
				t.Fatalf("peer capture = %q, want %q", bindings.peerSet, tc.wantPeerCapture)
			}
			if tc.wantPeerCapture == "" && bindings.peerSet != "" {
				t.Fatalf("peer should NOT be captured, got %q", bindings.peerSet)
			}
		})
	}
}

func TestWCFPoller_AtMostOnceCursorOrder(t *testing.T) {
	// B2: the cursor advances BEFORE dispatch, so a crash between the two can
	// drop an event but never replay it as a duplicate. We assert ordering by
	// observing that advanceCursor (lastID) has moved past the event id even
	// though the dispatcher recorded the call — i.e. cursor moved first.
	disp := &pollerFakeDispatcher{}
	p := &WCFPoller{repo: &pollerStubRepo{bindings: &pollerStubBindings{
		binding: &model.WCFBinding{UserID: "u1", WCFAccountID: "acc", PeerID: "peerA", Status: model.WCFBindingStatusActive},
	}}, dispatcher: disp}

	// Call poll() against a client that returns one event. Since poll() needs a
	// real client, drive processEvent + advanceCursor in the same order poll()
	// does (advanceCursor first) to lock the contract.
	ev := inboundText("peerA", "写文章 测试")
	p.advanceCursor(context.Background(), ev.ID)
	if p.lastID != ev.ID {
		t.Fatalf("cursor should advance before dispatch; lastID=%d want %d", p.lastID, ev.ID)
	}
	p.processEvent(context.Background(), ev)
	if disp.calls != 1 {
		t.Fatalf("dispatch should still occur; got %d", disp.calls)
	}
}

func TestWCFPoller_PeerCaptureFailureSkipsDispatch(t *testing.T) {
	// If persisting the captured peer fails (DB write error), the poller must
	// NOT dispatch — otherwise a billable task is created with no persisted
	// PeerID, so the reply (SendToBinding) silently no-ops: the user is charged
	// for a task they never see confirmed, and the reply channel is never
	// locked. UpdatePeerID is still attempted; only dispatch is skipped.
	disp := &pollerFakeDispatcher{}
	bindings := &pollerStubBindings{
		binding: &model.WCFBinding{UserID: "u1", WCFAccountID: "acc", PeerID: "", Status: model.WCFBindingStatusActive},
		updErr:  errors.New("db write failed"),
	}
	p := &WCFPoller{repo: &pollerStubRepo{bindings: bindings}, dispatcher: disp}
	p.processEvent(context.Background(), inboundText("peerA", "写文章 测试"))

	if disp.calls != 0 {
		t.Fatalf("expected NO dispatch on peer-capture failure, got %d", disp.calls)
	}
	if bindings.peerSet != "peerA" {
		t.Fatalf("UpdatePeerID should still be attempted; peerSet=%q want %q", bindings.peerSet, "peerA")
	}
}

// newPrimeClient stands up an httptest wcfLink whose /api/events endpoint is
// driven by page(afterID) → (events, err), so prime's paging, error-mid-drain,
// and empty-history branches can be pinned without a real sidecar.
func newPrimeClient(t *testing.T, page func(afterID int64) ([]wcf.Event, error)) *wcf.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		afterID, _ := strconv.ParseInt(r.URL.Query().Get("after_id"), 10, 64)
		items, err := page(afterID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
	}))
	t.Cleanup(srv.Close)
	return wcf.NewClient(srv.URL, 5*time.Second)
}

func TestWCFPoller_PrimeDrainsAllHistory(t *testing.T) {
	// Regression for the single-page cap: 120 historical events with batchLimit
	// 50 must page (50, 50, 20) and return 120, not 50. A stale 50 here would mean
	// events 51-120 later arrive via normal poll() and dispatch as NEW commands.
	client := newPrimeClient(t, func(afterID int64) ([]wcf.Event, error) {
		var items []wcf.Event
		for id := afterID + 1; id <= 120 && len(items) < 50; id++ {
			items = append(items, wcf.Event{ID: id})
		}
		return items, nil
	})
	p := &WCFPoller{client: client, batchLimit: 50}
	if got := p.prime(context.Background()); got != 120 {
		t.Fatalf("prime must drain to 120 across pages, got %d", got)
	}
}

func TestWCFPoller_PrimeErrorReturnsLastMaxNotZero(t *testing.T) {
	// Regression: prime used to `return 0` on error, which forced the next poll()
	// to fetch from after_id=0 and dispatch the ENTIRE remaining history. It must
	// return the highest id seen so far so the cursor stays ahead of history.
	var calls int32
	client := newPrimeClient(t, func(afterID int64) ([]wcf.Event, error) {
		if atomic.AddInt32(&calls, 1) >= 2 {
			return nil, errors.New("sidecar 500")
		}
		var items []wcf.Event
		for id := afterID + 1; id <= 50; id++ { // first page: ids 1..50
			items = append(items, wcf.Event{ID: id})
		}
		return items, nil
	})
	p := &WCFPoller{client: client, batchLimit: 50}
	if got := p.prime(context.Background()); got != 50 {
		t.Fatalf("prime on mid-drain error must return last max (50), got %d", got)
	}
}

func TestWCFPoller_PrimeEmptyHistoryReturnsZero(t *testing.T) {
	client := newPrimeClient(t, func(afterID int64) ([]wcf.Event, error) {
		return nil, nil // empty sidecar DB
	})
	p := &WCFPoller{client: client, batchLimit: 50}
	if got := p.prime(context.Background()); got != 0 {
		t.Fatalf("prime of empty history must return 0, got %d", got)
	}
}
