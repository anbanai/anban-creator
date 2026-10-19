package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

// WechatAccountAnalyticsService reads account-level facts. It intentionally
// does not use the generic per-content analytics projection: account totals
// are sums of dated account observations only.
type WechatAccountAnalyticsService struct{ repo repository.Repository }

var ErrWechatAccountAnalyticsUnavailable = errors.New("WeChat account analytics unavailable")

func NewWechatAccountAnalyticsService(repo repository.Repository) *WechatAccountAnalyticsService {
	return &WechatAccountAnalyticsService{repo: repo}
}

type WechatAccountCoverage struct {
	CoveredDays  int  `json:"covered_days"`
	ExpectedDays int  `json:"expected_days"`
	Complete     bool `json:"complete"`
}

type WechatAccountComparison struct {
	Label string   `json:"label"`
	Value *float64 `json:"value"`
	From  string   `json:"from,omitempty"`
	To    string   `json:"to,omitempty"`
}

// WechatAccountComparisonSet keeps the two comparison windows used by the
// account overview. Single-day ranges expose previous_day; longer ranges use
// previous_period for the immediately preceding equal-length window. Both
// range types may expose previous_week when that window is fully covered.
type WechatAccountComparisonSet struct {
	PreviousDay    *WechatAccountComparison `json:"previous_day,omitempty"`
	PreviousPeriod *WechatAccountComparison `json:"previous_period,omitempty"`
	PreviousWeek   *WechatAccountComparison `json:"previous_week,omitempty"`
}

type WechatAccountOverview struct {
	Revision           int64                                  `json:"revision"`
	From               string                                 `json:"from"`
	To                 string                                 `json:"to"`
	DataThrough        *string                                `json:"data_through"`
	CoveredDates       []string                               `json:"covered_dates"`
	Coverage           WechatAccountCoverage                  `json:"coverage"`
	MetricCoverage     map[string]WechatAccountCoverage       `json:"metric_coverage"`
	Series             []map[string]any                       `json:"series"`
	Totals             map[string]*int64                      `json:"totals"`
	Comparisons        map[string]*WechatAccountComparisonSet `json:"comparisons,omitempty"`
	UnavailableMetrics map[string]string                      `json:"unavailable_metrics,omitempty"`
}

type WechatTrafficSourceItem struct {
	Source string   `json:"source"`
	Label  string   `json:"label"`
	Value  *int64   `json:"value"`
	Share  *float64 `json:"share"`
}

type WechatTrafficSourcesResponse struct {
	Revision     int64                     `json:"revision"`
	From         string                    `json:"from"`
	To           string                    `json:"to"`
	Metric       string                    `json:"metric"`
	Total        *int64                    `json:"total"`
	Coverage     WechatAccountCoverage     `json:"coverage"`
	CoveredDates []string                  `json:"covered_dates"`
	DataQuality  *string                   `json:"data_quality"`
	Series       []map[string]any          `json:"series"`
	Items        []WechatTrafficSourceItem `json:"items"`
}

var wechatAccountMetrics = []string{
	"read_users", "share_users", "like_users", "collection_users",
	"comment_count", "follower_gain_count", "published_articles", "original_read_users",
}

func wechatAccountMetricValid(metric string) bool {
	for _, candidate := range wechatAccountMetrics {
		if metric == candidate {
			return true
		}
	}
	return false
}

func wechatAccountMetricValue(day *model.WechatAccountAnalyticsDay, metric string) *int64 {
	switch metric {
	case "read_users":
		return day.ReadUsers
	case "share_users":
		return day.ShareUsers
	case "original_read_users":
		return day.OriginalReadUsers
	case "like_users":
		return day.LikeUsers
	case "collection_users":
		return day.CollectionUsers
	case "comment_count":
		return day.CommentCount
	case "follower_gain_count":
		return day.FollowerGainCount
	case "published_articles":
		return day.PublishedArticles
	default:
		return nil
	}
}

func parseWechatAccountDate(value string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", value, repository.AnalyticsLocation)
}

func formatWechatAccountDate(value time.Time) string {
	return value.In(repository.AnalyticsLocation).Format("2006-01-02")
}

