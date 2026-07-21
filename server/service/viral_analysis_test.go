package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	serverbilling "github.com/anbanai/anban-creator/server/billing"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/platform"
	"github.com/anbanai/anban-creator/server/repository"
)

type fakeViralAnalysisLLM struct {
	systemPrompt string
	userPrompt   string
	response     string
}

func (f *fakeViralAnalysisLLM) Complete(_ context.Context, systemPrompt, userPrompt string) (string, error) {
	f.systemPrompt = systemPrompt
	f.userPrompt = userPrompt
	return f.response, nil
}

func (f *fakeViralAnalysisLLM) CompleteWithImage(context.Context, string, string, string) (string, error) {
	return "", nil
}

type failingViralAnalysisEnqueuer struct{}

func (failingViralAnalysisEnqueuer) Enqueue(string, []byte) error {
	return errors.New("queue down")
}

func (failingViralAnalysisEnqueuer) EnqueueIn(string, []byte, time.Duration) error {
	return errors.New("queue down")
}

type fakeViralNoteFetcher struct {
	content *platform.SeednoteNoteContent
	err     error
}

func (f fakeViralNoteFetcher) FetchNoteContent(context.Context, string) (*platform.SeednoteNoteContent, error) {
	return f.content, f.err
}

func setupViralAnalysisRepo(t *testing.T) repository.Repository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.New().String()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	return repository.New(db)
}

