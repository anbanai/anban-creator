package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	appwechat "github.com/anbanai/anban-creator/server/app/wechat"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	maxWechatAccountSyncDays = 31
	wechatUserSummaryMaxDays = 7
)

var (
	ErrWechatAccountAnalyticsAuthorizationRequired = errors.New("a connected WeChat Open Platform account is required")
	ErrWechatAccountAnalyticsCapabilityMissing     = errors.New("WeChat authorization lacks user analytics capability")
	ErrWechatAccountAnalyticsSyncRange             = errors.New("invalid WeChat account analytics sync range")
)

type WechatUserSummaryAPI interface {
	GetUserSummary(context.Context, appwechat.UserSummaryRequest) (*appwechat.UserSummaryResponse, error)
}

type WechatUserSummaryAPIFactory func(context.Context, *model.Project, *model.WechatOpenPlatformAuthorization) (WechatUserSummaryAPI, error)

type WechatAccountAnalyticsSyncService struct {
	repo       repository.Repository
	apiFactory WechatUserSummaryAPIFactory
	now        func() time.Time
}

type WechatAccountAnalyticsSyncResult struct {
	Revision    int64                              `json:"revision"`
	Batch       *model.WechatAccountAnalyticsBatch `json:"batch"`
	From        string                             `json:"from"`
	To          string                             `json:"to"`
	AccountDays int                                `json:"account_days"`
}

type wechatUserSummarySource struct {
	Date       string `json:"date"`
	UserSource int    `json:"user_source"`
	NewUser    int64  `json:"new_user"`
	CancelUser int64  `json:"cancel_user"`
}

type wechatUserSummaryFingerprint struct {
	From string                    `json:"from"`
	To   string                    `json:"to"`
	Rows []wechatUserSummarySource `json:"rows"`
}

type wechatUserSummaryTotals struct {
	NewUser    int64
	CancelUser int64
	Sources    map[model.WechatTrafficSource]*wechatUserSummarySourceTotals
}

type wechatUserSummarySourceTotals struct {
	NewUser    int64
	CancelUser int64
	RawRows    []wechatUserSummarySource
}

func NewWechatAccountAnalyticsSyncService(repo repository.Repository, apiFactory WechatUserSummaryAPIFactory) *WechatAccountAnalyticsSyncService {
	return &WechatAccountAnalyticsSyncService{repo: repo, apiFactory: apiFactory, now: time.Now}
}

