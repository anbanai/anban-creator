package platform

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// RednoteProvider fetches profile data from Xiaohongshu (小红书).
// It uses direct HTTP requests to scrape public profile pages.
type RednoteProvider struct {
	client *http.Client
}

// NewRednoteProvider creates a new Xiaohongshu platform provider.
func NewRednoteProvider() *RednoteProvider {
	return &RednoteProvider{
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// FetchProfile fetches a Xiaohongshu user's profile from their public profile page.
func (p *RednoteProvider) FetchProfile(ctx context.Context, profileURL string) (*PlatformProfile, error) {
	if profileURL == "" {
		return nil, fmt.Errorf("profile URL is required")
	}

	// Validate URL pattern.
	matched, _ := regexp.MatchString(`^https?://(www\.)?xiaohongshu\.com/user/profile/`, profileURL)
	if !matched {
		return nil, fmt.Errorf("invalid xiaohongshu profile URL: %s", profileURL)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, profileURL, nil)
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

// parseProfile extracts profile data from Xiaohongshu HTML.
// Xiaohongshu embeds user data in script tags as JSON.
func (p *RednoteProvider) parseProfile(html string) *PlatformProfile {
	profile := &PlatformProfile{
		RawData: make(map[string]interface{}),
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
			if idx := strings.Index(title, " - "); idx >= 0 {
				profile.Name = strings.TrimSpace(title[:idx])
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
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	i += len(start)
	j := strings.Index(s[i:], end)
	if j < 0 {
		return ""
	}
	return s[i : i+j]
}
