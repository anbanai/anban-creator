package draft

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/rs/zerolog"
	"github.com/royalrick/anbanwriter/app/config"
	"github.com/royalrick/anbanwriter/app/wechat"
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
	log zerolog.Logger
	ws  *wechat.Service
}

// NewService 创建草稿服务
func NewService(cfg *config.Config, log zerolog.Logger) *Service {
	var ws *wechat.Service
	if cfg.Wechat.AppID != "" && cfg.Wechat.Secret != "" {
		ws = wechat.NewService(cfg, log)
	}
	return &Service{
		cfg: cfg,
		log: log,
		ws:  ws,
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
	MediaID  string `json:"media_id"`
	DraftURL string `json:"draft_url,omitempty"`
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

	if s.ws == nil {
		return nil, &ServiceError{Message: "wechat not configured", HintText: "请先配置微信公众号 AppID 和 Secret"}
	}
	ws := s.ws

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
		MediaID:  result.MediaID,
		DraftURL: result.DraftURL,
	}, nil
}

// ListDraftsResult 草稿列表结果
type ListDraftsResult struct {
	TotalCount int64       `json:"total_count"`
	ItemCount  int64       `json:"item_count"`
	Items      []DraftItem `json:"items"`
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
	if s.ws == nil {
		return nil, &ServiceError{Message: "wechat not configured", HintText: "请先配置微信公众号 AppID 和 Secret"}
	}
	ws := s.ws
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
	if s.ws == nil {
		return nil, &ServiceError{Message: "wechat not configured", HintText: "请先配置微信公众号 AppID 和 Secret"}
	}
	ws := s.ws
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

// ImageXlsRequest 创建小绿书请求
type ImageXlsRequest struct {
	Title        string   // 标题（必需）
	Content      string   // 纯文本描述
	Images       []string // 本地图片文件路径列表（将自动上传到微信素材库）
	MediaIDs     []string // 微信素材 ID（media_id）列表（已上传到微信素材库的图片，跳过重复上传）
	OpenComment  bool     // 开启评论
	FansOnly     bool     // 仅粉丝评论
	FromMarkdown string   // 从 MD 文件提取图片
}

// ImageXlsResult 创建结果
type ImageXlsResult struct {
	MediaID     string   `json:"media_id"`
	DraftURL    string   `json:"draft_url"`
	Count       int      `json:"count"`
	UploadedIDs []string `json:"uploaded_ids"`
}

// CreateImageXls 创建小绿书（图文笔记）
func (s *Service) CreateImageXls(req *ImageXlsRequest) (*ImageXlsResult, error) {
	s.log.Info().Str("title", req.Title).Msg("creating image xls")

	// 验证标题
	if req.Title == "" {
		return nil, &ServiceError{Message: "title is required", HintText: "使用 -t 参数指定帖子标题"}
	}

	// 获取本地图片列表
	images := req.Images
	if req.FromMarkdown != "" {
		extracted := extractImagesFromMarkdown(req.FromMarkdown)
		images = append(images, extracted...)
	}

	if len(req.MediaIDs) == 0 && len(images) == 0 {
		return nil, &ServiceError{Message: "no images provided", HintText: "使用 --images, --media-ids 或 --from-markdown 提供图片"}
	}

	// 微信限制最多 20 张图片
	totalCount := len(req.MediaIDs) + len(images)
	if totalCount > 20 {
		return nil, &ServiceError{Message: fmt.Sprintf("too many images: %d (max 20)", totalCount), HintText: "拆分为多个帖子，每个不超过 20 张图片"}
	}

	// 检查内容是否含 HTML 标签
	if strings.Contains(req.Content, "<") {
		return nil, &ServiceError{
			Message:  "image post content contains HTML tags",
			HintText: "小绿书内容仅支持纯文本，不支持 HTML",
		}
	}

	// 创建 WeChat Service
	if s.ws == nil {
		return nil, &ServiceError{Message: "wechat not configured", HintText: "请先配置微信公众号 AppID 和 Secret"}
	}
	ws := s.ws
	var imageList []wechat.NewspicImageItem
	var uploadedIDs []string

	// 先添加已上传的 media_id（跳过重复上传）
	for _, mediaID := range req.MediaIDs {
		imageList = append(imageList, wechat.NewspicImageItem{ImageMediaID: mediaID})
		uploadedIDs = append(uploadedIDs, mediaID)
	}

	// 再上传本地图片
	for i, imgPath := range images {
			s.log.Info().Int("index", len(req.MediaIDs)+i+1).Int("total", totalCount).Str("path", imgPath).Msg("uploading image")

		result, err := ws.UploadMaterialWithRetry(imgPath, 3)
		if err != nil {
			return nil, fmt.Errorf("upload image %d (%s): %w", len(req.MediaIDs)+i+1, imgPath, err)
		}

		imageList = append(imageList, wechat.NewspicImageItem{
			ImageMediaID: result.MediaID,
		})
		uploadedIDs = append(uploadedIDs, result.MediaID)
	}

	// 构造文章
	article := wechat.NewspicArticle{
		Title:       req.Title,
		Content:     req.Content,
		ArticleType: "newspic",
		ImageInfo: wechat.NewspicImageInfo{
			ImageList: imageList,
		},
	}

	// 评论设置
	if req.OpenComment {
		article.NeedOpenComment = 1
		if req.FansOnly {
			article.OnlyFansCanComment = 1
		}
	}

	// 调用微信 API 创建草稿
	result, err := ws.CreateNewspicDraft([]wechat.NewspicArticle{article})
	if err != nil {
		return nil, fmt.Errorf("create draft: %w", err)
	}

	return &ImageXlsResult{
		MediaID:     result.MediaID,
		DraftURL:    result.DraftURL,
		Count:       totalCount,
		UploadedIDs: uploadedIDs,
	}, nil
}

// GetImageXlsPreview 获取小绿书预览信息（dry-run 用）
func (s *Service) GetImageXlsPreview(req *ImageXlsRequest) (map[string]any, error) {
	// 获取本地图片列表
	images := req.Images
	if req.FromMarkdown != "" {
		extracted := extractImagesFromMarkdown(req.FromMarkdown)
		images = append(images, extracted...)
	}

	if len(req.MediaIDs) == 0 && len(images) == 0 {
		return nil, &ServiceError{Message: "no images provided", HintText: "使用 --images, --media-ids 或 --from-markdown 提供图片"}
	}

	totalCount := len(req.MediaIDs) + len(images)
	if totalCount > 20 {
		return nil, &ServiceError{Message: fmt.Sprintf("too many images: %d (max 20)", totalCount), HintText: "拆分为多个帖子，每个不超过 20 张图片"}
	}

	// 检查本地图片文件是否存在
	var imageDetails []map[string]any
	for _, imgPath := range images {
		info, err := os.Stat(imgPath)
		detail := map[string]any{
			"path":   imgPath,
			"exists": err == nil,
		}
		if err == nil {
			detail["size"] = info.Size()
		}
		imageDetails = append(imageDetails, detail)
	}

	return map[string]any{
		"title":        req.Title,
		"content":      req.Content,
		"count":        totalCount,
		"images":       imageDetails,
		"media_ids":    req.MediaIDs,
		"open_comment": req.OpenComment,
		"fans_only":    req.FansOnly,
	}, nil
}

// extractImagesFromMarkdown 从 Markdown 文件提取图片路径
func extractImagesFromMarkdown(mdFile string) []string {
	content, err := os.ReadFile(mdFile)
	if err != nil {
		return nil
	}

	// 匹配 Markdown 图片语法: ![alt](path) 或 ![alt](path "title")
	re := regexp.MustCompile(`!\[[^\]]*\]\(([^)"\s]+)(?:\s+"[^"]*")?\)`)
	matches := re.FindAllStringSubmatch(string(content), -1)

	// 获取 Markdown 文件所在目录
	mdDir := filepath.Dir(mdFile)

	var images []string
	for _, match := range matches {
		if len(match) > 1 {
			imgPath := match[1]
			// 跳过网络图片
			if strings.HasPrefix(imgPath, "http://") || strings.HasPrefix(imgPath, "https://") {
				continue
			}
			// 转换相对路径为绝对路径
			if !filepath.IsAbs(imgPath) {
				imgPath = filepath.Join(mdDir, imgPath)
			}
			images = append(images, imgPath)
		}
	}

	return images
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
	if utf8.RuneCountInString(content) > maxLen {
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
