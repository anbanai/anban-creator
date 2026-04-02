package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
)

func newTestLogger() *zerolog.Logger {
	l := zerolog.New(zerolog.NewTestWriter(nil))
	return &l
}

func TestCode2Session_Success(t *testing.T) {
	session := &WxSession{
		OpenID:     "oTestOpenID123",
		SessionKey: "testSessionKey456",
		UnionID:    "oTestUnionID789",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify query parameters.
		if r.URL.Query().Get("appid") != "test-app-id" {
			t.Errorf("expected appid=test-app-id, got %s", r.URL.Query().Get("appid"))
		}
		if r.URL.Query().Get("secret") != "test-app-secret" {
			t.Errorf("expected secret=test-app-secret, got %s", r.URL.Query().Get("secret"))
		}
		if r.URL.Query().Get("js_code") != "test-code" {
			t.Errorf("expected js_code=test-code, got %s", r.URL.Query().Get("js_code"))
		}
		if r.URL.Query().Get("grant_type") != "authorization_code" {
			t.Errorf("expected grant_type=authorization_code, got %s", r.URL.Query().Get("grant_type"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(session)
	}))
	defer server.Close()

	svc := NewWeChatService("test-app-id", "test-app-secret", newTestLogger())
	svc.SetBaseURL(server.URL)

	result, err := svc.Code2Session("test-code")
	if err != nil {
		t.Fatalf("Code2Session: %v", err)
	}

	if result.OpenID != session.OpenID {
		t.Errorf("expected OpenID=%s, got %s", session.OpenID, result.OpenID)
	}
	if result.SessionKey != session.SessionKey {
		t.Errorf("expected SessionKey=%s, got %s", session.SessionKey, result.SessionKey)
	}
	if result.UnionID != session.UnionID {
		t.Errorf("expected UnionID=%s, got %s", session.UnionID, result.UnionID)
	}
}

func TestCode2Session_APIError(t *testing.T) {
	errResp := &WxSession{
		ErrCode: 40029,
		ErrMsg:  "invalid code",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(errResp)
	}))
	defer server.Close()

	svc := NewWeChatService("test-app-id", "test-app-secret", newTestLogger())
	svc.SetBaseURL(server.URL)

	_, err := svc.Code2Session("bad-code")
	if err == nil {
		t.Fatal("expected error for API error response, got nil")
	}
}

func TestCode2Session_EmptyOpenID(t *testing.T) {
	resp := map[string]interface{}{
		"openid":      "",
		"session_key": "some-key",
		"errcode":     0,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	svc := NewWeChatService("test-app-id", "test-app-secret", newTestLogger())
	svc.SetBaseURL(server.URL)

	_, err := svc.Code2Session("test-code")
	if err == nil {
		t.Fatal("expected error for empty openid, got nil")
	}
}

func TestCode2Session_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	svc := NewWeChatService("test-app-id", "test-app-secret", newTestLogger())
	svc.SetBaseURL(server.URL)

	_, err := svc.Code2Session("test-code")
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}
