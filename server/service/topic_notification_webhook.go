package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const (
	topicNotificationHTTPTimeout      = 10 * time.Second
	topicNotificationDialTimeout      = 5 * time.Second
	topicNotificationDNSLookupTimeout = 2 * time.Second
	topicNotificationMaxResponseBody  = 64 << 10
)

func newTopicNotificationHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: topicNotificationDialTimeout}
	transport := &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		DialContext:           topicNotificationDialContext(dialer),
		TLSHandshakeTimeout:   topicNotificationDialTimeout,
		ResponseHeaderTimeout: topicNotificationHTTPTimeout,
	}
	return &http.Client{
		Timeout:   topicNotificationHTTPTimeout,
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func validateTopicNotificationWebhookURL(ctx context.Context, raw string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.Host == "" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || strings.Contains(raw, "#") || strings.ToLower(u.Scheme) != "https" {
		return errors.New("webhook URL must be an absolute HTTPS URL without credentials or fragments")
	}
	host := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(u.Hostname())), ".")
	if host == "" || strings.ContainsAny(host, "\r\n") {
		return errors.New("webhook URL host is invalid")
	}
	if err := validateTopicNotificationWebhookHost(ctx, host); err != nil {
		return err
	}
	return nil
}

func validateTopicNotificationWebhookHost(ctx context.Context, host string) error {
	addresses, err := lookupTopicNotificationAddresses(ctx, host)
	if err != nil {
		return fmt.Errorf("webhook URL host could not be resolved: %w", err)
	}
	if len(addresses) == 0 {
		return errors.New("webhook URL host could not be resolved")
	}
	for _, address := range addresses {
		if !isPublicAddr(address) {
			return errors.New("webhook URL host resolves to a non-public address")
		}
	}
	return nil
}

func lookupTopicNotificationAddresses(ctx context.Context, host string) ([]netip.Addr, error) {
	if parsed := net.ParseIP(host); parsed != nil {
		address, ok := netip.AddrFromSlice(parsed)
		if !ok {
			return nil, errors.New("invalid IP address")
		}
		return []netip.Addr{address}, nil
	}
	lookupCtx, cancel := context.WithTimeout(ctx, topicNotificationDNSLookupTimeout)
	defer cancel()
	resolved, err := net.DefaultResolver.LookupIPAddr(lookupCtx, host)
	if err != nil {
		return nil, err
	}
	addresses := make([]netip.Addr, 0, len(resolved))
	for _, item := range resolved {
		address, ok := netip.AddrFromSlice(item.IP)
		if !ok {
			return nil, errors.New("resolver returned an invalid IP address")
		}
		addresses = append(addresses, address)
	}
	return addresses, nil
}

func topicNotificationDialContext(dialer *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("invalid webhook address: %w", err)
		}
		addresses, err := lookupTopicNotificationAddresses(ctx, strings.Trim(host, "[]"))
		if err != nil {
			return nil, fmt.Errorf("resolve webhook host: %w", err)
		}
		for _, candidate := range addresses {
			if !isPublicAddr(candidate) {
				return nil, errors.New("webhook URL host resolves to a non-public address")
			}
		}
		var lastErr error
		for _, candidate := range addresses {
			conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			lastErr = dialErr
		}
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, errors.New("webhook host did not resolve")
	}
}
