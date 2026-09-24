package service

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
)

func TestWechatAnalyticsImportPreservesPublicationAndTracking(t *testing.T) {
	ctx := context.Background()
	f, svc, req := newWechatSelectionImport(t, [][]string{wechatSelectionRow("另一个标题", "https://mp.weixin.qq.com/s/imported")})
	before, err := f.repo.WechatPublications().FindByID(ctx, f.publication.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeTracking, err := f.repo.WechatTrackings().FindByTaskID(ctx, f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	req.Selections = []AnalyticsSelection{{2, AnalyticsTarget{"task", f.taskID}}}
	imported, err := svc.Import(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	after, err := f.repo.WechatPublications().FindByID(ctx, f.publication.ID)
	if err != nil {
		t.Fatal(err)
	}
	afterTracking, err := f.repo.WechatTrackings().FindByTaskID(ctx, f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(beforeTracking, afterTracking) {
		t.Fatal("analytics import changed publication/tracking")
	}
	if imported.Rows[0].TaskID != f.taskID {
		t.Fatalf("missing canonical task identity: %+v", imported.Rows[0])
	}
}

func TestWechatAnalyticsHistoricalImportURL(t *testing.T) {
	for _, tc := range []struct{ name, existing, second, want string }{
		{name: "reuses a previously imported URL", want: "https://mp.weixin.qq.com/s/historical"},
		{name: "tracking queries preserve article identity", second: "https://mp.weixin.qq.com/s/historical?scene=2", want: "https://mp.weixin.qq.com/s/historical"},
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
			if tc.want != "" {
				candidates, err := loadAnalyticsCandidates(ctx, f.repo, f.userID, f.projectID)
				if err != nil {
					t.Fatal(err)
				}
				matched, ambiguous := matchAnalyticsCandidate("renamed article", nil, tc.want+"?scene=3", candidates)
				if matched == nil || ambiguous || matched.Target.ID != f.taskID {
					t.Fatalf("historical identity did not match: %+v ambiguous=%v", matched, ambiguous)
				}
			}
		})
	}
}
