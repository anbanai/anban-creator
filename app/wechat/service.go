package wechat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"mime/multipart"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/rs/zerolog"
	"github.com/royalrick/anbanwriter/app/config"
	"github.com/silenceper/wechat/v2"
	wechatcache "github.com/silenceper/wechat/v2/cache"
	"github.com/silenceper/wechat/v2/officialaccount"
	wechatconfig "github.com/silenceper/wechat/v2/officialaccount/config"
	"github.com/silenceper/wechat/v2/officialaccount/draft"
	"github.com/silenceper/wechat/v2/officialaccount/material"
)

// accessTokenRegex matches access_token=<value> in URL query strings.
var accessTokenRegex = regexp.MustCompile(`access_token=[^&\s"]+`)

// maskAccessToken replaces access_token=<value> with access_token=*** in a string
// to prevent leaking credentials in log and error messages.
func maskAccessToken(s string) string {
	return accessTokenRegex.ReplaceAllString(s, "access_token=***")
}

// maskedError wraps an error with its message masked to prevent leaking
// access tokens in error strings. Supports Unwrap for errors.Is/As.
type maskedError struct {
	msg string
	err error
}

func (e *maskedError) Error() string   { return e.msg }
func (e *maskedError) Unwrap() error   { return e.err }

// maskError wraps err so that Error() returns the access-token-masked message.
// The original error is preserved via Unwrap for errors.Is/As compatibility.
func maskError(prefix string, err error) error {
	return &maskedError{
		msg: fmt.Sprintf("%s: %s", prefix, maskAccessToken(err.Error())),
		err: err,
	}
}

// Service 微信服务
type Service struct {
	cfg *config.Config
	log zerolog.Logger
	oa  *officialaccount.OfficialAccount
}

// NewService 创建微信服务
func NewService(cfg *config.Config, log zerolog.Logger) *Service {
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
		s.log.Error().Str("path", filePath).Str("error", maskAccessToken(err.Error())).Msg("upload material failed")
		if wErr := ParseWechatError(err); wErr != nil {
			return nil, wErr
		}
		return nil, maskError("upload material", err)
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
		s.log.Error().Str("error", maskAccessToken(err.Error())).Msg("create draft failed")
		return nil, maskError("create draft", err)
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

// NewspicImageItem 小绿书图片项
type NewspicImageItem struct {
	ImageMediaID string `json:"image_media_id"`
}

// NewspicImageInfo 小绿书图片信息
type NewspicImageInfo struct {
	ImageList []NewspicImageItem `json:"image_list"`
}

// NewspicArticle 小绿书文章
type NewspicArticle struct {
	Title              string           `json:"title"`
	Content            string           `json:"content"`
	ArticleType        string           `json:"article_type"`
	ImageInfo          NewspicImageInfo `json:"image_info"`
	NeedOpenComment    int              `json:"need_open_comment,omitempty"`
	OnlyFansCanComment int              `json:"only_fans_can_comment,omitempty"`
}

// NewspicDraftRequest 小绿书草稿请求
type NewspicDraftRequest struct {
	Articles []NewspicArticle `json:"articles"`
}

// NewspicDraftResponse 微信 API 响应
type NewspicDraftResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
	MediaID string `json:"media_id"`
}

