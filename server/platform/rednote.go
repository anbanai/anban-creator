package platform

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
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

var rednoteURLPattern = regexp.MustCompile(`^https?://((m\.|www\.)?xiaohongshu\.com|xhslink\.com)/`)
var rednoteURLInTextPattern = regexp.MustCompile(`https?://((m\.|www\.)?xiaohongshu\.com|xhslink\.com)/[^\s"'<>，。！？；、]+`)
var xhslinkPattern = regexp.MustCompile(`^https?://xhslink\.com/`)
var rednoteAvatarImgPattern = regexp.MustCompile(`<img[^>]+src=["']([^"']+)["'][^>]*>`)
var rednoteAnchorPattern = regexp.MustCompile(`(?is)<a[^>]+href=["']([^"']*(?:/explore/|/discovery/item/)[^"']*)["'][^>]*>(.*?)</a>`)
var rednoteTagPattern = regexp.MustCompile(`(?is)<[^>]+>`)
var rednoteImgSrcPattern = regexp.MustCompile(`(?is)<img[^>]+src=["']([^"']+)["']`)
var rednoteNumberPattern = regexp.MustCompile(`\d+(?:,\d{3})*(?:\.\d+)?`)

const rednoteTopPostLimit = 5
const rednoteMaxProfileBytes = 2 * 1024 * 1024

const rednoteBrowserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// allowedHosts is the set of hosts permitted after redirect resolution.
var allowedHosts = map[string]bool{
	"m.xiaohongshu.com":   true,
	"xiaohongshu.com":     true,
	"www.xiaohongshu.com": true,
}

// FetchProfile fetches a Xiaohongshu user's profile from their public profile page.
func (p *RednoteProvider) FetchProfile(ctx context.Context, profileURL string) (*PlatformProfile, error) {
	if profileURL == "" {
		return nil, fmt.Errorf("profile URL is required")
	}

	extractedURL, err := extractRednoteURLFromText(profileURL)
	if err != nil {
		return nil, err
	}

	// Resolve short links (xhslink.com) to their final xiaohongshu.com URL.
	fetchURL := extractedURL
	if xhslinkPattern.MatchString(extractedURL) {
		resolved, err := p.resolveRedirect(ctx, extractedURL)
		if err != nil {
			return nil, fmt.Errorf("resolve short link: %w", err)
		}
		parsed, err := url.Parse(resolved)
		if err != nil ||
			(parsed.Scheme != "http" && parsed.Scheme != "https") ||
			!allowedHosts[strings.ToLower(parsed.Hostname())] {
			return nil, fmt.Errorf("short link resolved to disallowed host: %s", resolved)
		}
		fetchURL = resolved
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	setRednoteHeaders(req)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch profile page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, rednoteMaxProfileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if len(body) > rednoteMaxProfileBytes {
		return nil, fmt.Errorf("profile page is too large")
	}

	html := string(body)
	profile := p.parseProfile(html)
	profile.RawData["source_text"] = profileURL
	profile.RawData["profile_url"] = extractedURL
	profile.RawData["resolved_profile_url"] = fetchURL
	return profile, nil
}

// resolveRedirect follows HTTP redirects and returns the final URL.
func (p *RednoteProvider) resolveRedirect(ctx context.Context, shortURL string) (string, error) {
	current := shortURL
	for range 10 {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, current, nil)
		if err != nil {
			return "", fmt.Errorf("create redirect request: %w", err)
		}
		setRednoteHeaders(req)

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

func setRednoteHeaders(req *http.Request) {
	// Xiaohongshu short links are sensitive to very bare requests. Use a normal
	// browser-like GET flow instead of HEAD-style probing.
	req.Header.Set("User-Agent", rednoteBrowserUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Referer", "https://www.xiaohongshu.com/")
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
			if before, after, found := strings.Cut(title, " - "); found {
				switch strings.TrimSpace(before) {
				case "小红书":
					profile.Name = strings.TrimSuffix(strings.TrimSpace(after), "的主页")
				default:
					profile.Name = strings.TrimSpace(before)
				}
				if profile.Name == "小红书" {
					profile.Name = ""
				}
			} else if before, _, found := strings.Cut(title, "的主页"); found {
				if strings.TrimSpace(before) != "小红书" {
					profile.Name = strings.TrimSpace(before)
				}
			}
		}
	}

	// Extract avatar URL.
	if avatar := extractBetween(html, `"avatar":"`, `"`); avatar != "" {
		// Unescape URL encoding.
		avatar = strings.ReplaceAll(avatar, `\u002F`, "/")
		profile.AvatarURL = avatar
	}
	if profile.AvatarURL == "" {
		for _, match := range rednoteAvatarImgPattern.FindAllStringSubmatch(html, -1) {
			if len(match) < 2 {
				continue
			}
			if strings.Contains(match[1], "sns-avatar") || strings.Contains(match[1], "avatar") {
				profile.AvatarURL = strings.ReplaceAll(match[1], `\u002F`, "/")
				break
			}
		}
	}

	// Extract description / positioning.
	if desc := extractBetween(html, `"desc":"`, `"`); desc != "" {
		profile.Positioning = desc
	}

	posts := parseRednotePosts(html)
	topPosts := selectTopRednotePosts(posts, rednoteTopPostLimit)
	profile.RawData["posts"] = posts
	profile.RawData["top_posts"] = topPosts

	return profile
}

func extractRednoteURLFromText(text string) (string, error) {
	text = strings.TrimSpace(text)
	match := rednoteURLInTextPattern.FindString(text)
	if match == "" {
		return "", fmt.Errorf("profile text must contain a supported xiaohongshu URL")
	}
	return sanitizeExtractedRednoteURL(match), nil
}

func sanitizeExtractedRednoteURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), ".,;:!?)]}）】。！？；，、")
}

