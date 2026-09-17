package draft

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/anbanai/anban-creator/server/app/config"
	"github.com/anbanai/anban-creator/server/app/wechat"
	"github.com/rs/zerolog"
	"github.com/silenceper/wechat/v2/officialaccount/draft"
)

// ServiceError 草稿服务错误，携带修复建议
type ServiceError struct {
	Message  string
	HintText string
}

func (e *ServiceError) Error() string { return e.Message }
func (e *ServiceError) Hint() string  { return e.HintText }

// Service 草稿服务
type Service struct {
	cfg *config.Config
	log *zerolog.Logger
}

// NewService 创建草稿服务
func NewService(cfg *config.Config, log *zerolog.Logger) *Service {
	return &Service{
		cfg: cfg,
		log: log,
	}
}

// DraftRequest 草稿请求
type DraftRequest struct {
	Articles []Article `json:"articles"`
}

// Article 文章
type Article struct {
	Title            string `json:"title"`
	Author           string `json:"author,omitempty"`
	Digest           string `json:"digest,omitempty"`
	Content          string `json:"content,omitempty"`
	ContentFile      string `json:"content_file,omitempty"`
	ContentSourceURL string `json:"content_source_url,omitempty"`
	ThumbMediaID     string `json:"thumb_media_id,omitempty"`
	ShowCoverPic     int    `json:"show_cover_pic,omitempty"`
}

// DraftResult 草稿结果
type DraftResult struct {
	MediaID string `json:"media_id"`
}

// CreateDraftFromFile 从 JSON 文件创建草稿
func (s *Service) CreateDraftFromFile(jsonFile string) (*DraftResult, error) {
	s.log.Info().Str("file", jsonFile).Msg("creating draft from file")

	// 读取 JSON 文件
	data, err := os.ReadFile(jsonFile)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	// 解析请求
	var req DraftRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("parse json: %w", err)
	}

	// 验证
	if len(req.Articles) == 0 {
		return nil, &ServiceError{Message: "no articles in request", HintText: "JSON 文件需包含 articles 数组"}
	}

	// 使用 CreateDraft 方法（会自动选择账号）
	return s.CreateDraft(req.Articles)
}

// CreateDraft 创建草稿
func (s *Service) CreateDraft(articles []Article) (*DraftResult, error) {
	s.log.Info().Msg("creating draft")

	// 创建 WeChat Service
	ws := wechat.NewService(s.cfg, s.log)

	// 转换为 SDK 格式
	var draftArticles []*draft.Article
	for _, a := range articles {
		article := &draft.Article{
			Title:   a.Title,
			Content: a.Content,
			Digest:  a.Digest,
			Author:  a.Author,
		}

		if a.ThumbMediaID != "" {
			if !isValidMediaID(a.ThumbMediaID) {
				return nil, &ServiceError{
					Message:  fmt.Sprintf("invalid thumb_media_id: %q", a.ThumbMediaID),
					HintText: "thumb_media_id 必须是微信素材上传接口返回的 media_id（字母数字字符串），不能使用 URL 或文件路径",
				}
			}
			article.ThumbMediaID = a.ThumbMediaID
			article.ShowCoverPic = uint(a.ShowCoverPic)
		}

		if a.ContentSourceURL != "" {
			article.ContentSourceURL = a.ContentSourceURL
		}

		draftArticles = append(draftArticles, article)
	}

	// 检查内容大小
	for _, a := range articles {
		if len(a.Content) > 20000 {
			return nil, &ServiceError{
				Message:  fmt.Sprintf("article content too large: %d chars (max 20000)", len(a.Content)),
				HintText: "缩减内容或使用更简单的主题减少内联 CSS",
			}
		}
	}

	// 调用微信 API
	result, err := ws.CreateDraft(draftArticles)
	if err != nil {
		return nil, err
	}

	return &DraftResult{
		MediaID: result.MediaID,
	}, nil
}

// ListDraftsResult 草稿列表结果
type ListDraftsResult struct {
	TotalCount int64       `json:"total_count"`
	ItemCount  int64       `json:"item_count"`
	Items      []DraftItem `json:"items"`
	Note       string      `json:"note,omitempty"`
}

