package service

import (
	"archive/zip"
	"bytes"
	"testing"
	"time"
)

func TestParseSeednoteWorkbookRecognizesOfficialLayout(t *testing.T) {
	data := testSeednoteWorkbook(t, []string{
		"<row r=\"1\"><c r=\"A1\" t=\"inlineStr\"><is><t>最多导出排序后前1000条笔记</t></is></c></row>",
		"<row r=\"2\"><c r=\"A2\" t=\"inlineStr\"><is><t>笔记标题</t></is></c><c r=\"B2\" t=\"inlineStr\"><is><t>首次发布时间</t></is></c><c r=\"C2\" t=\"inlineStr\"><is><t>体裁</t></is></c><c r=\"D2\" t=\"inlineStr\"><is><t>曝光</t></is></c><c r=\"E2\" t=\"inlineStr\"><is><t>观看量</t></is></c><c r=\"F2\" t=\"inlineStr\"><is><t>封面点击率</t></is></c><c r=\"G2\" t=\"inlineStr\"><is><t>点赞</t></is></c><c r=\"H2\" t=\"inlineStr\"><is><t>评论</t></is></c><c r=\"I2\" t=\"inlineStr\"><is><t>收藏</t></is></c><c r=\"J2\" t=\"inlineStr\"><is><t>涨粉</t></is></c><c r=\"K2\" t=\"inlineStr\"><is><t>分享</t></is></c><c r=\"L2\" t=\"inlineStr\"><is><t>人均观看时长</t></is></c><c r=\"M2\" t=\"inlineStr\"><is><t>弹幕</t></is></c></row>",
		"<row r=\"3\"><c r=\"A3\" t=\"inlineStr\"><is><t>AI生图 不再开盲盒</t></is></c><c r=\"B3\" t=\"inlineStr\"><is><t>2026年08月17日16时05分57秒</t></is></c><c r=\"C3\" t=\"inlineStr\"><is><t>图文</t></is></c><c r=\"D3\"><v>1155</v></c><c r=\"E3\"><v>81</v></c><c r=\"F3\"><v>0.07</v></c><c r=\"G3\"><v>3</v></c><c r=\"H3\"><v>0</v></c><c r=\"I3\"><v>4</v></c><c r=\"J3\"><v>0</v></c><c r=\"K3\"><v>1</v></c><c r=\"L3\"><v>11</v></c><c r=\"M3\"><v>0</v></c></row>",
		"<row r=\"4\"><c r=\"A4\" t=\"inlineStr\"><is><t>缺少数据</t></is></c><c r=\"B4\" t=\"inlineStr\"><is><t>2026-08-18 10:00:00</t></is></c><c r=\"C4\" t=\"inlineStr\"><is><t>视频</t></is></c><c r=\"D4\"><v>bad</v></c><c r=\"F4\" t=\"inlineStr\"><is><t>12.2%</t></is></c></row>",
	})

	parsed, err := ParseSeednoteWorkbook(data, time.FixedZone("CST", 8*60*60))
	if err != nil {
		t.Fatalf("ParseSeednoteWorkbook() error = %v", err)
	}
	if parsed.Metadata.Creator != "Apache POI" {
		t.Fatalf("creator = %q", parsed.Metadata.Creator)
	}
	if len(parsed.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(parsed.Rows))
	}
	row := parsed.Rows[0]
	if row.Title != "AI生图 不再开盲盒" || row.FirstPublishedAt == nil || row.FirstPublishedAt.Year() != 2026 || row.Exposure == nil || *row.Exposure != 1155 || row.CoverClickRate == nil || *row.CoverClickRate != 0.07 {
		t.Fatalf("parsed row = %+v", row)
	}
	if parsed.Rows[1].ParseError == "" || parsed.Rows[1].Exposure != nil || parsed.Rows[1].CoverClickRate == nil || *parsed.Rows[1].CoverClickRate != 0.122 {
		t.Fatalf("invalid row = %+v", parsed.Rows[1])
	}
}

func testSeednoteWorkbook(t *testing.T, rows []string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]string{
		"[Content_Types].xml":        `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/><Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/></Types>`,
		"xl/workbook.xml":            `<?xml version="1.0"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Sheet1" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml":   `<?xml version="1.0"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>` + joinXMLRows(rows) + `</sheetData></worksheet>`,
		"docProps/core.xml":          `<?xml version="1.0"?><cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:creator>Apache POI</dc:creator></cp:coreProperties>`,
	}
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func joinXMLRows(rows []string) string {
	var b bytes.Buffer
	for _, row := range rows {
		b.WriteString(row)
	}
	return b.String()
}
