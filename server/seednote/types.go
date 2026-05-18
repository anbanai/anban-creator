package seednote

// APIResponse is the generic response wrapper from the xiaohongshu-mcp REST API.
type APIResponse[T any] struct {
	Success bool   `json:"success"`
	Data    T      `json:"data"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
	Code    string `json:"code,omitempty"`
	Details any    `json:"details,omitempty"`
}

// --- Feed types ---

// Feed represents a single feed item from feed list or search results.
type Feed struct {
	XsecToken string   `json:"xsecToken"`
	ID        string   `json:"id"`
	ModelType string   `json:"modelType"`
	NoteCard  NoteCard `json:"noteCard"`
	Index     int      `json:"index"`
}

// NoteCard holds note card metadata.
type NoteCard struct {
	Type         string       `json:"type"`
	DisplayTitle string       `json:"displayTitle"`
	User         User         `json:"user"`
	InteractInfo InteractInfo `json:"interactInfo"`
	Cover        Cover        `json:"cover"`
	Video        *Video       `json:"video,omitempty"`
}

// User holds basic user information.
type User struct {
	UserID   string `json:"userId"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
}

// InteractInfo holds engagement metrics for a note.
type InteractInfo struct {
	Liked          bool   `json:"liked"`
	LikedCount     string `json:"likedCount"`
	SharedCount    string `json:"sharedCount"`
	CommentCount   string `json:"commentCount"`
	CollectedCount string `json:"collectedCount"`
	Collected      bool   `json:"collected"`
}

// Cover holds cover image information.
type Cover struct {
	Width      int         `json:"width"`
	Height     int         `json:"height"`
	URL        string      `json:"url"`
	URLDefault string      `json:"urlDefault"`
	InfoList   []ImageInfo `json:"infoList"`
}

// ImageInfo holds alternative image URLs.
type ImageInfo struct {
	ImageScene string `json:"imageScene"`
	URL        string `json:"url"`
}

// Video holds video metadata (nil for image notes).
type Video struct {
	Duration int `json:"duration"`
}

// --- Feed detail types ---

// FeedDetail is the full response from /api/v1/feeds/detail.
type FeedDetail struct {
	Note     FeedNote    `json:"note"`
	Comments CommentList `json:"comments"`
}

// FeedNote holds the complete note data from the detail page.
type FeedNote struct {
	NoteID       string        `json:"noteId"`
	XsecToken    string        `json:"xsecToken"`
	Title        string        `json:"title"`
	Desc         string        `json:"desc"`
	Type         string        `json:"type"`
	Time         int64         `json:"time"`
	IPLocation   string        `json:"ipLocation"`
	User         User          `json:"user"`
	InteractInfo InteractInfo  `json:"interactInfo"`
	ImageList    []DetailImage `json:"imageList"`
}

// DetailImage holds image metadata from the note detail page.
type DetailImage struct {
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	URLDefault string `json:"urlDefault"`
	URLPre     string `json:"urlPre"`
	LivePhoto  bool   `json:"livePhoto,omitempty"`
}

// CommentList holds paginated comments.
type CommentList struct {
	List    []Comment `json:"list"`
	Cursor  string    `json:"cursor"`
	HasMore bool      `json:"hasMore"`
}

// Comment holds a single comment with optional sub-comments.
type Comment struct {
	ID              string    `json:"id"`
	Content         string    `json:"content"`
	LikeCount       string    `json:"likeCount"`
	CreateTime      int64     `json:"createTime"`
	IPLocation      string    `json:"ipLocation"`
	Liked           bool      `json:"liked"`
	UserInfo        User      `json:"userInfo"`
	SubCommentCount string    `json:"subCommentCount"`
	SubComments     []Comment `json:"subComments"`
	ShowTags        []string  `json:"showTags"`
}

// --- User profile types ---

// UserProfile is the full response from /api/v1/user/profile.
type UserProfile struct {
	UserBasicInfo UserBasicInfo      `json:"userBasicInfo"`
	Interactions  []UserInteractions `json:"interactions"`
	Feeds         []Feed             `json:"feeds"`
}

// UserBasicInfo holds basic user profile data.
type UserBasicInfo struct {
	Gender     int    `json:"gender"`
	IPLocation string `json:"ipLocation"`
	Desc       string `json:"desc"`
	Nickname   string `json:"nickname"`
	Avatar     string `json:"images"`
	Background string `json:"imageb"`
	RedID      string `json:"redId"`
}

// UserInteractions holds follower/following/like counts.
type UserInteractions struct {
	Type  string `json:"type"` // follows, fans, interaction
	Name  string `json:"name"` // 关注, 粉丝, 获赞与收藏
	Count string `json:"count"`
}

// --- Request types ---

// FeedDetailRequest is the request body for /api/v1/feeds/detail.
type FeedDetailRequest struct {
	FeedID          string             `json:"feed_id"`
	XsecToken       string             `json:"xsec_token"`
	LoadAllComments bool               `json:"load_all_comments,omitempty"`
	CommentConfig   *CommentLoadConfig `json:"comment_config,omitempty,omitzero"`
}

// CommentLoadConfig controls comment loading behavior.
type CommentLoadConfig struct {
	ClickMoreReplies    bool   `json:"click_more_replies,omitempty"`
	MaxRepliesThreshold int    `json:"max_replies_threshold,omitempty"`
	MaxCommentItems     int    `json:"max_comment_items,omitempty"`
	ScrollSpeed         string `json:"scroll_speed,omitempty"`
}

// SearchRequest is the request body for /api/v1/feeds/search.
type SearchRequest struct {
	Keyword string        `json:"keyword"`
	Filters SearchFilters `json:"filters,omitempty"`
}

// SearchFilters controls search result filtering.
type SearchFilters struct {
	SortBy      string `json:"sort_by,omitempty"`
	NoteType    string `json:"note_type,omitempty"`
	PublishTime string `json:"publish_time,omitempty"`
	SearchScope string `json:"search_scope,omitempty"`
	Location    string `json:"location,omitempty"`
}

// UserProfileRequest is the request body for /api/v1/user/profile.
type UserProfileRequest struct {
	UserID    string `json:"user_id"`
	XsecToken string `json:"xsec_token"`
}

// --- Login types ---

// LoginStatusResponse is the response from /api/v1/login/status.
type LoginStatusResponse struct {
	Success  bool   `json:"success"`
	LoggedIn bool   `json:"logged_in"`
	Message  string `json:"message"`
}

// QRCodeResponse is the response from /api/v1/login/qrcode.
type QRCodeResponse struct {
	Success bool `json:"success"`
	Data    struct {
		QRCodeImage string `json:"qrcode_image"`
	} `json:"data"`
	Message string `json:"message"`
}
