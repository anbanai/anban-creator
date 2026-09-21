package service

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/storage"
	"github.com/google/uuid"
	"github.com/tealeg/xlsx/v3"
)

func TestWechatAnalyticsImportReusesArticleURL(t *testing.T) {
	for _, tc := range []struct {
		name, existingURL, title, wantURL string
		resolve                           bool
	}{
		{name: "automatic match fills missing URL", title: "文章", wantURL: "https://mp.weixin.qq.com/s/imported"},
		{name: "explicit association fills missing URL", title: "另一个标题", resolve: true, wantURL: "https://mp.weixin.qq.com/s/imported"},
		{name: "existing URL is preserved", title: "文章", existingURL: "https://mp.weixin.qq.com/s/original", wantURL: "https://mp.weixin.qq.com/s/original"},
		{name: "unmatched row cannot fill URL", title: "另一个标题"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			f := newWechatTrackingFixture(t, model.WechatPublicationSourceWechatConsole, "", "", tc.existingURL, time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC))
			f.publication.DraftTitle = "文章"
			f.publication.AnalyticsStatus = "import_available" // Repeated imports must still fill missing URLs.
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
			header := sheet.AddRow()
			for _, value := range wechatAnalyticsHeaders {
				header.AddCell().SetString(value)
			}
			row := sheet.AddRow()
			for _, value := range []string{"公众号后台", tc.title, "20260911", "10", "1", "0", "100", "1", "0.5", "https://mp.weixin.qq.com/s/imported#rd"} {
				row.AddCell().SetString(value)
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
			svc := NewWechatAnalyticsImportService(f.repo, store)
			result, err := svc.Import(ctx, WechatAnalyticsImportRequest{UserID: f.userID, ProjectID: f.projectID, UploadID: asset.ID})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Rows) != 1 || result.Rows[0].ArticleURL != "https://mp.weixin.qq.com/s/imported" {
				t.Fatalf("import rows = %+v", result.Rows)
			}
			if tc.resolve {
				if _, err := svc.Resolve(ctx, f.userID, f.projectID, result.Batch.ID, []WechatAnalyticsResolveAction{{RowID: result.Rows[0].ID, Action: "link_existing", PublicationID: f.publication.ID}}); err != nil {
					t.Fatal(err)
				}
			}
			got, err := f.repo.WechatPublications().FindByID(ctx, f.publication.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.ArticleURL != tc.wantURL {
				t.Fatalf("article URL = %q, want %q", got.ArticleURL, tc.wantURL)
			}
			tracking, err := f.repo.WechatTrackings().FindByTaskID(ctx, f.taskID)
			if err != nil {
				t.Fatal(err)
			}
			if tracking.ArticleURL != tc.wantURL {
				t.Fatalf("tracking URL = %q, want %q", tracking.ArticleURL, tc.wantURL)
			}
			if got.Status != model.WechatPublicationStatusPublished || got.Source != model.WechatPublicationSourceWechatConsole {
				t.Fatalf("publication state changed: %+v", got)
			}
			if _, err := svc.Revoke(ctx, f.userID, f.projectID, result.Batch.ID); err != nil {
				t.Fatal(err)
			}
			reimported, err := svc.Import(ctx, WechatAnalyticsImportRequest{UserID: f.userID, ProjectID: f.projectID, UploadID: asset.ID})
			if err != nil {
				t.Fatal(err)
			}
			if reimported.Batch.ID == result.Batch.ID || reimported.Batch.RevokedAt != nil || len(reimported.Rows) != 1 {
				t.Fatalf("reimport did not create a fresh batch: %+v", reimported)
			}
			if tc.resolve {
				if _, err := svc.Resolve(ctx, f.userID, f.projectID, reimported.Batch.ID, []WechatAnalyticsResolveAction{{RowID: reimported.Rows[0].ID, Action: "link_existing", PublicationID: f.publication.ID}}); err != nil {
					t.Fatal(err)
				}
			}
			got, err = f.repo.WechatPublications().FindByID(ctx, f.publication.ID)
			if err != nil || got.ArticleURL != tc.wantURL {
				t.Fatalf("reimport URL = %q, want %q, err=%v", got.ArticleURL, tc.wantURL, err)
			}
		})
	}
}

func TestWechatAnalyticsHistoricalImportURL(t *testing.T) {
	for _, tc := range []struct{ name, existing, second, want string }{
		{name: "reuses a previously imported URL", want: "https://mp.weixin.qq.com/s/historical"},
		{name: "preserves existing publication URL", existing: "https://mp.weixin.qq.com/s/original", want: "https://mp.weixin.qq.com/s/original"},
		{name: "conflicting historical URLs need review", second: "https://mp.weixin.qq.com/s/other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			f := newWechatTrackingFixture(t, model.WechatPublicationSourceWechatConsole, "", "", tc.existing, time.Now())
			for i, value := range []string{"https://mp.weixin.qq.com/s/historical", tc.second} {
				if value == "" {
					continue
				}
				snapshot := &model.WechatAnalyticsSnapshot{ID: uuid.NewString(), ProjectID: f.projectID, PublicationID: f.publication.ID, ImportRowID: uuid.NewString(), BatchID: uuid.NewString(), DataAsOfAt: time.Now().Add(time.Duration(i) * time.Hour), ImportedAt: time.Now(), RawData: `{"内容url":"` + value + `"}`}
				if err := f.repo.WechatAnalyticsImports().CreateSnapshot(ctx, snapshot); err != nil {
					t.Fatal(err)
				}
			}
			articles, err := NewWechatAnalyticsImportService(f.repo, nil).ListArticles(ctx, f.userID, f.projectID)
			if err != nil {
				t.Fatal(err)
			}
			if len(articles) != 1 || articles[0].Publication.ArticleURL != tc.want {
				t.Fatalf("historical URL = %q, want %q", articles[0].Publication.ArticleURL, tc.want)
			}
			detail, err := NewWechatPublicationService(f.repo, nil, nil).Get(ctx, f.userID, f.taskID)
			if err != nil {
				t.Fatal(err)
			}
			if detail.ArticleURL != tc.want {
				t.Fatalf("task detail URL = %q, want %q", detail.ArticleURL, tc.want)
			}
			stored, err := f.repo.WechatPublications().FindByID(ctx, f.publication.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.ArticleURL != tc.existing {
				t.Fatalf("read changed stored URL to %q", stored.ArticleURL)
			}
		})
	}
}
