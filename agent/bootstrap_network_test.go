package main

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"
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
