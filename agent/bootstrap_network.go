package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

const bootstrapDialAttemptTimeout = 10 * time.Second

type bootstrapIPResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type bootstrapDialFunc func(context.Context, string, string) (net.Conn, error)

type loopbackTestTransport struct{ base http.RoundTripper }

func (t loopbackTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.base.RoundTrip(req)
}

func (loopbackTestTransport) allowsHTTPLoopback() bool { return true }

func bootstrapClientAllowsHTTPLoopback(client *http.Client) bool {
	if client == nil || client.Transport == nil {
		return false
	}
	allowed, ok := client.Transport.(interface{ allowsHTTPLoopback() bool })
	return ok && allowed.allowsHTTPLoopback()
}

func newBootstrapDownloadClient(resolver bootstrapIPResolver, dial bootstrapDialFunc) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = bootstrapSecureDialContext(resolver, dial)
	return &http.Client{
		Timeout:       bootstrapDownloadTimeout,
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func bootstrapSecureDialContext(resolver bootstrapIPResolver, dial bootstrapDialFunc) bootstrapDialFunc {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || strings.TrimSpace(host) == "" || strings.Contains(host, "%") {
			return nil, fmt.Errorf("invalid bootstrap download address")
		}
		var addresses []net.IPAddr
		if literal := net.ParseIP(host); literal != nil {
			addresses = []net.IPAddr{{IP: literal}}
		} else {
			if resolver == nil {
				return nil, fmt.Errorf("bootstrap DNS resolver is unavailable")
			}
			addresses, err = resolver.LookupIPAddr(ctx, host)
			if cause := context.Cause(ctx); cause != nil {
				return nil, cause
			}
			if err != nil || len(addresses) == 0 {
				return nil, fmt.Errorf("resolve bootstrap download host")
			}
		}
		for _, address := range addresses {
			if cause := context.Cause(ctx); cause != nil {
				return nil, cause
			}
			if !safeBootstrapIP(address.IP) {
				return nil, fmt.Errorf("bootstrap download host resolved to an unsafe address")
			}
		}
		if dial == nil {
			return nil, fmt.Errorf("bootstrap network dialer is unavailable")
		}
		for i, address := range addresses {
			if cause := context.Cause(ctx); cause != nil {
				return nil, cause
			}
			attemptCtx, cancel := bootstrapDialAttemptContext(ctx, len(addresses)-i)
			// Dial the validated address directly. The HTTP transport still uses
			// the request hostname for TLS SNI and certificate verification.
			conn, dialErr := dial(attemptCtx, network, net.JoinHostPort(address.IP.String(), port))
			cancel()
			if dialErr == nil && conn != nil {
				return conn, nil
			}
			if cause := context.Cause(ctx); cause != nil {
				return nil, cause
			}
		}
		return nil, fmt.Errorf("dial bootstrap download host: all %d validated addresses failed", len(addresses))
	}
}

func bootstrapDialAttemptContext(ctx context.Context, remainingAddresses int) (context.Context, context.CancelFunc) {
	timeout := bootstrapDialAttemptTimeout
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remainingAddresses > 0 {
			remaining /= time.Duration(remainingAddresses)
		}
		if remaining < timeout {
			timeout = remaining
		}
	}
	if timeout < time.Nanosecond {
		timeout = time.Nanosecond
	}
	return context.WithTimeout(ctx, timeout)
}

func safeBootstrapIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	_, shared, _ := net.ParseCIDR("100.64.0.0/10")
	return !shared.Contains(ip)
}
