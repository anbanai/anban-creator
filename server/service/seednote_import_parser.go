package service

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const (
	SeednoteImportParserVersion = "1"
	MaxSeednoteImportRows       = 1000
	MaxSeednoteImportBytes      = 20 * 1024 * 1024
)

var (
	ErrSeednoteWorkbookInvalid     = errors.New("invalid seednote workbook")
	ErrSeednoteWorkbookUnsupported = errors.New("unsupported seednote workbook")
)

type SeednoteWorkbookMetadata struct {
	Creator  string     `json:"creator,omitempty"`
	Created  *time.Time `json:"created_at,omitempty"`
	Modified *time.Time `json:"modified_at,omitempty"`
}

type ParsedSeednoteWorkbook struct {
	Metadata SeednoteWorkbookMetadata `json:"metadata"`
	Rows     []ParsedSeednoteRow      `json:"rows"`
}

type ParsedSeednoteRow struct {
	SourceRow        int               `json:"source_row"`
	Title            string            `json:"title"`
	NormalizedTitle  string            `json:"normalized_title"`
	FirstPublishedAt *time.Time        `json:"first_published_at,omitempty"`
	Genre            string            `json:"genre,omitempty"`
	Exposure         *int64            `json:"exposure_count,omitempty"`
	ViewCount        *int64            `json:"view_count,omitempty"`
	CoverClickRate   *float64          `json:"cover_click_rate,omitempty"`
	LikeCount        *int64            `json:"like_count,omitempty"`
	CommentCount     *int64            `json:"comment_count,omitempty"`
	CollectCount     *int64            `json:"collect_count,omitempty"`
	FollowerGain     *int64            `json:"follower_gain_count,omitempty"`
	ShareCount       *int64            `json:"share_count,omitempty"`
	AvgWatchDuration *float64          `json:"avg_watch_duration,omitempty"`
	BarrageCount     *int64            `json:"barrage_count,omitempty"`
	Raw              map[string]string `json:"raw"`
	ParseError       string            `json:"parse_error,omitempty"`
}

var seednoteImportHeaders = []string{
	"笔记标题", "首次发布时间", "体裁", "曝光", "观看量", "封面点击率",
	"点赞", "评论", "收藏", "涨粉", "分享", "人均观看时长", "弹幕",
}

type xlsxWorksheet struct {
	Rows []xlsxRow `xml:"sheetData>row"`
}

type xlsxRow struct {
	Number int        `xml:"r,attr"`
	Cells  []xlsxCell `xml:"c"`
}

type xlsxCell struct {
	Ref    string `xml:"r,attr"`
	Type   string `xml:"t,attr"`
	Value  string `xml:"v"`
	Inline struct {
		Text string `xml:"t"`
	} `xml:"is"`
}

type xlsxSharedStrings struct {
	Items []struct {
		Text string `xml:"t"`
	} `xml:"si"`
}

type xlsxCoreProperties struct {
	Creator  string `xml:"creator"`
	Created  string `xml:"created"`
	Modified string `xml:"modified"`
}

