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
var rednoteAnchorPattern = regexp.MustCompile(`(?is)<a[^>]+href=["']([^"']*(?:/explore/|/discovery/item/)[^"']*)["'][^>]*>(.*?)</a>`)
var rednoteTagPattern = regexp.MustCompile(`(?is)<[^>]+>`)
var rednoteImgSrcPattern = regexp.MustCompile(`(?is)<img[^>]+src=["']([^"']+)["']`)
var rednoteNumberPattern = regexp.MustCompile(`\d+(?:,\d{3})*(?:\.\d+)?`)
var rednoteMetricElementPattern = regexp.MustCompile(`(?is)<(?:div|span|button|li|section|footer|script)[^>]*>(.*?)</(?:div|span|button|li|section|footer|script)>`)
var rednoteNoteIDPatterns = []*regexp.Regexp{
	regexp.MustCompile(`/explore/([^/?#]+)`),
	regexp.MustCompile(`/discovery/item/([^/?#]+)`),
}

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

// FetchProfilePosts fetches visible public posts from a Xiaohongshu profile.
func (p *RednoteProvider) FetchProfilePosts(ctx context.Context, profileURL string) ([]RednotePost, error) {
	profile, err := p.FetchProfile(ctx, profileURL)
	if err != nil {
		return nil, err
	}
	posts, ok := profile.RawData["posts"].([]RednotePost)
	if !ok {
		return []RednotePost{}, nil
	}
	return posts, nil
}

// FetchPostMetrics fetches public metrics from a Xiaohongshu note page.
func (p *RednoteProvider) FetchPostMetrics(ctx context.Context, noteURL string) (*RednotePostMetrics, error) {
	noteURL = strings.TrimSpace(noteURL)
	if noteURL == "" {
		return nil, fmt.Errorf("note URL is required")
	}
	fetchURL, err := p.resolveRednoteNoteURL(ctx, noteURL)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	setRednoteHeaders(req)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch note page: %w", err)
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
		return nil, fmt.Errorf("note page is too large")
	}

	metrics := parseRednotePostMetrics(string(body))
	return &metrics, nil
}

func (p *RednoteProvider) resolveRednoteNoteURL(ctx context.Context, noteURL string) (string, error) {
	fetchURL := noteURL
	if xhslinkPattern.MatchString(noteURL) {
		resolved, err := p.resolveRedirect(ctx, noteURL)
		if err != nil {
			return "", fmt.Errorf("resolve short link: %w", err)
		}
		fetchURL = strings.TrimSpace(resolved)
	}
	if !isSupportedRednoteNoteURL(fetchURL) {
		return "", fmt.Errorf("unsupported Rednote note URL: %s", fetchURL)
	}
	return fetchURL, nil
}

func isSupportedRednoteNoteURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	if !allowedHosts[strings.ToLower(parsed.Hostname())] {
		return false
	}
	return ExtractRednoteNoteID(parsed.Path) != ""
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


