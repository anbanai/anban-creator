package service

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newContentMetadataFixture(t *testing.T, name, taskType string) (repository.Repository, *gorm.DB, *ContentMetadataService, *model.Task, *model.TaskExecution) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+name+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.TaskExecution{}, &model.ContentMetadataReport{}, &model.ContentTagAssignment{}, &model.ContentTagVocabulary{}, &model.AgentFeedback{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	task := &model.Task{ID: name + "-task", UserID: "user-1", ProjectID: "project-1", Type: taskType, Status: model.TaskStatusCompleted}
	if err := repo.Tasks().Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	role := "final"
	if taskType == model.TaskTypeWechatPicture {
		role = "content"
	}
	execution := contentMetadataTestExecution(name+"-execution", task.ID, role)
	if err := repo.TaskExecutions().Create(context.Background(), execution); err != nil {
		t.Fatal(err)
	}
	return repo, db, NewContentMetadataService(repo, nil), task, execution
}

func contentMetadataTestExecution(id, taskID, role string) *model.TaskExecution {
	return &model.TaskExecution{
		ID: id, TaskID: taskID, Attempt: 1, Status: model.TaskExecutionSucceeded,
		AgentPackTaggingContract:  datatypes.JSON(`{"enabled":true,"profile":"generic-content","required_artifact":"output/content-tags.json","subject_roles":["` + role + `"]}`),
		AgentPackDeliveryContract: datatypes.JSON(`[{"role":"` + role + `","path":"output/final.md","mime_type":"text/markdown"}]`),
	}
}