func parseRednotePosts(html string) []RednotePost {
	matches := rednoteAnchorPattern.FindAllStringSubmatch(html, -1)
	posts := make([]RednotePost, 0, len(matches))
	seen := map[string]bool{}
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		postURL := normalizeRednotePostURL(match[1])
		if postURL == "" || seen[postURL] {
			continue
		}
		seen[postURL] = true
		fragment := match[2]
		text := normalizeRednoteText(rednoteTagPattern.ReplaceAllString(fragment, " "))
		title := extractRednoteTitle(text)
		if title == "" {
			continue
		}
		post := RednotePost{
			Title:        title,
			URL:          postURL,
			CoverURL:     extractRednoteCover(fragment),
			LikeCount:    parseMetricAfterLabels(text, "点赞", "赞", "喜欢", "like"),
			CollectCount: parseMetricAfterLabels(text, "收藏", "collect"),
			CommentCount: parseMetricAfterLabels(text, "评论", "comment"),
			ShareCount:   parseMetricAfterLabels(text, "分享", "share"),
		}
		post.EngagementScore = post.LikeCount + post.CollectCount + post.CommentCount + post.ShareCount
		posts = append(posts, post)
	}
	return posts
}

func normalizeRednotePostURL(raw string) string {
	raw = strings.ReplaceAll(raw, `\u002F`, "/")
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	if strings.HasPrefix(raw, "/") {
		return "https://www.xiaohongshu.com" + raw
	}
	return ""
}

func extractRednoteCover(fragment string) string {
	match := rednoteImgSrcPattern.FindStringSubmatch(fragment)
	if len(match) < 2 {
		return ""
	}
	return strings.ReplaceAll(match[1], `\u002F`, "/")
}

func normalizeRednoteText(text string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(text, `\u002F`, "/")), " ")
}

func extractRednoteTitle(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	for _, label := range []string{"点赞", "赞", "喜欢", "收藏", "评论", "分享", "like", "collect", "comment", "share"} {
		if before, _, found := strings.Cut(text, label); found {
			text = strings.TrimSpace(before)
			break
		}
	}
	if len([]rune(text)) > 80 {
		text = string([]rune(text)[:80])
	}
	return text
}

func parseMetricAfterLabels(text string, labels ...string) int {
	lower := strings.ToLower(text)
	for _, label := range labels {
		idx := strings.Index(lower, strings.ToLower(label))
		if idx < 0 {
			continue
		}
		after := text[idx+len(label):]
		if len([]rune(after)) > 20 {
			after = string([]rune(after)[:20])
		}
		if n := parseRednoteCount(after); n > 0 {
			return n
		}
	}
	return 0
}

func parseRednoteCount(text string) int {
	text = strings.ReplaceAll(strings.TrimSpace(text), ",", "")
	if text == "" {
		return 0
	}
	match := rednoteNumberPattern.FindString(text)
	if match == "" {
		return 0
	}
	value, err := strconv.ParseFloat(match, 64)
	if err != nil {
		return 0
	}
	switch {
	case strings.Contains(text, "万"):
		value *= 10000
	case strings.Contains(text, "千"):
		value *= 1000
	}
	return int(value)
}

func selectTopRednotePosts(posts []RednotePost, limit int) []RednotePost {
	if limit <= 0 || len(posts) == 0 {
		return nil
	}
	ranked := make([]RednotePost, len(posts))
	copy(ranked, posts)
	for i := range ranked {
		ranked[i].EngagementScore = ranked[i].LikeCount + ranked[i].CollectCount + ranked[i].CommentCount + ranked[i].ShareCount
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].EngagementScore > ranked[j].EngagementScore
	})
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return ranked
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
