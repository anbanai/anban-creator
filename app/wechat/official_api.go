package wechat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const defaultOfficialAPIBaseURL = "https://api.weixin.qq.com"

// AccessTokenSource obtains a current official-account access token.
type AccessTokenSource func(context.Context) (string, error)

// OfficialAPI is the typed transport for the publication lifecycle endpoints.
// It intentionally avoids the SDK's integer publish ID types: WeChat IDs are
// provider identifiers and must remain strings end to end.
type OfficialAPI struct {
	client      *http.Client
	baseURL     string
	accessToken AccessTokenSource
}

func NewOfficialAPI(client *http.Client, baseURL string, accessToken AccessTokenSource) *OfficialAPI {
	if client == nil {
		client = http.DefaultClient
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultOfficialAPIBaseURL
	}
	return &OfficialAPI{client: client, baseURL: strings.TrimRight(baseURL, "/"), accessToken: accessToken}
}

type DraftArticle struct {
	Title              string `json:"title"`
	Author             string `json:"author,omitempty"`
	Digest             string `json:"digest,omitempty"`
	Content            string `json:"content,omitempty"`
	ContentSourceURL   string `json:"content_source_url,omitempty"`
	ThumbMediaID       string `json:"thumb_media_id,omitempty"`
	ShowCoverPic       int    `json:"show_cover_pic,omitempty"`
	NeedOpenComment    int    `json:"need_open_comment,omitempty"`
	OnlyFansCanComment int    `json:"only_fans_can_comment,omitempty"`
	URL                string `json:"url,omitempty"`
}

type DraftAddRequest struct {
	Articles []DraftArticle `json:"articles"`
}
type DraftAddResponse struct {
	MediaID string `json:"media_id"`
}
type DraftBatchGetRequest struct {
	Offset    int64 `json:"offset"`
	Count     int64 `json:"count"`
	NoContent bool  `json:"no_content"`
}
type DraftBatchGetResponse struct {
	TotalCount int64            `json:"total_count"`
	ItemCount  int64            `json:"item_count"`
	Items      []DraftBatchItem `json:"item"`
}
type DraftBatchItem struct {
	MediaID    string       `json:"media_id"`
	Content    DraftContent `json:"content"`
	UpdateTime int64        `json:"update_time"`
}
type DraftContent struct {
	NewsItems []DraftArticle `json:"news_item"`
}
type FreePublishSubmitRequest struct {
	MediaID string `json:"media_id"`
}
type FreePublishSubmitResponse struct {
	PublishID string `json:"publish_id"`
	MsgDataID string `json:"msg_data_id"`
}

// FreePublishStatus values are documented by the official freepublish/get API.
const (
	FreePublishStatusSucceeded      = 0
	FreePublishStatusPublishing     = 1
	FreePublishStatusOriginalFailed = 2
	FreePublishStatusFailed         = 3
	FreePublishStatusAuditRejected  = 4
	FreePublishStatusUserDeleted    = 5
	FreePublishStatusSystemBanned   = 6
)

type FreePublishGetRequest struct {
	PublishID string `json:"publish_id"`
}
type FreePublishGetResponse struct {
	PublishID     string                   `json:"publish_id"`
	PublishStatus int                      `json:"publish_status"`
	ArticleID     string                   `json:"article_id"`
	ArticleDetail FreePublishArticleDetail `json:"article_detail"`
	FailIndexes   []int                    `json:"fail_idx"`
}
type FreePublishArticleDetail struct {
	Count int                      `json:"count"`
	Items []FreePublishArticleItem `json:"item"`
}
type FreePublishArticleItem struct {
	Index      int    `json:"idx"`
	ArticleURL string `json:"article_url"`
}
type FreePublishBatchGetRequest struct {
	Offset    int64 `json:"offset"`
	Count     int64 `json:"count"`
	NoContent bool  `json:"no_content"`
}
type FreePublishBatchGetResponse struct {
	TotalCount int64                  `json:"total_count"`
	ItemCount  int64                  `json:"item_count"`
	Items      []FreePublishBatchItem `json:"item"`
}
type FreePublishBatchItem struct {
	ArticleID  string             `json:"article_id"`
	Content    FreePublishContent `json:"content"`
	UpdateTime int64              `json:"update_time"`
}
type FreePublishContent struct {
	NewsItems []DraftArticle `json:"news_item"`
}

type ArticleTotalDetailRequest struct {
	BeginDate string `json:"begin_date"`
	EndDate   string `json:"end_date"`
}
type ArticleTotalDetailResponse struct {
	IsDelay     bool                     `json:"is_delay"`
	List        []ArticleTotalDetailItem `json:"list"`
	RawResponse json.RawMessage          `json:"-"`
}

func (r *ArticleTotalDetailResponse) setRawResponse(raw []byte) {
	r.RawResponse = append(r.RawResponse[:0], raw...)
}

type ArticleTotalDetailItem struct {
	RefDate     string                     `json:"ref_date"`
	MsgID       string                     `json:"msgid"`
	ContentURL  string                     `json:"content_url"`
	Title       string                     `json:"title"`
	PublishType int                        `json:"publish_type"`
	DetailList  []ArticleTotalDetailMetric `json:"detail_list"`
}