// CreateNewspicDraft 创建小绿书草稿（直接调用微信 API，SDK 不支持 newspic）
func (s *Service) CreateNewspicDraft(articles []NewspicArticle) (*CreateDraftResult, error) {
	startTime := time.Now()

	// 获取 access_token
	oa := s.getOfficialAccount()
	accessToken, err := oa.GetAccessToken()
	if err != nil {
		return nil, maskError("get access token", err)
	}

	// 构造请求
	req := NewspicDraftRequest{Articles: articles}
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// 调用微信 API
	apiURL := fmt.Sprintf("https://api.weixin.qq.com/cgi-bin/draft/add?access_token=%s", accessToken)

	httpResp, err := http.Post(apiURL, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("call wechat api: %s", maskAccessToken(err.Error()))
	}
	defer httpResp.Body.Close()

	// 解析响应
	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var resp NewspicDraftResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	// 检查错误
	if resp.ErrCode != 0 {
		s.log.Error().Int("errcode", resp.ErrCode).Str("errmsg", resp.ErrMsg).Msg("create newspic draft failed")
		syntheticErr := fmt.Errorf("errcode=%d, %s", resp.ErrCode, resp.ErrMsg)
		if wErr := ParseWechatError(syntheticErr); wErr != nil {
			return nil, wErr
		}
		return nil, syntheticErr
	}

	duration := time.Since(startTime)
	s.log.Info().Str("media_id", MaskMediaID(resp.MediaID)).Dur("duration", duration).Msg("xls draft created")

	return &CreateDraftResult{
		MediaID:  resp.MediaID,
		DraftURL: fmt.Sprintf("https://mp.weixin.qq.com/cgi-bin/appmsg?t=media/appmsg_edit_v2&action=edit&createType=0&token="),
	}, nil
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
		s.log.Error().Str("error", maskAccessToken(err.Error())).Msg("list drafts failed")
		if wErr := ParseWechatError(err); wErr != nil {
			return nil, wErr
		}
		return nil, maskError("list drafts", err)
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
	oa := s.getOfficialAccount()
	fp := oa.GetFreePublish()

	list, err := fp.Paginate(offset, count, true)
	if err != nil {
		if wErr := ParseWechatError(err); wErr != nil {
			s.log.Debug().Int("errcode", wErr.ErrCode).Str("msg", wErr.UserMsg).Msg("list published failed")
			return nil, wErr
		}
		s.log.Error().Str("error", maskAccessToken(err.Error())).Msg("list published failed")
		return nil, maskError("list published", err)
	}

	result := &ListPublishedResult{
		TotalCount: list.TotalCount,
		ItemCount:  list.ItemCount,
		Items:      make([]PublishedItem, 0, len(list.Item)),
	}

	for _, item := range list.Item {
		pi := PublishedItem{
			ArticleID:  item.ArticleID,
			UpdateTime: item.UpdateTime,
		}
		if len(item.Content.NewsItem) > 0 {
			pi.Title = item.Content.NewsItem[0].Title
			pi.Digest = item.Content.NewsItem[0].Digest
			pi.URL = item.Content.NewsItem[0].URL
		}
		result.Items = append(result.Items, pi)
	}

	return result, nil
}

// UploadMaterialFromBytes 从字节数据上传素材
func (s *Service) UploadMaterialFromBytes(data []byte, filename string) (*UploadMaterialResult, error) {
	// 创建临时文件
	tmpFile, err := os.CreateTemp("", "anbanwriter_*_"+filename)
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
		return nil, maskError("get access token", err)
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
			s.log.Info().Str("error", maskAccessToken(err.Error())).Msg("upload error is not retryable, aborting")
			return nil, err
		}
		if i < maxRetries-1 {
			delay := time.Duration(math.Pow(2, float64(i+1)) * float64(time.Second))
				jitter := time.Duration(rand.Int63n(int64(delay / 2)))
				delay += jitter
			s.log.Info().Int("attempt", i+2).Dur("delay", delay).Msg("retrying upload")
			time.Sleep(delay)
		}
	}
	return nil, lastErr
}

// DownloadFile 下载文件到临时目录
func DownloadFile(url string) (string, error) {
	// 创建 HTTP 客户端
	client := &http.Client{
		Timeout: 60 * time.Second,
	}

	// 发起请求
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed with status: %d", resp.StatusCode)
	}

	// 创建临时文件
	// 从 URL 路径中提取扩展名，排除查询参数
	ext := ".jpg" // 默认扩展名
	if parsedURL, err := neturl.Parse(url); err == nil {
		if pathExt := filepath.Ext(parsedURL.Path); pathExt != "" {
			ext = pathExt
		}
	}
	tmpFile, err := os.CreateTemp("", "anbanwriter_download_*"+ext)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer tmpFile.Close()
	tmpPath := tmpFile.Name()

	// 写入文件
	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("write file: %w", err)
	}

	return tmpPath, nil
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
