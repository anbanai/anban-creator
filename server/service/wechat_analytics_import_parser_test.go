package service

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/tealeg/xlsx/v3"
)

func TestParseWechatAnalyticsWorkbookXLSX(t *testing.T) {
	file := xlsx.NewFile()
	sheet, err := file.AddSheet("数据来源概况")
	if err != nil {
		t.Fatal(err)
	}
	headers := []string{"数据来源概况", "内容标题", "发表日期", "阅读人数", "分享人数", "阅读后关注人数", "送达人数", "送达完成率", "阅读完成率", "内容url"}
	row := sheet.AddRow()
	for _, header := range headers {
		row.AddCell().SetString(header)
	}
	row = sheet.AddRow()
	values := []string{"公众号后台", "白茶到底分几类？", "20260911", "642", "51", "7", "1", "0", "0.401869148015976", "http://mp.weixin.qq.com/s?mid=1#rd"}
	for _, value := range values {
		row.AddCell().SetString(value)
	}
	var buf bytes.Buffer
	if err := file.Write(&buf); err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseWechatAnalyticsWorkbook(buf.Bytes(), time.FixedZone("Asia/Shanghai", 8*60*60))
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Rows) != 1 || parsed.Rows[0].ReadUsers == nil || *parsed.Rows[0].ReadUsers != 642 {
		t.Fatalf("rows = %#v", parsed.Rows)
	}
	if parsed.Rows[0].PublishedDate == nil || parsed.Rows[0].PublishedDate.Format("20060102") != "20260911" {
		t.Fatalf("published date = %#v", parsed.Rows[0].PublishedDate)
	}
	if parsed.Rows[0].ArticleURL != "http://mp.weixin.qq.com/s?mid=1" {
		t.Fatalf("url = %q", parsed.Rows[0].ArticleURL)
	}
	if parsed.Rows[0].ReadCompletionRate == nil || *parsed.Rows[0].ReadCompletionRate != 0.401869148015976 {
		t.Fatalf("rate = %#v", parsed.Rows[0].ReadCompletionRate)
	}
}

func TestWechatImportedURLMustBeWebLink(t *testing.T) {
	for _, value := range []string{"javascript://example.com/alert", "ftp://mp.weixin.qq.com/s/one", "https://user:password@mp.weixin.qq.com/s/one"} {
		t.Run(value, func(t *testing.T) {
			if got := normalizeWechatArticleURL(value); got != "" {
				t.Fatalf("unsafe import link accepted: %q", got)
			}
		})
	}
}

func TestWechatAnalyticsImportReceiptUsesPublicBatchContract(t *testing.T) {
	summary := &WechatAnalyticsImportSummary{Batch: &model.WechatAnalyticsImportBatch{
		ID: "batch-1", FileName: "total.xls", TotalRows: 120, MatchedRows: 108,
		ReviewRows: 7, UnmatchedRows: 3, InvalidRows: 2, ParserVersion: WechatAnalyticsImportParserVersion,
	}}
	raw, err := json.Marshal(summary.Receipt())
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(raw)
	for _, fragment := range []string{
		`"batch_id":"batch-1"`, `"source_file":"total.xls"`, `"total_rows":120`,
		`"matched_rows":108`, `"ambiguous_rows":7`, `"unmatched_rows":3`,
		`"invalid_rows":2`, `"parser_version":"wechat-xls-v1"`,
	} {
		if !strings.Contains(encoded, fragment) {
			t.Fatalf("receipt %s missing %s", encoded, fragment)
		}
	}
}

