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

	appconfig "github.com/royalrick/anbanwriter/app/config"
	srvconfig "github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/service"
)

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

	provider, mdl := resolveImageBillingModel(context.Background(), "user-1", "openai-gpt-image")

	if provider != "openai" || mdl != "gpt-image-2" {
		t.Fatalf("billing model = %s/%s, want openai/gpt-image-2", provider, mdl)
	}
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
