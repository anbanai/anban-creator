package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	serverconfig "github.com/anbanai/anban-creator/server/config"
)

var ErrPageFetchUnavailable = errors.New("page fetch provider unavailable")

// ValidatePublicHTTPURL rejects URLs that could make a provider or a server
// side fetcher access internal services. DNS is resolved before use and all
// resolved addresses must be globally routable.
func ValidatePublicHTTPURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("url must be an absolute HTTP(S) URL without credentials")
	}
	host := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(u.Hostname())), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return errors.New("url host is not public")
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if !isPublicAddr(ip) {
			return errors.New("url host is not public")
		}
		return nil
	}
	if strings.Contains(host, "..") || strings.Contains(host, "/") {
		return errors.New("url host is invalid")
	}
	addrs, err := net.LookupIP(host)
	if err != nil || len(addrs) == 0 {
		return errors.New("url host could not be resolved")
	}
	for _, addr := range addrs {
		parsed, ok := netip.AddrFromSlice(addr)
		if !ok || !isPublicAddr(parsed) {
			return errors.New("url host resolves to a non-public address")
		}
	}
	return nil
}

func isPublicAddr(addr netip.Addr) bool {
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsUnspecified() || addr.IsMulticast() || addr.IsInterfaceLocalMulticast() {
		return false
	}
	if v4 := addr.Unmap(); v4.Is4() {
		bytes := v4.As4()
		first, second := bytes[0], bytes[1]
		if first == 0 || first == 10 || first == 127 || (first == 100 && second >= 64 && second <= 127) || (first == 169 && second == 254) || (first == 192 && (second == 0 || second == 168)) || (first == 198 && (second == 18 || second == 19 || second == 51)) || (first == 203 && second == 0) || first >= 224 {
			return false
		}
	}
	return true
}

func safeFetchHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	dialer := &net.Dialer{Timeout: timeout}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			if err := ValidatePublicHTTPURL("http://" + net.JoinHostPort(host, port)); err != nil {
				return nil, err
			}
			ips, err := net.LookupIP(host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				parsed, ok := netip.AddrFromSlice(ip)
				if !ok || !isPublicAddr(parsed) {
					continue
				}
				conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(parsed.String(), port))
				if dialErr == nil {
					return conn, nil
				}
			}
			return nil, errors.New("no public address available for host")
		},
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many redirects")
			}
			if err := ValidatePublicHTTPURL(req.URL.String()); err != nil {
				return err
			}
			return nil
		},
	}
}

type DeepSeekFetchProvider struct {
	client   *http.Client
	endpoint string
	apiKey   string
}

func (p *DeepSeekFetchProvider) Name() string {
	if p == nil {
		return ""
	}
	return "deepseek"
}

func NewDeepSeekFetchProvider(cfg serverconfig.SearchDeepSeekConfig) *DeepSeekFetchProvider {
	endpoint := cfg.FetchEndpoint
	if endpoint == "" {
		endpoint = "https://api.deepseek.com/v1/tools/fetch"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &DeepSeekFetchProvider{client: &http.Client{Timeout: timeout, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, endpoint: strings.TrimSpace(endpoint), apiKey: strings.TrimSpace(cfg.APIKey)}
}

func (p *DeepSeekFetchProvider) Fetch(ctx context.Context, req PageFetchRequest) (*PageFetchResponse, error) {
	if p == nil || p.client == nil || p.endpoint == "" || p.apiKey == "" {
		return nil, ErrPageFetchUnavailable
	}
	if err := ValidatePublicHTTPURL(req.URL); err != nil {
		return nil, fmt.Errorf("invalid page URL: %w", err)
	}
	payload, err := json.Marshal(map[string]string{"url": strings.TrimSpace(req.URL)})
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: upstream request failed", ErrPageFetchUnavailable)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20+1))
	if err != nil || len(data) > 8<<20 {
		return nil, fmt.Errorf("%w: response too large or unreadable", ErrPageFetchUnavailable)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: upstream status %d", ErrPageFetchUnavailable, resp.StatusCode)
	}
	if contentType := strings.ToLower(resp.Header.Get("Content-Type")); contentType != "" && !strings.Contains(contentType, "application/json") {
		return nil, fmt.Errorf("%w: invalid response content type", ErrPageFetchUnavailable)
	}
	var raw struct {
		Title    string `json:"title"`
		URL      string `json:"url"`
		Content  string `json:"content"`
		Markdown string `json:"markdown"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("%w: invalid response", ErrPageFetchUnavailable)
	}
	content := raw.Markdown
	if content == "" {
		content = raw.Content
	}
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("%w: response has no markdown content", ErrPageFetchUnavailable)
	}
	pageURL := strings.TrimSpace(raw.URL)
	if pageURL == "" {
		pageURL = strings.TrimSpace(req.URL)
	} else if err := ValidatePublicHTTPURL(pageURL); err != nil {
		return nil, fmt.Errorf("%w: invalid response URL", ErrPageFetchUnavailable)
	}
	return &PageFetchResponse{Title: raw.Title, URL: pageURL, ContentMarkdown: content, FetchedAt: time.Now().UTC(), Provider: "deepseek", ProviderRequestID: firstHeader(resp.Header, "X-Request-ID", "Request-ID"), ProviderMetadata: map[string]any{"api": "fetch"}}, nil
}
