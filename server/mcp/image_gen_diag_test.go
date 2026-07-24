package mcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"
)

// timeoutNetErr is a net.Error that reports Timeout()=true, so we can exercise
// the net.Error.Timeout() branch of categorizeImageGenFailure without a real
// network call.
type timeoutNetErr struct{ msg string }

func (e *timeoutNetErr) Error() string   { return e.msg }
func (e *timeoutNetErr) Timeout() bool   { return true }
func (e *timeoutNetErr) Temporary() bool { return false }

func TestCategorizeImageGenFailure(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		refPath string
		want    string
	}{
		{"nil error", nil, "", ""},
		{"deadline exceeded wins over ref", context.DeadlineExceeded, "$DIR/cover.png", "timeout"},
		{"deadline exceeded no ref", context.DeadlineExceeded, "", "timeout"},
		{"canceled", context.Canceled, "", "canceled"},
		{"net timeout error", &timeoutNetErr{msg: "i/o timeout"}, "", "timeout"},
		{"net timeout beats ref mode", &timeoutNetErr{msg: "i/o timeout"}, "$DIR/cover.png", "timeout"},
		{"ref image mode generic err", errors.New("seedream rejected ref"), "$DIR/cover.png", "ref_image_mode"},
		{"provider generic err no ref", errors.New("500 internal"), "", "provider"},
		{"wrapped deadline", fmt.Errorf("gen: %w", context.DeadlineExceeded), "", "timeout"},
	}
	// sanity: assert timeoutNetErr satisfies net.Error
	var _ net.Error = (*timeoutNetErr)(nil)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := categorizeImageGenFailure(tc.err, tc.refPath)
			if got != tc.want {
				t.Fatalf("categorizeImageGenFailure(%v, %q) = %q, want %q", tc.err, tc.refPath, got, tc.want)
			}
		})
	}
}

func TestClassifyImageToolFailureProviderTimeout(t *testing.T) {
	parentCtx := context.Background()
	operationCtx, cancel := context.WithTimeout(parentCtx, time.Minute)
	defer cancel()
	failure := classifyImageToolFailure(parentCtx, operationCtx, context.DeadlineExceeded, "generate", time.Minute, false)
	if failure.Code != "provider_timeout" {
		t.Fatalf("failure code = %q, want provider_timeout", failure.Code)
	}
}