func validateWechatAccountRange(from, to string) (time.Time, time.Time, error) {
	start, err := parseWechatAccountDate(from)
	if err != nil || start.Format("2006-01-02") != from {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: invalid from date", ErrAnalyticsInvalidQuery)
	}
	finish, err := parseWechatAccountDate(to)
	if err != nil || finish.Format("2006-01-02") != to {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: invalid to date", ErrAnalyticsInvalidQuery)
	}
	if finish.Before(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: to must be on or after from", ErrAnalyticsInvalidQuery)
	}
	if finish.Sub(start)/(24*time.Hour) > 3660 {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: date range cannot exceed 10 years", ErrAnalyticsInvalidQuery)
	}
	return start, finish, nil
}

func dateList(start, finish time.Time) []string {
	result := make([]string, 0, int(finish.Sub(start)/(24*time.Hour))+1)
	for current := start; !current.After(finish); current = current.AddDate(0, 0, 1) {
		result = append(result, formatWechatAccountDate(current))
	}
	return result
}

func datesSet(days []*model.WechatAccountAnalyticsDay) map[string]*model.WechatAccountAnalyticsDay {
	result := make(map[string]*model.WechatAccountAnalyticsDay, len(days))
	for _, day := range days {
		if day != nil {
			result[day.StatDate] = day
		}
	}
	return result
}

func coverageFor(expected []string, days []*model.WechatAccountAnalyticsDay) (WechatAccountCoverage, []string) {
	set := datesSet(days)
	covered := make([]string, 0, len(set))
	for _, date := range expected {
		if _, ok := set[date]; ok {
			covered = append(covered, date)
		}
	}
	return WechatAccountCoverage{CoveredDays: len(covered), ExpectedDays: len(expected), Complete: len(covered) == len(expected)}, covered
}

func metricCoverageFor(expected []string, days []*model.WechatAccountAnalyticsDay, metric string) WechatAccountCoverage {
	set := datesSet(days)
	covered := 0
	for _, date := range expected {
		if day := set[date]; day != nil && wechatAccountMetricValue(day, metric) != nil {
			covered++
		}
	}
	return WechatAccountCoverage{CoveredDays: covered, ExpectedDays: len(expected), Complete: covered == len(expected)}
}

func sumWechatMetric(days []*model.WechatAccountAnalyticsDay, metric string) (*int64, string) {
	var total int64
	seen := false
	for _, day := range days {
		if day == nil {
			return nil, "所选日期范围内存在未提供的日期"
		}
		value := wechatAccountMetricValue(day, metric)
		if value == nil {
			return nil, "该指标在所选日期范围内存在未提供的日期"
		}
		seen = true
		total += *value
	}
	if !seen {
		return nil, "该指标暂无可用数据"
	}
	return &total, ""
}

func sumAvailableWechatMetric(days []*model.WechatAccountAnalyticsDay, metric string) (*int64, string) {
	var total int64
	seen := false
	for _, day := range days {
		if day == nil {
			continue
		}
		value := wechatAccountMetricValue(day, metric)
		if value == nil {
			continue
		}
		seen = true
		total += *value
	}
	if !seen {
		return nil, "该指标暂无可用数据"
	}
	return &total, ""
}

func previousWechatRange(start, finish time.Time) (time.Time, time.Time) {
	days := int(finish.Sub(start)/(24*time.Hour)) + 1
	previousFinish := start.AddDate(0, 0, -1)
	return previousFinish.AddDate(0, 0, -(days - 1)), previousFinish
}

func previousWechatWeekRange(start, finish time.Time) (time.Time, time.Time) {
	return start.AddDate(0, 0, -7), finish.AddDate(0, 0, -7)
}

func percentChange(current, previous int64) *float64 {
	if previous == 0 {
		return nil
	}
	value := float64(current-previous) / float64(previous)
	return &value
}

func (s *WechatAccountAnalyticsService) ownedProject(ctx context.Context, userID, projectID string) (*model.Project, error) {
	if s == nil || s.repo == nil {
		return nil, ErrWechatAccountAnalyticsUnavailable
	}
	project, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if project.UserID != userID {
		return nil, ErrAnalyticsForbidden
	}
	if project.Platform != model.PlatformWechat {
		return nil, ErrAnalyticsForbidden
	}
	return project, nil
}

