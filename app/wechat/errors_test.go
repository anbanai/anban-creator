package wechat

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseWechatError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantNil   bool
		wantCode  int
		wantRetry bool
	}{
		{"nil error", nil, true, 0, false},
		{"network error", errors.New("connection refused"), true, 0, false},
		{"40164 IP whitelist", errors.New("errcode=40164, IP not in whitelist"), false, 40164, false},
		{"40001 invalid credential", errors.New("errcode=40001, invalid credential"), false, 40001, false},
		{"42001 token expired", errors.New("errcode=42001, access_token expired"), false, 42001, true},
		{"45009 rate limit", errors.New("errcode=45009, api freq out of limit"), false, 45009, true},
		{"40007 invalid media_id", errors.New("errcode=40007, errmsg=invalid media_id"), false, 40007, false},
		{"40009 invalid img media_id", errors.New("errcode=40009, errmsg=invalid media_id"), false, 40009, false},
		{"41006 missing media_id", errors.New("errcode=41006, errmsg=invalid media_id"), false, 41006, false},
		{"-1 system busy", errors.New("errcode=-1, system error"), false, -1, true},
		{"unknown code", errors.New("errcode=99999, unknown"), false, 99999, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseWechatError(tt.err)
			if tt.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil WechatAPIError")
			}
			if got.ErrCode != tt.wantCode {
				t.Errorf("ErrCode = %d, want %d", got.ErrCode, tt.wantCode)
			}
			if got.Retryable != tt.wantRetry {
				t.Errorf("Retryable = %v, want %v", got.Retryable, tt.wantRetry)
			}
		})
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"network error", errors.New("connection refused"), true},
		{"40164 not retryable", errors.New("errcode=40164, IP not in whitelist"), false},
		{"42001 retryable", errors.New("errcode=42001, token expired"), true},
		{"WechatAPIError direct", &WechatAPIError{ErrCode: 40164, Retryable: false}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRetryable(tt.err); got != tt.want {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDownloadFileUsesBrowserCompatibleHeaders(t *testing.T) {
	const pngHeader = "\x89PNG\r\n\x1a\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.UserAgent(), "Go-http-client") {
			http.Error(w, "default go client blocked", http.StatusForbidden)
			return
		}
		if !strings.Contains(r.Header.Get("Accept"), "image/") {
			http.Error(w, "image accept header required", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte(pngHeader + "image-bytes"))
	}))
	defer srv.Close()

	path, err := DownloadFile(srv.URL + "/generated.png")
	if err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	defer os.Remove(path)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if !strings.HasPrefix(string(got), pngHeader) {
		t.Fatalf("downloaded data = %q, want PNG bytes", string(got))
	}
}

func TestDownloadFileReturnsDiagnosticsOnHTTPFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Server", "cloudflare")
		w.Header().Set("Cf-Ray", "test-ray")
		w.Header().Set("Location", "https://example.com/blocked")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("blocked by edge policy with a long body that should be truncated"))
	}))
	defer srv.Close()

	_, err := DownloadFile(srv.URL + "/generated.png")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var dlErr *DownloadError
	if !errors.As(err, &dlErr) {
		t.Fatalf("error type = %T, want *DownloadError", err)
	}
	if dlErr.StatusCode != http.StatusForbidden {
		t.Fatalf("StatusCode = %d, want 403", dlErr.StatusCode)
	}
	if dlErr.ContentType != "text/plain; charset=utf-8" || dlErr.Server != "cloudflare" || dlErr.CFRay != "test-ray" {
		t.Fatalf("diagnostics = %#v", dlErr)
	}
	if dlErr.BodyPreview == "" || len(dlErr.BodyPreview) > 80 {
		t.Fatalf("BodyPreview = %q, want short preview", dlErr.BodyPreview)
	}
}

func TestDownloadFileContextHonorsCancellation(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type result struct {
		path string
		err  error
	}
	resultCh := make(chan result, 1)
	go func() {
		path, err := DownloadFileContext(ctx, srv.URL+"/generated.png")
		resultCh <- result{path: path, err: err}
	}()

	select {
	case <-started:
		cancel()
	case <-time.After(time.Second):
		t.Fatal("server did not receive download request")
	}
	select {
	case result := <-resultCh:
		defer os.Remove(result.path)
		if !errors.Is(result.err, context.Canceled) {
			t.Fatalf("DownloadFileContext error = %v, want context.Canceled", result.err)
		}
	case <-time.After(time.Second):
		t.Fatal("DownloadFileContext did not return after cancellation")
	}
}

func TestDownloadFileRemainsSingleAttempt(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	path, err := DownloadFile(srv.URL + "/generated.png")
	defer os.Remove(path)
	if err == nil {
		t.Fatal("DownloadFile error = nil, want error")
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("request count = %d, want 1", got)
	}
}

func TestDownloadFileContextRemovesPartialFile(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "64")
		_, _ = w.Write([]byte("short"))
	}))
	defer srv.Close()

	path, err := DownloadFileContext(context.Background(), srv.URL+"/generated.png")
	defer os.Remove(path)
	if err == nil {
		t.Fatal("DownloadFileContext error = nil, want error")
	}
	var dlErr *DownloadError
	if !errors.As(err, &dlErr) {
		t.Fatalf("error type = %T, want *DownloadError", err)
	}
	if dlErr.Original == nil {
		t.Fatal("DownloadError.Original = nil, want size mismatch error")
	}
	if !strings.Contains(err.Error(), "does not match content length") {
		t.Fatalf("error = %q, want size mismatch text", err)
	}
	if dlErr.StatusCode != 0 {
		t.Fatalf("StatusCode = %d, want 0 for body failure", dlErr.StatusCode)
	}
	entries, err := os.ReadDir(os.Getenv("TMPDIR"))
	if err != nil {
		t.Fatalf("read temp directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("temp directory entries = %d, want 0", len(entries))
	}
}
