package wcf

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// reqRecorder captures the last request the test server saw. It's a pointer so
// handler-goroutine mutations are visible to the test after the call returns.
type reqRecorder struct {
	req  http.Request
	body []byte
}

// newTestClient builds a Client pointed at a test server and returns a recorder
// of the last request seen, so each test can assert method/path/body.
func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *reqRecorder) {
	t.Helper()
	rec := &reqRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.body = body
		rec.req = *r // copy the value; fields we assert (Method, URL) are stable
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, 5*time.Second)
	return c, rec
}

func jsonResp(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func TestClient_HealthCheck(t *testing.T) {
	// 200 → nil; non-200 → error.
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	if err := c.HealthCheck(context.Background()); err != nil {
		t.Fatalf("200 health check should succeed, got %v", err)
	}

	cBad, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	if err := cBad.HealthCheck(context.Background()); err == nil {
		t.Fatal("500 health check should fail")
	}
}

func TestClient_StartLogin(t *testing.T) {
	c, rec := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, map[string]any{
			"session_id": "sess-1",
			"base_url":   "https://ilink.example",
			"status":     "wait",
		})
	})
	session, err := c.StartLogin(context.Background(), "https://ilink.example")
	if err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	if session.SessionID != "sess-1" || session.Status != "wait" {
		t.Fatalf("decoded session = %+v", session)
	}
	// Pins the request contract: POST to /api/accounts/login/start with {base_url}.
	if rec.req.Method != http.MethodPost || !strings.HasSuffix(rec.req.URL.Path, "/api/accounts/login/start") {
		t.Fatalf("request = %s %s", rec.req.Method, rec.req.URL.Path)
	}
	var sent map[string]string
	if err := json.Unmarshal(rec.body, &sent); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	if sent["base_url"] != "https://ilink.example" {
		t.Fatalf("request base_url = %q", sent["base_url"])
	}
}

func TestClient_GetLoginStatus(t *testing.T) {
	c, rec := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		// Confirmed login carries the account id.
		jsonResp(w, map[string]any{
			"session_id": "sess-1",
			"status":     "confirmed",
			"account_id": "wxid_abc@im.bot",
		})
	})
	session, err := c.GetLoginStatus(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("GetLoginStatus: %v", err)
	}
	if !session.Confirmed() {
		t.Fatalf("expected Confirmed(), got status=%q account=%q", session.Status, session.AccountID)
	}
	if !strings.Contains(rec.req.URL.RawQuery, "session_id=sess-1") {
		t.Fatalf("status request must carry session_id, got %q", rec.req.URL.RawQuery)
	}
}

func TestClient_GetLoginQR(t *testing.T) {
	wantPNG := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A}
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(wantPNG)
	})
	got, err := c.GetLoginQR(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("GetLoginQR: %v", err)
	}
	if string(got) != string(wantPNG) {
		t.Fatalf("qr bytes = %v, want raw png", got)
	}
}

func TestClient_ListAccounts(t *testing.T) {
	// Pins the {items:[...]} wrapper assumption.
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, map[string]any{
			"items": []map[string]any{
				{"account_id": "acc-1@im.bot", "enabled": true, "login_status": "online"},
				{"account_id": "acc-2@im.bot", "enabled": false, "login_status": "offline"},
			},
		})
	})
	accounts, err := c.ListAccounts(context.Background())
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(accounts) != 2 || accounts[0].AccountID != "acc-1@im.bot" || !accounts[0].Enabled {
		t.Fatalf("accounts = %+v", accounts)
	}
}

func TestClient_ListEvents(t *testing.T) {
	// Pins the {items:[Event]} wrapper + Event field tags, including that an
	// inbound text event decodes such that IsInboundText() is true.
	c, rec := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, map[string]any{
			"items": []map[string]any{
				{
					"id":           42,
					"account_id":   "acc-1@im.bot",
					"direction":    "inbound",
					"event_type":   "text",
					"from_user_id": "filehelper",
					"body_text":    "写文章 测试",
				},
				{
					"id":         43,
					"account_id": "acc-1@im.bot",
					"direction":  "outbound",
					"event_type": "text",
					"to_user_id": "filehelper",
					"body_text":  "✅ 已完成",
				},
			},
		})
	})
	events, err := c.ListEvents(context.Background(), 40, 50)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %+v", events)
	}
	if events[0].ID != 42 || events[0].FromUserID != "filehelper" || events[0].BodyText != "写文章 测试" {
		t.Fatalf("first event decoded wrong: %+v", events[0])
	}
	if !events[0].IsInboundText() {
		t.Fatalf("event 0 should be inbound text: %+v", events[0])
	}
	if events[1].IsInboundText() {
		t.Fatalf("outbound event should not be inbound text: %+v", events[1])
	}
	// Pins the query contract: after_id + limit.
	if !strings.Contains(rec.req.URL.RawQuery, "after_id=40") || !strings.Contains(rec.req.URL.RawQuery, "limit=50") {
		t.Fatalf("events query = %q", rec.req.URL.RawQuery)
	}
}

func TestClient_SendText(t *testing.T) {
	c, rec := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK) // empty body is fine
	})
	if err := c.SendText(context.Background(), "acc-1@im.bot", "filehelper", "hello"); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if rec.req.Method != http.MethodPost || !strings.HasSuffix(rec.req.URL.Path, "/api/messages/send-text") {
		t.Fatalf("request = %s %s", rec.req.Method, rec.req.URL.Path)
	}
	var sent map[string]string
	if err := json.Unmarshal(rec.body, &sent); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	// Pins the send-text request body: account_id / to_user_id / text, no context_token.
	if sent["account_id"] != "acc-1@im.bot" || sent["to_user_id"] != "filehelper" || sent["text"] != "hello" {
		t.Fatalf("send-text body = %+v", sent)
	}
	if _, ok := sent["context_token"]; ok {
		t.Fatalf("send-text must omit context_token (auto-resolved by wcfLink), got %q", sent["context_token"])
	}
}

func TestClient_NonOKReturnsError(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad"}`))
	})
	if _, err := c.ListAccounts(context.Background()); err == nil {
		t.Fatal("non-200 should return an error")
	}
}