// Overview reads all fields, including the revision, inside one repository
// transaction so a concurrent import/revoke cannot produce a mixed response.
func (s *WechatAccountAnalyticsService) Overview(ctx context.Context, userID, projectID, from, to string) (*WechatAccountOverview, error) {
	if s == nil || s.repo == nil {
		return nil, ErrWechatAccountAnalyticsUnavailable
	}
	var result *WechatAccountOverview
	err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		reader := &WechatAccountAnalyticsService{repo: tx}
		var err error
		result, err = reader.overview(ctx, userID, projectID, from, to)
		return err
	})
	return result, err
}

func (s *WechatAccountAnalyticsService) overview(ctx context.Context, userID, projectID, from, to string) (*WechatAccountOverview, error) {
	if _, err := s.ownedProject(ctx, userID, projectID); err != nil {
		return nil, err
	}
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	start, finish, err := validateWechatAccountRange(from, to)
	if err != nil {
		return nil, err
	}
	expected := dateList(start, finish)
	current, err := s.repo.WechatAccountAnalytics().ListWinningDays(ctx, projectID, from, to)
	if err != nil {
		return nil, err
	}
	coverage, coveredDates := coverageFor(expected, current)
	set := datesSet(current)
	series := make([]map[string]any, 0, len(expected))
	for _, date := range expected {
		point := map[string]any{"date": date}
		if day := set[date]; day != nil {
			for _, metric := range wechatAccountMetrics {
				value := wechatAccountMetricValue(day, metric)
				if value == nil {
					point[metric] = nil
				} else {
					point[metric] = *value
				}
			}
		} else {
			for _, metric := range wechatAccountMetrics {
				point[metric] = nil
			}
		}
		series = append(series, point)
	}
	totals := make(map[string]*int64, len(wechatAccountMetrics))
	metricCoverage := make(map[string]WechatAccountCoverage, len(wechatAccountMetrics))
	unavailable := make(map[string]string)
	for _, metric := range wechatAccountMetrics {
		metricCoverage[metric] = metricCoverageFor(expected, current, metric)
		value, reason := sumAvailableWechatMetric(current, metric)
		totals[metric] = value
		if reason != "" {
			unavailable[metric] = reason
		}
	}
	previousStart, previousFinish := previousWechatRange(start, finish)
	previous, err := s.repo.WechatAccountAnalytics().ListWinningDays(ctx, projectID, formatWechatAccountDate(previousStart), formatWechatAccountDate(previousFinish))
	if err != nil {
		return nil, err
	}
	previousExpected := dateList(previousStart, previousFinish)
	previousCoverage, _ := coverageFor(previousExpected, previous)
	previousTotals := make(map[string]*int64, len(wechatAccountMetrics))
	previousMetricCoverage := make(map[string]WechatAccountCoverage, len(wechatAccountMetrics))
	for _, metric := range wechatAccountMetrics {
		previousTotals[metric], _ = sumWechatMetric(previous, metric)
		previousMetricCoverage[metric] = metricCoverageFor(previousExpected, previous, metric)
	}
	previousWeekStart, previousWeekFinish := previousWechatWeekRange(start, finish)
	previousWeek, err := s.repo.WechatAccountAnalytics().ListWinningDays(ctx, projectID, formatWechatAccountDate(previousWeekStart), formatWechatAccountDate(previousWeekFinish))
	if err != nil {
		return nil, err
	}
	previousWeekExpected := dateList(previousWeekStart, previousWeekFinish)
	previousWeekCoverage, _ := coverageFor(previousWeekExpected, previousWeek)
	previousWeekTotals := make(map[string]*int64, len(wechatAccountMetrics))
	previousWeekMetricCoverage := make(map[string]WechatAccountCoverage, len(wechatAccountMetrics))
	for _, metric := range wechatAccountMetrics {
		previousWeekTotals[metric], _ = sumWechatMetric(previousWeek, metric)
		previousWeekMetricCoverage[metric] = metricCoverageFor(previousWeekExpected, previousWeek, metric)
	}
	comparisons := make(map[string]*WechatAccountComparisonSet, len(wechatAccountMetrics))
	periodLabel := fmt.Sprintf("较前一周期（%s 至 %s）", formatWechatAccountDate(previousStart), formatWechatAccountDate(previousFinish))
	if previousStart.Equal(previousFinish) {
		periodLabel = "较前一日"
	}
	weekLabel := fmt.Sprintf("较上周同期（%s 至 %s）", formatWechatAccountDate(previousWeekStart), formatWechatAccountDate(previousWeekFinish))
	for _, metric := range wechatAccountMetrics {
		set := &WechatAccountComparisonSet{}
		comparison := &WechatAccountComparison{Label: periodLabel, From: formatWechatAccountDate(previousStart), To: formatWechatAccountDate(previousFinish)}
		if metricCoverage[metric].Complete && previousMetricCoverage[metric].Complete && previousCoverage.Complete && totals[metric] != nil && previousTotals[metric] != nil {
			comparison.Value = percentChange(*totals[metric], *previousTotals[metric])
		}
		if previousStart.Equal(previousFinish) {
			set.PreviousDay = comparison
		} else {
			set.PreviousPeriod = comparison
		}
		weekComparison := &WechatAccountComparison{Label: weekLabel, From: formatWechatAccountDate(previousWeekStart), To: formatWechatAccountDate(previousWeekFinish)}
		if metricCoverage[metric].Complete && previousWeekMetricCoverage[metric].Complete && previousWeekCoverage.Complete && totals[metric] != nil && previousWeekTotals[metric] != nil {
			weekComparison.Value = percentChange(*totals[metric], *previousWeekTotals[metric])
		}
		set.PreviousWeek = weekComparison
		comparisons[metric] = set
	}
	var dataThrough *string
	if len(coveredDates) > 0 {
		latest := coveredDates[len(coveredDates)-1]
		dataThrough = &latest
	}
	revision, err := s.repo.WechatAccountAnalytics().CurrentRevision(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return &WechatAccountOverview{Revision: revision, From: from, To: to, DataThrough: dataThrough, CoveredDates: coveredDates, Coverage: coverage, MetricCoverage: metricCoverage, Series: series, Totals: totals, Comparisons: comparisons, UnavailableMetrics: unavailable}, nil
}

var wechatTrafficLabels = map[model.WechatTrafficSource]string{
	model.WechatTrafficSourceMessage:             "公众号消息",
	model.WechatTrafficSourceRecommendation:      "推荐",
	model.WechatTrafficSourceChat:                "聊天",
	model.WechatTrafficSourceMoments:             "朋友圈",
	model.WechatTrafficSourceOfficialAccountHome: "公众号主页",
	model.WechatTrafficSourceSearch:              "搜一搜",
	model.WechatTrafficSourceOther:               "其他",
}

// TrafficSources uses the same snapshot guarantee as Overview.
func (s *WechatAccountAnalyticsService) TrafficSources(ctx context.Context, userID, projectID, from, to, metric, source string) (*WechatTrafficSourcesResponse, error) {
	if s == nil || s.repo == nil {
		return nil, ErrWechatAccountAnalyticsUnavailable
	}
	var result *WechatTrafficSourcesResponse
	err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		reader := &WechatAccountAnalyticsService{repo: tx}
		var err error
		result, err = reader.trafficSources(ctx, userID, projectID, from, to, metric, source)
		return err
	})
	return result, err
}