// ArticleReadUserSource is one source bucket in WeChat article analytics.
type ArticleReadUserSource struct {
	UserCount int    `json:"user_count"`
	SceneDesc string `json:"scene_desc"`
}

// ArticleReadJumpPosition is one reading-position bucket in WeChat analytics.
type ArticleReadJumpPosition struct {
	Position int     `json:"position"`
	Rate     float64 `json:"rate"`
}

// ArticleTotalDetailMetric is one official getarticletotaldetail sample.
type ArticleTotalDetailMetric struct {
	StatDate          string                    `json:"stat_date"`
	ReadUser          int                       `json:"read_user"`
	ReadUserSource    []ArticleReadUserSource   `json:"read_user_source"`
	ShareUser         int                       `json:"share_user"`
	ZaikanUser        int                       `json:"zaikan_user"`
	LikeUser          int                       `json:"like_user"`
	CommentCount      int                       `json:"comment_count"`
	CollectionUser    int                       `json:"collection_user"`
	PraiseMoney       int                       `json:"praise_money"`
	ReadSubscribeUser int                       `json:"read_subscribe_user"`
	ReadDeliveryRate  float64                   `json:"read_delivery_rate"`
	ReadFinishRate    float64                   `json:"read_finish_rate"`
	ReadAvgActiveTime float64                   `json:"read_avg_activetime"`
	ReadJumpPosition  []ArticleReadJumpPosition `json:"read_jump_position"`
}

func (p *OfficialAPI) AddDraft(ctx context.Context, request DraftAddRequest) (*DraftAddResponse, error) {
	var response DraftAddResponse
	return &response, p.post(ctx, "/cgi-bin/draft/add", request, &response)
}
func (p *OfficialAPI) BatchGetDrafts(ctx context.Context, request DraftBatchGetRequest) (*DraftBatchGetResponse, error) {
	var response DraftBatchGetResponse
	return &response, p.post(ctx, "/cgi-bin/draft/batchget", request, &response)
}
func (p *OfficialAPI) SubmitFreePublish(ctx context.Context, request FreePublishSubmitRequest) (*FreePublishSubmitResponse, error) {
	var response FreePublishSubmitResponse
	return &response, p.post(ctx, "/cgi-bin/freepublish/submit", request, &response)
}
func (p *OfficialAPI) GetFreePublish(ctx context.Context, request FreePublishGetRequest) (*FreePublishGetResponse, error) {
	var response FreePublishGetResponse
	return &response, p.post(ctx, "/cgi-bin/freepublish/get", request, &response)
}
func (p *OfficialAPI) BatchGetFreePublishes(ctx context.Context, request FreePublishBatchGetRequest) (*FreePublishBatchGetResponse, error) {
	var response FreePublishBatchGetResponse
	return &response, p.post(ctx, "/cgi-bin/freepublish/batchget", request, &response)
}
func (p *OfficialAPI) GetArticleTotalDetail(ctx context.Context, request ArticleTotalDetailRequest) (*ArticleTotalDetailResponse, error) {
	var response ArticleTotalDetailResponse
	return &response, p.post(ctx, "/datacube/getarticletotaldetail", request, &response)
}

func (p *OfficialAPI) post(ctx context.Context, path string, payload, target any) error {
	if p == nil || p.accessToken == nil {
		return fmt.Errorf("wechat access token source is required")
	}
	token, err := p.accessToken(ctx)
	if err != nil {
		return sanitizeAccessTokenSourceError(err)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode WeChat request: %w", err)
	}
	endpoint, err := url.Parse(p.baseURL + path)
	if err != nil {
		return fmt.Errorf("build WeChat endpoint: %w", err)
	}
	query := endpoint.Query()
	query.Set("access_token", token)
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := p.client.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) && urlErr.Err != nil {
			return fmt.Errorf("call WeChat %s: %w", path, urlErr.Err)
		}
		return fmt.Errorf("call WeChat %s: %w", path, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("WeChat %s returned HTTP %d", path, response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("read WeChat %s response: %w", path, err)
	}
	var apiError struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(body, &apiError); err != nil {
		return fmt.Errorf("decode WeChat %s response: %w", path, err)
	}
	if apiError.ErrCode != 0 {
		raw := fmt.Errorf("errcode=%d errmsg=%s", apiError.ErrCode, apiError.ErrMsg)
		return ParseWechatError(raw)
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode WeChat %s response: %w", path, err)
	}
	if receiver, ok := target.(interface{ setRawResponse([]byte) }); ok {
		receiver.setRawResponse(body)
	}
	return nil
}

func sanitizeAccessTokenSourceError(err error) error {
	var apiErr *WechatAPIError
	if errors.As(err, &apiErr) {
		return sanitizedWechatAPIError(apiErr.ErrCode)
	}
	if parsed := ParseWechatError(err); parsed != nil {
		return sanitizedWechatAPIError(parsed.ErrCode)
	}
	return errors.New("get WeChat access token failed")
}

func sanitizedWechatAPIError(code int) *WechatAPIError {
	parsed := ParseWechatError(fmt.Errorf("errcode=%d", code))
	return &WechatAPIError{
		ErrCode:   parsed.ErrCode,
		UserMsg:   parsed.UserMsg,
		HintMsg:   parsed.HintMsg,
		Retryable: parsed.Retryable,
	}
}
