package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/storage"
	"github.com/google/uuid"
	"github.com/tealeg/xlsx/v3"
)

func newWechatSelectionImport(t *testing.T, dataRows [][]string) (*wechatTrackingFixture, *WechatAnalyticsImportService, WechatAnalyticsImportRequest) {
	t.Helper()
	ctx := context.Background()
	f := newWechatTrackingFixture(t, model.WechatPublicationSourceWechatConsole, "", "", "https://mp.weixin.qq.com/s/matched", time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC))
	f.publication.DraftTitle = "文章"
	if err := f.repo.WechatPublications().Update(ctx, f.publication); err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := xlsx.NewFile()
	sheet, err := file.AddSheet("数据")
	if err != nil {
		t.Fatal(err)
	}
	for _, values := range append([][]string{wechatAnalyticsHeaders}, dataRows...) {
		row := sheet.AddRow()
		for _, value := range values {
			row.AddCell().SetString(value)
		}
	}
	var buf bytes.Buffer
	if err := file.Write(&buf); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upload(ctx, "data.xlsx", bytes.NewReader(buf.Bytes()), "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"); err != nil {
		t.Fatal(err)
	}
	asset := &model.Asset{ID: uuid.NewString(), UserID: f.userID, Purpose: DirectUploadPurposeWechatAnalyticsImport, StorageKey: "data.xlsx", FileName: "data.xlsx"}
	if err := f.repo.Assets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}
	return f, NewWechatAnalyticsImportService(f.repo, store), WechatAnalyticsImportRequest{UserID: f.userID, ProjectID: f.projectID, UploadID: asset.ID}
}

func wechatSelectionRow(title, link string) []string {
	return []string{"公众号后台", title, "20260911", "10", "1", "0", "100", "1", "0.5", link}
}

func TestWechatAnalyticsPreviewAllRowsWithAuthoritativeMatches(t *testing.T) {
	data := [][]string{wechatSelectionRow("URL 标题不同", "https://mp.weixin.qq.com/s/matched#rd"), wechatSelectionRow("文章", ""), wechatSelectionRow("找不到", ""), wechatSelectionRow("", "")}
	for i := 0; i < 9; i++ {
		data = append(data, wechatSelectionRow(fmt.Sprintf("其他 %d", i), ""))
	}
	f, svc, req := newWechatSelectionImport(t, data)
	duplicate := *f.publication
	duplicate.ID, duplicate.TaskID, duplicate.ArticleURL = uuid.NewString(), uuid.NewString(), ""
	if err := f.repo.WechatPublications().Create(context.Background(), &duplicate); err != nil {
		t.Fatal(err)
	}
	preview, err := svc.Preview(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Rows) != 13 || preview.TotalRows != 13 {
		t.Fatalf("preview truncated rows: %d / %d", len(preview.Rows), preview.TotalRows)
	}
	raw, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		Rows []struct {
			MatchStatus   string `json:"match_status"`
			PublicationID string `json:"publication_id"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"matched", "needs_review", "unmatched", "invalid"} {
		if contract.Rows[i].MatchStatus != want {
			t.Errorf("row %d status = %q, want %q", i, contract.Rows[i].MatchStatus, want)
		}
		wantID := ""
		if i == 0 {
			wantID = f.publication.ID
		}
		if contract.Rows[i].PublicationID != wantID {
			t.Errorf("row %d publication = %q, want %q", i, contract.Rows[i].PublicationID, wantID)
		}
	}
}

func TestWechatAnalyticsImportSelectedRows(t *testing.T) {
	ctx := context.Background()
	f, svc, req := newWechatSelectionImport(t, [][]string{wechatSelectionRow("文章", ""), wechatSelectionRow("找不到", ""), wechatSelectionRow("", ""), wechatSelectionRow("文章", "")})
	// Decode the public request contract, including workbook line numbers.
	if err := json.Unmarshal([]byte(fmt.Sprintf(`{"selections":[{"source_row":5,"target":{"kind":"task","id":%q}}]}`, f.taskID)), &req); err != nil {
		t.Fatal(err)
	}
	result, err := svc.Import(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Batch.TotalRows != 1 || result.Batch.MatchedRows != 1 || len(result.Rows) != 1 || result.Rows[0].SourceRow != 5 {
		t.Fatalf("selected import = %+v, rows = %+v", result.Batch, result.Rows)
	}
	snapshots, err := f.repo.WechatAnalyticsImports().FindSnapshotsByPublicationID(ctx, f.projectID, f.publication.ID)
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("snapshots=%d, err=%v", len(snapshots), err)
	}
}

func TestWechatAnalyticsImportRejectsInvalidSelectionsWithoutPersistence(t *testing.T) {
	for _, name := range []string{"empty", "unknown row", "duplicate row", "duplicate target alias", "foreign target", "invalid row"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			f, svc, req := newWechatSelectionImport(t, [][]string{wechatSelectionRow("文章", ""), wechatSelectionRow("文章", ""), wechatSelectionRow("", "")})
			target := AnalyticsTarget{"task", f.taskID}
			switch name {
			case "unknown row":
				req.Selections = []AnalyticsSelection{{99, target}}
			case "duplicate row":
				req.Selections = []AnalyticsSelection{{2, target}, {2, target}}
			case "duplicate target alias":
				req.Selections = []AnalyticsSelection{{2, target}, {3, AnalyticsTarget{"wechat_publication", f.publication.ID}}}
			case "foreign target":
				req.Selections = []AnalyticsSelection{{2, AnalyticsTarget{"task", uuid.NewString()}}}
			case "invalid row":
				req.Selections = []AnalyticsSelection{{4, target}}
			}
			if _, err := svc.Import(ctx, req); err == nil {
				t.Fatal("selection must be rejected")
			}
			_, total, err := f.repo.WechatAnalyticsImports().ListBatches(ctx, f.projectID, 0, 10)
			if err != nil || total != 0 {
				t.Fatalf("persisted batches=%d, err=%v", total, err)
			}
		})
	}
}

func TestWechatAnalyticsSelectionUsesActualWorkbookRowNumbers(t *testing.T) {
	f, svc, req := newWechatSelectionImport(t, [][]string{wechatSelectionRow("文章", ""), {}, wechatSelectionRow("文章", "")})
	preview, err := svc.Preview(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Rows) != 2 || preview.Rows[1].SourceRow != 4 {
		t.Fatalf("row after empty Excel row = %+v", preview.Rows)
	}
	if err := json.Unmarshal([]byte(fmt.Sprintf(`{"selections":[{"source_row":4,"target":{"kind":"task","id":%q}}]}`, f.taskID)), &req); err != nil {
		t.Fatal(err)
	}
	result, err := svc.Import(context.Background(), req)
	if err != nil || len(result.Rows) != 1 || result.Rows[0].SourceRow != 4 {
		t.Fatalf("selected workbook row = %+v, err=%v", result, err)
	}
}
