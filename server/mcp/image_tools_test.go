package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"

	appconfig "github.com/anbanai/anban-creator/app/config"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
)

func TestGenerateImageSchemaDoesNotExposeModelSelection(t *testing.T) {
	schema := generateImageInputSchema()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties missing or wrong type: %#v", schema["properties"])
	}
	if _, ok := props["image_model_key"]; ok {
		t.Fatalf("generate_image schema must not expose image_model_key")
	}
	required, ok := schema["required"].([]any)
	if !ok {
		t.Fatalf("schema required missing or wrong type: %#v", schema["required"])
	}
	if !containsAnyString(required, "task_id") {
		t.Fatalf("generate_image schema must require task_id, got %#v", required)
	}
}

func TestGenerateImageRejectsExplicitImageModelKey(t *testing.T) {
	oldSvcs := svcs
	t.Cleanup(func() { svcs = oldSvcs })
	svcs = &Services{}

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"project_id":"project-1",
			"task_id":"task-1",
			"prompt":"test prompt",
			"image_model_key":"seedream-4.0"
		}`)},
	}

	res, err := generateImageHandler(withMCPUserID(context.Background(), "user-1"), req)
	if err != nil {
		t.Fatalf("generateImageHandler returned error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected tool error, got %#v", res)
	}
	if !strings.Contains(callToolText(res), "image_model_key is not accepted") {
		t.Fatalf("error text = %q, want image_model_key rejection", callToolText(res))
	}
}

func TestParseVisionVerificationJSON_Pass(t *testing.T) {
	raw := `{"all_entities_present": true, "missing_entities": [], "relevance_score": "high", "overall_pass": true}`
	v := parseVisionVerificationJSON(raw)
	if !v.Passed {
		t.Errorf("[FAIL] Passed = false, want true")
	}
	if v.Score != "high" {
		t.Errorf("[FAIL] Score = %q, want high", v.Score)
	}
	if len(v.MissingEntities) != 0 {
		t.Errorf("[FAIL] MissingEntities = %v, want empty", v.MissingEntities)
	}
	if v.Raw != raw {
		t.Error("[FAIL] Raw not preserved")
	}
}

func TestParseVisionVerificationJSON_MissingEntities(t *testing.T) {
	raw := `{"all_entities_present": false, "missing_entities": ["green shoots", "stone crack"], "relevance_score": "medium", "overall_pass": false}`
	v := parseVisionVerificationJSON(raw)
	if v.Passed {
		t.Errorf("[FAIL] Passed = true, want false")
	}
	if v.Score != "medium" {
		t.Errorf("[FAIL] Score = %q, want medium", v.Score)
	}
	if len(v.MissingEntities) != 2 {
		t.Fatalf("[FAIL] MissingEntities len = %d, want 2", len(v.MissingEntities))
	}
	if v.MissingEntities[0] != "green shoots" {
		t.Errorf("[FAIL] MissingEntities[0] = %q", v.MissingEntities[0])
	}
}

func TestParseVisionVerificationJSON_MarkdownFenced(t *testing.T) {
	raw := "```json\n" + `{"all_entities_present": true, "relevance_score": "high", "overall_pass": true}` + "\n```"
	v := parseVisionVerificationJSON(raw)
	if !v.Passed {
		t.Errorf("[FAIL] Passed = false, want true (fenced JSON)")
	}
	if v.Score != "high" {
		t.Errorf("[FAIL] Score = %q, want high", v.Score)
	}
}

func TestParseVisionVerificationJSON_SurroundedByProse(t *testing.T) {
	raw := `Sure, here is my analysis: {"all_entities_present": true, "relevance_score": "high", "overall_pass": true} Hope that helps!`
	v := parseVisionVerificationJSON(raw)
	if !v.Passed {
		t.Errorf("[FAIL] Passed = false, want true (JSON surrounded by prose)")
	}
}

func TestParseVisionVerificationJSON_NotJSON(t *testing.T) {
	raw := "I cannot analyze this image."
	v := parseVisionVerificationJSON(raw)
	if v.Passed {
		t.Error("[FAIL] Passed = true for non-JSON response")
	}
	if v.Score != "unknown" {
		t.Errorf("[FAIL] Score = %q, want unknown", v.Score)
	}
	if v.Notes == "" {
		t.Error("[FAIL] Notes should explain the failure")
	}
}

func TestParseVisionVerificationJSON_ForbiddenContentFails(t *testing.T) {
	raw := `{"all_entities_present": true, "missing_entities": [], "relevance_score": "high", "overall_pass": true, "has_forbidden_content": true, "forbidden_notes": "text watermark visible in corner"}`
	v := parseVisionVerificationJSON(raw)
	if v.Passed {
		t.Error("[FAIL] Passed should be false when forbidden content detected")
	}
	if !strings.Contains(v.Notes, "watermark") {
		t.Errorf("[FAIL] Notes should include forbidden details, got: %q", v.Notes)
	}
}

func TestParseVisionVerificationJSON_OverallPassOverridesAllEntities(t *testing.T) {
	// overall_pass is authoritative; AllEntitiesPresent false but overall_pass true.
	// (e.g. missing a minor entity but the composition still matches.)
	raw := `{"all_entities_present": false, "missing_entities": ["optional detail"], "relevance_score": "high", "overall_pass": true}`
	v := parseVisionVerificationJSON(raw)
	if !v.Passed {
		t.Error("[FAIL] OverallPass=true should make Passed=true")
	}
}

func TestCoerceBool(t *testing.T) {
	cases := []struct {
		name string
		args []any
		want bool
	}{
		{"native true", []any{true}, true},
		{"native false", []any{false}, false},
		{"any true wins", []any{false, true}, true},
		{"string true", []any{"true"}, true},
		{"string yes", []any{"yes"}, true},
		{"string YES capitalized", []any{"YES"}, true},
		{"string false", []any{"false"}, false},
		{"int 1", []any{1}, true},
		{"int 0", []any{0}, false},
		{"float64 1.0", []any{float64(1)}, true},
		{"garbage string", []any{"maybe"}, false},
		{"nil alone", []any{nil}, false},
		{"all nil", []any{nil, nil, nil}, false},
		// Real-world case: LLM returns string instead of bool.
		{"string true among nils", []any{nil, "true", nil}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := coerceBool(c.args...)
			if got != c.want {
				t.Errorf("[FAIL] coerceBool(%v) = %v, want %v", c.args, got, c.want)
			}
		})
	}
}

func TestParseVisionVerificationJSON_RawPreservedOnParseError(t *testing.T) {
	raw := "{broken"
	v := parseVisionVerificationJSON(raw)
	if v.Raw != raw {
		t.Errorf("[FAIL] Raw not preserved on parse error: got %q", v.Raw)
	}
	if v.Passed {
		t.Error("[FAIL] Should not pass on parse error")
	}
}

func TestVisionVerification_JSONTags(t *testing.T) {
	// Smoke test the JSON tags match the documented schema.
	v := &service.VisionVerification{
		Passed:          true,
		Score:           "high",
		MissingEntities: []string{"a", "b"},
		Notes:           "ok",
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("[FAIL] marshal: %v", err)
	}
	s := string(b)
	for _, want := range []string{`"passed"`, `"score"`, `"missing_entities"`, `"notes"`, `"high"`, `"a"`} {
		if !strings.Contains(s, want) {
			t.Errorf("[FAIL] JSON missing %q in: %s", want, s)
		}
	}
}

func TestShouldUploadAfterVerification(t *testing.T) {
	// Every combination of the three inputs. This is the gate that makes
	// generate_image(upload_to_cdn=true) upload atomically: a rejected image
	// (passed=false) must never consume a material slot, and a missing
	// verification object when one was requested must default to skip.
	passed := &service.VisionVerification{Passed: true}
	failed := &service.VisionVerification{Passed: false, Score: "medium"}

	cases := []struct {
		name             string
		uploadToCDN      bool
		verifyWithVision bool
		verification     *service.VisionVerification
		want             bool
	}{
		{"upload off, no verify", false, false, nil, false},
		{"upload off, verify passed", false, true, passed, false},
		{"upload off, verify failed", false, true, failed, false},
		{"upload on, no verify (always upload)", true, false, nil, true},
		{"upload on, verify passed", true, true, passed, true},
		{"upload on, verify failed -> skip", true, true, failed, false},
		{"upload on, verify requested but nil -> skip (safe default)", true, true, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := shouldUploadAfterVerification(c.uploadToCDN, c.verifyWithVision, c.verification)
			if got != c.want {
				t.Errorf("[FAIL] shouldUploadAfterVerification(%v, %v, %+v) = %v, want %v",
					c.uploadToCDN, c.verifyWithVision, c.verification, got, c.want)
			}
		})
	}
}

func TestImageResult_UploadFields_JSONTags(t *testing.T) {
	// The atomic-upload contract: wechat_url + media_id appear on success;
	// upload_error appears (and the URL fields stay omitempty) on failure.
	// Verify the omitempty so a normal (non-upload) generate result doesn't
	// leak empty "wechat_url":"" / "media_id":"" keys to the agent.
	success := &service.ImageResult{
		FilePath:  "/tmp/img.png",
		WeChatURL: "https://cdn.example/img.png",
		MediaID:   "media_123",
	}
	b, err := json.Marshal(success)
	if err != nil {
		t.Fatalf("[FAIL] marshal success: %v", err)
	}
	s := string(b)
	for _, want := range []string{`"wechat_url":"https://cdn.example/img.png"`, `"media_id":"media_123"`} {
		if !strings.Contains(s, want) {
			t.Errorf("[FAIL] success JSON missing %q in: %s", want, s)
		}
	}

	// A result with no upload fields must not emit empty upload keys.
	plain := &service.ImageResult{FilePath: "/tmp/img.png"}
	bp, err := json.Marshal(plain)
	if err != nil {
		t.Fatalf("[FAIL] marshal plain: %v", err)
	}
	sp := string(bp)
	for _, unwanted := range []string{`"wechat_url"`, `"media_id"`, `"upload_error"`} {
		if strings.Contains(sp, unwanted) {
			t.Errorf("[FAIL] plain JSON should omit %q, got: %s", unwanted, sp)
		}
	}

	// Upload-failure result carries upload_error and omits the URL fields.
	failedUp := &service.ImageResult{
		FilePath:    "/tmp/img.png",
		UploadError: "boom",
	}
	bf, err := json.Marshal(failedUp)
	if err != nil {
		t.Fatalf("[FAIL] marshal failed: %v", err)
	}
	sf := string(bf)
	if !strings.Contains(sf, `"upload_error":"boom"`) {
		t.Errorf("[FAIL] failed JSON missing upload_error in: %s", sf)
	}
	if strings.Contains(sf, `"wechat_url"`) || strings.Contains(sf, `"media_id"`) {
		t.Errorf("[FAIL] failed JSON should not carry URL fields when upload errored: %s", sf)
	}
}

func TestResolveImageBillingModelUsesTaskSelectedPreset(t *testing.T) {
	old := billSvc
	t.Cleanup(func() { billSvc = old })

	billSvc = &billingServices{
		config: &srvconfig.Config{
			ImageAPI: srvconfig.ImageAPIConfig{
				Cover: &appconfig.ImageAPI{Provider: "volcengine", Model: "doubao-seedream", Credits: 9},
			},
			ImagePresets: []srvconfig.ImageModelPreset{
				{
					Key:      "openai-gpt-image",
					Provider: "openai",
					Model:    "gpt-image-2",
					MinTier:  "free",
				},
			},
		},
	}

	provider, mdl, source, err := resolveImageBillingModel(context.Background(), "user-1", "openai-gpt-image")
	if err != nil {
		t.Fatalf("resolveImageBillingModel returned error: %v", err)
	}

	if provider != "openai" || mdl != "gpt-image-2" {
		t.Fatalf("billing model = %s/%s, want openai/gpt-image-2", provider, mdl)
	}
	if source != "preset:openai-gpt-image" {
		t.Fatalf("billing source = %q, want preset:openai-gpt-image", source)
	}
}

func containsAnyString(values []any, want string) bool {
	for _, v := range values {
		if s, ok := v.(string); ok && s == want {
			return true
		}
	}
	return false
}

func callToolText(res *mcp.CallToolResult) string {
	if res == nil || len(res.Content) == 0 {
		return ""
	}
	if text, ok := res.Content[0].(*mcp.TextContent); ok {
		return text.Text
	}
	return ""
}

// fakeTaskFileRegistrar implements taskFileRegistrar for unit tests. Its Enrich
// is a no-op so tests can exercise both the URL and OSSURL branches of the
// helper (the real EnrichFilesWithURLs only sets URL when OSSKey != "").
type fakeTaskFileRegistrar struct {
	uploadCalls  []fakeUploadCall
	uploadResult *model.TaskFile
	uploadErr    error
	enrichCalled bool
}

type fakeUploadCall struct {
	taskID, userID, relPath, mime string
	size                          int64
}

func (f *fakeTaskFileRegistrar) UploadTaskFileFromReader(_ context.Context, taskID, userID, relPath string, reader io.Reader, mimeType string, fileSize int64) (*model.TaskFile, error) {
	io.Copy(io.Discard, reader) // drain so the helper's file handle closes cleanly
	f.uploadCalls = append(f.uploadCalls, fakeUploadCall{taskID, userID, relPath, mimeType, fileSize})
	return f.uploadResult, f.uploadErr
}

func (f *fakeTaskFileRegistrar) EnrichFilesWithURLs(_ context.Context, _ []*model.TaskFile) {
	f.enrichCalled = true
}

func TestRegisterGeneratedImageTaskFile_RegistersAndReturnsFetchableURL(t *testing.T) {
	// P1 + P2 at the unit level: the helper must (a) register with the correct
	// (taskID, relPath, mime, size) and (b) return a fetchable URL that is NOT
	// an inline base64 data URL — the value the handler will assign to
	// ImageResult.DownloadURL.
	tmp := filepath.Join(t.TempDir(), "cover.png")
	body := []byte("fake-png-bytes")
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		t.Fatalf("write temp: %v", err)
	}

	fake := &fakeTaskFileRegistrar{
		uploadResult: &model.TaskFile{URL: "https://cdn.example.com/u/t/output/cover.png"},
	}
	res := &service.ImageResult{FilePath: tmp, OutputMIME: "image/png"}

	url, err := registerGeneratedImageTaskFile(context.Background(), fake, "task-1", "user-1", res)
	if err != nil {
		t.Fatalf("[FAIL] unexpected error: %v", err)
	}
	if url != "https://cdn.example.com/u/t/output/cover.png" {
		t.Errorf("[FAIL] url = %q, want the enriched fetchable URL", url)
	}
	if strings.HasPrefix(url, "data:") {
		t.Errorf("[FAIL] returned url must never be an inline base64 data URL, got %q", url)
	}
	if len(fake.uploadCalls) != 1 {
		t.Fatalf("[FAIL] expected exactly 1 Upload call, got %d", len(fake.uploadCalls))
	}
	c := fake.uploadCalls[0]
	if c.taskID != "task-1" || c.userID != "user-1" {
		t.Errorf("[FAIL] ids = (%q,%q), want (task-1,user-1)", c.taskID, c.userID)
	}
	if c.relPath != tmp {
		t.Errorf("[FAIL] relPath = %q, want %q (result.FilePath passed through)", c.relPath, tmp)
	}
	if c.mime != "image/png" {
		t.Errorf("[FAIL] mime = %q, want image/png", c.mime)
	}
	if c.size != int64(len(body)) {
		t.Errorf("[FAIL] size = %d, want %d", c.size, len(body))
	}
	if !fake.enrichCalled {
		t.Error("[FAIL] EnrichFilesWithURLs was not called")
	}
}

func TestRegisterGeneratedImageTaskFile_FallsBackToOSSURL(t *testing.T) {
	// When Enrich leaves URL empty (OSSKey == "" branch), the helper must fall
	// back to OSSURL so the caller still gets a fetchable download_url.
	tmp := filepath.Join(t.TempDir(), "image_01.png")
	if err := os.WriteFile(tmp, []byte("x"), 0o644); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	fake := &fakeTaskFileRegistrar{
		uploadResult: &model.TaskFile{OSSURL: "https://oss.example.com/u/t/output/image_01.png"},
	}
	res := &service.ImageResult{FilePath: tmp, OutputMIME: "image/png"}

	url, err := registerGeneratedImageTaskFile(context.Background(), fake, "task-1", "user-1", res)
	if err != nil {
		t.Fatalf("[FAIL] unexpected error: %v", err)
	}
	if url != "https://oss.example.com/u/t/output/image_01.png" {
		t.Errorf("[FAIL] url = %q, want OSSURL fallback when URL unset", url)
	}
}

func TestRegisterGeneratedImageTaskFile_StatErrorSkipsUpload(t *testing.T) {
	fake := &fakeTaskFileRegistrar{}
	res := &service.ImageResult{FilePath: "/does/not/exist/cover.png", OutputMIME: "image/png"}

	if _, err := registerGeneratedImageTaskFile(context.Background(), fake, "task-1", "user-1", res); err == nil {
		t.Fatal("[FAIL] expected error for missing file, got nil")
	}
	if len(fake.uploadCalls) != 0 {
		t.Errorf("[FAIL] Upload must not be called when stat fails, got %d calls", len(fake.uploadCalls))
	}
}

func TestRegisterGeneratedImageTaskFile_UploadErrorPropagates(t *testing.T) {
	// The handler treats this as a soft failure (logs + keeps generation); the
	// helper itself must surface the error so the handler can decide.
	tmp := filepath.Join(t.TempDir(), "cover.png")
	if err := os.WriteFile(tmp, []byte("x"), 0o644); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	fake := &fakeTaskFileRegistrar{uploadErr: fmt.Errorf("storage down")}
	res := &service.ImageResult{FilePath: tmp, OutputMIME: "image/png"}

	_, err := registerGeneratedImageTaskFile(context.Background(), fake, "task-1", "user-1", res)
	if err == nil {
		t.Fatal("[FAIL] expected error when Upload fails, got nil")
	}
	if !strings.Contains(err.Error(), "storage down") {
		t.Errorf("[FAIL] error should wrap the upload error, got %q", err.Error())
	}
}

func TestRunImageVerificationAssociatesUnderstandingChargeWithTask(t *testing.T) {
	oldSvcs := svcs
	oldBillSvc := billSvc
	t.Cleanup(func() {
		svcs = oldSvcs
		billSvc = oldBillSvc
	})

	db := repositoryTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := "image-understanding-task-user"
	projectID := "image-understanding-task-project"
	taskID := "image-understanding-task"
	if err := repo.Users().Create(ctx, &model.User{
		ID:             userID,
		Email:          userID + "@example.com",
		Password:       "hashed",
		InviteCode:     "imgtask",
		Tier:           model.TierFree,
		CreditsBalance: 10_000,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformArticle,
		Name:     "Image Task Project",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID:        taskID,
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusRunning,
		Prompt:    "generate image",
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}

	cfg := &srvconfig.Config{
		ImageUnderstanding: srvconfig.UnderstandingRuntimeConfig{
			ProviderKey: "moonshot",
			Model:       "kimi-k2.7-code-highspeed",
		},
		Billing: srvconfig.BillingConfig{
			CreditsPerCNY: 1000,
			TierMultipliers: map[string]float64{
				"free": 1,
			},
			MinimumChargeCredits: 1,
		},
		ModelPrices: srvconfig.ModelPricesConfig{
			TokenModels: map[string]srvconfig.TokenModelPrice{
				"moonshot/kimi-k2.7-code-highspeed": {
					Currency: "CNY",
					Unit:     1_000,
					Input:    srvconfig.FlexibleFloat(1),
					Output:   srvconfig.FlexibleFloat(1),
				},
			},
		},
		Credits: srvconfig.CreditsConfig{},
	}
	logger := zerolog.New(io.Discard)
	creditSvc := service.NewCreditService(repo, &cfg.Credits, &logger)
	writingSvc := service.NewWritingService(repo, nil, "", 0, &logger)
	writingSvc.SetImageUnderstandingClient(&fakeMCPWritingLLM{
		response: `{"overall_pass":true}`,
		usage: srvconfig.TokenUsage{
			InputTokens:  100,
			OutputTokens: 100,
			TotalTokens:  200,
		},
	})
	taskSvc := service.NewTaskService(repo, nil, nil, nil, nil, &logger, "", nil, "", nil, nil)
	svcs = &Services{WritingSvc: writingSvc, TaskSvc: taskSvc}
	billSvc = &billingServices{creditSvc: creditSvc, config: cfg}

	imagePath := filepath.Join(t.TempDir(), "verified.png")
	if err := os.WriteFile(imagePath, []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR"), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}
	_, err := runImageVerification(ctx, userID, taskID, &service.ImageResult{FilePath: imagePath}, "verify")
	if err != nil {
		t.Fatalf("runImageVerification: %v", err)
	}

	txs, _, err := creditSvc.ListTransactions(ctx, userID, 0, 10)
	if err != nil {
		t.Fatalf("list transactions: %v", err)
	}
	if len(txs) != 1 {
		t.Fatalf("transactions len = %d, want 1", len(txs))
	}
	if txs[0].Type != model.CreditTypeImageUnderstanding {
		t.Fatalf("transaction type = %s, want image_understanding", txs[0].Type)
	}
	if txs[0].TaskID == nil || *txs[0].TaskID != taskID {
		t.Fatalf("transaction task_id = %v, want %s", txs[0].TaskID, taskID)
	}
}

func TestAnalyzeImagePreflightsForeignTaskBeforeCallingVision(t *testing.T) {
	oldSvcs := svcs
	oldBillSvc := billSvc
	t.Cleanup(func() {
		svcs = oldSvcs
		billSvc = oldBillSvc
	})

	db := repositoryTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	logger := zerolog.New(io.Discard)

	userID := "user-analyze-preflight"
	projectID := "project-analyze-preflight"
	otherUserID := userID + "-other"
	foreignProjectID := projectID + "-other"
	foreignTaskID := "task-analyze-preflight-other"
	for _, user := range []*model.User{
		{
			ID:             userID,
			Email:          userID + "@example.com",
			Password:       "hashed",
			InviteCode:     "invite-" + userID,
			Tier:           model.TierFree,
			CreditsBalance: 10_000,
		},
		{
			ID:             otherUserID,
			Email:          otherUserID + "@example.com",
			Password:       "hashed",
			InviteCode:     "invite-" + otherUserID,
			Tier:           model.TierFree,
			CreditsBalance: 10_000,
		},
	} {
		if err := repo.Users().Create(ctx, user); err != nil {
			t.Fatalf("create user %s: %v", user.ID, err)
		}
	}
	for _, project := range []*model.Project{
		{
			ID:       projectID,
			UserID:   userID,
			Platform: model.PlatformArticle,
			Name:     "Analyze Project",
			Status:   model.ProjectStatusActive,
		},
		{
			ID:       foreignProjectID,
			UserID:   otherUserID,
			Platform: model.PlatformArticle,
			Name:     "Foreign Project",
			Status:   model.ProjectStatusActive,
		},
	} {
		if err := repo.Projects().Create(ctx, project); err != nil {
			t.Fatalf("create project %s: %v", project.ID, err)
		}
	}
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID:        foreignTaskID,
		UserID:    otherUserID,
		ProjectID: foreignProjectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusRunning,
		Prompt:    "foreign task",
	}); err != nil {
		t.Fatalf("create foreign task: %v", err)
	}

	cfg := &srvconfig.Config{
		ImageUnderstanding: srvconfig.UnderstandingRuntimeConfig{
			ProviderKey: "moonshot",
			Model:       "kimi-k2.7-code-highspeed",
		},
		Billing: srvconfig.BillingConfig{
			CreditsPerCNY:         1000,
			TierMultipliers:       map[string]float64{"free": 1},
			DefaultUserMultiplier: 1,
			MinimumChargeCredits:  1,
		},
		ModelPrices: srvconfig.ModelPricesConfig{
			TokenModels: map[string]srvconfig.TokenModelPrice{
				"moonshot/kimi-k2.7-code-highspeed": {
					Currency: "CNY",
					Unit:     1_000,
					Input:    srvconfig.FlexibleFloat(1),
					Output:   srvconfig.FlexibleFloat(1),
				},
			},
		},
	}
	creditSvc := service.NewCreditService(repo, &cfg.Credits, &logger)
	visionClient := &fakeMCPWritingLLM{
		response: `{"overall_pass":true}`,
		usage: srvconfig.TokenUsage{
			InputTokens:  100,
			OutputTokens: 100,
			TotalTokens:  200,
		},
	}
	writingSvc := service.NewWritingService(repo, nil, "", 0, &logger)
	writingSvc.SetImageUnderstandingClient(visionClient)
	svcs = &Services{
		WritingSvc: writingSvc,
		TaskSvc:    service.NewTaskService(repo, nil, nil, nil, nil, &logger, "", nil, "", nil, nil),
	}
	billSvc = &billingServices{creditSvc: creditSvc, config: cfg}

	imagePath := filepath.Join(t.TempDir(), "analyze.png")
	if err := os.WriteFile(imagePath, []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR"), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}

	res, err := analyzeImageHandler(withMCPUserID(ctx, userID), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(fmt.Sprintf(`{
			"project_id": %q,
			"task_id": %q,
			"file_path": %q,
			"prompt": "verify"
		}`, projectID, foreignTaskID, imagePath))},
	})
	if err != nil {
		t.Fatalf("analyzeImageHandler returned error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected tool error for foreign task, got %#v", res)
	}
	if !strings.Contains(callToolText(res), "task does not belong to user") {
		t.Fatalf("response = %q, want foreign task ownership error", callToolText(res))
	}
	if visionClient.calls != 0 {
		t.Fatalf("vision calls = %d, want 0 before task ownership passes", visionClient.calls)
	}
}
