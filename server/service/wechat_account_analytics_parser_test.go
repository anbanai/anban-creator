package service

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
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

func TestParseWechatAccountAnalyticsWorkbookAllowsNegativeNetFollowerGain(t *testing.T) {
	file := xlsx.NewFile()
	sheet, err := file.AddSheet("数据")
	if err != nil {
		t.Fatal(err)
	}
	for _, values := range [][]string{
		{"日期", "净增关注", "阅读人数"},
		{"2026-10-08", "-3", "10"},
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
	if parsed.Rows[0].FollowerGainCount == nil || *parsed.Rows[0].FollowerGainCount != -3 {
		t.Fatalf("follower gain = %#v", parsed.Rows[0].FollowerGainCount)
	}
	if parsed.Rows[0].ReadUsers == nil || *parsed.Rows[0].ReadUsers != 10 {
		t.Fatalf("read users = %#v", parsed.Rows[0].ReadUsers)
	}
}

func TestParseWechatAccountAnalyticsWorkbookReadsAllWorksheetsAndRemapsHeaders(t *testing.T) {
	file := xlsx.NewFile()
	for sheetIndex, order := range [][]string{
		{"日期", "阅读人数", "分享人数"},
		{"分享人数", "自定义备注", "阅读人数", "日期"},
	} {
		sheet, err := file.AddSheet(fmt.Sprintf("数据%d", sheetIndex+1))
		if err != nil {
			t.Fatal(err)
		}
		row := sheet.AddRow()
		for _, header := range order {
			row.AddCell().SetString(header)
		}
		values := map[string]string{
			"日期": fmt.Sprintf("2026100%d", sheetIndex+8), "阅读人数": fmt.Sprintf("%d", 100+sheetIndex), "分享人数": "2", "自定义备注": "第二张表",
		}
		row = sheet.AddRow()
		for _, header := range order {
			row.AddCell().SetString(values[header])
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
	if len(parsed.Rows) != 2 {
		t.Fatalf("rows = %#v", parsed.Rows)
	}
	if parsed.Rows[1].StatDate != "2026-10-09" || parsed.Rows[1].ReadUsers == nil || *parsed.Rows[1].ReadUsers != 101 {
		t.Fatalf("second worksheet row = %#v", parsed.Rows[1])
	}
	if len(parsed.UnknownHeaders) != 1 || parsed.UnknownHeaders[0] != "自定义备注" {
		t.Fatalf("unknown headers = %#v", parsed.UnknownHeaders)
	}
}

func TestParseWechatAccountAnalyticsWorkbookTreatsExplicitSourceRowsAsSourceOnly(t *testing.T) {
	file := xlsx.NewFile()
	sheet, err := file.AddSheet("流量来源")
	if err != nil {
		t.Fatal(err)
	}
	for _, values := range [][]string{
		{"日期", "来源", "阅读人数"},
		{"2026-10-08", "推荐", "100"},
		{"2026-10-08", "聊天", "20"},
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
	if len(parsed.Rows) != 0 {
		t.Fatalf("rows = %#v, want no account rows", parsed.Rows)
	}
	if len(parsed.Sources) != 2 {
		t.Fatalf("sources = %#v, want two source rows", parsed.Sources)
	}
	if len(parsed.DuplicateDates) != 0 {
		t.Fatalf("duplicate dates = %#v, want none for source rows", parsed.DuplicateDates)
	}
	if parsed.Sources[0].Source != "recommendation" || parsed.Sources[1].Source != "chat" {
		t.Fatalf("sources = %#v", parsed.Sources)
	}
}

func TestParseWechatAccountAnalyticsWorkbookReportsDuplicateSourceFacts(t *testing.T) {
	file := xlsx.NewFile()
	sheet, err := file.AddSheet("流量来源")
	if err != nil {
		t.Fatal(err)
	}
	for _, values := range [][]string{
		{"日期", "来源", "阅读人数"},
		{"2026-10-08", "推荐", "100"},
		{"2026-10-08", "推荐", "20"},
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
	if len(parsed.DuplicateSources) != 1 || parsed.DuplicateSources[0] != "2026-10-08 / recommendation" {
		t.Fatalf("duplicate sources = %#v", parsed.DuplicateSources)
	}
}

func TestParseWechatAccountAnalyticsWorkbookTreatsSourceColumnsAsSourceOnly(t *testing.T) {
	file := xlsx.NewFile()
	sheet, err := file.AddSheet("流量来源")
	if err != nil {
		t.Fatal(err)
	}
	for _, values := range [][]string{
		{"日期", "公众号消息", "推荐", "聊天"},
		{"2026-10-08", "100", "20", "5"},
		{"2026-10-09", "110", "25", "6"},
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
	if len(parsed.Rows) != 0 {
		t.Fatalf("rows = %#v, want no account rows", parsed.Rows)
	}
	if len(parsed.Sources) != 6 {
		t.Fatalf("sources = %#v, want six source facts", parsed.Sources)
	}
	if len(parsed.DuplicateDates) != 0 {
		t.Fatalf("duplicate dates = %#v, want none for source columns", parsed.DuplicateDates)
	}
	if parsed.Sources[0].Source != model.WechatTrafficSourceMessage || parsed.Sources[0].ReadUsers == nil || *parsed.Sources[0].ReadUsers != 100 {
		t.Fatalf("first source = %#v", parsed.Sources[0])
	}
}
