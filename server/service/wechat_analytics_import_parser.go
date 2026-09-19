package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/extrame/xls"
	"github.com/tealeg/xlsx/v3"
	"golang.org/x/text/unicode/norm"
)

const (
	WechatAnalyticsImportParserVersion = "wechat-xls-v1"
	MaxWechatAnalyticsImportRows       = 5000
	MaxWechatAnalyticsImportBytes      = 20 * 1024 * 1024
)

var (
	ErrWechatAnalyticsWorkbookInvalid     = errors.New("invalid WeChat analytics workbook")
	ErrWechatAnalyticsWorkbookUnsupported = errors.New("unsupported WeChat analytics workbook")
)

type ParsedWechatAnalyticsWorkbook struct {
	Source string
	Rows   []ParsedWechatAnalyticsRow
}

type ParsedWechatAnalyticsRow struct {
	SourceRow              int               `json:"source_row"`
	Source                 string            `json:"source"`
	Title                  string            `json:"title"`
	NormalizedTitle        string            `json:"-"`
	PublishedDate          *time.Time        `json:"published_date,omitempty"`
	ArticleURL             string            `json:"article_url,omitempty"`
	ReadUsers              *int64            `json:"read_users,omitempty"`
	ShareUsers             *int64            `json:"share_users,omitempty"`
	ReadToFollowUsers      *int64            `json:"read_to_follow_users,omitempty"`
	DeliveredUsers         *int64            `json:"delivered_users,omitempty"`
	DeliveryCompletionRate *float64          `json:"delivery_completion_rate,omitempty"`
	ReadCompletionRate     *float64          `json:"read_completion_rate,omitempty"`
	Raw                    map[string]string `json:"raw"`
	ParseError             string            `json:"parse_error,omitempty"`
}

var wechatAnalyticsHeaders = []string{
	"数据来源概况", "内容标题", "发表日期", "阅读人数", "分享人数",
	"阅读后关注人数", "送达人数", "送达完成率", "阅读完成率", "内容url",
}

var wechatAnalyticsRequiredHeaders = wechatAnalyticsHeaders[1:]

