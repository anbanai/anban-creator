package seednote

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDeleteCookiesRequiresSuccessfulResponse(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "success", body: `{"success":true}`},
		{name: "business failure", body: `{"success":false,"message":"cookie store unavailable"}`, wantErr: "cookie store unavailable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/login/cookies" {
					t.Fatalf("request = %s %s", r.Method, r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(server.Close)

			err := NewClient(server.URL, time.Second).DeleteCookies(context.Background())
			if tt.wantErr == "" && err != nil {
				t.Fatalf("DeleteCookies: %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("DeleteCookies error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
