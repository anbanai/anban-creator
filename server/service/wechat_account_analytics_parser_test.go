package service

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/tealeg/xlsx/v3"
)

func TestParseWechatAccountAnalyticsWorkbookRecognizesOfficialColumns(t *testing.T) {
	file := xlsx.NewFile()
	sheet, err := file.AddSheet("数据来源概况")
	if err != nil {
		t.Fatal(err)
	}
	row := sheet.AddRow()
	for _, header := range []string{"日期", "阅读人数", "分享人数", "点赞人数", "收藏人数", "评论数", "新增关注", "发表篇数", "消息", "自定义备注"} {
		row.AddCell().SetString(header)
	}
	row = sheet.AddRow()
	for _, value := range []string{"2026-10-08", "100", "2", "3", "4", "5", "6", "1", "80", "本次导出"} {
		row.AddCell().SetString(value)
	}
	var buf bytes.Buffer
	if err := file.Write(&buf); err != nil {
		t.Fatal(err)
	}

	parsed, err := ParseWechatAccountAnalyticsWorkbook(buf.Bytes(), time.FixedZone("Asia/Shanghai", 8*60*60))
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Rows) != 1 || parsed.Rows[0].StatDate != "2026-10-08" {
		t.Fatalf("rows = %#v", parsed.Rows)
	}
	if len(parsed.UnknownHeaders) != 1 || parsed.UnknownHeaders[0] != "自定义备注" {
		t.Fatalf("unknown headers = %#v, want only the custom column", parsed.UnknownHeaders)
	}
}

func TestParseWechatAccountAnalyticsWorkbookReportsDuplicateDates(t *testing.T) {
	file := xlsx.NewFile()
	sheet, err := file.AddSheet("数据")
	if err != nil {
		t.Fatal(err)
	}
	for _, values := range [][]string{
		{"日期", "阅读人数"},
		{"2026-10-08", "10"},
		{"2026-10-08", "20"},
	} {
		row := sheet.AddRow()
		for _, value := range values {
			row.AddCell().SetString(value)
		}
	}
	var buf bytes.Buffer
	if err := file.Write(&buf); err != nil {
		t.Fatal(err)
	}

	parsed, err := ParseWechatAccountAnalyticsWorkbook(buf.Bytes(), time.FixedZone("Asia/Shanghai", 8*60*60))
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.DuplicateDates) != 1 || parsed.DuplicateDates[0] != "2026-10-08" {
		t.Fatalf("duplicate dates = %#v", parsed.DuplicateDates)
	}
}

func TestParseWechatAccountAnalyticsWorkbookMergesWorksheets(t *testing.T) {
	file := xlsx.NewFile()
	for sheetIndex, values := range [][][]string{
		{{"日期", "阅读人数"}, {"2026-10-08", "10"}},
		{{"阅读人数", "日期"}, {"20", "2026-10-09"}},
	} {
		sheet, err := file.AddSheet("数据" + string(rune('A'+sheetIndex)))
		if err != nil {
			t.Fatal(err)
		}
		for _, rowValues := range values {
			row := sheet.AddRow()
			for _, value := range rowValues {
				row.AddCell().SetString(value)
			}
		}
	}
	var buf bytes.Buffer
	if err := file.Write(&buf); err != nil {
		t.Fatal(err)
	}

	parsed, err := ParseWechatAccountAnalyticsWorkbook(buf.Bytes(), time.FixedZone("Asia/Shanghai", 8*60*60))
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Rows) != 2 || parsed.Rows[0].StatDate != "2026-10-08" || parsed.Rows[1].StatDate != "2026-10-09" {
		t.Fatalf("rows = %#v", parsed.Rows)
	}
	if len(parsed.Errors) != 0 {
		t.Fatalf("repeated worksheet headers should not be parse errors: %#v", parsed.Errors)
	}
}