func TestContentMetadataServiceSubmitIsIdempotentPerExecution(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:content-metadata-service?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.TaskExecution{}, &model.ContentMetadataReport{}, &model.ContentTagAssignment{}, &model.ContentTagVocabulary{}, &model.AgentFeedback{}); err != nil {
		t.Fatal(err)
	}
	svc := NewContentMetadataService(repository.New(db), nil)
	repo := repository.New(db)
	if err := repo.Tasks().Create(context.Background(), &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1", Type: model.TaskTypeWechatArticle, Status: model.TaskStatusCompleted}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(context.Background(), contentMetadataTestExecution("exec-1", "task-1", "final")); err != nil {
		t.Fatal(err)
	}
	input := ContentMetadataInput{TaskID: "task-1", ExecutionID: "exec-1", TaxonomyVersion: model.ContentTaxonomyVersion, RawMetadata: []byte(`{"schema_version":"2.0","taxonomy_version":"2.0","task_type":"article","tagging_status":"succeeded","subjects":[{"artifact_role":"final","tags":[{"dimension":"domain","value":"茶叶","confidence":0.9,"evidence":[{"source_file":"output/04-article-final.md","line_start":1,"line_end":2,"quote":"茶叶","evidence_kind":"explicit_topic"}]}]}],"feedback":{"scores":{"quality":8},"summary":"ok"}}`)}
	first, err := svc.Submit(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Submit(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("ids differ: %q %q", first.ID, second.ID)
	}
	var count int64
	if err := db.Model(&model.ContentMetadataReport{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("reports = %d, want 1", count)
	}
	var feedback model.AgentFeedback
	if err := db.First(&feedback, "task_id = ? AND agent_name = ?", "task-1", "article").Error; err != nil {
		t.Fatal(err)
	}
	if feedback.Source != "hook" || feedback.ExecutionID != "exec-1" {
		t.Fatalf("feedback provenance = %#v", feedback)
	}
}

func TestContentMetadataServiceAddsHotSearchOriginAndKeepsViralCategorySeparate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:content-metadata-source-tags?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.TaskExecution{}, &model.ContentMetadataReport{}, &model.ContentTagAssignment{}, &model.ContentTagVocabulary{}, &model.AgentFeedback{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	svc := NewContentMetadataService(repo, nil)
	ctx := context.Background()

	for _, task := range []*model.Task{
		{ID: "task-hot", UserID: "user-1", ProjectID: "project-1", Type: model.TaskTypeWechatArticle, Status: model.TaskStatusCompleted, ContentOrigin: model.ContentOriginHotSearch},
		{ID: "task-plain", UserID: "user-1", ProjectID: "project-1", Type: model.TaskTypeWechatArticle, Status: model.TaskStatusCompleted},
		{ID: "task-viral", UserID: "user-1", ProjectID: "project-1", Type: model.TaskTypeWechatArticle, Status: model.TaskStatusCompleted},
	} {
		if err := repo.Tasks().Create(ctx, task); err != nil {
			t.Fatal(err)
		}
		executionID := "exec-" + strings.TrimPrefix(task.ID, "task-")
		if err := repo.TaskExecutions().Create(ctx, contentMetadataTestExecution(executionID, task.ID, "final")); err != nil {
			t.Fatal(err)
		}
	}

	for _, input := range []ContentMetadataInput{
		{TaskID: "task-hot", ExecutionID: "exec-hot", RawMetadata: []byte(`{"schema_version":"2.0","taxonomy_version":"2.0","tagging_status":"succeeded","subjects":[{"artifact_role":"final","tags":[{"dimension":"source_relation","value":"adapted","confidence":0.9,"evidence":[{"source_file":"output/final.md","line_start":1,"line_end":1,"quote":"改编","evidence_kind":"source_relation"}]}]}]}`)},
		{TaskID: "task-plain", ExecutionID: "exec-plain", RawMetadata: []byte(`{"schema_version":"2.0","taxonomy_version":"2.0","tagging_status":"succeeded","subjects":[{"artifact_role":"final","tags":[],"omitted_dimensions":["domain"]}]}`)},
		{TaskID: "task-viral", ExecutionID: "exec-viral", RawMetadata: []byte(`{"schema_version":"2.0","taxonomy_version":"2.0","tagging_status":"succeeded","subjects":[{"artifact_role":"final","tags":[]}]}`)},
	} {
		if _, err := svc.Submit(ctx, input); err != nil {
			t.Fatalf("Submit(%s): %v", input.TaskID, err)
		}
	}

	var hotTag model.ContentTagAssignment
	if err := db.Joins("JOIN content_metadata_reports ON content_metadata_reports.id = content_tag_assignments.report_id").Where("content_metadata_reports.task_id = ? AND content_tag_assignments.canonical_value = ?", "task-hot", "hot_search").First(&hotTag).Error; err != nil {
		t.Fatal(err)
	}
	if hotTag.Dimension != "source_relation" || hotTag.CanonicalValue != "hot_search" || hotTag.DisplayName != "热搜" {
		t.Fatalf("hot-search tag = %#v", hotTag)
	}
	var hotSourceTagCount int64
	if err := db.Model(&model.ContentTagAssignment{}).Joins("JOIN content_metadata_reports ON content_metadata_reports.id = content_tag_assignments.report_id").Where("content_metadata_reports.task_id = ? AND content_tag_assignments.dimension = ?", "task-hot", "source_relation").Count(&hotSourceTagCount).Error; err != nil {
		t.Fatal(err)
	}
	if hotSourceTagCount != 1 {
		t.Fatalf("hot-search source tags = %d, want exactly one server-owned tag", hotSourceTagCount)
	}
	var plainTagCount int64
	if err := db.Model(&model.ContentTagAssignment{}).Joins("JOIN content_metadata_reports ON content_metadata_reports.id = content_tag_assignments.report_id").Where("content_metadata_reports.task_id = ? AND content_tag_assignments.dimension = ?", "task-plain", "source_relation").Count(&plainTagCount).Error; err != nil {
		t.Fatal(err)
	}
	if plainTagCount != 0 {
		t.Fatalf("plain task source tags = %d, want 0", plainTagCount)
	}
	var viralTag model.ContentTagAssignment
	if err := db.Where("report_id IN (SELECT id FROM content_metadata_reports WHERE task_id = ?) AND dimension = ?", "task-viral", "performance").First(&viralTag).Error; err == nil {
		t.Fatal("agent performance tag must not be persisted")
	}
}

func TestContentMetadataServiceRejectsUnknownDimension(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:content-metadata-service-invalid?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.TaskExecution{}, &model.ContentMetadataReport{}, &model.ContentTagAssignment{}, &model.ContentTagVocabulary{}, &model.AgentFeedback{}); err != nil {
		t.Fatal(err)
	}
	svc := NewContentMetadataService(repository.New(db), nil)
	repo := repository.New(db)
	if err := repo.Tasks().Create(context.Background(), &model.Task{ID: "task-invalid", UserID: "user-1", ProjectID: "project-1", Type: model.TaskTypeWechatArticle, Status: model.TaskStatusCompleted}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(context.Background(), contentMetadataTestExecution("exec-invalid", "task-invalid", "final")); err != nil {
		t.Fatal(err)
	}
	_, err = svc.Submit(context.Background(), ContentMetadataInput{TaskID: "task-invalid", ExecutionID: "exec-invalid", RawMetadata: []byte(`{"tags":[{"dimension":"unknown","value":"x"}]}`)})
	if err == nil {
		t.Fatal("expected unknown dimension error")
	}
}

func TestContentMetadataServiceNormalizesVocabularyAliasAndPersistsDisplayName(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:content-metadata-service-vocab?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.TaskExecution{}, &model.ContentMetadataReport{}, &model.ContentTagAssignment{}, &model.ContentTagVocabulary{}, &model.AgentFeedback{}); err != nil {
		t.Fatal(err)
	}
	aliasJSON := []byte(`["茶饮"]`)
	if err := db.Create(&model.ContentTagVocabulary{ID: "vocab-1", Dimension: "domain", Value: "tea", DisplayName: "茶叶", Status: model.ContentTagCanonical, Aliases: aliasJSON, TaxonomyVersion: model.ContentTaxonomyVersion}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewContentMetadataService(repository.New(db), nil)
	repo := repository.New(db)
	if err := repo.Tasks().Create(context.Background(), &model.Task{ID: "task-vocab", UserID: "user-1", ProjectID: "project-1", Type: model.TaskTypeWechatArticle, Status: model.TaskStatusCompleted}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(context.Background(), contentMetadataTestExecution("exec-vocab", "task-vocab", "final")); err != nil {
		t.Fatal(err)
	}
	report, err := svc.Submit(context.Background(), ContentMetadataInput{TaskID: "task-vocab", ExecutionID: "exec-vocab", RawMetadata: []byte(`{"schema_version":"2.0","taxonomy_version":"2.0","tagging_status":"succeeded","subjects":[{"artifact_role":"final","tags":[{"dimension":"domain","value":"茶饮","confidence":0.9,"evidence":[{"source_file":"output/final.md","line_start":1,"line_end":1,"quote":"茶饮","evidence_kind":"explicit_topic"}]}]}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	var tag model.ContentTagAssignment
	if err := db.Where("report_id = ? AND dimension = ?", report.ID, "domain").First(&tag).Error; err != nil {
		t.Fatal(err)
	}
	if tag.CanonicalValue != "tea" || tag.DisplayName != "茶叶" || tag.LabelStatus != model.ContentTagCanonical {
		t.Fatalf("normalized tag = %#v", tag)
	}
}

func TestContentMetadataServiceRejectsForeignAuthenticatedUser(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:content-metadata-service-auth?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.TaskExecution{}, &model.ContentMetadataReport{}, &model.ContentTagAssignment{}, &model.ContentTagVocabulary{}, &model.AgentFeedback{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	if err := repo.Tasks().Create(context.Background(), &model.Task{ID: "task-owned", UserID: "owner-1", ProjectID: "project-1", Type: model.TaskTypeWechatArticle, Status: model.TaskStatusCompleted}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(context.Background(), contentMetadataTestExecution("exec-owned", "task-owned", "final")); err != nil {
		t.Fatal(err)
	}
	svc := NewContentMetadataService(repo, nil)
	_, err = svc.Submit(context.Background(), ContentMetadataInput{AuthenticatedUserID: "owner-2", TaskID: "task-owned", ExecutionID: "exec-owned", RawMetadata: []byte(`{"schema_version":"2.0","taxonomy_version":"2.0","tagging_status":"succeeded","subjects":[{"artifact_role":"final","tags":[],"omitted_dimensions":["domain"]}]}`)})
	if err == nil {
		t.Fatal("expected foreign user rejection")
	}
}

func TestContentMetadataServiceKeepsUnknownVocabularyDiagnosticAndOutOfAnalytics(t *testing.T) {
	_, db, svc, task, execution := newContentMetadataFixture(t, "content-metadata-unknown", model.TaskTypeWechatArticle)
	metadata := []byte(`{"schema_version":"2.0","taxonomy_version":"2.0","tagging_status":"succeeded","subjects":[{"artifact_role":"final","tags":[{"dimension":"domain","value":"space-tourism","confidence":0.6,"evidence":[{"source_file":"output/final.md","line_start":1,"line_end":1,"quote":"太空旅行","evidence_kind":"explicit_topic"}]}]}]}`)
	report, err := svc.Submit(context.Background(), ContentMetadataInput{TaskID: task.ID, ExecutionID: execution.ID, RawMetadata: metadata})
	if err != nil {
		t.Fatal(err)
	}
	var tag model.ContentTagAssignment
	if err := db.Where("report_id = ? AND dimension = ?", report.ID, "domain").First(&tag).Error; err != nil {
		t.Fatal(err)
	}
	if tag.LabelStatus != model.ContentTagDiagnostic || tag.CanonicalValue != "" || tag.AnalyticsVisible {
		t.Fatalf("unknown tag = %#v", tag)
	}
	var vocabularyCount int64
	if err := db.Model(&model.ContentTagVocabulary{}).Where("dimension = ? AND value = ?", "domain", "space-tourism").Count(&vocabularyCount).Error; err != nil {
		t.Fatal(err)
	}
	if vocabularyCount != 0 {
		t.Fatalf("unknown value entered vocabulary: %d", vocabularyCount)
	}
}

func TestContentMetadataServiceRejectsWhitespacePaddedServerOwnedDimension(t *testing.T) {
	_, _, svc, task, execution := newContentMetadataFixture(t, "content-metadata-server-owned-whitespace", model.TaskTypeWechatArticle)
	metadata := []byte(`{"schema_version":"2.0","taxonomy_version":"2.0","tagging_status":"succeeded","subjects":[{"artifact_role":"final","tags":[{"dimension":" performance ","value":"viral","confidence":0.9,"evidence":[{"source_file":"output/final.md","line_start":1,"line_end":1,"quote":"爆款","evidence_kind":"performance"}]}]}]}`)
	if _, err := svc.Submit(context.Background(), ContentMetadataInput{TaskID: task.ID, ExecutionID: execution.ID, RawMetadata: metadata}); err == nil || !strings.Contains(err.Error(), "server-owned") {
		t.Fatalf("error = %v, want server-owned dimension rejection", err)
	}
}

func TestContentMetadataServiceDoesNotMarkMissingRequiredSubjectSucceeded(t *testing.T) {
	for _, tc := range []struct {
		name     string
		subjects string
		want     string
	}{
		{name: "missing subject", subjects: `[]`, want: model.ContentMetadataFailed},
		{name: "empty subject without omissions", subjects: `[{"artifact_role":"final","tags":[]}]`, want: model.ContentMetadataFailed},
		{name: "explicit omissions", subjects: `[{"artifact_role":"final","tags":[],"omitted_dimensions":["domain"]}]`, want: model.ContentMetadataSucceeded},
		{name: "blank omission", subjects: `[{"artifact_role":"final","tags":[],"omitted_dimensions":[" "]}]`, want: model.ContentMetadataFailed},
		{name: "unknown omission", subjects: `[{"artifact_role":"final","tags":[],"omitted_dimensions":["typo"]}]`, want: model.ContentMetadataFailed},
		{name: "server-owned omission", subjects: `[{"artifact_role":"final","tags":[],"omitted_dimensions":["media_form"]}]`, want: model.ContentMetadataFailed},
		{name: "duplicate omission", subjects: `[{"artifact_role":"final","tags":[],"omitted_dimensions":["domain","domain"]}]`, want: model.ContentMetadataFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, svc, task, execution := newContentMetadataFixture(t, "content-metadata-missing-succeeded-"+strings.ReplaceAll(tc.name, " ", "-"), model.TaskTypeWechatArticle)
			metadata := []byte(`{"schema_version":"2.0","taxonomy_version":"2.0","tagging_status":"succeeded","subjects":` + tc.subjects + `}`)
			report, err := svc.Submit(context.Background(), ContentMetadataInput{TaskID: task.ID, ExecutionID: execution.ID, RawMetadata: metadata})
			if err != nil {
				t.Fatal(err)
			}
			if report.TaggingStatus != tc.want {
				t.Fatalf("tagging status = %q, want %q", report.TaggingStatus, tc.want)
			}
		})
	}
}

func TestContentMetadataServiceUsesDisplayNamesForPictureAndPackRoles(t *testing.T) {
	_, db, svc, task, execution := newContentMetadataFixture(t, "content-metadata-picture-labels", model.TaskTypeWechatPicture)
	metadata := []byte(`{"schema_version":"2.0","taxonomy_version":"2.0","tagging_status":"failed","subjects":[]}`)
	report, err := svc.Submit(context.Background(), ContentMetadataInput{TaskID: task.ID, ExecutionID: execution.ID, RawMetadata: metadata})
	if err != nil {
		t.Fatal(err)
	}
	var tags []model.ContentTagAssignment
	if err := db.Where("report_id = ?", report.ID).Find(&tags).Error; err != nil {
		t.Fatal(err)
	}
	labels := map[string]string{}
	for _, tag := range tags {
		labels[tag.Dimension] = tag.DisplayName
	}
	if labels["content_type"] != "公众号贴图" || labels["artifact_role"] != "内容" {
		t.Fatalf("server tag display names = %#v", labels)
	}
}

func TestContentMetadataServiceAcceptsCanonicalCompletionMetadataFromWechatPictureAgent(t *testing.T) {
	_, db, svc, task, execution := newContentMetadataFixture(t, "content-metadata-wechat-picture-agent", model.TaskTypeWechatPicture)
	metadata := []byte(`{"schema_version":"2.0","taxonomy_version":"2.0","task_id":"` + task.ID + `","execution_id":"` + execution.ID + `","task_type":"wechat-picture","tagging_status":"succeeded","subjects":[{"artifact_role":"content","tags":[{"dimension":"domain","value":"茶文化","evidence":[{"source_file":"output/content.md","line_start":1,"line_end":21,"quote":"吃蟹配什么茶？老茶客只认这三杯","evidence_kind":"explicit"}]},{"dimension":"audience","value":"都市银发茶文化爱好者","evidence":[{"source_file":"output/topic-analysis.md","line_start":66,"line_end":66,"quote":"都市银发女性为主，高净值，重传统，家里常备茶具，也愿意为一场蟹宴花心思。","evidence_kind":"explicit"}]}],"omitted_dimensions":["funnel_stage","presentation_device"]}],"feedback":{"scores":{"completeness":8}}}`)
	report, err := svc.Submit(context.Background(), ContentMetadataInput{TaskID: task.ID, ExecutionID: execution.ID, RawMetadata: metadata})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if report.TaggingStatus != model.ContentMetadataSucceeded {
		t.Fatalf("tagging status = %q, want succeeded", report.TaggingStatus)
	}
	var tags []model.ContentTagAssignment
	if err := db.Where("report_id = ?", report.ID).Find(&tags).Error; err != nil {
		t.Fatal(err)
	}
	if len(tags) != 6 {
		t.Fatalf("tag count = %d, want semantic and server-owned tags", len(tags))
	}
	for _, tag := range tags {
		if tag.ArtifactRole != "content" {
			t.Fatalf("artifact role = %q, want content", tag.ArtifactRole)
		}
	}
}

func TestContentMetadataServiceRecomputesHistoricalArtifactWithoutReport(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:content-metadata-historical?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.TaskExecution{}, &model.TaskFile{}, &model.ContentMetadataReport{}, &model.ContentTagAssignment{}, &model.ContentTagVocabulary{}, &model.AgentFeedback{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	task := &model.Task{ID: "historical-task", UserID: "user-1", ProjectID: "project-1", Type: model.TaskTypeWechatPicture, Status: model.TaskStatusCompleted}
	if err := repo.Tasks().Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	execution := contentMetadataTestExecution("historical-execution", task.ID, "content")
	if err := repo.TaskExecutions().Create(context.Background(), execution); err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"schema_version":"2.0","taxonomy_version":"2.0","status":"succeeded","subjects":[{"artifact_role":"content","tags":[{"dimension":"domain","value":"茶文化","evidence":[{"source_file":"output/content.md","line_start":1,"line_end":1,"quote":"茶","evidence_kind":"explicit"}]}],"omitted_dimensions":["audience","funnel_stage","presentation_device"]}]}`)
	const objectKey = "users/user-1/tasks/historical-task/output/content-tags.json"
	if _, err := store.Upload(context.Background(), objectKey, bytes.NewReader(raw), "application/json"); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().Create(context.Background(), &model.TaskFile{TaskID: task.ID, ExecutionID: execution.ID, State: model.TaskFileStateDelivered, Role: model.FileRoleContentTags, FilePath: "output/content-tags.json", FileName: "content-tags.json", MimeType: "application/json", FileSize: int64(len(raw)), ContentHash: "historical-hash", OSSKey: objectKey, StorageProvider: store.Name()}); err != nil {
		t.Fatal(err)
	}
	svc := NewContentMetadataService(repo, nil, store)
	report, err := svc.RecomputeTags(context.Background(), task.UserID, task.ID, execution.ID)
	if err != nil {
		t.Fatalf("RecomputeTags: %v", err)
	}
	if report.TaggingStatus != model.ContentMetadataSucceeded {
		t.Fatalf("tagging status = %q, want succeeded", report.TaggingStatus)
	}
	var tags []model.ContentTagAssignment
	if err := db.Where("report_id = ?", report.ID).Find(&tags).Error; err != nil {
		t.Fatal(err)
	}
	if len(tags) != 5 {
		t.Fatalf("tag count = %d, want semantic and server-owned tags", len(tags))
	}
}

func TestContentMetadataServiceMissingSubjectsKeepsServerTagsAndFailedStatus(t *testing.T) {
	_, db, svc, task, execution := newContentMetadataFixture(t, "content-metadata-missing", model.TaskTypeWechatArticle)
	execution.AgentPackTaggingContract = datatypes.JSON(`{"enabled":true,"profile":"generic-content","required_artifact":"output/content-tags.json","subject_roles":["final_markdown"]}`)
	execution.AgentPackDeliveryContract = datatypes.JSON(`[{"role":"final_markdown","path":"output/04-article-final.md","mime_type":"text/markdown"}]`)
	if err := db.Save(execution).Error; err != nil {
		t.Fatal(err)
	}
	metadata := []byte(`{"schema_version":"2.0","taxonomy_version":"2.0","tagging_status":"failed","subjects":[],"tagging_diagnostic":"tagging_missing"}`)
	report, err := svc.Submit(context.Background(), ContentMetadataInput{TaskID: task.ID, ExecutionID: execution.ID, RawMetadata: metadata})
	if err != nil {
		t.Fatal(err)
	}
	if report.TaggingStatus != model.ContentMetadataFailed {
		t.Fatalf("tagging status = %q", report.TaggingStatus)
	}
	var tags []model.ContentTagAssignment
	if err := db.Where("report_id = ?", report.ID).Find(&tags).Error; err != nil {
		t.Fatal(err)
	}
	if len(tags) != 4 {
		t.Fatalf("server tags = %d, want 4", len(tags))
	}
	for _, tag := range tags {
		if tag.Source != "server" || tag.LabelStatus != model.ContentTagCanonical || !tag.AnalyticsVisible {
			t.Fatalf("server-derived tag = %#v", tag)
		}
		if tag.ArtifactRole != "final_markdown" {
			t.Fatalf("artifact role = %q", tag.ArtifactRole)
		}
	}
}

func TestContentMetadataServiceRejectsNonContentSubject(t *testing.T) {
	_, _, svc, task, execution := newContentMetadataFixture(t, "content-metadata-analysis", model.TaskTypeViralAnalysis)
	_, err := svc.Submit(context.Background(), ContentMetadataInput{TaskID: task.ID, ExecutionID: execution.ID, RawMetadata: []byte(`{"schema_version":"2.0","taxonomy_version":"2.0","tagging_status":"succeeded","subjects":[]}`)})
	if err == nil || !strings.Contains(err.Error(), "not a content subject") {
		t.Fatalf("error = %v", err)
	}
}

func TestContentMetadataServiceRejectsKnownContentWithoutFrozenTaggingContract(t *testing.T) {
	_, db, svc, task, execution := newContentMetadataFixture(t, "content-metadata-no-contract", model.TaskTypeWechatArticle)
	if err := db.Model(execution).Updates(map[string]any{"agent_pack_tagging_contract": nil, "agent_pack_delivery_contract": nil}).Error; err != nil {
		t.Fatal(err)
	}
	_, err := svc.Submit(context.Background(), ContentMetadataInput{TaskID: task.ID, ExecutionID: execution.ID, RawMetadata: []byte(`{"schema_version":"2.0","taxonomy_version":"2.0","tagging_status":"failed","subjects":[]}`)})
	if err == nil || !strings.Contains(err.Error(), "requires a valid frozen tagging contract") {
		t.Fatalf("error = %v", err)
	}
}

func TestContentMetadataServiceAcceptsFutureContentTypeFromFrozenTaggingContract(t *testing.T) {
	_, db, svc, task, execution := newContentMetadataFixture(t, "content-metadata-future", "future-platform")
	execution.AgentPackTaggingContract = datatypes.JSON(`{"enabled":true,"profile":"generic-content","required_artifact":"output/content-tags.json","subject_roles":["final"]}`)
	execution.AgentPackDeliveryContract = datatypes.JSON(`[{"role":"final","path":"output/final.md","mime_type":"text/markdown"}]`)
	if err := db.Save(execution).Error; err != nil {
		t.Fatal(err)
	}
	report, err := svc.Submit(context.Background(), ContentMetadataInput{TaskID: task.ID, ExecutionID: execution.ID, RawMetadata: []byte(`{"schema_version":"2.0","taxonomy_version":"2.0","tagging_status":"failed","subjects":[]}`)})
	if err != nil {
		t.Fatal(err)
	}
	if report.ContentType != "future-platform" || report.TaggingStatus != model.ContentMetadataFailed {
		t.Fatalf("future content report = %#v", report)
	}
	var count int64
	if err := db.Model(&model.ContentTagAssignment{}).Where("report_id = ? AND source = ?", report.ID, "server").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 4 {
		t.Fatalf("future content server tags = %d, want 4", count)
	}
}