func newViralAnalysisBillingFixture(t *testing.T, paid, debt int64) (*billingWalletFixture, string) {
	t.Helper()
	repo := setupViralAnalysisRepo(t)
	userID := uuid.New().String()
	if err := repo.Users().Create(context.Background(), &model.User{
		ID: userID, Email: userID + "@example.com", Nickname: "Viral User",
		Password: "hashed", InviteCode: strings.ReplaceAll(userID[:8], "-", ""),
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	bundle, err := serverbilling.LoadBundle("../billing")
	if err != nil {
		t.Fatalf("load billing bundle: %v", err)
	}
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	catalog := NewBillingCatalogService(repo, bundle, BillingCatalogOptions{Now: func() time.Time { return now }})
	if _, err := catalog.Publish(context.Background()); err != nil {
		t.Fatalf("publish billing catalog: %v", err)
	}
	if err := repo.Billing().CreateAccount(context.Background(), &model.BillingWalletAccount{
		UserID: userID, PaidCredits: paid, DebtCredits: debt,
	}); err != nil {
		t.Fatalf("create billing account: %v", err)
	}
	if paid > 0 {
		createBillingLot(t, repo, model.BillingCreditLot{
			ID: uuid.NewString(), UserID: userID, Kind: model.BillingCreditLotKindPaid,
			SourceType: "fixture", SourceID: "viral-paid-" + userID, CatalogID: bundle.Products.CatalogID,
			OriginalCredits: paid, AvailableCredits: paid, CreatedAt: now.Add(-time.Hour),
		})
	}
	wallet := NewBillingWalletService(repo, bundle, BillingWalletOptions{Now: func() time.Time { return now }})
	return &billingWalletFixture{repo: repo, catalog: catalog, wallet: wallet, now: now}, userID
}

func TestValidateEvidenceDrivenResultAcceptsCompleteResult(t *testing.T) {
	result := completeEvidenceDrivenResult()
	if err := ValidateEvidenceDrivenResult(&result); err != nil {
		t.Fatalf("ValidateEvidenceDrivenResult: %v", err)
	}
}

func TestValidateEvidenceDrivenResultRejectsMissingDimension(t *testing.T) {
	result := completeEvidenceDrivenResult()
	result.Dimensions = result.Dimensions[:6]

	err := ValidateEvidenceDrivenResult(&result)
	if err == nil || !strings.Contains(err.Error(), "comment_signals") {
		t.Fatalf("expected missing comment_signals error, got %v", err)
	}
}

func TestValidateEvidenceDrivenResultRejectsLegacyDimensionNames(t *testing.T) {
	result := completeEvidenceDrivenResult()
	result.Dimensions[0].Name = "topic"

	err := ValidateEvidenceDrivenResult(&result)
	if err == nil || !strings.Contains(err.Error(), "topic_angle") {
		t.Fatalf("expected topic_angle validation error, got %v", err)
	}
}

func TestValidateEvidenceDrivenResultRejectsInvalidEnum(t *testing.T) {
	result := completeEvidenceDrivenResult()
	result.Dimensions[0].Transferability = "very-high"

	err := ValidateEvidenceDrivenResult(&result)
	if err == nil || !strings.Contains(err.Error(), "transferability") {
		t.Fatalf("expected transferability validation error, got %v", err)
	}
}

func TestValidateEvidenceDrivenResultRejectsOutOfRangeScore(t *testing.T) {
	result := completeEvidenceDrivenResult()
	result.OverallScore.Score = 101

	err := ValidateEvidenceDrivenResult(&result)
	if err == nil || !strings.Contains(err.Error(), "overall_score.score") {
		t.Fatalf("expected score validation error, got %v", err)
	}
}

func TestValidateEvidenceDrivenResultRejectsOldSchema(t *testing.T) {
	llm := &fakeViralAnalysisLLM{response: `{
		"overall_score": 88,
		"viral_factors": {"topic": 90},
		"title_analysis": {"score": 80},
		"suggestions": ["优化标题"]
	}`}
	logger := zerolog.New(io.Discard)
	svc := NewViralAnalysisService(nil, nil, llm, nil, &logger)

	_, err := svc.analyzeWithLLM(context.Background(), &platform.SeednoteNoteContent{NoteID: "note-1"}, "https://example.com/note/1")
	if err == nil || !strings.Contains(err.Error(), "parse LLM response as JSON") {
		t.Fatalf("expected old schema to fail strict validation, got %v", err)
	}
}

func TestAnalyzeWithLLMProducesStrictSchemaAndBackendMetadata(t *testing.T) {
	input := completeEvidenceDrivenResult()
	input.TemplateMeta.TemplateHash = "model-generated-hash"
	input.TemplateMeta.SourceFeedID = "model-source"
	input.TemplateMeta.SourceURL = "https://model.example"

	response, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	llm := &fakeViralAnalysisLLM{response: "```json\n" + string(response) + "\n```"}
	logger := zerolog.New(io.Discard)
	svc := NewViralAnalysisService(nil, nil, llm, nil, &logger)

	raw, err := svc.analyzeWithLLM(context.Background(), &platform.SeednoteNoteContent{
		NoteID:       "note-1",
		Title:        "新手7天学会选咖啡豆",
		Description:  "先说结论：别只看产区。\n1. 看烘焙度\n2. 看处理法",
		Tags:         "咖啡,咖啡豆,新手咖啡",
		CoverURL:     "https://example.com/cover.jpg",
		LikeCount:    1200,
		CollectCount: 980,
		CommentCount: 0,
		ShareCount:   90,
	}, "https://example.com/note/1")
	if err != nil {
		t.Fatalf("analyzeWithLLM: %v", err)
	}

	requiredPromptTerms := []string{
		"证据驱动",
		"只输出 JSON",
		"topic_angle",
		"comment_signals",
		"observation",
		"mechanism",
		"transferability",
		"action",
		"overall_score",
		"confidence",
		"evidence_count",
		"missing_data",
		"why_not_higher",
		"viral_template",
		"template_meta",
	}
	for _, term := range requiredPromptTerms {
		if !strings.Contains(llm.systemPrompt, term) {
			t.Fatalf("system prompt missing %q\nprompt:\n%s", term, llm.systemPrompt)
		}
	}
	if !strings.Contains(llm.userPrompt, `"source_url":"https://example.com/note/1"`) {
		t.Fatalf("user prompt missing source_url: %s", llm.userPrompt)
	}

	var result EvidenceDrivenAnalysisResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	if err := ValidateEvidenceDrivenResult(&result); err != nil {
		t.Fatalf("validated result: %v", err)
	}
	if result.TemplateMeta.SourceFeedID != "note-1" {
		t.Fatalf("source_feed_id should be backend-derived, got %q", result.TemplateMeta.SourceFeedID)
	}
	if result.TemplateMeta.SourceURL != "https://example.com/note/1" {
		t.Fatalf("source_url should be backend-derived, got %q", result.TemplateMeta.SourceURL)
	}
	if result.TemplateMeta.TemplateHash == "" || result.TemplateMeta.TemplateHash == "model-generated-hash" {
		t.Fatalf("template_hash should be backend-derived, got %q", result.TemplateMeta.TemplateHash)
	}
	if !result.TemplateMeta.SaveEligible {
		t.Fatalf("template should be save eligible")
	}
	if result.OverallScore.Confidence != "medium" {
		t.Fatalf("overall score confidence changed: %q", result.OverallScore.Confidence)
	}
}

func TestAnalyzeWithLLMUsesNormalizedURLHashWhenNoteIDMissing(t *testing.T) {
	input := completeEvidenceDrivenResult()
	response, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	logger := zerolog.New(io.Discard)
	svc := NewViralAnalysisService(nil, nil, &fakeViralAnalysisLLM{response: string(response)}, nil, &logger)

	raw, err := svc.analyzeWithLLM(context.Background(), &platform.SeednoteNoteContent{
		Title: "新手7天学会选咖啡豆",
		Tags:  "咖啡,新手",
	}, " HTTPS://Example.COM/note/1?b=2#frag ")
	if err != nil {
		t.Fatalf("analyzeWithLLM: %v", err)
	}
	var result EvidenceDrivenAnalysisResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	want := sourceFeedID("", "https://example.com/note/1?b=2")
	if result.TemplateMeta.SourceFeedID != want {
		t.Fatalf("source_feed_id = %q, want normalized hash %q", result.TemplateMeta.SourceFeedID, want)
	}
	if result.TemplateMeta.SourceURL != "HTTPS://Example.COM/note/1?b=2#frag" {
		t.Fatalf("source_url should preserve submitted URL after trim, got %q", result.TemplateMeta.SourceURL)
	}
}

func TestAnalyzeWithLLMExtractsJSONFromExtraText(t *testing.T) {
	input := completeEvidenceDrivenResult()
	response, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	llm := &fakeViralAnalysisLLM{response: "以下是分析结果：\n" + string(response) + "\n请查收。"}
	logger := zerolog.New(io.Discard)
	svc := NewViralAnalysisService(nil, nil, llm, nil, &logger)

	raw, err := svc.analyzeWithLLM(context.Background(), &platform.SeednoteNoteContent{
		NoteID: "note-1",
		Title:  "新手7天学会选咖啡豆",
		Tags:   "咖啡,新手",
	}, "https://example.com/note/1")
	if err != nil {
		t.Fatalf("analyzeWithLLM should extract embedded JSON: %v", err)
	}

	var result EvidenceDrivenAnalysisResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	if result.TemplateMeta.SourceFeedID != "note-1" {
		t.Fatalf("source_feed_id = %q, want note-1", result.TemplateMeta.SourceFeedID)
	}
}

func TestViralAnalysisExecuteReversesFixedChargeWhenFetcherReturnsNilContent(t *testing.T) {
	fixture, userID := newViralAnalysisBillingFixture(t, 2_000, 0)
	logger := zerolog.New(io.Discard)
	svc := NewViralAnalysisService(fixture.repo, fakeViralNoteFetcher{}, &fakeViralAnalysisLLM{}, &mockEnqueuer{}, &logger)
	svc.SetBillingServices(fixture.catalog, fixture.wallet)
	analysis, err := svc.Create(context.Background(), userID, model.ViralAnalysisSourceNote, "https://example.com/note/1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := svc.ExecuteAnalysis(context.Background(), analysis.ID); err != nil {
		t.Fatalf("ExecuteAnalysis should persist failure instead of returning error: %v", err)
	}
	found, err := fixture.repo.ViralAnalyses().FindByID(context.Background(), analysis.ID)
	if err != nil {
		t.Fatalf("find analysis: %v", err)
	}
	if found.Status != model.ViralAnalysisStatusFailed {
		t.Fatalf("status = %q, want failed", found.Status)
	}
	if !strings.Contains(found.ErrorMessage, "empty response") {
		t.Fatalf("error message = %q, want empty response", found.ErrorMessage)
	}
	if found.BillingTerminalReason != model.TaskBillingTerminalProviderError {
		t.Fatalf("terminal reason = %q, want provider_error", found.BillingTerminalReason)
	}
	if account := fixture.account(t, userID); account.PaidCredits != 2_000 || account.DebtCredits != 0 {
		t.Fatalf("wallet after reversal = %#v", account)
	}
}

func TestAnalyzeWithLLMRejectsInvalidResponse(t *testing.T) {
	invalid := completeEvidenceDrivenResult()
	invalid.Dimensions = invalid.Dimensions[:5]
	response, err := json.Marshal(invalid)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	llm := &fakeViralAnalysisLLM{response: string(response)}
	logger := zerolog.New(io.Discard)
	svc := NewViralAnalysisService(nil, nil, llm, nil, &logger)

	_, err = svc.analyzeWithLLM(context.Background(), &platform.SeednoteNoteContent{NoteID: "note-1"}, "https://example.com/note/1")
	if err == nil || !strings.Contains(err.Error(), "validate evidence-driven result") {
		t.Fatalf("expected validation failure, got %v", err)
	}
}

func TestViralAnalysisCreateReversesFixedChargeWhenEnqueueFails(t *testing.T) {
	fixture, userID := newViralAnalysisBillingFixture(t, 2_000, 0)
	logger := zerolog.New(io.Discard)
	svc := NewViralAnalysisService(fixture.repo, nil, nil, failingViralAnalysisEnqueuer{}, &logger)
	svc.SetBillingServices(fixture.catalog, fixture.wallet)

	analysis, err := svc.Create(context.Background(), userID, model.ViralAnalysisSourceNote, "https://example.com/note/1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if analysis.Status != model.ViralAnalysisStatusFailed {
		t.Fatalf("status = %q, want failed", analysis.Status)
	}

	failed, err := fixture.repo.ViralAnalyses().FindByID(context.Background(), analysis.ID)
	if err != nil {
		t.Fatalf("reload failed analysis: %v", err)
	}
	if failed.BillingTerminalReason != model.TaskBillingTerminalPlatformError {
		t.Fatalf("terminal reason = %q, want platform_error", failed.BillingTerminalReason)
	}
	if account := fixture.account(t, userID); account.PaidCredits != 2_000 || account.DebtCredits != 0 {
		t.Fatalf("wallet after reversal = %#v", account)
	}
	if _, err := fixture.repo.Billing().FindReversal(context.Background(), *analysis.BillingChargeID); err != nil {
		t.Fatalf("find fixed charge reversal: %v", err)
	}
}

func TestViralAnalysisExecuteReversesFixedChargeWhenLLMValidationFails(t *testing.T) {
	fixture, userID := newViralAnalysisBillingFixture(t, 2_000, 0)
	invalid := completeEvidenceDrivenResult()
	invalid.Dimensions = invalid.Dimensions[:5]
	response, err := json.Marshal(invalid)
	if err != nil {
		t.Fatalf("marshal invalid fixture: %v", err)
	}
	logger := zerolog.New(io.Discard)
	svc := NewViralAnalysisService(
		fixture.repo,
		fakeViralNoteFetcher{content: &platform.SeednoteNoteContent{NoteID: "note-1", Title: "测试"}},
		&fakeViralAnalysisLLM{response: string(response)},
		&mockEnqueuer{},
		&logger,
	)
	svc.SetBillingServices(fixture.catalog, fixture.wallet)
	analysis, err := svc.Create(context.Background(), userID, model.ViralAnalysisSourceNote, "https://example.com/note/1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := svc.ExecuteAnalysis(context.Background(), analysis.ID); err != nil {
		t.Fatalf("ExecuteAnalysis should persist failure instead of returning error: %v", err)
	}
	found, err := fixture.repo.ViralAnalyses().FindByID(context.Background(), analysis.ID)
	if err != nil {
		t.Fatalf("find analysis: %v", err)
	}
	if found.Status != model.ViralAnalysisStatusFailed {
		t.Fatalf("status = %q, want failed", found.Status)
	}
	if !strings.Contains(found.ErrorMessage, "validate evidence-driven result") {
		t.Fatalf("error message missing validation reason: %q", found.ErrorMessage)
	}
	if account := fixture.account(t, userID); account.PaidCredits != 2_000 || account.DebtCredits != 0 {
		t.Fatalf("wallet after reversal = %#v", account)
	}
}

func completeEvidenceDrivenResult() EvidenceDrivenAnalysisResult {
	dimensions := []AnalysisDimension{
		dimension("topic_angle", "观察到标题原文「新手7天学会选咖啡豆」直接点名新手身份和时间承诺。"),
		dimension("title", "观察到标题原文「新手7天学会选咖啡豆」使用人群+时间+结果结构。"),
		dimension("cover", "观察到封面数据为 https://example.com/cover.jpg，主视觉可识别为产品图。"),
		dimension("body", "观察到正文片段「先说结论：别只看产区」先给判断再列清单。"),
		dimension("interaction", "观察到互动数据点赞1200、收藏980、评论0，收藏高于评论。"),
		dimension("tags", "观察到标签「咖啡,咖啡豆,新手咖啡」组合了大词和长尾词。"),
		dimension("comment_signals", "观察到评论数据 comment_count=0，缺少真实评论文本。"),
	}
	return EvidenceDrivenAnalysisResult{
		Summary: []string{
			"低门槛人群承诺带来点击",
			"清单正文增强收藏",
			"标签组合覆盖搜索",
		},
		EvidenceTable: []EvidenceItem{
			{Claim: "标题降低理解门槛", Evidence: "标题原文「新手7天学会选咖啡豆」", Source: "title"},
			{Claim: "收藏理由明确", Evidence: "正文片段「1. 看烘焙度」", Source: "body"},
			{Claim: "评论证据不足", Evidence: "互动数据 comment_count=0", Source: "metrics"},
		},
		Dimensions:            dimensions,
		CloneSuggestions:      CloneSuggestions{Title: []string{"人群+时间+结果"}, Body: []string{"开头先给结论"}, Cover: []string{"大字标题+产品图"}, Tags: []string{"大词+垂直词+长尾词"}, Interaction: []string{"结尾二选一"}},
		Risks:                 []string{"不要复制源作者经历"},
		RecommendedCloneDepth: "style-only",
		OverallScore: ScoreResult{
			Score:         79,
			Confidence:    "medium",
			EvidenceCount: 12,
			MissingData:   []string{"无评论内容", "无发布时间"},
			WhyNotHigher:  "评论和封面细节不足",
		},
		ViralTemplate: ViralTemplate{
			TitleTemplate:         "人群+时间+结果",
			CoverTemplate:         "大字标题+主视觉",
			BodyTemplate:          "结论先行+清单",
			InteractionTemplate:   "二选一提问",
			TagTemplate:           "大词+垂直词+长尾词",
			AudienceInsight:       "新手怕踩坑",
			ViralMechanism:        "降低门槛并给收藏理由",
			RewriteConstraints:    []string{"换产品和个人经验"},
			DoNotCopy:             []string{"不要复制原句"},
			RecommendedCloneDepth: "style-only",
			Confidence:            "medium",
		},
		TemplateMeta: TemplateMeta{
			Type:         "seednote",
			Name:         "新手避坑模板",
			Category:     "viral_analysis",
			SourceFeedID: "note-1",
			SourceURL:    "https://example.com/note/1",
			Tags:         []string{"新手"},
			TemplateHash: "abc",
			SaveEligible: true,
		},
	}
}

func dimension(name, observation string) AnalysisDimension {
	return AnalysisDimension{
		Name:            name,
		Observation:     observation,
		Mechanism:       "这个元素可能降低理解成本并提高收藏意愿。",
		Transferability: "high",
		Action:          "下一篇保留结构但替换产品和个人经验。",
	}
}