func ParseSeednoteWorkbook(data []byte, location *time.Location) (*ParsedSeednoteWorkbook, error) {
	if len(data) == 0 || len(data) > MaxSeednoteImportBytes {
		return nil, fmt.Errorf("%w: file size must be between 1 and %d bytes", ErrSeednoteWorkbookInvalid, MaxSeednoteImportBytes)
	}
	if location == nil {
		location = time.FixedZone("CST", 8*60*60)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: open xlsx: %v", ErrSeednoteWorkbookInvalid, err)
	}
	entries := make(map[string]*zip.File, len(zr.File))
	for _, file := range zr.File {
		entries[file.Name] = file
	}
	contentTypes, ok := entries["[Content_Types].xml"]
	if !ok {
		return nil, fmt.Errorf("%w: missing content types", ErrSeednoteWorkbookInvalid)
	}
	contentXML, err := readZipEntry(contentTypes, 2*1024*1024)
	if err != nil {
		return nil, err
	}
	if bytes.Contains(bytes.ToLower(contentXML), []byte("macroenabled")) {
		return nil, fmt.Errorf("%w: macro-enabled workbooks are not accepted", ErrSeednoteWorkbookUnsupported)
	}
	worksheetFile := entries["xl/worksheets/sheet1.xml"]
	if worksheetFile == nil {
		return nil, fmt.Errorf("%w: first worksheet not found", ErrSeednoteWorkbookInvalid)
	}
	worksheetXML, err := readZipEntry(worksheetFile, 32*1024*1024)
	if err != nil {
		return nil, err
	}
	var worksheet xlsxWorksheet
	if err := xml.Unmarshal(worksheetXML, &worksheet); err != nil {
		return nil, fmt.Errorf("%w: decode worksheet: %v", ErrSeednoteWorkbookInvalid, err)
	}
	shared, err := parseSharedStrings(entries["xl/sharedStrings.xml"])
	if err != nil {
		return nil, err
	}
	rows := make([]map[int]string, 0, len(worksheet.Rows))
	rowNumbers := make([]int, 0, len(worksheet.Rows))
	for _, row := range worksheet.Rows {
		values := make(map[int]string, len(row.Cells))
		for _, cell := range row.Cells {
			col, err := xlsxColumnIndex(cell.Ref)
			if err != nil {
				continue
			}
			value := strings.TrimSpace(cell.Value)
			switch cell.Type {
			case "inlineStr":
				value = cell.Inline.Text
			case "s":
				idx, convErr := strconv.Atoi(value)
				if convErr == nil && idx >= 0 && idx < len(shared) {
					value = shared[idx]
				}
			}
			values[col] = strings.TrimSpace(value)
		}
		rows = append(rows, values)
		rowNumbers = append(rowNumbers, row.Number)
	}
	headerIndex, columns := findSeednoteHeader(rows)
	if headerIndex < 0 {
		return nil, fmt.Errorf("%w: required official headers were not found", ErrSeednoteWorkbookUnsupported)
	}
	if len(rows)-headerIndex-1 > MaxSeednoteImportRows {
		return nil, fmt.Errorf("%w: workbook contains more than %d data rows", ErrSeednoteWorkbookUnsupported, MaxSeednoteImportRows)
	}
	result := &ParsedSeednoteWorkbook{Rows: make([]ParsedSeednoteRow, 0, len(rows)-headerIndex-1)}
	result.Metadata = parseWorkbookMetadata(entries["docProps/core.xml"])
	for i := headerIndex + 1; i < len(rows); i++ {
		if rowIsEmpty(rows[i]) {
			continue
		}
		result.Rows = append(result.Rows, parseSeednoteRow(rowNumbers[i], rows[i], columns, location))
	}
	return result, nil
}

func readZipEntry(file *zip.File, maxBytes int64) ([]byte, error) {
	if file == nil {
		return nil, nil
	}
	if int64(file.UncompressedSize64) > maxBytes {
		return nil, fmt.Errorf("%w: xlsx entry %q is too large", ErrSeednoteWorkbookInvalid, file.Name)
	}
	r, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("%w: open %q: %v", ErrSeednoteWorkbookInvalid, file.Name, err)
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil || int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: read %q", ErrSeednoteWorkbookInvalid, file.Name)
	}
	return data, nil
}

func parseSharedStrings(file *zip.File) ([]string, error) {
	if file == nil {
		return nil, nil
	}
	data, err := readZipEntry(file, 16*1024*1024)
	if err != nil {
		return nil, err
	}
	var shared xlsxSharedStrings
	if err := xml.Unmarshal(data, &shared); err != nil {
		return nil, fmt.Errorf("%w: decode shared strings: %v", ErrSeednoteWorkbookInvalid, err)
	}
	values := make([]string, len(shared.Items))
	for i := range shared.Items {
		values[i] = shared.Items[i].Text
	}
	return values, nil
}

func parseWorkbookMetadata(file *zip.File) SeednoteWorkbookMetadata {
	data, err := readZipEntry(file, 1024*1024)
	if err != nil || len(data) == 0 {
		return SeednoteWorkbookMetadata{}
	}
	var core xlsxCoreProperties
	if xml.Unmarshal(data, &core) != nil {
		return SeednoteWorkbookMetadata{}
	}
	return SeednoteWorkbookMetadata{
		Creator:  strings.TrimSpace(core.Creator),
		Created:  parseRFC3339Pointer(core.Created),
		Modified: parseRFC3339Pointer(core.Modified),
	}
}

func parseRFC3339Pointer(raw string) *time.Time {
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw))
	if err != nil {
		return nil
	}
	return &t
}

