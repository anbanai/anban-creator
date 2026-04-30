package platform

import "context"

// PlatformProfile holds profile data fetched from a platform.
type PlatformProfile struct {
	Name        string                 `json:"name"`
	AvatarURL   string                 `json:"avatar_url"`
	Positioning string                 `json:"positioning"`
	Keywords    string                 `json:"keywords,omitempty"`
	Style       string                 `json:"style,omitempty"`
	RawData     map[string]interface{} `json:"raw_data"`
}

// RednotePost holds visible Xiaohongshu post metadata parsed from a profile page.
type RednotePost struct {
	Title           string `json:"title,omitempty"`
	URL             string `json:"url,omitempty"`
	NoteID          string `json:"note_id,omitempty"`
	CoverURL        string `json:"cover_url,omitempty"`
	LikeCount       int    `json:"like_count,omitempty"`
	CollectCount    int    `json:"collect_count,omitempty"`
	CommentCount    int    `json:"comment_count,omitempty"`
	ShareCount      int    `json:"share_count,omitempty"`
	EngagementScore int    `json:"engagement_score,omitempty"`
}

// PlatformDataProvider defines the interface for fetching data from a platform.
type PlatformDataProvider interface {
	// FetchProfile fetches a user's profile from the given profile URL.
	FetchProfile(ctx context.Context, profileURL string) (*PlatformProfile, error)
}
