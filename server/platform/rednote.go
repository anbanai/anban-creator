package platform

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/royalrick/anbanwriter/server/xhs"
)

// RednoteProvider fetches Xiaohongshu data via the xiaohongshu-mcp Docker sidecar.
type RednoteProvider struct {
	xhs *xhs.Client
}

// NewRednoteProvider creates a new Xiaohongshu platform provider backed by the XHS SDK client.
func NewRednoteProvider(xhsClient *xhs.Client) *RednoteProvider {
	return &RednoteProvider{xhs: xhsClient}
}

// FetchProfile fetches a user's profile from their public profile page URL.
// Accepts a profile URL or share text containing a profile URL.
func (p *RednoteProvider) FetchProfile(ctx context.Context, profileURL string) (*PlatformProfile, error) {
	if profileURL == "" {
		return nil, fmt.Errorf("profile URL is required")
	}

	extractedURL, err := extractRednoteURLFromText(profileURL)
	if err != nil {
		return nil, err
	}

	userID, xsecToken, err := resolveProfileURLToUser(extractedURL)
	if err != nil {
		return nil, err
	}

	if p.xhs == nil {
		return nil, fmt.Errorf("XHS sidecar is not configured")
	}

	profile, err := p.xhs.GetUserProfile(ctx, userID, xsecToken)
	if err != nil {
		return nil, fmt.Errorf("fetch user profile: %w", err)
	}

	return mapUserProfile(profile, extractedURL), nil
}

// FetchProfilePosts fetches visible public posts from a Xiaohongshu profile.
func (p *RednoteProvider) FetchProfilePosts(ctx context.Context, profileURL string) ([]RednotePost, error) {
	if profileURL == "" {
		return nil, fmt.Errorf("profile URL is required")
	}

	extractedURL, err := extractRednoteURLFromText(profileURL)
	if err != nil {
		return nil, err
	}

	userID, xsecToken, err := resolveProfileURLToUser(extractedURL)
	if err != nil {
		return nil, err
	}

	if p.xhs == nil {
		return nil, fmt.Errorf("XHS sidecar is not configured")
	}

	profile, err := p.xhs.GetUserProfile(ctx, userID, xsecToken)
	if err != nil {
		return nil, fmt.Errorf("fetch profile posts: %w", err)
	}

	return mapFeedsToPosts(profile.Feeds), nil
}

// FetchPostMetrics fetches public metrics from a Xiaohongshu note page.
func (p *RednoteProvider) FetchPostMetrics(ctx context.Context, noteURL string) (*RednotePostMetrics, error) {
	noteURL = strings.TrimSpace(noteURL)
	if noteURL == "" {
		return nil, fmt.Errorf("note URL is required")
	}

	feedID, xsecToken, err := resolveNoteURL(noteURL)
	if err != nil {
		return nil, err
	}

	if p.xhs == nil {
		return nil, fmt.Errorf("XHS sidecar is not configured")
	}

	detail, err := p.xhs.GetFeedDetail(ctx, &xhs.FeedDetailRequest{
		FeedID:    feedID,
		XsecToken: xsecToken,
	})
	if err != nil {
		return nil, fmt.Errorf("fetch post metrics: %w", err)
	}

	return &RednotePostMetrics{
		LikeCount:    parseCountString(detail.Note.InteractInfo.LikedCount),
		CollectCount: parseCountString(detail.Note.InteractInfo.CollectedCount),
		CommentCount: parseCountString(detail.Note.InteractInfo.CommentCount),
		ShareCount:   parseCountString(detail.Note.InteractInfo.SharedCount),
	}, nil
}