func (s *WechatAccountAnalyticsSyncService) Sync(ctx context.Context, userID, projectID, from, to string) (*WechatAccountAnalyticsSyncResult, error) {
	if s == nil || s.repo == nil || s.apiFactory == nil {
		return nil, ErrWechatAccountAnalyticsUnavailable
	}
	project, err := s.repo.Projects().FindByID(ctx, strings.TrimSpace(projectID))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrProjectNotFound
	}
	if err != nil {
		return nil, err
	}
	if project.UserID != userID || project.Platform != model.PlatformWechat {
		return nil, ErrAnalyticsForbidden
	}

	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	start, requestedFinish, err := validateWechatAccountRange(from, to)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWechatAccountAnalyticsSyncRange, err)
	}
	loc := repository.AnalyticsLocation
	yesterday := s.now().In(loc).AddDate(0, 0, -1)
	if requestedFinish.After(yesterday) {
		requestedFinish = yesterday
	}
	if requestedFinish.Before(start) {
		return nil, fmt.Errorf("%w: only dates through yesterday are available", ErrWechatAccountAnalyticsSyncRange)
	}
	if int(requestedFinish.Sub(start)/(24*time.Hour))+1 > maxWechatAccountSyncDays {
		return nil, fmt.Errorf("%w: a single sync can cover at most %d days", ErrWechatAccountAnalyticsSyncRange, maxWechatAccountSyncDays)
	}
	from = formatWechatAccountDate(start)
	to = formatWechatAccountDate(requestedFinish)

	authorization, err := s.repo.WechatOpenPlatformAuthorizations().FindByProjectID(ctx, project.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrWechatAccountAnalyticsAuthorizationRequired
	}
	if err != nil {
		return nil, err
	}
	if authorization.Status != model.WechatAuthorizationStatusConnected {
		return nil, ErrWechatAccountAnalyticsAuthorizationRequired
	}
	if !hasWechatScope(authorization.GrantedScopes, wechatOpenPlatformAccountAnalyticsScope) {
		return nil, ErrWechatAccountAnalyticsCapabilityMissing
	}
	api, err := s.apiFactory(ctx, project, authorization)
	if err != nil {
		return nil, err
	}
	if api == nil {
		return nil, ErrWechatAccountAnalyticsUnavailable
	}

	rows, err := s.fetchUserSummary(ctx, api, start, requestedFinish)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	days, sources, err := normalizeWechatUserSummary(project.ID, rows, start, requestedFinish, now)
	if err != nil {
		return nil, err
	}
	request, err := json.Marshal(wechatUserSummaryFingerprint{From: from, To: to, Rows: rows})
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(request)
	requestHash := hex.EncodeToString(hash[:])
	key := "wechat-user-summary:" + from + ":" + to + ":" + requestHash
	if len(key) > 128 {
		return nil, fmt.Errorf("%w: generated idempotency key exceeds storage limit", ErrWechatAccountAnalyticsUnavailable)
	}
	batch := &model.WechatAccountAnalyticsBatch{
		ID: uuid.NewString(), UserID: userID, ProjectID: project.ID,
		FileName: "微信官方账号统计", Source: "wechat_open_platform_user_summary",
		Timezone: repository.AnalyticsLocation.String(), ParserVersion: "wechat-user-summary-v1",
		Status: model.WechatAccountAnalyticsBatchCompleted, IdempotencyKey: key, RequestHash: requestHash,
		RawProvenance: string(request), ReceivedAt: now, EffectiveAt: now, DataAsOfAt: now,
		CreatedAt: now, UpdatedAt: now,
	}
	for _, day := range days {
		day.BatchID = batch.ID
	}
	for _, source := range sources {
		source.BatchID = batch.ID
	}

	result := &WechatAccountAnalyticsSyncResult{Batch: batch, From: from, To: to, AccountDays: len(days)}
	err = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		state, err := tx.Analytics().LockProject(ctx, project.ID)
		if err != nil {
			return err
		}
		analyticsRepo := tx.WechatAccountAnalytics()
		if existing, findErr := analyticsRepo.FindBatchByIdempotency(ctx, project.ID, key); findErr == nil {
			if existing.RequestHash != requestHash {
				return repository.ErrAnalyticsIdempotencyConflict
			}
			result.Batch = existing
			result.Revision = state.Revision
			return nil
		} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}
		if err := analyticsRepo.CreateBatch(ctx, batch); err != nil {
			return err
		}
		if err := analyticsRepo.CreateDays(ctx, days); err != nil {
			return err
		}
		if err := analyticsRepo.CreateSources(ctx, sources); err != nil {
			return err
		}
		if err := tx.Analytics().AdvanceRevision(ctx, state); err != nil {
			return err
		}
		result.Revision = state.Revision
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("persist WeChat account analytics sync: %w", err)
	}
	return result, nil
}

func (s *WechatAccountAnalyticsSyncService) fetchUserSummary(ctx context.Context, api WechatUserSummaryAPI, start, finish time.Time) ([]wechatUserSummarySource, error) {
	rows := make([]wechatUserSummarySource, 0)
	for chunkStart := start; !chunkStart.After(finish); {
		chunkFinish := chunkStart.AddDate(0, 0, wechatUserSummaryMaxDays-1)
		if chunkFinish.After(finish) {
			chunkFinish = finish
		}
		response, err := api.GetUserSummary(ctx, appwechat.UserSummaryRequest{
			BeginDate: formatWechatAccountDate(chunkStart), EndDate: formatWechatAccountDate(chunkFinish),
		})
		if err != nil {
			return nil, fmt.Errorf("fetch WeChat user summary for %s..%s: %w", formatWechatAccountDate(chunkStart), formatWechatAccountDate(chunkFinish), err)
		}
		if response == nil {
			return nil, errors.New("WeChat user summary returned an empty response")
		}
		for _, item := range response.List {
			row := wechatUserSummarySource{Date: item.RefDate, UserSource: item.UserSource, NewUser: item.NewUser, CancelUser: item.CancelUser}
			date, err := time.ParseInLocation("2006-01-02", row.Date, repository.AnalyticsLocation)
			if err != nil || date.Format("2006-01-02") != row.Date || date.Before(chunkStart) || date.After(chunkFinish) {
				return nil, fmt.Errorf("invalid WeChat user summary date %q", row.Date)
			}
			if row.NewUser < 0 || row.CancelUser < 0 {
				return nil, fmt.Errorf("invalid negative WeChat user summary value for %s", row.Date)
			}
			rows = append(rows, row)
		}
		chunkStart = chunkFinish.AddDate(0, 0, 1)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Date != rows[j].Date {
			return rows[i].Date < rows[j].Date
		}
		return rows[i].UserSource < rows[j].UserSource
	})
	return rows, nil
}

