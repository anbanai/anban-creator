package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
)

const WechatAccountAnalyticsImportParserVersion = "wechat-account-xls-v1"

type ParsedWechatAccountAnalyticsWorkbook struct {
	Source           string                            `json:"source"`
	Rows             []ParsedWechatAccountAnalyticsRow `json:"rows"`
	Sources          []ParsedWechatTrafficSourceRow    `json:"sources"`
	UnknownHeaders   []string                          `json:"unknown_headers,omitempty"`
	DuplicateDates   []string                          `json:"duplicate_dates,omitempty"`
	DuplicateSources []string                          `json:"duplicate_sources,omitempty"`
	Errors           []string                          `json:"errors,omitempty"`
}

type ParsedWechatAccountAnalyticsRow struct {
	SourceRow         int               `json:"source_row"`
	StatDate          string            `json:"stat_date"`
	ReadUsers         *int64            `json:"read_users"`
	ShareUsers        *int64            `json:"share_users"`
	OriginalReadUsers *int64            `json:"original_read_users"`
	LikeUsers         *int64            `json:"like_users"`
	CollectionUsers   *int64            `json:"collection_users"`
	CommentCount      *int64            `json:"comment_count"`
	FollowerGainCount *int64            `json:"follower_gain_count"`
	PublishedArticles *int64            `json:"published_articles"`
	Raw               map[string]string `json:"raw"`
	ParseError        string            `json:"parse_error,omitempty"`
}

type ParsedWechatTrafficSourceRow struct {
	SourceRow     int                       `json:"source_row"`
	StatDate      string                    `json:"stat_date"`
	Source        model.WechatTrafficSource `json:"source"`
	OriginalLabel string                    `json:"original_label"`
	ReadUsers     *int64                    `json:"read_users"`
	Raw           map[string]string         `json:"raw"`
	ParseError    string                    `json:"parse_error,omitempty"`
}

var wechatAccountDateHeaders = []string{"日期", "统计日期", "数据日期", "时间", "date", "stat_date"}

var wechatAccountMetricHeaders = map[string][]string{
	"read_users":          {"阅读人数", "阅读人数（人）", "阅读用户数", "阅读人数(人)", "read_users", "read users"},
	"share_users":         {"分享人数", "分享用户数", "share_users", "share users"},
	"original_read_users": {"跳转阅读原文", "跳转阅读原文人数", "阅读原文", "阅读原文人数", "original_read_users", "jump_original_users", "original article clicks"},
	"like_users":          {"点赞人数", "点赞数", "点赞用户数", "like_users", "likes"},
	"collection_users":    {"收藏人数", "收藏数", "收藏用户数", "collection_users", "收藏"},
	"comment_count":       {"留言人数", "评论数", "评论人数", "comment_count", "comments"},
	"follower_gain_count": {"新增关注", "新增关注人数", "净增关注", "follower_gain_count", "new followers"},
	"published_articles":  {"发表篇数", "发布篇数", "发文篇数", "published_articles", "articles"},
}

var wechatAccountSourceHeaders = map[model.WechatTrafficSource][]string{
	model.WechatTrafficSourceMessage:             {"消息", "公众号消息", "公众号会话", "message"},
	model.WechatTrafficSourceRecommendation:      {"朋友在看", "看一看", "推荐", "recommendation"},
	model.WechatTrafficSourceChat:                {"聊天", "朋友聊天", "chat"},
	model.WechatTrafficSourceMoments:             {"朋友圈", "moments"},
	model.WechatTrafficSourceOfficialAccountHome: {"公众号主页", "公众号主页访问", "official account home", "official_account_home"},
	model.WechatTrafficSourceSearch:              {"搜一搜", "搜索", "search"},
	model.WechatTrafficSourceOther:               {"其他", "其它", "other"},
}

var ErrWechatAccountAnalyticsWorkbookInvalid = errors.New("invalid WeChat account analytics workbook")
var ErrWechatAccountAnalyticsWorkbookUnsupported = errors.New("unsupported WeChat account analytics workbook")