func TestParseWechatAccountAnalyticsWorkbookPreservesZeroAndNormalizesSources(t *testing.T) {
	file := xlsx.NewFile()
	sheet, err := file.AddSheet("数据来源概况")
	if err != nil {
		t.Fatal(err)
	}
	for _, values := range [][]string{
		{"日期", "阅读人数", "消息", "朋友在看", "朋友圈", "搜一搜", "其他"},
		{"2026-10-08", "0", "10", "20", "30", "40", ""},
	} {
		row := sheet.AddRow()
		for _, value := range values {
			row.AddCell().SetString(value)
		}
	}
	var buf bytes.Buffer
	if err := file.Write(&buf); err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseWechatAccountAnalyticsWorkbook(buf.Bytes(), time.FixedZone("Asia/Shanghai", 8*60*60))
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Rows) != 1 || parsed.Rows[0].ReadUsers == nil || *parsed.Rows[0].ReadUsers != 0 {
		t.Fatalf("account zero = %#v", parsed.Rows)
	}
	if len(parsed.Sources) != 4 {
		t.Fatalf("sources = %#v, want four non-empty source facts", parsed.Sources)
	}
	got := map[string]int64{}
	for _, source := range parsed.Sources {
		if source.ReadUsers != nil {
			got[string(source.Source)] = *source.ReadUsers
		}
	}
	for source, want := range map[string]int64{"message": 10, "recommendation": 20, "moments": 30, "search": 40} {
		if got[source] != want {
			t.Fatalf("source %s = %d, want %d", source, got[source], want)
		}
	}
}

func TestParseWechatAccountAnalyticsWorkbookRejectsNegativeAndUnsupportedData(t *testing.T) {
	file := xlsx.NewFile()
	sheet, err := file.AddSheet("数据")
	if err != nil {
		t.Fatal(err)
	}
	for _, values := range [][]string{{"日期", "阅读人数"}, {"2026-10-08", "-1"}} {
		row := sheet.AddRow()
		for _, value := range values {
			row.AddCell().SetString(value)
		}
	}
	var buf bytes.Buffer
	if err := file.Write(&buf); err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseWechatAccountAnalyticsWorkbook(buf.Bytes(), time.FixedZone("Asia/Shanghai", 8*60*60))
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Errors) != 1 || parsed.Rows[0].ParseError == "" {
		t.Fatalf("negative value errors = %#v rows=%#v", parsed.Errors, parsed.Rows)
	}

	unsupported := xlsx.NewFile()
	unsupportedSheet, err := unsupported.AddSheet("数据")
	if err != nil {
		t.Fatal(err)
	}
	unsupportedSheet.AddRow().AddCell().SetString("无关列")
	var unsupportedBuf bytes.Buffer
	if err := unsupported.Write(&unsupportedBuf); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseWechatAccountAnalyticsWorkbook(unsupportedBuf.Bytes(), time.FixedZone("Asia/Shanghai", 8*60*60)); err == nil {
		t.Fatal("unsupported workbook should fail preview")
	}
}

func TestParseWechatAccountAnalyticsWorkbookExplainsContentAnalyticsExport(t *testing.T) {
	file := xlsx.NewFile()
	sheet, err := file.AddSheet("数据来源概况")
	if err != nil {
		t.Fatal(err)
	}
	header := sheet.AddRow()
	for _, value := range []string{"内容标题", "发表日期", "阅读人数", "分享人数", "阅读后关注人数", "送达人数", "送达完成率", "阅读完成率", "内容url"} {
		header.AddCell().SetString(value)
	}
	data := sheet.AddRow()
	for _, value := range []string{"文章", "20261010", "1", "0", "0", "1", "1", "1", "https://example.com"} {
		data.AddCell().SetString(value)
	}
	var buf bytes.Buffer
	if err := file.Write(&buf); err != nil {
		t.Fatal(err)
	}

	_, err = ParseWechatAccountAnalyticsWorkbook(buf.Bytes(), time.FixedZone("Asia/Shanghai", 8*60*60))
	if err == nil {
		t.Fatal("content analytics export should be rejected by account parser")
	}
	if !errors.Is(err, ErrWechatAccountAnalyticsWorkbookUnsupported) {
		t.Fatalf("error = %q, want unsupported workbook sentinel", err)
	}
	if !bytes.Contains([]byte(err.Error()), []byte("公众号内容分析报表")) || !bytes.Contains([]byte(err.Error()), []byte("导入单篇内容")) {
		t.Fatalf("error = %q, want actionable content-import guidance", err)
	}
}