// FetchNoteContent fetches and parses a Xiaohongshu note page.
func (p *RednoteProvider) FetchNoteContent(ctx context.Context, noteURL string) (*RednoteNoteContent, error) {
	noteURL = strings.TrimSpace(noteURL)
	if noteURL == "" {
		return nil, fmt.Errorf("note URL is required")
	}

	feedID, xsecToken, err := resolveNoteURL(noteURL)
	if err != nil {
		return nil, err
	}

	if p.xhs == nil {
		return nil, fmt.Errorf("XHS sidecar is not configured")
	}

	detail, err := p.xhs.GetFeedDetail(ctx, &xhs.FeedDetailRequest{
		FeedID:    feedID,
		XsecToken: xsecToken,
	})
	if err != nil {
		return nil, fmt.Errorf("fetch note content: %w", err)
	}

	content := &RednoteNoteContent{
		NoteID:       detail.Note.NoteID,
		Title:        detail.Note.Title,
		Description:  detail.Note.Desc,
		AuthorName:   detail.Note.User.Nickname,
		AuthorID:     detail.Note.User.UserID,
		Type:         detail.Note.Type,
		LikeCount:    parseCountString(detail.Note.InteractInfo.LikedCount),
		CollectCount: parseCountString(detail.Note.InteractInfo.CollectedCount),
		CommentCount: parseCountString(detail.Note.InteractInfo.CommentCount),
		ShareCount:   parseCountString(detail.Note.InteractInfo.SharedCount),
	}

	if len(detail.Note.ImageList) > 0 {
		content.CoverURL = detail.Note.ImageList[0].URLDefault
	}

	// Extract tags from description (#tag format).
	var tagParts []string
	seen := map[string]bool{}
	for _, tag := range extractHashtags(detail.Note.Desc) {
		if !seen[tag] {
			seen[tag] = true
			tagParts = append(tagParts, tag)
		}
	}
	if len(tagParts) > 0 {
		content.Tags = strings.Join(tagParts, ", ")
	}

	content.InteractCount = content.LikeCount + content.CollectCount + content.CommentCount + content.ShareCount
	return content, nil
}

// --- URL resolution helpers ---

func resolveProfileURLToUser(rawURL string) (userID, xsecToken string, err error) {
	return xhs.ResolveProfileURL(rawURL)
}

func resolveNoteURL(rawURL string) (feedID, xsecToken string, err error) {
	feedID = ExtractRednoteNoteID(rawURL)
	if feedID == "" {
		return "", "", fmt.Errorf("unsupported Rednote note URL: %s", rawURL)
	}
	parsed, parseErr := url.Parse(rawURL)
	if parseErr != nil {
		return feedID, "", nil
	}
	xsecToken = parsed.Query().Get("xsec_token")
	return feedID, xsecToken, nil
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

// --- Data mapping helpers ---

func mapUserProfile(profile *xhs.UserProfile, sourceURL string) *PlatformProfile {
	result := &PlatformProfile{
		Name:      profile.UserBasicInfo.Nickname,
		AvatarURL: profile.UserBasicInfo.Avatar,
		RawData:   make(map[string]any),
	}
	result.RawData["source_text"] = sourceURL
	result.RawData["profile_url"] = sourceURL
	result.RawData["red_id"] = profile.UserBasicInfo.RedID
	result.RawData["desc"] = profile.UserBasicInfo.Desc

	// Map interactions.
	for _, interaction := range profile.Interactions {
		switch interaction.Type {
		case "follows":
			result.RawData["follows"] = interaction.Count
		case "fans":
			result.RawData["fans"] = interaction.Count
		case "interaction":
			result.RawData["interaction"] = interaction.Count
		}
	}

	// Map top posts.
	posts := mapFeedsToPosts(profile.Feeds)
	result.RawData["posts"] = posts
	result.RawData["top_posts"] = selectTopRednotePosts(posts, rednoteTopPostLimit)

	return result
}

func mapFeedsToPosts(feeds []xhs.Feed) []RednotePost {
	posts := make([]RednotePost, 0, len(feeds))
	for _, f := range feeds {
		post := RednotePost{
			Title:        f.NoteCard.DisplayTitle,
			NoteID:       f.ID,
			CoverURL:     f.NoteCard.Cover.URLDefault,
			LikeCount:    parseCountString(f.NoteCard.InteractInfo.LikedCount),
			CollectCount: parseCountString(f.NoteCard.InteractInfo.CollectedCount),
			CommentCount: parseCountString(f.NoteCard.InteractInfo.CommentCount),
			ShareCount:   parseCountString(f.NoteCard.InteractInfo.SharedCount),
		}
		post.EngagementScore = post.LikeCount + post.CollectCount + post.CommentCount + post.ShareCount
		if f.XsecToken != "" {
			post.URL = fmt.Sprintf("https://www.xiaohongshu.com/explore/%s?xsec_token=%s", f.ID, f.XsecToken)
		} else {
			post.URL = fmt.Sprintf("https://www.xiaohongshu.com/explore/%s", f.ID)
		}
		posts = append(posts, post)
	}
	return posts
}

func parseCountString(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return NormalizeRednoteMetricCount(s)
	}
	switch {
	case strings.Contains(s, "万"):
		n *= 10000
	case strings.Contains(s, "千"):
		n *= 1000
	}
	return int(n)
}