func ParseWechatAccountAnalyticsWorkbook(data []byte, location *time.Location) (*ParsedWechatAccountAnalyticsWorkbook, error) {
	if len(data) == 0 || len(data) > MaxWechatAnalyticsImportBytes {
		return nil, fmt.Errorf("%w: file size must be between 1 and %d bytes", ErrWechatAccountAnalyticsWorkbookInvalid, MaxWechatAnalyticsImportBytes)
	}
	if location == nil {
		location = time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	var rows [][]string
	var err error
	if bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		rows, err = readWechatXLSX(data)
	} else {
		rows, err = readWechatXLS(data)
	}
	if err != nil {
		return nil, err
	}
	headerIndex, columns := findWechatAccountHeader(rows)
	if headerIndex < 0 {
		return nil, fmt.Errorf("%w: date and account metric headers were not found", ErrWechatAccountAnalyticsWorkbookUnsupported)
	}
	if len(rows)-headerIndex-1 > MaxWechatAnalyticsImportRows {
		return nil, fmt.Errorf("%w: workbook contains more than %d data rows", ErrWechatAccountAnalyticsWorkbookUnsupported, MaxWechatAnalyticsImportRows)
	}
	result := &ParsedWechatAccountAnalyticsWorkbook{Source: "wechat_console", Rows: make([]ParsedWechatAccountAnalyticsRow, 0), Sources: make([]ParsedWechatTrafficSourceRow, 0)}
	unknownHeaders := make([]string, 0)
	unknownHeaderSeen := map[string]bool{}
	sourceOnlyTable := hasWechatExplicitSourceColumn(columns)
	if _, headers, ok := parseWechatAccountHeader(rows[headerIndex]); ok {
		unknownHeaders = collectWechatAccountUnknownHeaders(unknownHeaders, unknownHeaderSeen, columns, headers)
	}
	for _, row := range rows[:headerIndex] {
		for _, v := range row {
			if strings.TrimSpace(v) == "数据来源概况" {
				result.Source = "wechat_console"
			}
		}
	}
	for i, row := range rows[headerIndex+1:] {
		if rowEmpty(row) {
			continue
		}
		if rowHeaderIndex, rowColumns := findWechatAccountHeader([][]string{row}); rowHeaderIndex == 0 {
			columns = rowColumns
			sourceOnlyTable = hasWechatExplicitSourceColumn(columns)
			if _, headers, ok := parseWechatAccountHeader(row); ok {
				unknownHeaders = collectWechatAccountUnknownHeaders(unknownHeaders, unknownHeaderSeen, columns, headers)
			}
			continue
		}
		sourceRow := headerIndex + i + 2
		value := func(index int) string {
			if index < 0 || index >= len(row) {
				return ""
			}
			return strings.TrimSpace(row[index])
		}
		raw := map[string]string{}
		for header, index := range columns {
			raw[header] = value(index)
		}
		date, dateErr := parseAccountDate(value(columns["__date"]), location)
		if dateErr != "" {
			result.Errors = append(result.Errors, fmt.Sprintf("第%d行：%s", sourceRow, dateErr))
			continue
		}
		if !sourceOnlyTable && hasWechatAccountMetricColumn(columns) {
			parsed := ParsedWechatAccountAnalyticsRow{SourceRow: sourceRow, StatDate: date, Raw: raw}
			for key := range wechatAccountMetricHeaders {
				idx := accountMetricIndex(columns, key)
				parsedMetric, msg := parseAccountMetric(value(idx), key)
				if msg != "" {
					parsed.ParseError = joinAccountError(parsed.ParseError, msg)
				}
				switch key {
				case "read_users":
					parsed.ReadUsers = parsedMetric
				case "share_users":
					parsed.ShareUsers = parsedMetric
				case "original_read_users":
					parsed.OriginalReadUsers = parsedMetric
				case "like_users":
					parsed.LikeUsers = parsedMetric
				case "collection_users":
					parsed.CollectionUsers = parsedMetric
				case "comment_count":
					parsed.CommentCount = parsedMetric
				case "follower_gain_count":
					parsed.FollowerGainCount = parsedMetric
				case "published_articles":
					parsed.PublishedArticles = parsedMetric
				}
			}
			if parsed.ParseError != "" {
				result.Errors = append(result.Errors, fmt.Sprintf("第%d行：%s", sourceRow, parsed.ParseError))
			}
			result.Rows = append(result.Rows, parsed)
		}
		for _, sourceColumn := range sourceColumnKeys(columns) {
			v, msg := parseAccountMetric(value(sourceColumn.index), "流量来源")
			if msg != "" {
				result.Errors = append(result.Errors, fmt.Sprintf("第%d行：%s", sourceRow, msg))
				continue
			}
			if v == nil && value(sourceColumn.index) == "" {
				continue
			}
			result.Sources = append(result.Sources, ParsedWechatTrafficSourceRow{SourceRow: sourceRow, StatDate: date, Source: sourceColumn.source, OriginalLabel: sourceLabelForColumn(columns, sourceColumn.index), ReadUsers: v, Raw: raw})
		}
		if sourceIndex, ok := columns["__source"]; ok {
			label := value(sourceIndex)
			if label != "" {
				source, sourceErr := model.NormalizeWechatTrafficSource(label)
				if sourceErr != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("第%d行：%s", sourceRow, sourceErr))
				} else if readIndex := accountMetricIndex(columns, "read_users"); readIndex >= 0 {
					v, msg := parseAccountMetric(value(readIndex), "阅读人数")
					if msg != "" {
						result.Errors = append(result.Errors, fmt.Sprintf("第%d行：%s", sourceRow, msg))
					} else {
						result.Sources = append(result.Sources, ParsedWechatTrafficSourceRow{SourceRow: sourceRow, StatDate: date, Source: source, OriginalLabel: label, ReadUsers: v, Raw: raw})
					}
				}
			}
		}
	}
	seen := map[string]bool{}
	for _, row := range result.Rows {
		if seen[row.StatDate] {
			result.DuplicateDates = appendUniqueString(result.DuplicateDates, row.StatDate)
		}
		seen[row.StatDate] = true
	}
	sourceSeen := map[string]bool{}
	for _, row := range result.Sources {
		key := row.StatDate + "\x00" + string(row.Source)
		if sourceSeen[key] {
			result.DuplicateSources = appendUniqueString(result.DuplicateSources, row.StatDate+" / "+string(row.Source))
		}
		sourceSeen[key] = true
	}
	if len(result.Rows) == 0 && len(result.Sources) == 0 {
		return nil, fmt.Errorf("%w: workbook has no usable data rows", ErrWechatAccountAnalyticsWorkbookUnsupported)
	}
	result.UnknownHeaders = unknownHeaders
	return result, nil
}

