package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func TestUnifiedTopicMigrationPreservesDeletedPersonaHistory(t *testing.T) {
	for _, kind := range []string{"evidence", "link", "reuse", "research"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			repo := topicRadarTestRepository(t)
			db := repo.Analytics().DB()
			projectID := createTestProject(t, repo, "owner", model.PlatformWechat)
			publication := &model.WechatPublication{ID: "imported", UserID: "owner", ProjectID: projectID, Source: "wechat_console", Status: model.WechatPublicationStatusPublished}
			if err := db.Create(publication).Error; err != nil {
				t.Fatal(err)
			}
			svc := NewContentAssetService(repo)
			asset := model.NewContentAsset("owner", "surviving imported topic")
			var err error
			if kind == "evidence" {
				var detail *ContentAssetDetail
				detail, err = svc.Ingest(ctx, IngestContentAssetParams{CreateContentAssetParams: CreateContentAssetParams{UserID: "owner", Title: asset.Title}, Evidence: []model.ContentAssetEvidence{{SourceType: model.ContentAssetEvidencePublished, SourceKind: "wechat_publication", SourceID: publication.ID, ProjectID: projectID}}})
				if err == nil {
					asset = detail.Asset
				}
			} else {
				err = db.Create(asset).Error
			}
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "link":
				_, err = svc.LinkContent(ctx, LinkContentAssetParams{UserID: "owner", AssetID: asset.ID, ProjectID: projectID, ContentKind: "wechat_publication", ContentID: publication.ID, Relation: model.ContentAssetLinkOrigin})
			case "reuse":
				err = db.Create(&model.ContentReuseRun{ID: "historical-run", UserID: "owner", AssetID: asset.ID, TargetProjectID: projectID, TargetPlatform: model.PlatformWechat, Stage: "research", Status: model.ContentStageFailed, IdempotencyKey: "original", RequestFingerprint: "frozen", PolicySnapshot: datatypes.JSON(`{"revision":1}`)}).Error
			case "research":
				job := model.NewContentAssetResearchJob("owner", projectID, "frozen", datatypes.JSON(`{"revision":1}`))
				job.CandidateAssetID = asset.ID
				err = db.Create(job).Error
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := NewProjectService(repo, nil).Delete(ctx, "owner", projectID); err != nil {
				t.Fatalf("service delete: %v", err)
			}
			if _, err := repo.Projects().FindByID(ctx, projectID); !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("project survived deletion: %v", err)
			}
			// Compare raw rows, including private fingerprints and frozen context, not public JSON.
			history := func() map[string][]map[string]any {
				t.Helper()
				result := map[string][]map[string]any{}
				for _, table := range []string{"content_assets", "content_asset_evidence", "content_asset_links", "content_reuse_runs", "content_asset_research_jobs", "wechat_publications", "topic_pool"} {
					var rows []map[string]any
					if err := db.Table(table).Order("id").Find(&rows).Error; err != nil {
						t.Fatal(err)
					}
					result[table] = rows
				}
				return result
			}
			before := history()
			for i := 0; i < 2; i++ {
				if err := MigrateTopicPoolToContentAssets(ctx, db, nil); err != nil {
					t.Fatalf("migration %d: %v", i+1, err)
				}
				if after := history(); !reflect.DeepEqual(before, after) {
					t.Fatalf("migration %d changed historical records", i+1)
				}
			}
			// Live admission still requires a present, owned persona.
			if err := repo.ContentAssets().ProjectTopicAssociation(ctx, "owner", projectID, asset.ID, "", false, time.Now()); !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("live association accepted deleted persona: %v", err)
			}
			if _, err := svc.LinkContent(ctx, LinkContentAssetParams{UserID: "owner", AssetID: asset.ID, ProjectID: projectID, ContentKind: "wechat_publication", ContentID: publication.ID, Relation: model.ContentAssetLinkReference}); err == nil {
				t.Fatal("live linking accepted deleted persona")
			}
		})
	}
}

func TestUnifiedTopicMigrationRejectsInvalidExistingPersonaHistory(t *testing.T) {
	for _, kind := range []string{"foreign_project", "missing_asset", "database_error"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			repo := topicRadarTestRepository(t)
			db := repo.Analytics().DB()
			projectID := createTestProject(t, repo, "owner", model.PlatformWechat)
			asset := model.NewContentAsset("owner", "canonical")
			if err := db.Create(asset).Error; err != nil {
				t.Fatal(err)
			}
			evidence := &model.ContentAssetEvidence{ID: "history", UserID: "owner", AssetID: asset.ID, ProjectID: projectID, SourceType: model.ContentAssetEvidenceExternal, SourceKind: "web", SourceID: "source"}
			if kind == "foreign_project" {
				evidence.UserID = "another-owner"
			}
			if kind == "missing_asset" {
				evidence.AssetID = "absent"
			}
			if err := db.Create(evidence).Error; err != nil {
				t.Fatal(err)
			}
			if kind == "database_error" {
				if err := db.Callback().Query().Before("gorm:query").Register("fail_project_read", func(tx *gorm.DB) {
					if tx.Statement.Table == "projects" {
						tx.AddError(errors.New("project read unavailable"))
					}
				}); err != nil {
					t.Fatal(err)
				}
			}
			if err := MigrateTopicPoolToContentAssets(ctx, db, nil); err == nil {
				t.Fatal("invalid history or database failure silently skipped")
			}
		})
	}
}

func TestUnifiedTopicDigestSerializationHidesQueueIdentity(t *testing.T) {
	queueID := uint(42)
	detail := TopicDigestDetail{Adoptions: []*model.TopicSuggestionAdoption{{ID: "adoption", SuggestionID: "suggestion", TopicPoolID: &queueID}}}
	payload, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Adoptions []map[string]json.RawMessage `json:"adoptions"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Adoptions) != 1 {
		t.Fatalf("missing adoption: %s", payload)
	}
	if _, exists := response.Adoptions[0]["topic_pool_id"]; exists {
		t.Fatalf("internal queue identity leaked: %s", payload)
	}
	if string(response.Adoptions[0]["id"]) != `"adoption"` {
		t.Fatalf("lost adoption identity: %s", payload)
	}
	if detail.Adoptions[0].TopicPoolID == nil || *detail.Adoptions[0].TopicPoolID != queueID {
		t.Fatal("lost internal queue linkage")
	}
}