func extractHashtags(text string) []string {
	var tags []string
	for _, t := range hashtagPattern.FindAllStringSubmatch(text, 20) {
		if len(t) >= 2 && len(t[1]) >= 2 && len(t[1]) <= 20 {
			tags = append(tags, t[1])
		}
	}
	return tags
}

// --- Shared patterns and utilities ---

var (
	rednoteURLInTextPattern = regexp.MustCompile(`https?://((m\.|www\.)?xiaohongshu\.com|xhslink\.com)/[^\s"'<>，。！？；、]+`)
	rednoteNoteIDPatterns   = []*regexp.Regexp{
		regexp.MustCompile(`/explore/([^/?#]+)`),
		regexp.MustCompile(`/discovery/item/([^/?#]+)`),
	}
	hashtagPattern = regexp.MustCompile(`#([^\s#]{2,20})`)
)

const rednoteTopPostLimit = 5

func selectTopRednotePosts(posts []RednotePost, limit int) []RednotePost {
	if limit <= 0 || len(posts) == 0 {
		return nil
	}
	ranked := make([]RednotePost, len(posts))
	copy(ranked, posts)
	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].EngagementScore > ranked[j].EngagementScore
	})
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return ranked
}

// ExtractRednoteNoteID extracts a Xiaohongshu note ID from URLs.
func ExtractRednoteNoteID(raw string) string {
	raw = strings.ReplaceAll(raw, `/`, "/")
	raw = strings.ReplaceAll(raw, `\/`, "/")
	for _, pattern := range rednoteNoteIDPatterns {
		match := pattern.FindStringSubmatch(raw)
		if len(match) >= 2 {
			return strings.TrimSpace(match[1])
		}
	}
	return ""
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

var rednoteNumberPattern = regexp.MustCompile(`\d+(?:,\d{3})*(?:\.\d+)?`)

// --- Types (kept for interface compatibility) ---

// RednotePostMetrics holds public engagement counters.
type RednotePostMetrics struct {
	LikeCount    int  `json:"like_count"`
	CollectCount int  `json:"collect_count"`
	CommentCount int  `json:"comment_count"`
	ShareCount   int  `json:"share_count"`
	ViewCount    *int `json:"view_count,omitempty"`
}

// RednoteNoteContent holds parsed content from a note.
type RednoteNoteContent struct {
	NoteID        string `json:"note_id"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	Tags          string `json:"tags"`
	CoverURL      string `json:"cover_url"`
	Type          string `json:"type"`
	LikeCount     int    `json:"like_count"`
	CollectCount  int    `json:"collect_count"`
	CommentCount  int    `json:"comment_count"`
	ShareCount    int    `json:"share_count"`
	AuthorName    string `json:"author_name,omitempty"`
	AuthorID      string `json:"author_id,omitempty"`
	InteractCount int    `json:"interact_count"`
}
