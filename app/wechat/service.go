package wechat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/anbanai/anban-creator/app/config"
	"github.com/rs/zerolog"
	"github.com/silenceper/wechat/v2"
	wechatcache "github.com/silenceper/wechat/v2/cache"
	"github.com/silenceper/wechat/v2/officialaccount"
	wechatconfig "github.com/silenceper/wechat/v2/officialaccount/config"
	"github.com/silenceper/wechat/v2/officialaccount/datacube"
	"github.com/silenceper/wechat/v2/officialaccount/draft"
	"github.com/silenceper/wechat/v2/officialaccount/freepublish"
	"github.com/silenceper/wechat/v2/officialaccount/material"
	"resty.dev/v3"
)

// Service 微信服务
type Service struct {
	cfg *config.Config
	log *zerolog.Logger
	oa  *officialaccount.OfficialAccount
}

// NewService 创建微信服务
func NewService(cfg *config.Config, log *zerolog.Logger) *Service {
	wc := wechat.NewWechat()
	memory := wechatcache.NewMemory()
	wechatCfg := &wechatconfig.Config{
		AppID:     cfg.Wechat.AppID,
		AppSecret: cfg.Wechat.Secret,
		Cache:     memory,
	}
	oa := wc.GetOfficialAccount(wechatCfg)

	return &Service{
		cfg: cfg,
		log: log,
		oa:  oa,
	}
}

// getOfficialAccount 获取公众号实例（缓存在 Service 中，避免每次调用重新创建）
func (s *Service) getOfficialAccount() *officialaccount.OfficialAccount {
	return s.oa
}

// OfficialAPI returns the typed publication-lifecycle transport using this
// service's managed Official Account access-token cache.
func (s *Service) OfficialAPI() *OfficialAPI {
	return NewOfficialAPI(nil, "", func(context.Context) (string, error) {
		return s.getOfficialAccount().GetAccessToken()
	})
}