func recognizedWechatAccountColumn(columns map[string]int, index int) bool {
	if dateIndex, ok := columns["__date"]; ok && dateIndex == index {
		return true
	}
	for _, header := range wechatAccountDateHeaders {
		if dateIndex, ok := columns[header]; ok && dateIndex == index {
			return true
		}
	}
	for key := range wechatAccountMetricHeaders {
		if metricIndex, ok := columns[key]; ok && metricIndex == index {
			return true
		}
	}
	for source := range wechatAccountSourceHeaders {
		if sourceIndex, ok := columns["__source_"+string(source)]; ok && sourceIndex == index {
			return true
		}
	}
	if sourceIndex, ok := columns["__source"]; ok && sourceIndex == index {
		return true
	}
	return false
}

func hasWechatExplicitSourceColumn(columns map[string]int) bool {
	_, ok := columns["__source"]
	return ok
}

func hasWechatAccountMetricColumn(columns map[string]int) bool {
	for key := range wechatAccountMetricHeaders {
		if _, ok := columns[key]; ok {
			return true
		}
	}
	return false
}

func findWechatAccountHeader(rows [][]string) (int, map[string]int) {
	for i, row := range rows {
		if columns, _, ok := parseWechatAccountHeader(row); ok {
			return i, columns
		}
	}
	return -1, nil
}