func (s *WechatAccountAnalyticsService) trafficSources(ctx context.Context, userID, projectID, from, to, metric, source string) (*WechatTrafficSourcesResponse, error) {
	if _, err := s.ownedProject(ctx, userID, projectID); err != nil {
		return nil, err
	}
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	start, finish, err := validateWechatAccountRange(from, to)
	if err != nil {
		return nil, err
	}
	metric = strings.TrimSpace(metric)
	if metric == "" {
		metric = "read_users"
	}
	if metric != "read_users" {
		return nil, fmt.Errorf("%w: traffic source analysis only supports read_users", ErrAnalyticsInvalidQuery)
	}
	var selected model.WechatTrafficSource
	if strings.TrimSpace(source) != "" && strings.TrimSpace(source) != "all" {
		selected, err = model.NormalizeWechatTrafficSource(source)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrAnalyticsInvalidQuery, err)
		}
	}
	expected := dateList(start, finish)
	days, err := s.repo.WechatAccountAnalytics().ListWinningDays(ctx, projectID, from, to)
	if err != nil {
		return nil, err
	}
	coverage, coveredDates := coverageFor(expected, days)
	total, reason := sumWechatMetric(days, metric)
	if !coverage.Complete {
		total = nil
		reason = "所选日期范围内存在未提供的日期，来源总量暂不可用"
	}
	sources, err := s.repo.WechatAccountAnalytics().ListWinningSources(ctx, projectID, from, to)
	if err != nil {
		return nil, err
	}
	dayByDate := datesSet(days)
	sourceByDate := make(map[model.WechatTrafficSource]map[string]*model.WechatTrafficSourceDay)
	type aggregate struct {
		value   int64
		missing bool
		dates   map[string]bool
	}
	aggregates := make(map[model.WechatTrafficSource]*aggregate)
	for _, row := range sources {
		if row == nil {
			continue
		}
		byDate := sourceByDate[row.Source]
		if byDate == nil {
			byDate = make(map[string]*model.WechatTrafficSourceDay)
			sourceByDate[row.Source] = byDate
		}
		byDate[row.StatDate] = row
		item := aggregates[row.Source]
		if item == nil {
			item = &aggregate{dates: make(map[string]bool)}
			aggregates[row.Source] = item
		}
		item.dates[row.StatDate] = true
		if row.ReadUsers == nil {
			item.missing = true
		} else {
			item.value += *row.ReadUsers
		}
	}
	items := make([]WechatTrafficSourceItem, 0, len(aggregates))
	var sourceSum int64
	allSourceDatesCovered := true
	coveredSet := make(map[string]bool, len(coveredDates))
	for _, date := range coveredDates {
		coveredSet[date] = true
	}
	for sourceKey, aggregate := range aggregates {
		var value *int64
		if metric == "read_users" && !aggregate.missing {
			v := aggregate.value
			value = &v
			sourceSum += v
		} else {
			allSourceDatesCovered = false
		}
		for date := range coveredSet {
			if !aggregate.dates[date] {
				allSourceDatesCovered = false
				break
			}
		}
		items = append(items, WechatTrafficSourceItem{Source: string(sourceKey), Label: wechatTrafficLabels[sourceKey], Value: value})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Value == nil {
			return false
		}
		if items[j].Value == nil {
			return true
		}
		if *items[i].Value != *items[j].Value {
			return *items[i].Value > *items[j].Value
		}
		return items[i].Source < items[j].Source
	})
	if total == nil {
		if reason == "" {
			reason = "账号总量不可用，无法计算来源占比"
		}
	} else if metric != "read_users" {
		reason = "官方来源明细仅支持阅读人数"
	} else if *total == 0 {
		reason = "账号阅读人数为 0，无法计算来源占比"
	} else if !coverage.Complete || !allSourceDatesCovered || sourceSum != *total {
		reason = "来源明细与账号阅读人数未在相同覆盖范围内对账，占比暂不可用"
	} else {
		for i := range items {
			if items[i].Value != nil {
				share := float64(*items[i].Value) / float64(*total)
				items[i].Share = &share
			}
		}
	}
	if selected != "" {
		filtered := items[:0]
		for _, item := range items {
			if item.Source == string(selected) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	series := make([]map[string]any, 0, len(expected))
	selectedDates := sourceByDate[selected]
	for _, date := range expected {
		point := map[string]any{"date": date}
		if metric == "read_users" {
			if selected != "" {
				if row := selectedDates[date]; row != nil && row.ReadUsers != nil {
					point[metric] = *row.ReadUsers
				} else {
					point[metric] = nil
				}
			} else if day := dayByDate[date]; day != nil && day.ReadUsers != nil {
				point[metric] = *day.ReadUsers
			} else {
				point[metric] = nil
			}
		} else {
			point[metric] = nil
		}
		series = append(series, point)
	}
	var quality *string
	if reason != "" {
		quality = &reason
	}
	revision, err := s.repo.WechatAccountAnalytics().CurrentRevision(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return &WechatTrafficSourcesResponse{Revision: revision, From: from, To: to, Metric: metric, Total: total, Coverage: coverage, CoveredDates: coveredDates, DataQuality: quality, Series: series, Items: items}, nil
}