func ParseWechatAnalyticsWorkbook(data []byte, location *time.Location) (*ParsedWechatAnalyticsWorkbook, error) {
	if len(data) == 0 || len(data) > MaxWechatAnalyticsImportBytes {
		return nil, fmt.Errorf("%w: file size must be between 1 and %d bytes", ErrWechatAnalyticsWorkbookInvalid, MaxWechatAnalyticsImportBytes)
	}
	if location == nil {
		location = time.FixedZone("CST", 8*60*60)
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
	headerIndex, columns := findWechatHeader(rows)
	if headerIndex < 0 {
		return nil, fmt.Errorf("%w: required headers were not found", ErrWechatAnalyticsWorkbookUnsupported)
	}
	if len(rows)-headerIndex-1 > MaxWechatAnalyticsImportRows {
		return nil, fmt.Errorf("%w: workbook contains more than %d data rows", ErrWechatAnalyticsWorkbookUnsupported, MaxWechatAnalyticsImportRows)
	}
	source := "wechat_console"
	for i := 0; i < headerIndex; i++ {
		for _, value := range rows[i] {
			if strings.TrimSpace(value) == "数据来源概况" {
				source = strings.TrimSpace(value)
			}
		}
	}
	result := &ParsedWechatAnalyticsWorkbook{Source: source, Rows: make([]ParsedWechatAnalyticsRow, 0, len(rows)-headerIndex-1)}
	for i := headerIndex + 1; i < len(rows); i++ {
		if rowEmpty(rows[i]) {
			continue
		}
		parsedRow := parseWechatAnalyticsRow(i+1, rows[i], columns, location)
		if parsedRow.Source == "" {
			parsedRow.Source = source
			parsedRow.Raw["数据来源概况"] = source
		}
		result.Rows = append(result.Rows, parsedRow)
	}
	return result, nil
}

func readWechatXLS(data []byte) ([][]string, error) {
	wb, err := xls.OpenReader(bytes.NewReader(data), "utf-8")
	if err != nil {
		return nil, fmt.Errorf("%w: open xls: %v", ErrWechatAnalyticsWorkbookInvalid, err)
	}
	if wb.NumSheets() == 0 || wb.GetSheet(0) == nil {
		return nil, fmt.Errorf("%w: first worksheet not found", ErrWechatAnalyticsWorkbookInvalid)
	}
	return wb.ReadAllCells(MaxWechatAnalyticsImportRows + 2), nil
}

func readWechatXLSX(data []byte) ([][]string, error) {
	file, err := xlsx.OpenBinary(data)
	if err != nil {
		return nil, fmt.Errorf("%w: open xlsx: %v", ErrWechatAnalyticsWorkbookInvalid, err)
	}
	if len(file.Sheets) == 0 {
		return nil, fmt.Errorf("%w: first worksheet not found", ErrWechatAnalyticsWorkbookInvalid)
	}
	rows := make([][]string, 0, file.Sheets[0].MaxRow)
	err = file.Sheets[0].ForEachRow(func(row *xlsx.Row) error {
		values := make([]string, 0, file.Sheets[0].MaxCol)
		err := row.ForEachCell(func(cell *xlsx.Cell) error {
			values = append(values, strings.TrimSpace(cell.String()))
			return nil
		})
		rows = append(rows, values)
		return err
	}, xlsx.SkipEmptyRows)
	if err != nil {
		return nil, fmt.Errorf("%w: read xlsx rows: %v", ErrWechatAnalyticsWorkbookInvalid, err)
	}
	return rows, nil
}

func findWechatHeader(rows [][]string) (int, map[string]int) {
	for i, row := range rows {
		columns := make(map[string]int, len(row))
		for col, value := range row {
			columns[strings.TrimSpace(value)] = col
		}
		missing := false
		for _, header := range wechatAnalyticsRequiredHeaders {
			if _, ok := columns[header]; !ok {
				missing = true
				break
			}
		}
		if !missing {
			return i, columns
		}
	}
	return -1, nil
}

func parseWechatAnalyticsRow(sourceRow int, row []string, columns map[string]int, location *time.Location) ParsedWechatAnalyticsRow {
	value := func(header string) string {
		index, ok := columns[header]
		if !ok || index >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[index])
	}
	parsed := ParsedWechatAnalyticsRow{
		SourceRow:       sourceRow,
		Source:          value("数据来源概况"),
		Title:           value("内容标题"),
		NormalizedTitle: normalizeWechatAnalyticsTitle(value("内容标题")),
		ArticleURL:      normalizeWechatArticleURL(value("内容url")),
		Raw:             map[string]string{},
	}
	for _, header := range wechatAnalyticsHeaders {
		parsed.Raw[header] = value(header)
	}
	if parsed.Title == "" {
		parsed.ParseError = "内容标题不能为空"
		return parsed
	}
	var errorsFound []string
	parsed.PublishedDate = parseWechatDate(value("发表日期"), location)
	if value("发表日期") != "" && parsed.PublishedDate == nil {
		errorsFound = append(errorsFound, "发表日期格式无效")
	}
	parsed.ReadUsers, errorsFound = parseWechatInteger(value("阅读人数"), "阅读人数", errorsFound)
	parsed.ShareUsers, errorsFound = parseWechatInteger(value("分享人数"), "分享人数", errorsFound)
	parsed.ReadToFollowUsers, errorsFound = parseWechatInteger(value("阅读后关注人数"), "阅读后关注人数", errorsFound)
	parsed.DeliveredUsers, errorsFound = parseWechatInteger(value("送达人数"), "送达人数", errorsFound)
	parsed.DeliveryCompletionRate, errorsFound = parseWechatRate(value("送达完成率"), "送达完成率", errorsFound)
	parsed.ReadCompletionRate, errorsFound = parseWechatRate(value("阅读完成率"), "阅读完成率", errorsFound)
	if rawURL := value("内容url"); rawURL != "" && parsed.ArticleURL == "" {
		errorsFound = append(errorsFound, "内容url 格式无效")
	}
	parsed.ParseError = strings.Join(errorsFound, "; ")
	return parsed
}

func parseWechatDate(value string, location *time.Location) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	for _, layout := range []string{"20060102", "2006-01-02", "2006/01/02", time.RFC3339} {
		if parsed, err := time.ParseInLocation(layout, value, location); err == nil {
			return &parsed
		}
	}
	if number, err := strconv.ParseFloat(value, 64); err == nil && number > 20000 && number < 100000 {
		parsed := time.Unix(int64((number-25569)*86400), 0).In(location)
		return &parsed
	}
	return nil
}

func parseWechatInteger(value, field string, errorsFound []string) (*int64, []string) {
	if strings.TrimSpace(value) == "" {
		return nil, errorsFound
	}
	parsed, err := strconv.ParseInt(strings.ReplaceAll(strings.TrimSpace(value), ",", ""), 10, 64)
	if err != nil || parsed < 0 {
		return nil, append(errorsFound, field+"必须是非负整数")
	}
	return &parsed, errorsFound
}

func parseWechatRate(value, field string, errorsFound []string) (*float64, []string) {
	if strings.TrimSpace(value) == "" {
		return nil, errorsFound
	}
	raw := strings.TrimSpace(strings.TrimSuffix(value, "%"))
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return nil, append(errorsFound, field+"必须是百分比")
	}
	if strings.Contains(value, "%") || parsed > 1 {
		parsed /= 100
	}
	if parsed < 0 || parsed > 1 {
		return nil, append(errorsFound, field+"必须在 0 到 1 之间")
	}
	return &parsed, errorsFound
}

func normalizeWechatAnalyticsTitle(value string) string {
	value = norm.NFKC.String(strings.TrimSpace(value))
	return strings.Join(strings.FieldsFunc(value, func(r rune) bool { return unicode.IsSpace(r) }), "")
}

func normalizeWechatArticleURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	return parsed.String()
}

func rowEmpty(row []string) bool {
	for _, value := range row {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

func marshalWechatRaw(row ParsedWechatAnalyticsRow) string {
	raw, _ := json.Marshal(row.Raw)
	return string(raw)
}