func parseWechatAccountHeader(row []string) (map[string]int, []string, bool) {
	columns := map[string]int{}
	headers := make([]string, 0, len(row))
	for col, raw := range row {
		header := strings.TrimSpace(raw)
		if header == "" {
			continue
		}
		columns[header] = col
		headers = append(headers, header)
	}
	dateIndex := -1
	for _, header := range wechatAccountDateHeaders {
		if idx, ok := columns[header]; ok {
			dateIndex = idx
			break
		}
	}
	if dateIndex < 0 {
		return nil, nil, false
	}
	mapped := map[string]int{"__date": dateIndex}
	for key, aliases := range wechatAccountMetricHeaders {
		for _, alias := range aliases {
			if idx, ok := columns[alias]; ok {
				mapped[key] = idx
				break
			}
		}
	}
	for source, aliases := range wechatAccountSourceHeaders {
		for _, alias := range aliases {
			if idx, ok := columns[alias]; ok {
				mapped["__source_"+string(source)] = idx
				break
			}
		}
	}
	for _, alias := range []string{"来源", "流量来源", "source", "original_label"} {
		if idx, ok := columns[alias]; ok {
			mapped["__source"] = idx
			break
		}
	}
	if len(mapped) <= 1 {
		return nil, nil, false
	}
	return mergeAccountColumns(columns, mapped), headers, true
}

func collectWechatAccountUnknownHeaders(unknown []string, seen map[string]bool, columns map[string]int, headers []string) []string {
	for _, header := range headers {
		if seen[header] {
			continue
		}
		index, ok := columns[header]
		if !ok || recognizedWechatAccountColumn(columns, index) {
			continue
		}
		seen[header] = true
		unknown = append(unknown, header)
	}
	return unknown
}

func mergeAccountColumns(all, mapped map[string]int) map[string]int {
	result := map[string]int{}
	for h, i := range all {
		result[h] = i
	}
	for h, i := range mapped {
		result[h] = i
	}
	return result
}

func accountMetricIndex(columns map[string]int, key string) int {
	if idx, ok := columns[key]; ok {
		return idx
	}
	return -1
}

type wechatSourceColumn struct {
	source model.WechatTrafficSource
	index  int
}

func sourceColumnKeys(columns map[string]int) []wechatSourceColumn {
	result := make([]wechatSourceColumn, 0, len(wechatAccountSourceHeaders))
	for source := range wechatAccountSourceHeaders {
		if idx, ok := columns["__source_"+string(source)]; ok {
			result = append(result, wechatSourceColumn{source: source, index: idx})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].index < result[j].index })
	return result
}

func sourceLabelForColumn(columns map[string]int, index int) string {
	for header, i := range columns {
		if i == index && !strings.HasPrefix(header, "__") {
			return header
		}
	}
	return ""
}

func parseAccountDate(value string, location *time.Location) (string, string) {
	parsed := parseWechatDate(value, location)
	if parsed == nil {
		return "", "日期格式无效"
	}
	return parsed.Format("2006-01-02"), ""
}

func parseAccountMetric(value, field string) (*int64, string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, ""
	}
	n, err := strconv.ParseInt(strings.ReplaceAll(value, ",", ""), 10, 64)
	if err != nil || (field != "follower_gain_count" && n < 0) {
		if field == "follower_gain_count" {
			return nil, field + "必须是整数"
		}
		return nil, field + "必须是非负整数"
	}
	return &n, ""
}
func joinAccountError(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "; " + b
}
func appendUniqueString(items []string, value string) []string {
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}
func marshalWechatAccountRaw(raw map[string]string) string {
	data, _ := json.Marshal(raw)
	return string(data)
}
