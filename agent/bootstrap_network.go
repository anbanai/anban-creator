package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
)

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
			if err != nil || len(addresses) == 0 {
				return nil, fmt.Errorf("resolve bootstrap download host")
			}
		}
		for _, address := range addresses {
			if !safeBootstrapIP(address.IP) {
				return nil, fmt.Errorf("bootstrap download host resolved to an unsafe address")
			}
		}
		if dial == nil {
			return nil, fmt.Errorf("bootstrap network dialer is unavailable")
		}
		return dial(ctx, network, net.JoinHostPort(addresses[0].IP.String(), port))
	}
}

func safeBootstrapIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	_, shared, _ := net.ParseCIDR("100.64.0.0/10")
	return !shared.Contains(ip)
}