// parseProfile extracts the __INITIAL_STATE__ JSON and post data from Xiaohongshu HTML.
// User identity fields (name, avatar, positioning) are extracted by AI downstream.
func (p *RednoteProvider) parseProfile(html string) *PlatformProfile {
	profile := &PlatformProfile{
		RawData: make(map[string]any),
	}

	// Extract __INITIAL_STATE__ JSON for AI parsing in the handler layer.
	match := rednoteInitialStatePattern.FindStringSubmatch(html)
	if len(match) >= 2 {
		raw := match[1]
		if idx := strings.Index(raw, ";"); idx > 0 {
			raw = raw[:idx]
		}
		raw = strings.ReplaceAll(raw, "undefined", "null")
		profile.RawData["initial_state"] = raw
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
			NoteID:       ExtractRednoteNoteID(postURL),
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

// RednotePostMetrics holds public engagement counters parsed from a note page.
type RednotePostMetrics struct {
	LikeCount    int  `json:"like_count"`
	CollectCount int  `json:"collect_count"`
	CommentCount int  `json:"comment_count"`
	ShareCount   int  `json:"share_count"`
	ViewCount    *int `json:"view_count,omitempty"`
}

// ExtractRednoteNoteID extracts a Xiaohongshu note ID from public note URLs.
func ExtractRednoteNoteID(raw string) string {
	raw = strings.ReplaceAll(raw, `\u002F`, "/")
	raw = strings.ReplaceAll(raw, `\/`, "/")
	for _, pattern := range rednoteNoteIDPatterns {
		match := pattern.FindStringSubmatch(raw)
		if len(match) >= 2 {
			return strings.TrimSpace(match[1])
		}
	}
	return ""
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
		if n := NormalizeRednoteMetricCount(after); n > 0 {
			return n
		}
	}
	return 0
}

func parseRednoteCount(text string) int {
	return NormalizeRednoteMetricCount(text)
}

// NormalizeRednoteMetricCount converts public Xiaohongshu metric text into an integer count.
func NormalizeRednoteMetricCount(text string) int {
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

func parseRednotePostMetrics(html string) RednotePostMetrics {
	if metrics, ok := parseRednotePostMetricsFromFragments(html); ok {
		return metrics
	}
	text := normalizeRednoteText(rednoteTagPattern.ReplaceAllString(html, " "))
	return RednotePostMetrics{
		LikeCount:    parseMetricAfterLabels(text, "点赞", "赞", "喜欢", "like"),
		CollectCount: parseMetricAfterLabels(text, "收藏", "collect"),
		CommentCount: parseMetricAfterLabels(text, "评论", "comment"),
		ShareCount:   parseMetricAfterLabels(text, "分享", "share"),
		ViewCount:    nil,
	}
}

func parseRednotePostMetricsFromFragments(html string) (RednotePostMetrics, bool) {
	bestText := ""
	bestScore := 0
	for _, match := range rednoteMetricElementPattern.FindAllStringSubmatch(html, -1) {
		if len(match) < 2 {
			continue
		}
		text := normalizeRednoteText(rednoteTagPattern.ReplaceAllString(match[1], " "))
		score := rednoteMetricLabelScore(text)
		if score < 2 || score < bestScore {
			continue
		}
		bestText = text
		bestScore = score
	}
	if bestText == "" {
		return RednotePostMetrics{}, false
	}
	return RednotePostMetrics{
		LikeCount:    parseMetricAfterLabels(bestText, "点赞", "赞", "喜欢", "like"),
		CollectCount: parseMetricAfterLabels(bestText, "收藏", "collect"),
		CommentCount: parseMetricAfterLabels(bestText, "评论", "comment"),
		ShareCount:   parseMetricAfterLabels(bestText, "分享", "share"),
		ViewCount:    nil,
	}, true
}

func rednoteMetricLabelScore(text string) int {
	lower := strings.ToLower(text)
	score := 0
	for _, labels := range [][]string{
		{"点赞", "赞", "喜欢", "like"},
		{"收藏", "collect"},
		{"评论", "comment"},
		{"分享", "share"},
	} {
		for _, label := range labels {
			if strings.Contains(lower, strings.ToLower(label)) {
				score++
				break
			}
		}
	}
	return score
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

// RednoteNoteContent holds parsed content from a Xiaohongshu note page.
type RednoteNoteContent struct {
	NoteID        string `json:"note_id"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	Tags          string `json:"tags"`
	CoverURL      string `json:"cover_url"`
	Type          string `json:"type"` // "normal" (image) or "video"
	LikeCount     int    `json:"like_count"`
	CollectCount  int    `json:"collect_count"`
	CommentCount  int    `json:"comment_count"`
	ShareCount    int    `json:"share_count"`
	AuthorName    string `json:"author_name,omitempty"`
	AuthorID      string `json:"author_id,omitempty"`
	InteractCount int    `json:"interact_count"`
}

// FetchNoteContent fetches and parses a Xiaohongshu note page, extracting
// title, description, tags, cover image, and engagement metrics.
func (p *RednoteProvider) FetchNoteContent(ctx context.Context, noteURL string) (*RednoteNoteContent, error) {
	noteURL = strings.TrimSpace(noteURL)
	if noteURL == "" {
		return nil, fmt.Errorf("note URL is required")
	}
	fetchURL, err := p.resolveRednoteNoteURL(ctx, noteURL)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	setRednoteHeaders(req)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch note page: %w", err)
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
		return nil, fmt.Errorf("note page is too large")
	}

	html := string(body)
	noteID := ExtractRednoteNoteID(fetchURL)

	content := parseRednoteNoteContent(html)
	content.NoteID = noteID
	return content, nil
}


var (
	rednoteInitialStatePattern = regexp.MustCompile(`(?s)window\.__INITIAL_STATE__\s*=\s*(\{.+?\})\s*</script>`)
	rednoteJSONStringPattern    = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
	rednoteNoteTagPattern      = regexp.MustCompile(`#([^\s#]{2,20})`)
)

func parseRednoteNoteContent(html string) *RednoteNoteContent {
	content := &RednoteNoteContent{}

	// Try extracting from __INITIAL_STATE__ JSON first.
	if match := rednoteInitialStatePattern.FindStringSubmatch(html); len(match) >= 2 {
		extractNoteFromInitialState(content, match[1])
	}

	// Fallback: extract title from <title> tag or meta.
	if content.Title == "" {
		if title := extractBetween(html, "<title>", "</title>"); title != "" {
			title = strings.TrimSpace(title)
			if idx := strings.Index(title, " - 小红书"); idx > 0 {
				title = strings.TrimSpace(title[:idx])
			}
			content.Title = title
		}
	}

	// Extract tags from the page text (filter out hex colors).
	if content.Tags == "" {
		allTags := rednoteNoteTagPattern.FindAllStringSubmatch(html, 20)
		tagParts := make([]string, 0, len(allTags))
		seen := map[string]bool{}
		for _, t := range allTags {
			if len(t) >= 2 && !seen[t[1]] && !isHexColor(t[1]) {
				seen[t[1]] = true
				tagParts = append(tagParts, t[1])
			}
		}
		if len(tagParts) > 0 {
			content.Tags = strings.Join(tagParts, ", ")
		}
	}

	// Fallback: extract description using JSON-aware extraction.
	if content.Description == "" {
		if desc := extractJSONStringAfter(html, `"desc":"`); desc != "" {
			content.Description = unescapeJSONString(desc)
		}
	}

	// Fallback: extract cover image.
	if content.CoverURL == "" {
		for _, m := range rednoteImgSrcPattern.FindAllStringSubmatch(html, 5) {
			if len(m) >= 2 && strings.Contains(m[1], "xhscdn") {
				content.CoverURL = strings.ReplaceAll(m[1], `/`, "/")
				break
			}
		}
	}

	// Always try to extract metrics from the page.
	metrics := parseRednotePostMetrics(html)
	content.LikeCount = metrics.LikeCount
	content.CollectCount = metrics.CollectCount
	content.CommentCount = metrics.CommentCount
	content.ShareCount = metrics.ShareCount
	content.InteractCount = content.LikeCount + content.CollectCount + content.CommentCount + content.ShareCount

	return content
}

// isHexColor returns true if the string looks like a CSS hex color code (#fff, #3b82f6).
func isHexColor(s string) bool {
	if len(s) != 4 && len(s) != 7 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func extractNoteFromInitialState(content *RednoteNoteContent, raw string) {
	s := strings.ReplaceAll(raw, "undefined", "null")

	if title := extractJSONStringAfter(s, `"title":"`); title != "" {
		content.Title = unescapeJSONString(title)
	}
	if desc := extractJSONStringAfter(s, `"desc":"`); desc != "" {
		content.Description = unescapeJSONString(desc)
	}
	if cover := extractJSONStringAfter(s, `"urlDefault":"`); cover != "" {
		content.CoverURL = unescapeJSONString(cover)
	}
	if author := extractJSONStringAfter(s, `"nickname":"`); author != "" {
		content.AuthorName = unescapeJSONString(author)
	}
	if authorID := extractJSONStringAfter(s, `"userId":"`); authorID != "" {
		content.AuthorID = authorID
	}

	// Extract tags from the note data.
	if tags := extractBetween(s, `"tagList":\[`, `]`); tags != "" {
		tagNamePattern := regexp.MustCompile(`"name":"([^"]*)"`)
		matches := tagNamePattern.FindAllStringSubmatch(tags, 20)
		parts := make([]string, 0, len(matches))
		for _, m := range matches {
			if len(m) >= 2 {
				parts = append(parts, unescapeJSONString(m[1]))
			}
		}
		if len(parts) > 0 {
			content.Tags = strings.Join(parts, ", ")
		}
	}
}

// extractJSONStringAfter finds key and extracts the complete JSON string value
// that follows, handling escaped quotes via regex "(?:[^"\\]|\\.)*".
func extractJSONStringAfter(s, key string) string {
	_, after, found := strings.Cut(s, key)
	if !found {
		return ""
	}
	if m := rednoteJSONStringPattern.FindStringSubmatch(after); len(m) >= 1 {
		val := m[0]
		if len(val) >= 2 {
			return val[1 : len(val)-1]
		}
	}
	return ""
}

func unescapeJSONString(s string) string {
	s = strings.ReplaceAll(s, `\/`, "/")
	s = strings.ReplaceAll(s, `\\"`, `"`)
	s = strings.ReplaceAll(s, `\\`, `\`)
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\t`, "\t")
	return s
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
