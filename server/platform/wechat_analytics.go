package platform

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	appconfig "github.com/anbanai/anban-creator/app/config"
	appwechat "github.com/anbanai/anban-creator/app/wechat"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/rs/zerolog"
)

var (
	ErrWechatCredentialsMissing       = errors.New("project missing WeChat credentials")
	ErrWechatPublishedArticleNotFound = errors.New("article URL was not found in this official account")
)

type WechatPublishedArticle struct {
	ArticleID    string
	ArticleURL   string
	ArticleTitle string
	PublishedAt  time.Time
}

type WechatArticleTotal struct {
	RefDate string
	MsgID   string
	Title   string
	Details []WechatArticleMetric
}

type WechatArticleMetric struct {
	StatDate         string
	TargetUser       int
	IntPageReadUser  int
	IntPageReadCount int
	OriPageReadUser  int
	OriPageReadCount int
	ShareUser        int
	ShareCount       int
	AddToFavUser     int
	AddToFavCount    int
}

type WechatOfficialAnalyticsProvider struct {
	logger *zerolog.Logger
}

func NewWechatOfficialAnalyticsProvider(logger *zerolog.Logger) *WechatOfficialAnalyticsProvider {
	return &WechatOfficialAnalyticsProvider{logger: logger}
}

func (p *WechatOfficialAnalyticsProvider) service(project *model.Project) (*appwechat.Service, error) {
	if project == nil || project.GetWechatAppID() == "" || project.GetWechatSecret() == "" {
		return nil, ErrWechatCredentialsMissing
	}
	cfg := &appconfig.Config{}
	cfg.Wechat.AppID = project.GetWechatAppID()
	cfg.Wechat.Secret = project.GetWechatSecret()
	return appwechat.NewService(cfg, p.logger), nil
}

func (p *WechatOfficialAnalyticsProvider) ResolvePublishedArticle(ctx context.Context, project *model.Project, articleURL string) (*WechatPublishedArticle, error) {
	_ = ctx
	wanted, err := NormalizeWechatArticleURL(articleURL)
	if err != nil {
		return nil, err
	}
	client, err := p.service(project)
	if err != nil {
		return nil, err
	}
	const pageSize int64 = 20
	for offset := int64(0); ; offset += pageSize {
		result, err := client.ListPublishedWithContent(offset, pageSize)
		if err != nil {
			return nil, err
		}
		for _, item := range result.Items {
			candidate, normalizeErr := NormalizeWechatArticleURL(item.URL)
			if normalizeErr == nil && candidate == wanted {
				return &WechatPublishedArticle{
					ArticleID:    item.ArticleID,
					ArticleURL:   item.URL,
					ArticleTitle: item.Title,
					PublishedAt:  time.Unix(item.UpdateTime, 0),
				}, nil
			}
		}
		if result.ItemCount == 0 || offset+result.ItemCount >= result.TotalCount {
			break
		}
	}
	return nil, ErrWechatPublishedArticleNotFound
}

func (p *WechatOfficialAnalyticsProvider) FetchArticleTotals(ctx context.Context, project *model.Project, publicationDate string) ([]WechatArticleTotal, error) {
	_ = ctx
	client, err := p.service(project)
	if err != nil {
		return nil, err
	}
	items, err := client.GetArticleTotal(publicationDate, publicationDate)
	if err != nil {
		return nil, err
	}
	result := make([]WechatArticleTotal, 0, len(items))
	for _, item := range items {
		mapped := WechatArticleTotal{RefDate: item.RefDate, MsgID: item.MsgID, Title: item.Title}
		for _, detail := range item.Details {
			mapped.Details = append(mapped.Details, WechatArticleMetric{
				StatDate:         detail.StatDate,
				TargetUser:       detail.TargetUser,
				IntPageReadUser:  detail.IntPageReadUser,
				IntPageReadCount: detail.IntPageReadCount,
				OriPageReadUser:  detail.OriPageReadUser,
				OriPageReadCount: detail.OriPageReadCount,
				ShareUser:        detail.ShareUser,
				ShareCount:       detail.ShareCount,
				AddToFavUser:     detail.AddToFavUser,
				AddToFavCount:    detail.AddToFavCount,
			})
		}
		result = append(result, mapped)
	}
	return result, nil
}

// NormalizeWechatArticleURL validates an official-account article URL and
// removes share-session parameters while preserving the stable article keys.
func NormalizeWechatArticleURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || strings.ToLower(parsed.Hostname()) != "mp.weixin.qq.com" {
		return "", fmt.Errorf("invalid WeChat article URL")
	}
	if parsed.Path != "/s" && !strings.HasPrefix(parsed.Path, "/s/") {
		return "", fmt.Errorf("invalid WeChat article URL")
	}
	stable := url.Values{}
	for _, key := range []string{"__biz", "mid", "idx", "sn"} {
		if value := parsed.Query().Get(key); value != "" {
			stable.Set(key, value)
		}
	}
	parsed.Scheme = "https"
	parsed.Host = "mp.weixin.qq.com"
	parsed.RawQuery = stable.Encode()
	parsed.Fragment = ""
	return parsed.String(), nil
}

func LatestWechatMetric(details []WechatArticleMetric) (WechatArticleMetric, bool) {
	if len(details) == 0 {
		return WechatArticleMetric{}, false
	}
	ordered := append([]WechatArticleMetric(nil), details...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].StatDate < ordered[j].StatDate })
	return ordered[len(ordered)-1], true
}
