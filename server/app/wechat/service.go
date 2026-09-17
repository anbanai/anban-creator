package wechat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/netip"
	neturl "net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/anbanai/anban-creator/server/app/config"
	"github.com/rs/zerolog"
	"github.com/silenceper/wechat/v2"
	wechatcache "github.com/silenceper/wechat/v2/cache"
	"github.com/silenceper/wechat/v2/officialaccount"
	wechatconfig "github.com/silenceper/wechat/v2/officialaccount/config"
	"github.com/silenceper/wechat/v2/officialaccount/draft"
	"github.com/silenceper/wechat/v2/officialaccount/freepublish"
	"github.com/silenceper/wechat/v2/officialaccount/material"
)

const maxDownloadedFileBytes int64 = 25 << 20

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
	MediaID string `json:"media_id"`
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

	return &CreateDraftResult{
		MediaID: mediaID,
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

// DownloadPublicImageFile downloads an image only from a public HTTPS URL.
func DownloadPublicImageFile(url string) (string, error) {
	return DownloadPublicImageFileContext(context.Background(), url)
}

// DownloadPublicImageFileContext prevents generated-image URLs from reaching
// loopback, private, link-local, or other special-use networks.
func DownloadPublicImageFileContext(ctx context.Context, url string) (string, error) {
	if err := validatePublicImageURL(url); err != nil {
		return "", &DownloadError{URL: url, Original: err}
	}
	return downloadFileContextWithClient(ctx, url, maxDownloadedFileBytes, publicImageHTTPClient)
}

func downloadFileContextWithClient(ctx context.Context, url string, maxBytes int64, client *http.Client) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if maxBytes <= 0 {
		return "", fmt.Errorf("download file: invalid maximum size %d", maxBytes)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", &DownloadError{URL: url, Original: err}
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 (compatible; AnbanCreator/1.0; +https://anbanai.com)")
	request.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	start := time.Now()
	resp, err := client.Do(request)
	elapsed := time.Since(start)
	if err != nil {
		return "", &DownloadError{URL: url, Elapsed: elapsed, Original: err}
	}
	defer resp.Body.Close()

	downloadError := func(original error) *DownloadError {
		return &DownloadError{
			URL:           url,
			ContentType:   resp.Header.Get("Content-Type"),
			ContentLength: resp.Header.Get("Content-Length"),
			Server:        resp.Header.Get("Server"),
			CFRay:         resp.Header.Get("Cf-Ray"),
			Location:      resp.Header.Get("Location"),
			Elapsed:       elapsed,
			Original:      original,
		}
	}
	if resp.StatusCode != http.StatusOK {
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 81))
		downloadErr := downloadError(nil)
		downloadErr.StatusCode = resp.StatusCode
		downloadErr.BodyPreview = truncateDownloadBodyPreview(preview, 80)
		return "", downloadErr
	}

	expectedSize := int64(-1)
	if parsed, parseErr := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64); parseErr == nil && parsed >= 0 {
		expectedSize = parsed
		if expectedSize > maxBytes {
			return "", downloadError(fmt.Errorf("%w: declared %d bytes, limit is %d", ErrDownloadExceedsMaxSize, expectedSize, maxBytes))
		}
	}

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
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(tmpPath)
		}
	}()

	written, copyErr := io.Copy(tmpFile, io.LimitReader(resp.Body, maxBytes+1))
	closeErr := tmpFile.Close()
	if written > maxBytes {
		return "", downloadError(fmt.Errorf("%w: limit is %d bytes", ErrDownloadExceedsMaxSize, maxBytes))
	}
	if expectedSize >= 0 && written != expectedSize {
		return "", downloadError(fmt.Errorf("downloaded file size %d does not match content length %d", written, expectedSize))
	}
	if copyErr != nil {
		return "", downloadError(copyErr)
	}
	if closeErr != nil {
		return "", downloadError(closeErr)
	}
	keep = true
	return tmpPath, nil
}

var publicImageHTTPClient = &http.Client{
	Timeout: 60 * time.Second,
	Transport: &http.Transport{
		DialContext: publicImageDialContext,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return fmt.Errorf("%w: too many redirects", ErrUnsafeDownloadURL)
		}
		if req == nil || req.URL == nil {
			return fmt.Errorf("%w: redirect URL is missing", ErrUnsafeDownloadURL)
		}
		return validatePublicImageURL(req.URL.String())
	},
}

func validatePublicImageURL(rawURL string) error {
	parsed, err := neturl.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return fmt.Errorf("%w: generated image URL must be public HTTPS without credentials", ErrUnsafeDownloadURL)
	}
	if ip := net.ParseIP(parsed.Hostname()); ip != nil && !isPublicImageIP(ip) {
		return fmt.Errorf("%w: generated image host is not public", ErrUnsafeDownloadURL)
	}
	return nil
}

func publicImageDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid generated image address", ErrUnsafeDownloadURL)
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve generated image host: %w", err)
	}
	if len(ips) == 0 {
		return nil, errors.New("generated image host did not resolve")
	}
	for _, resolved := range ips {
		if !isPublicImageIP(resolved.IP) {
			return nil, fmt.Errorf("%w: generated image host resolves to a non-public address", ErrUnsafeDownloadURL)
		}
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
}

func isPublicImageIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}
	for _, prefix := range publicImageSpecialUsePrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

var publicImageSpecialUsePrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("100:0:0:1::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
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
