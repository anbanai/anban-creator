package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"syscall"
	"testing"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

type artifactDiagnosticNetError struct{ timeout bool }

func (e artifactDiagnosticNetError) Error() string   { return "network detail must stay private" }
func (e artifactDiagnosticNetError) Timeout() bool   { return e.timeout }
func (e artifactDiagnosticNetError) Temporary() bool { return true }

func TestArtifactDiagnosticFieldsRedactCredentialBearingURLError(t *testing.T) {
	const secret = "FAKE_SECRET"
	err := &url.Error{
		Op:  "POST",
		URL: "https://storage.invalid/object?Signature=" + secret + "&SecurityToken=TOKEN",
		Err: syscall.ECONNRESET,
	}

	fields := NewArtifactDiagnosticFields("prepare", err)
	if fields.Operation != "prepare" || fields.Code != "connection_reset" || fields.NetworkCode != "ECONNRESET" {
		t.Fatalf("diagnostic fields = %#v", fields)
	}
	if fields.Error() != "artifact prepare failed: connection_reset" {
		t.Fatalf("safe error = %q", fields.Error())
	}
	encoded := fields.String()
	for _, forbidden := range []string{secret, "TOKEN", "Signature", "storage.invalid", "?"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("diagnostic %q leaked %q", encoded, forbidden)
		}
	}
	if errors.Is(err, syscall.ECONNRESET) == false {
		t.Fatal("test fixture must retain the wrapped network cause")
	}
}

func TestArtifactDiagnosticFieldsClassifySafeCauses(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantCode    string
		wantNetwork string
		wantStatus  int
		wantRequest string
	}{
		{name: "deadline", err: fmt.Errorf("wrapped: %w", context.DeadlineExceeded), wantCode: "deadline_exceeded"},
		{name: "network timeout", err: artifactDiagnosticNetError{timeout: true}, wantCode: "network_timeout"},
		{name: "network unavailable", err: artifactDiagnosticNetError{}, wantCode: "network_unavailable"},
		{name: "invalid request", err: fmt.Errorf("detail: %w", ErrTaskArtifactInvalid), wantCode: "invalid_request"},
		{name: "execution conflict", err: fmt.Errorf("detail: %w", ErrTaskArtifactExecutionConflict), wantCode: "execution_conflict"},
		{name: "oss throttled", err: fmt.Errorf("wrapped: %w", oss.ServiceError{StatusCode: 429, Code: "TooManyRequests", RequestID: "safe-request-id", Message: "raw body secret"}), wantCode: "rate_limited", wantStatus: 429, wantRequest: "safe-request-id"},
		{name: "oss unavailable", err: oss.ServiceError{StatusCode: 503, Code: "ServiceUnavailable", RequestID: "request-503"}, wantCode: "service_unavailable", wantStatus: 503, wantRequest: "request-503"},
		{name: "oss pointer redacts unsafe request id", err: &oss.ServiceError{StatusCode: 403, Code: "AccessDenied", RequestID: "request?SecurityToken=secret"}, wantCode: "unauthorized", wantStatus: 403},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields := NewArtifactDiagnosticFields("prepare", tt.err)
			if fields.Code != tt.wantCode || fields.NetworkCode != tt.wantNetwork || fields.HTTPStatus != tt.wantStatus || fields.RequestID != tt.wantRequest {
				t.Fatalf("diagnostic fields = %#v", fields)
			}
			if strings.Contains(fields.String(), "secret") || strings.Contains(fields.String(), "network detail") {
				t.Fatalf("safe fields leaked raw error: %q", fields.String())
			}
		})
	}
	var _ net.Error = artifactDiagnosticNetError{}
}

func TestPrepareTaskArtifactUploadPreservesWrappedConnectionReset(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	executionID := startTaskArtifactExecution(t, repo, task)
	store.statErr = &url.Error{Op: "HEAD", URL: "https://storage.invalid/object?Signature=SECRET", Err: syscall.ECONNRESET}

	_, err := svc.PrepareTaskArtifactUpload(context.Background(), task.ID, task.UserID, executionID, TaskArtifactUploadConfig{}, TaskArtifactPrepareRequest{
		TaskID: task.ID, ExecutionID: executionID, RelativePath: "output/article.md",
		ContentType: "text/markdown", Size: 7, SHA256: taskArtifactTestSHA256,
	})
	if !errors.Is(err, syscall.ECONNRESET) {
		t.Fatalf("prepare error = %v, want wrapped ECONNRESET", err)
	}
	fields := NewArtifactDiagnosticFields("prepare", err)
	if fields.Code != "connection_reset" || fields.NetworkCode != "ECONNRESET" {
		t.Fatalf("diagnostic fields = %#v", fields)
	}
}