// UploadMaterialResult 上传素材结果
type UploadMaterialResult struct {
	MediaID   string `json:"media_id"`
	WechatURL string `json:"wechat_url"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

// UploadMaterial 上传素材到微信
func (s *Service) UploadMaterial(filePath string) (*UploadMaterialResult, error) {
	startTime := time.Now()
	oa := s.getOfficialAccount()
	mat := oa.GetMaterial()

	// 调用微信 API 上传（SDK 接受文件路径字符串）
	mediaID, url, err := mat.AddMaterial(material.MediaTypeImage, filePath)
	if err != nil {
		s.log.Error().Str("path", filePath).Err(err).Msg("upload material failed")
		if wErr := ParseWechatError(err); wErr != nil {
			return nil, wErr
		}
		return nil, fmt.Errorf("upload material: %w", err)
	}

	duration := time.Since(startTime)
	s.log.Debug().Str("path", filePath).Str("media_id", MaskMediaID(mediaID)).Dur("duration", duration).Msg("material uploaded")

	return &UploadMaterialResult{
		MediaID:   mediaID,
		WechatURL: url,
	}, nil
}

// CreateDraftResult 创建草稿结果
type CreateDraftResult struct {
	MediaID  string `json:"media_id"`
	DraftURL string `json:"draft_url,omitempty"`
}

// CreateDraft 创建草稿
func (s *Service) CreateDraft(articles []*draft.Article) (*CreateDraftResult, error) {
	startTime := time.Now()
	oa := s.getOfficialAccount()
	dm := oa.GetDraft()

	// 直接调用 SDK 方法，SDK 接受 []*draft.Article
	mediaID, err := dm.AddDraft(articles)
	if err != nil {
		s.log.Error().Err(err).Msg("create draft failed")
		return nil, fmt.Errorf("create draft: %w", err)
	}

	duration := time.Since(startTime)
	s.log.Info().Str("media_id", MaskMediaID(mediaID)).Dur("duration", duration).Msg("article draft created")

	// 构造草稿 URL
	draftURL := fmt.Sprintf("https://mp.weixin.qq.com/cgi-bin/appmsg?t=media/appmsg_edit_v2&action=edit&createType=0&token=")

	return &CreateDraftResult{
		MediaID:  mediaID,
		DraftURL: draftURL,
	}, nil
}

// DraftItem 草稿列表项
type DraftItem struct {
	MediaID    string `json:"media_id"`
	Title      string `json:"title"`
	Digest     string `json:"digest,omitempty"`
	UpdateTime int64  `json:"update_time"`
}

// ListDraftsResult 草稿列表结果
type ListDraftsResult struct {
	TotalCount int64       `json:"total_count"`
	ItemCount  int64       `json:"item_count"`
	Items      []DraftItem `json:"items"`
}

// PublishedItem 已发布文章列表项
type PublishedItem struct {
	ArticleID  string `json:"article_id"`
	Title      string `json:"title"`
	Digest     string `json:"digest,omitempty"`
	URL        string `json:"url,omitempty"`
	UpdateTime int64  `json:"update_time"`
}

// ListPublishedResult 已发布文章列表结果
type ListPublishedResult struct {
	TotalCount int64           `json:"total_count"`
	ItemCount  int64           `json:"item_count"`
	Items      []PublishedItem `json:"items"`
}

// ArticleTotalItem is the official cumulative metric series for one published
// article returned by WeChat DataCube.
type ArticleTotalItem struct {
	RefDate string                         `json:"ref_date"`
	MsgID   string                         `json:"msgid"`
	Title   string                         `json:"title"`
	Details []datacube.ArticleTotalDetails `json:"details"`
}

// ListDrafts 获取草稿列表
func (s *Service) ListDrafts(offset, count int64) (*ListDraftsResult, error) {
	oa := s.getOfficialAccount()
	dm := oa.GetDraft()

	list, err := dm.PaginateDraft(offset, count, true)
	if err != nil {
		s.log.Error().Err(err).Msg("list drafts failed")
		if wErr := ParseWechatError(err); wErr != nil {
			return nil, wErr
		}
		return nil, fmt.Errorf("list drafts: %w", err)
	}

	result := &ListDraftsResult{
		TotalCount: list.TotalCount,
		ItemCount:  list.ItemCount,
		Items:      make([]DraftItem, 0, len(list.Item)),
	}

	for _, item := range list.Item {
		di := DraftItem{
			MediaID:    item.MediaID,
			UpdateTime: item.UpdateTime,
		}
		if len(item.Content.NewsItem) > 0 {
			di.Title = item.Content.NewsItem[0].Title
			di.Digest = item.Content.NewsItem[0].Digest
		}
		result.Items = append(result.Items, di)
	}

	return result, nil
}

// ListPublished 获取已发布文章列表
func (s *Service) ListPublished(offset, count int64) (*ListPublishedResult, error) {
	return s.listPublished(offset, count, true)
}

// ListPublishedWithContent returns published article metadata including each
// article URL. It is intended for link ownership and analytics validation.
func (s *Service) ListPublishedWithContent(offset, count int64) (*ListPublishedResult, error) {
	return s.listPublished(offset, count, false)
}

func (s *Service) listPublished(offset, count int64, noReturnContent bool) (*ListPublishedResult, error) {
	oa := s.getOfficialAccount()
	fp := oa.GetFreePublish()

	list, err := fp.Paginate(offset, count, noReturnContent)
	if err != nil {
		if wErr := ParseWechatError(err); wErr != nil {
			s.log.Debug().Int("errcode", wErr.ErrCode).Str("msg", wErr.UserMsg).Msg("list published failed")
			return nil, wErr
		}
		s.log.Error().Err(err).Msg("list published failed")
		return nil, fmt.Errorf("list published: %w", err)
	}

	result := &ListPublishedResult{
		TotalCount: list.TotalCount,
		ItemCount:  list.ItemCount,
		Items:      mapPublishedItems(list.Item),
	}
	return result, nil
}

func mapPublishedItems(items []freepublish.ArticleListItem) []PublishedItem {
	result := make([]PublishedItem, 0, len(items))
	for _, item := range items {
		for _, article := range item.Content.NewsItem {
			result = append(result, PublishedItem{
				ArticleID:  item.ArticleID,
				Title:      article.Title,
				Digest:     article.Digest,
				URL:        article.URL,
				UpdateTime: item.UpdateTime,
			})
		}
	}
	return result
}

// GetArticleTotal returns cumulative official-account article metrics for the
// requested publication date. WeChat requires beginDate and endDate in
// YYYY-MM-DD format and limits the available historical window.
func (s *Service) GetArticleTotal(beginDate, endDate string) ([]ArticleTotalItem, error) {
	cube := s.getOfficialAccount().GetDataCube()
	result, err := cube.GetArticleTotal(beginDate, endDate)
	if err != nil {
		if wErr := ParseWechatError(err); wErr != nil {
			return nil, wErr
		}
		return nil, fmt.Errorf("get article total: %w", err)
	}
	items := make([]ArticleTotalItem, 0, len(result.List))
	for _, item := range result.List {
		items = append(items, ArticleTotalItem{
			RefDate: item.RefDate,
			MsgID:   item.MsgID,
			Title:   item.Title,
			Details: item.Details,
		})
	}
	return items, nil
}

// UploadMaterialFromBytes 从字节数据上传素材
func (s *Service) UploadMaterialFromBytes(data []byte, filename string) (*UploadMaterialResult, error) {
	// 创建临时文件
	tmpFile, err := os.CreateTemp("", "anban-creator_*_"+filename)
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("write temp file: %w", err)
	}
	defer os.Remove(tmpPath)

	return s.UploadMaterial(tmpPath)
}

// AccessTokenResult 获取 access_token 结果（用于调试）
type AccessTokenResult struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// GetAccessToken 获取 access_token（调试用）
func (s *Service) GetAccessToken() (*AccessTokenResult, error) {
	oa := s.getOfficialAccount()
	accessToken, err := oa.GetAccessToken()
	if err != nil {
		return nil, fmt.Errorf("get access token: %w", err)
	}

	return &AccessTokenResult{
		AccessToken: accessToken,
		ExpiresIn:   7200, // 微信默认 7200 秒
	}, nil
}

// MaskMediaID 遮蔽 media_id 用于日志
func MaskMediaID(id string) string {
	if id == "" || len(id) < 8 {
		return "***"
	}
	return id[:4] + "***" + id[len(id)-4:]
}

// UploadMaterialWithRetry 带重试的上传（不可重试错误立即返回）
func (s *Service) UploadMaterialWithRetry(filePath string, maxRetries int) (*UploadMaterialResult, error) {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		result, err := s.UploadMaterial(filePath)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !IsRetryable(err) {
			s.log.Info().Err(err).Msg("upload error is not retryable, aborting")
			return nil, err
		}
		if i < maxRetries-1 {
			delay := time.Duration(i+1) * time.Second
			s.log.Info().Int("attempt", i+2).Dur("delay", delay).Msg("retrying upload")
			time.Sleep(delay)
		}
	}
	return nil, lastErr
}

// DownloadFile 下载文件到临时目录
func DownloadFile(url string) (string, error) {
	return DownloadFileContext(context.Background(), url)
}

// DownloadFileContext 下载文件到临时目录，并支持取消请求。
func DownloadFileContext(ctx context.Context, url string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	client := resty.New().
		SetTimeout(60*time.Second).
		SetHeader("User-Agent", "Mozilla/5.0 (compatible; AnbanCreator/1.0; +https://anbanai.com)").
		SetHeader("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8").
		SetHeader("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	defer client.Close()

	// 创建临时文件
	// 从 URL 路径中提取扩展名，排除查询参数
	ext := ".jpg" // 默认扩展名
	if parsedURL, err := neturl.Parse(url); err == nil {
		if pathExt := filepath.Ext(parsedURL.Path); pathExt != "" {
			ext = pathExt
		}
	}
	tmpFile, err := os.CreateTemp("", "anban-creator_download_*"+ext)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("close temp file: %w", err)
	}

	start := time.Now()
	resp, err := client.R().
		SetContext(ctx).
		SetResponseSaveFileName(tmpPath).
		Get(url)
	elapsed := time.Since(start)
	if err != nil {
		os.Remove(tmpPath)
		return "", &DownloadError{
			URL:      url,
			Elapsed:  elapsed,
			Original: err,
		}
	}

	if resp.StatusCode() != http.StatusOK {
		os.Remove(tmpPath)
		return "", &DownloadError{
			URL:           url,
			StatusCode:    resp.StatusCode(),
			ContentType:   resp.Header().Get("Content-Type"),
			ContentLength: resp.Header().Get("Content-Length"),
			Server:        resp.Header().Get("Server"),
			CFRay:         resp.Header().Get("Cf-Ray"),
			Location:      resp.Header().Get("Location"),
			BodyPreview:   truncateDownloadBodyPreview(resp.Bytes(), 80),
			Elapsed:       elapsed,
		}
	}
	if expectedSize, err := strconv.ParseInt(resp.Header().Get("Content-Length"), 10, 64); err == nil && expectedSize >= 0 {
		info, err := os.Stat(tmpPath)
		if err != nil || info.Size() != expectedSize {
			os.Remove(tmpPath)
			if err == nil {
				err = fmt.Errorf("downloaded file size %d does not match content length %d", info.Size(), expectedSize)
			}
			return "", &DownloadError{
				URL:           url,
				ContentType:   resp.Header().Get("Content-Type"),
				ContentLength: resp.Header().Get("Content-Length"),
				Server:        resp.Header().Get("Server"),
				CFRay:         resp.Header().Get("Cf-Ray"),
				Location:      resp.Header().Get("Location"),
				Elapsed:       elapsed,
				Original:      err,
			}
		}
	}

	return tmpPath, nil
}

func truncateDownloadBodyPreview(body []byte, max int) string {
	if len(body) == 0 || max <= 0 {
		return ""
	}
	if len(body) > max {
		body = body[:max]
	}
	return string(body)
}

// CreateMultipartFormData 创建 multipart 表单数据
func CreateMultipartFormData(fieldName, filename string, data []byte) (string, *bytes.Buffer, string) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile(fieldName, filename)
	if err != nil {
		writer.Close()
		return "", nil, ""
	}

	if _, err := part.Write(data); err != nil {
		writer.Close()
		return "", nil, ""
	}

	contentType := writer.FormDataContentType()
	writer.Close()

	return contentType, body, filename
}

// JSONMarshal 自定义 JSON 序列化
func JSONMarshal(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
