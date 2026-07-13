package main

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"
)

type sequenceResolver struct {
	answers [][]net.IPAddr
	calls   int
}

func (r *sequenceResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	if r.calls >= len(r.answers) {
		return nil, errors.New("no answer")
	}
	answer := r.answers[r.calls]
	r.calls++
	return answer, nil
}

func TestValidateBootstrapDownloadURLRejectsUnsafeDestinations(t *testing.T) {
	for _, raw := range []string{
		"http://example.com/file",
		"https://user@example.com/file",
		"https://example.com/file#fragment",
		"https://169.254.169.254/latest/meta-data",
		"https://127.0.0.1/file",
		"https://10.0.0.1/file",
		"https://192.168.1.1/file",
		"https://[::1]/file",
		"https://[fc00::1]/file",
		"https://[fe80::1]/file",
	} {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateBootstrapDownloadURL(parsed, false); err == nil {
			t.Fatalf("unsafe URL %q accepted", raw)
		}
	}
}

func TestBootstrapSecureDialRejectsUnsafeDNSAndRebinding(t *testing.T) {
	public := net.IPAddr{IP: net.ParseIP("93.184.216.34")}
	private := net.IPAddr{IP: net.ParseIP("10.0.0.8")}
	loopback := net.IPAddr{IP: net.ParseIP("127.0.0.1")}
	tests := []struct {
		name    string
		answers [][]net.IPAddr
		calls   int
	}{
		{name: "localhost", answers: [][]net.IPAddr{{loopback}}},
		{name: "mixed", answers: [][]net.IPAddr{{public, private}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dialed := 0
			dial := bootstrapSecureDialContext(&sequenceResolver{answers: tc.answers}, func(context.Context, string, string) (net.Conn, error) { dialed++; return nil, errors.New("dialed") })
			if _, err := dial(context.Background(), "tcp", "example.test:443"); err == nil || dialed != 0 {
				t.Fatalf("err=%v dialed=%d", err, dialed)
			}
		})
	}

	dialed := 0
	resolver := &sequenceResolver{answers: [][]net.IPAddr{{public}, {private}}}
	dial := bootstrapSecureDialContext(resolver, func(context.Context, string, string) (net.Conn, error) {
		dialed++
		return nil, errors.New("public dial reached")
	})
	_, _ = dial(context.Background(), "tcp", "rebind.test:443")
	if _, err := dial(context.Background(), "tcp", "rebind.test:443"); err == nil || dialed != 1 {
		t.Fatalf("rebind err=%v dialed=%d", err, dialed)
	}
}

func TestBootstrapSecureDialTriesValidatedAddressesUntilSuccess(t *testing.T) {
	first := net.IPAddr{IP: net.ParseIP("93.184.216.34")}
	second := net.IPAddr{IP: net.ParseIP("93.184.216.35")}
	resolver := &sequenceResolver{answers: [][]net.IPAddr{{first, second}}}
	var dialed []string
	dial := bootstrapSecureDialContext(resolver, func(_ context.Context, _ string, address string) (net.Conn, error) {
		dialed = append(dialed, address)
		if len(dialed) == 1 {
			return nil, errors.New("first unavailable")
		}
		return bootstrapTestConn(t), nil
	})

	conn, err := dial(context.Background(), "tcp", "example.test:443")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.Close()
	want := []string{"93.184.216.34:443", "93.184.216.35:443"}
	if len(dialed) != len(want) || dialed[0] != want[0] || dialed[1] != want[1] {
		t.Fatalf("dialed = %v, want %v", dialed, want)
	}
	if resolver.calls != 1 {
		t.Fatalf("resolver calls = %d, want one validated resolution", resolver.calls)
	}
}

func TestBootstrapSecureDialBoundsEachAddressAttempt(t *testing.T) {
	first := net.IPAddr{IP: net.ParseIP("93.184.216.34")}
	second := net.IPAddr{IP: net.ParseIP("93.184.216.35")}
	var attempts int
	dial := bootstrapSecureDialContext(&sequenceResolver{answers: [][]net.IPAddr{{first, second}}}, func(ctx context.Context, _ string, _ string) (net.Conn, error) {
		attempts++
		if attempts == 1 {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return bootstrapTestConn(t), nil
	})
	overallCtx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	started := time.Now()
	conn, err := dial(overallCtx, "tcp", "example.test:443")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.Close()
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if elapsed := time.Since(started); elapsed >= 350*time.Millisecond {
		t.Fatalf("first address consumed overall dial budget: %v", elapsed)
	}
	if overallCtx.Err() != nil {
		t.Fatalf("overall context expired before failover: %v", overallCtx.Err())
	}
}

func TestBootstrapSecureDialReportsSanitizedFailureAfterAllAddresses(t *testing.T) {
	addresses := []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}, {IP: net.ParseIP("93.184.216.35")}}
	attempts := 0
	dial := bootstrapSecureDialContext(&sequenceResolver{answers: [][]net.IPAddr{addresses}}, func(context.Context, string, string) (net.Conn, error) {
		attempts++
		return nil, errors.New("provider detail with credentials")
	})

	_, err := dial(context.Background(), "tcp", "example.test:443")
	if err == nil || attempts != 2 {
		t.Fatalf("err=%v attempts=%d, want sanitized failure after two attempts", err, attempts)
	}
	if strings.Contains(err.Error(), "credentials") || strings.Contains(err.Error(), "93.184") {
		t.Fatalf("dial error leaked internal detail: %v", err)
	}
}

func TestBootstrapSecureDialPreservesParentCancellation(t *testing.T) {
	addresses := []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}, {IP: net.ParseIP("93.184.216.35")}}
	attempts := 0
	dial := bootstrapSecureDialContext(&sequenceResolver{answers: [][]net.IPAddr{addresses}}, func(ctx context.Context, _ string, _ string) (net.Conn, error) {
		attempts++
		<-ctx.Done()
		return nil, ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)

	_, err := dial(ctx, "tcp", "example.test:443")
	if !errors.Is(err, context.Canceled) || attempts != 1 {
		t.Fatalf("err=%v attempts=%d, want parent cancellation after first attempt", err, attempts)
	}
}

func TestBootstrapSecureDialRejectsMixedUnsafeSetBeforeDial(t *testing.T) {
	addresses := []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}, {IP: net.ParseIP("10.0.0.8")}}
	attempts := 0
	dial := bootstrapSecureDialContext(&sequenceResolver{answers: [][]net.IPAddr{addresses}}, func(context.Context, string, string) (net.Conn, error) {
		attempts++
		return nil, errors.New("must not dial")
	})

	if _, err := dial(context.Background(), "tcp", "example.test:443"); err == nil || attempts != 0 {
		t.Fatalf("err=%v attempts=%d, unsafe mixed set reached dialer", err, attempts)
	}
}

func bootstrapTestConn(t *testing.T) net.Conn {
	t.Helper()
	client, server := net.Pipe()
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})
	return client
}