func findSeednoteHeader(rows []map[int]string) (int, map[string]int) {
	for i, row := range rows {
		columns := make(map[string]int)
		for col, value := range row {
			columns[strings.TrimSpace(value)] = col
		}
		all := true
		for _, header := range seednoteImportHeaders {
			if _, ok := columns[header]; !ok {
				all = false
				break
			}
		}
		if all {
			return i, columns
		}
	}
	return -1, nil
}

func parseSeednoteRow(sourceRow int, values map[int]string, columns map[string]int, location *time.Location) ParsedSeednoteRow {
	raw := make(map[string]string, len(seednoteImportHeaders))
	for _, header := range seednoteImportHeaders {
		raw[header] = values[columns[header]]
	}
	row := ParsedSeednoteRow{
		SourceRow: sourceRow,
		Title:     strings.TrimSpace(raw["笔记标题"]),
		Genre:     strings.TrimSpace(raw["体裁"]),
		Raw:       raw,
	}
	row.NormalizedTitle = NormalizeSeednoteTitle(row.Title)
	errs := make([]string, 0, 4)
	if row.Title == "" {
		errs = append(errs, "笔记标题为空")
	}
	if published, err := parseSeednoteTime(raw["首次发布时间"], location); err != nil {
		errs = append(errs, err.Error())
	} else {
		row.FirstPublishedAt = &published
	}
	row.Exposure = parseSeednoteCount("曝光", raw["曝光"], &errs)
	row.ViewCount = parseSeednoteCount("观看量", raw["观看量"], &errs)
	row.CoverClickRate = parseSeednoteRate("封面点击率", raw["封面点击率"], &errs)
	row.LikeCount = parseSeednoteCount("点赞", raw["点赞"], &errs)
	row.CommentCount = parseSeednoteCount("评论", raw["评论"], &errs)
	row.CollectCount = parseSeednoteCount("收藏", raw["收藏"], &errs)
	row.FollowerGain = parseSeednoteCount("涨粉", raw["涨粉"], &errs)
	row.ShareCount = parseSeednoteCount("分享", raw["分享"], &errs)
	row.AvgWatchDuration = parseSeednoteNumber("人均观看时长", raw["人均观看时长"], &errs)
	row.BarrageCount = parseSeednoteCount("弹幕", raw["弹幕"], &errs)
	row.ParseError = strings.Join(errs, "；")
	return row
}

func parseSeednoteTime(raw string, location *time.Location) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{"2006年01月02日15时04分05秒", "2006-01-02 15:04:05", time.RFC3339} {
		var t time.Time
		var err error
		if layout == time.RFC3339 {
			t, err = time.Parse(layout, raw)
		} else {
			t, err = time.ParseInLocation(layout, raw, location)
		}
		if err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("首次发布时间格式无效")
}

func parseSeednoteCount(label, raw string, errs *[]string) *int64 {
	value := parseSeednoteNumber(label, raw, errs)
	if value == nil {
		return nil
	}
	if *value != float64(int64(*value)) {
		*errs = append(*errs, label+"必须是整数")
		return nil
	}
	result := int64(*value)
	return &result
}

func parseSeednoteRate(label, raw string, errs *[]string) *float64 {
	raw = strings.TrimSpace(raw)
	percent := strings.HasSuffix(raw, "%")
	if percent {
		raw = strings.TrimSpace(strings.TrimSuffix(raw, "%"))
	}
	value := parseSeednoteNumber(label, raw, errs)
	if value == nil {
		return nil
	}
	if percent {
		*value /= 100
	}
	if *value > 1 {
		*errs = append(*errs, label+"必须在 0 到 100% 之间")
		return nil
	}
	return value
}

func parseSeednoteNumber(label, raw string, errs *[]string) *float64 {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, ",", ""))
	if raw == "" {
		return nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value < 0 {
		*errs = append(*errs, label+"不是有效的非负数")
		return nil
	}
	return &value
}

func NormalizeSeednoteTitle(value string) string {
	value = norm.NFKC.String(strings.TrimSpace(value))
	return strings.Join(strings.FieldsFunc(value, unicode.IsSpace), " ")
}

func xlsxColumnIndex(ref string) (int, error) {
	index := 0
	found := false
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		found = true
		index = index*26 + int(r-'A'+1)
	}
	if !found {
		return 0, fmt.Errorf("invalid cell reference")
	}
	return index - 1, nil
}

func rowIsEmpty(row map[int]string) bool {
	for _, value := range row {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}
