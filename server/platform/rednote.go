package platform

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// RednoteProvider fetches profile data from Xiaohongshu (小红书).
// It uses direct HTTP requests to scrape public profile pages.
type RednoteProvider struct {
	client     *http.Client
	noRedirect *http.Client
}

// NewRednoteProvider creates a new Xiaohongshu platform provider.
func NewRednoteProvider() *RednoteProvider {
	return &RednoteProvider{
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
		noRedirect: &http.Client{
			Timeout:   10 * time.Second,
			Transport: http.DefaultTransport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

var rednoteURLPattern = regexp.MustCompile(`^https?://((www\.)?xiaohongshu\.com|xhslink\.com)/`)
var xhslinkPattern = regexp.MustCompile(`^https?://xhslink\.com/`)

// allowedHosts is the set of hosts permitted after redirect resolution.
var allowedHosts = map[string]bool{
	"xiaohongshu.com":      true,
	"www.xiaohongshu.com":  true,
}

// FetchProfile fetches a Xiaohongshu user's profile from their public profile page.
func (p *RednoteProvider) FetchProfile(ctx context.Context, profileURL string) (*PlatformProfile, error) {
	if profileURL == "" {
		return nil, fmt.Errorf("profile URL is required")
	}

	if !rednoteURLPattern.MatchString(profileURL) {
		return nil, fmt.Errorf("invalid xiaohongshu profile URL: %s", profileURL)
	}

	// Resolve short links (xhslink.com) to their final xiaohongshu.com URL.
	fetchURL := profileURL
	if xhslinkPattern.MatchString(profileURL) {
		resolved, err := p.resolveRedirect(ctx, profileURL)
		if err != nil {
			return nil, fmt.Errorf("resolve short link: %w", err)
		}
		parsed, err := url.Parse(resolved)
		if err != nil || !allowedHosts[parsed.Host] {
			return nil, fmt.Errorf("short link resolved to disallowed host: %s", resolved)
		}
		fetchURL = resolved
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// Mimic a browser request to avoid basic blocking.
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch profile page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	html := string(body)
	return p.parseProfile(html), nil
}

// resolveRedirect follows HTTP redirects and returns the final URL.
func (p *RednoteProvider) resolveRedirect(ctx context.Context, shortURL string) (string, error) {
	current := shortURL
	for range 10 {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, current, nil)
		if err != nil {
			return "", fmt.Errorf("create redirect request: %w", err)
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

		resp, err := p.noRedirect.Do(req)
		if err != nil {
			return "", fmt.Errorf("follow redirect: %w", err)
		}
		resp.Body.Close()

		if resp.StatusCode < 300 || resp.StatusCode >= 400 {
			return current, nil
		}
		loc := resp.Header.Get("Location")
		if loc == "" {
			return current, nil
		}
		// Resolve relative URLs using standard URL resolution.
		if loc != "" {
			base, parseErr := url.Parse(current)
			if parseErr != nil {
				return current, nil
			}
			resolved, resolveErr := base.Parse(loc)
			if resolveErr != nil {
				return current, nil
			}
			loc = resolved.String()
		}
		current = loc
	}
	return current, nil
}

// parseProfile extracts profile data from Xiaohongshu HTML.
// Xiaohongshu embeds user data in script tags as JSON.
func (p *RednoteProvider) parseProfile(html string) *PlatformProfile {
	profile := &PlatformProfile{
		RawData: make(map[string]any),
	}

	// Try to extract from __INITIAL_STATE__ or similar embedded JSON.
	// Xiaohongshu stores user info in script tags.
	// Pattern: look for nickname, avatar, and desc in the page source.

	// Extract nickname.
	if name := extractBetween(html, `"nickname":"`, `"`); name != "" {
		profile.Name = name
	}
	// Fallback: try title tag.
	if profile.Name == "" {
		if title := extractBetween(html, `<title>`, `</title>`); title != "" {
			title = strings.TrimSpace(title)
			// Title format is usually "小红书 - 用户名的主页"
			if before, _, found := strings.Cut(title, " - "); found {
				profile.Name = strings.TrimSpace(before)
			}
		}
	}

	// Extract avatar URL.
	if avatar := extractBetween(html, `"avatar":"`, `"`); avatar != "" {
		// Unescape URL encoding.
		avatar = strings.ReplaceAll(avatar, `\u002F`, "/")
		profile.AvatarURL = avatar
	}

	// Extract description / positioning.
	if desc := extractBetween(html, `"desc":"`, `"`); desc != "" {
		profile.Positioning = desc
	}

	return profile
}

// extractBetween extracts the substring between two delimiters.
func extractBetween(s, start, end string) string {
	_, after, found := strings.Cut(s, start)
	if !found {
		return ""
	}
	result, _, found := strings.Cut(after, end)
	if !found {
		return ""
	}
	return result
}