// DraftItem 草稿列表项
type DraftItem struct {
	MediaID    string `json:"media_id"`
	Title      string `json:"title"`
	Digest     string `json:"digest,omitempty"`
	UpdateTime int64  `json:"update_time"`
}

// ListPublishedResult 已发布文章列表结果
type ListPublishedResult struct {
	TotalCount int64           `json:"total_count"`
	ItemCount  int64           `json:"item_count"`
	Items      []PublishedItem `json:"items"`
	Note       string          `json:"note,omitempty"`
}

// PublishedItem 已发布文章列表项
type PublishedItem struct {
	ArticleID  string `json:"article_id"`
	Title      string `json:"title"`
	Digest     string `json:"digest,omitempty"`
	URL        string `json:"url,omitempty"`
	UpdateTime int64  `json:"update_time"`
}

// ListDrafts 获取草稿列表
func (s *Service) ListDrafts(offset, count int64) (*ListDraftsResult, error) {
	ws := wechat.NewService(s.cfg, s.log)
	result, err := ws.ListDrafts(offset, count)
	if err != nil {
		return nil, err
	}

	return &ListDraftsResult{
		TotalCount: result.TotalCount,
		ItemCount:  result.ItemCount,
		Items: func() []DraftItem {
			items := make([]DraftItem, len(result.Items))
			for i, item := range result.Items {
				items[i] = DraftItem{
					MediaID:    item.MediaID,
					Title:      item.Title,
					Digest:     item.Digest,
					UpdateTime: item.UpdateTime,
				}
			}
			return items
		}(),
	}, nil
}

// ListPublished 获取已发布文章列表
func (s *Service) ListPublished(offset, count int64) (*ListPublishedResult, error) {
	ws := wechat.NewService(s.cfg, s.log)
	result, err := ws.ListPublished(offset, count)
	if err != nil {
		return nil, err
	}

	return &ListPublishedResult{
		TotalCount: result.TotalCount,
		ItemCount:  result.ItemCount,
		Items: func() []PublishedItem {
			items := make([]PublishedItem, len(result.Items))
			for i, item := range result.Items {
				items[i] = PublishedItem{
					ArticleID:  item.ArticleID,
					Title:      item.Title,
					Digest:     item.Digest,
					URL:        item.URL,
					UpdateTime: item.UpdateTime,
				}
			}
			return items
		}(),
	}, nil
}

// GenerateDigestFromContent 从内容生成摘要
func GenerateDigestFromContent(content string, maxLen int) string {
	if maxLen == 0 {
		maxLen = 120
	}

	// 简化实现：去除 HTML 标签后截取
	// 实际应该使用 HTML 解析器

	// 移除 HTML 标签的简单方法
	content = stripHTML(content)

	// 截取
	if len(content) > maxLen {
		runes := []rune(content)
		if len(runes) > maxLen {
			content = string(runes[:maxLen]) + "..."
		}
	}

	return content
}

// stripHTML 去除 HTML 标签（简化版）
func stripHTML(html string) string {
	// 简化实现：移除常见标签
	// 实际应该使用 proper HTML 解析器
	result := html
	for _, tag := range []string{"</p>", "<br/>", "<br>", "</div>", "</h1>", "</h2>", "</h3>"} {
		result = strings.ReplaceAll(result, tag, "\n")
	}

	// 移除所有标签
	inTag := false
	var clean strings.Builder
	for _, r := range result {
		if r == '<' {
			inTag = true
		} else if r == '>' {
			inTag = false
		} else if !inTag {
			clean.WriteRune(r)
		}
	}

	return clean.String()
}

// isValidMediaID checks whether a thumb_media_id looks like a valid WeChat
// permanent media ID. WeChat media IDs are alphanumeric strings returned by
// the material upload API. They should never be URLs or file paths.
func isValidMediaID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	if strings.HasPrefix(id, "http://") || strings.HasPrefix(id, "https://") {
		return false
	}
	if strings.HasPrefix(id, "/") || strings.HasPrefix(id, "./") || strings.HasPrefix(id, "..") {
		return false
	}
	for _, ch := range id {
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-') {
			return false
		}
	}
	return true
}