func TestMatchWechatAnalyticsPublicationPriority(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	date := time.Date(2026, 9, 11, 0, 0, 0, 0, location)
	otherDate := date.AddDate(0, 0, -1)
	candidates := []AnalyticsCandidate{
		{Target: AnalyticsTarget{"wechat_publication", "url"}, Title: "另一个标题", URL: "https://mp.weixin.qq.com/s/url#rd", Date: &otherDate, matchDate: &otherDate},
		{Target: AnalyticsTarget{"wechat_publication", "title-date"}, Title: "白茶到底分几类？", Date: &date, matchDate: &date},
	}

	tests := []struct {
		name      string
		row       ParsedWechatAnalyticsRow
		wantID    string
		ambiguous bool
	}{
		{name: "URL takes priority", row: ParsedWechatAnalyticsRow{ArticleURL: "https://mp.weixin.qq.com/s/url", NormalizedTitle: "不匹配", PublishedDate: &date}, wantID: "url"},
		{name: "title and date fallback", row: ParsedWechatAnalyticsRow{NormalizedTitle: normalizeWechatAnalyticsTitle("白茶到底分几类？"), PublishedDate: &date}, wantID: "title-date"},
		{name: "unique title without date", row: ParsedWechatAnalyticsRow{NormalizedTitle: normalizeWechatAnalyticsTitle("白茶到底分几类？")}, wantID: "title-date"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match, ambiguous := matchAnalyticsCandidate(tt.row.NormalizedTitle, tt.row.PublishedDate, tt.row.ArticleURL, candidates)
			if ambiguous != tt.ambiguous || (match != nil && match.Target.ID != tt.wantID) || (match == nil && tt.wantID != "") {
				t.Fatalf("match=%#v ambiguous=%v", match, ambiguous)
			}
		})
	}

	duplicate := append(candidates, AnalyticsCandidate{Target: AnalyticsTarget{"wechat_publication", "duplicate"}, Title: "白茶到底分几类？", Date: &date, matchDate: &date})
	if match, ambiguous := matchAnalyticsCandidate("白茶到底分几类？", &date, "", duplicate); match != nil || !ambiguous {
		t.Fatalf("same-title same-date match=%#v ambiguous=%v", match, ambiguous)
	}
}

func TestParseWechatAnalyticsRateAndMissingValues(t *testing.T) {
	row := parseWechatAnalyticsRow(2, []string{"来源", "标题", "2026-09-11", "", "", "", "", "40.5%", "", ""}, func() map[string]int {
		m := make(map[string]int, len(wechatAnalyticsHeaders))
		for i, header := range wechatAnalyticsHeaders {
			m[header] = i
		}
		return m
	}(), time.FixedZone("Asia/Shanghai", 8*60*60))
	if row.ParseError != "" {
		t.Fatalf("parse error = %q", row.ParseError)
	}
	if row.ReadUsers != nil || row.ReadCompletionRate != nil {
		t.Fatalf("missing values should stay nil: %#v", row)
	}
	if row.DeliveryCompletionRate == nil || *row.DeliveryCompletionRate != 0.405 {
		t.Fatalf("delivery rate = %#v", row.DeliveryCompletionRate)
	}
}

func TestWechatAnalyticsContentTypeRequiresExplicitEvidence(t *testing.T) {
	for _, tc := range []struct{ name, column, value, source, want string }{
		{"article", "内容类型", "图文", "公众号后台", "article"},
		{"image", "消息类型", "图片", "公众号后台", "image"},
		{"sticker image type", "内容类型", "贴图", "公众号后台", "image"},
		{"gallery image type", "作品类型", "图集", "公众号后台", "image"},
		{"sticker image source", "", "", "贴图", "image"},
		{"gallery image source", "", "", "图集", "image"},
		{"alternate type", "作品类型", "文章", "公众号后台", "article"},
		{"english type", "类型", "image", "公众号后台", "image"},
		{"explicit source", "", "", "图文", "article"},
		{"generic source", "", "", "数据来源概况", "unknown"},
		{"missing type", "", "", "公众号后台", "unknown"},
		{"unsupported explicit type", "内容类型", "视频", "图文", "unknown"},
		{"explicit type wins", "内容类型", "图片", "图文", "image"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			columns := map[string]int{}
			for i, header := range wechatAnalyticsHeaders {
				columns[header] = i
			}
			values := []string{tc.source, "标题", "2026-09-11", "1", "0", "0", "10", "1", "0.5", ""}
			if tc.column != "" {
				columns[tc.column] = len(values)
				values = append(values, tc.value)
			}
			row := parseWechatAnalyticsRow(2, values, columns, time.UTC)
			raw, err := json.Marshal(row)
			if err != nil {
				t.Fatal(err)
			}
			var contract struct {
				ContentType string `json:"content_type"`
			}
			if err := json.Unmarshal(raw, &contract); err != nil {
				t.Fatal(err)
			}
			if contract.ContentType != tc.want {
				t.Errorf("content type = %q, want %q", contract.ContentType, tc.want)
			}
			if row.Source != tc.source {
				t.Errorf("source changed: %q", row.Source)
			}
			if tc.column != "" && row.Raw[tc.column] != tc.value {
				t.Errorf("explicit type evidence missing from raw data: %+v", row.Raw)
			}
		})
	}
}