func normalizeWechatUserSummary(projectID string, rows []wechatUserSummarySource, start, finish time.Time, now time.Time) ([]*model.WechatAccountAnalyticsDay, []*model.WechatTrafficSourceDay, error) {
	daily := make(map[string]*wechatUserSummaryTotals)
	for _, row := range rows {
		rowDate, err := time.ParseInLocation("2006-01-02", row.Date, repository.AnalyticsLocation)
		if err != nil || rowDate.Format("2006-01-02") != row.Date || rowDate.Before(start) || rowDate.After(finish) {
			return nil, nil, fmt.Errorf("WeChat user summary contains an out-of-range date %q", row.Date)
		}
		day := daily[row.Date]
		if day == nil {
			day = &wechatUserSummaryTotals{Sources: make(map[model.WechatTrafficSource]*wechatUserSummarySourceTotals)}
			daily[row.Date] = day
		}
		source, _ := wechatUserSource(row.UserSource)
		sourceTotals := day.Sources[source]
		if sourceTotals == nil {
			sourceTotals = &wechatUserSummarySourceTotals{}
			day.Sources[source] = sourceTotals
		}
		sourceTotals.NewUser += row.NewUser
		sourceTotals.CancelUser += row.CancelUser
		sourceTotals.RawRows = append(sourceTotals.RawRows, row)
		day.NewUser += row.NewUser
		day.CancelUser += row.CancelUser
	}

	days := make([]*model.WechatAccountAnalyticsDay, 0, len(daily))
	sources := make([]*model.WechatTrafficSourceDay, 0)
	dates := make([]string, 0, len(daily))
	for date := range daily {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	for _, date := range dates {
		day := daily[date]
		newUsers, canceledUsers := day.NewUser, day.CancelUser
		netGain := newUsers - canceledUsers
		rawData, err := json.Marshal(map[string]any{"new_user": newUsers, "cancel_user": canceledUsers, "net_gain": netGain})
		if err != nil {
			return nil, nil, err
		}
		days = append(days, &model.WechatAccountAnalyticsDay{
			ID: uuid.NewString(), ProjectID: projectID, StatDate: date,
			FollowerGainCount: int64Address(newUsers), FollowerCancelCount: int64Address(canceledUsers),
			FollowerNetGainCount: int64Address(netGain), Source: "wechat_open_platform_user_summary",
			RawData: string(rawData), CreatedAt: now, UpdatedAt: now,
		})
		for source, totals := range day.Sources {
			rawRows, err := json.Marshal(totals.RawRows)
			if err != nil {
				return nil, nil, err
			}
			_, originalLabel := wechatUserSource(userSourceCode(totals.RawRows))
			sources = append(sources, &model.WechatTrafficSourceDay{
				ID: uuid.NewString(), ProjectID: projectID, StatDate: date, Source: source,
				OriginalLabel: originalLabel, NewFollowers: int64Address(totals.NewUser),
				CancelledFollowers: int64Address(totals.CancelUser), ReadUsers: nil,
				RawData: string(rawRows), CreatedAt: now, UpdatedAt: now,
			})
		}
	}
	return days, sources, nil
}

func int64Address(value int64) *int64 { return &value }

func userSourceCode(rows []wechatUserSummarySource) int {
	if len(rows) == 0 {
		return 0
	}
	return rows[0].UserSource
}

func wechatUserSource(source int) (model.WechatTrafficSource, string) {
	switch source {
	case 0:
		return model.WechatTrafficSourceOther, "其他"
	case 1:
		return model.WechatTrafficSourceSearch, "公众号搜索"
	case 17:
		return model.WechatTrafficSourceBusinessCard, "名片分享"
	case 30:
		return model.WechatTrafficSourceQRCode, "扫描二维码"
	case 57:
		return model.WechatTrafficSourceArticleAccount, "文章内账号名称"
	case 100:
		return model.WechatTrafficSourceWechatAds, "微信广告"
	case 149:
		return model.WechatTrafficSourceMiniProgram, "小程序关注"
	case 161:
		return model.WechatTrafficSourceReprint, "他人转载"
	case 200:
		return model.WechatTrafficSourceChannels, "视频号"
	case 201:
		return model.WechatTrafficSourceLiveStream, "直播"
	default:
		return model.WechatTrafficSource(fmt.Sprintf("source_%d", source)), fmt.Sprintf("其他来源代码 %d", source)
	}
}
