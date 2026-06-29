package service

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
)

func setupTemplateTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	// Use a unique table suffix per test to avoid cache=shared pollution.
	if err := db.AutoMigrate(&model.Template{}); err != nil {
		t.Fatalf("failed to migrate templates: %v", err)
	}
	// Clean slate.
	db.Exec("DELETE FROM templates")
	return db
}

func setupTemplateService(t *testing.T) (*TemplateService, repository.Repository, *gorm.DB) {
	t.Helper()
	db := setupTemplateTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.Nop()
	svc := NewTemplateService(repo, &logger)
	return svc, repo, db
}

func TestTemplateService_Create_AssignsDefaults(t *testing.T) {
	svc, _, _ := setupTemplateService(t)
	ctx := context.Background()

	tmpl := &model.Template{
		Name:         "我的种草模板",
		Type:         "seednote",
		ThumbnailURL: "https://example.com/x.png",
		VisualStyle:  "暖色调，柔和光线",
	}
	userID := uuid.New().String()

	created, err := svc.Create(ctx, tmpl, userID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == "" {
		t.Errorf("expected ID populated, got empty")
	}
	if created.UserID != userID {
		t.Errorf("UserID = %q, want %q", created.UserID, userID)
	}
	if created.Visibility != "public" {
		t.Errorf("default Visibility = %q, want public", created.Visibility)
	}
	if !created.IsActive {
		t.Errorf("expected IsActive = true by default")
	}
}

func TestTemplateService_Create_DerivesNameFromStyle(t *testing.T) {
	svc, _, _ := setupTemplateService(t)
	ctx := context.Background()

	// name 为空但 style_prompt 非空 → 从 style 截取前 20 字
	created, err := svc.Create(ctx, &model.Template{
		Name:        "",
		Type:        "seednote",
		VisualStyle: "治愈系水彩风格，柔和色调，手绘质感",
	}, uuid.New().String())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Name != "治愈系水彩风格" {
		t.Errorf("Name = %q, want 治愈系水彩风格", created.Name)
	}
}

func TestTemplateService_Create_RejectsEmptyNameAndStyle(t *testing.T) {
	svc, _, _ := setupTemplateService(t)
	_, err := svc.Create(context.Background(), &model.Template{Name: "", Type: "seednote"}, uuid.New().String())
	if err == nil {
		t.Fatalf("expected error when both name and style_prompt are empty, got nil")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("expected name-related error, got %v", err)
	}
}

func TestTemplateService_Create_InitializesEmptyTags(t *testing.T) {
	svc, _, _ := setupTemplateService(t)
	ctx := context.Background()

	// Tags 未设置（nil）→ 返回时必须是非 nil 空 slice，避免前端 tags.length 崩
	created, err := svc.Create(ctx, &model.Template{
		Name: "无标签模板",
		Type: "poster",
	}, uuid.New().String())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Tags == nil {
		t.Fatalf("Tags = nil, want non-nil empty slice")
	}
	if len(created.Tags) != 0 {
		t.Errorf("len(Tags) = %d, want 0", len(created.Tags))
	}
}

func TestTemplateService_List_ReturnsNonNilTags(t *testing.T) {
	svc, _, _ := setupTemplateService(t)
	ctx := context.Background()
	userID := uuid.New().String()

	if _, err := svc.Create(ctx, &model.Template{
		Name:       "test",
		Type:       "seednote",
		Visibility: "public",
	}, userID); err != nil {
		t.Fatalf("Create: %v", err)
	}

	list, _, err := svc.List(ctx, "", "", "", userID, "all", 0, 100)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}
	if list[0].Tags == nil {
		t.Errorf("List[0].Tags = nil, want non-nil empty slice (otherwise frontend tags.length crashes)")
	}
}

func TestDeriveNameFromStyle(t *testing.T) {
	cases := []struct {
		name  string
		style string
		want  string
	}{
		{"empty", "", ""},
		{"whitespace only", "   \n\t ", ""},
		{"short", "治愈系水彩风格", "治愈系水彩风格"},
		{"truncate at comma (Chinese)", "治愈系水彩风格，柔和色调", "治愈系水彩风格"},
		{"truncate at period (English)", "Soft watercolor style. Pastel tones.", "Soft watercolor styl"},
		{"truncate at newline", "第一行\n第二行", "第一行"},
		{"strip dimension prefix", "整体氛围：治愈系水彩\n色彩色调：暖色调", "治愈系水彩"},
		{"strip ascii colon prefix", "Style: soft watercolor, pastel tones", "soft watercolor"},
		{"cap at 20 runes", "一二三四五六七八九十一二三四五六七八九十一二三四五六七八九十", "一二三四五六七八九十一二三四五六七八九十"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveNameFromStyle(tc.style)
			if got != tc.want {
				t.Errorf("deriveNameFromStyle(%q) = %q, want %q", tc.style, got, tc.want)
			}
		})
	}
}

func TestTemplateService_Update_OwnerOnly(t *testing.T) {
	svc, _, _ := setupTemplateService(t)
	ctx := context.Background()
	ownerID := uuid.New().String()
	otherID := uuid.New().String()

	created, err := svc.Create(ctx, &model.Template{Name: "原始", Type: "article"}, ownerID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Non-owner update fails.
	if _, err := svc.Update(ctx, created.ID, otherID, &model.Template{Name: "篡改"}); err == nil {
		t.Fatalf("expected error for non-owner update")
	}

	// Owner update succeeds.
	updated, err := svc.Update(ctx, created.ID, ownerID, &model.Template{Name: "新名字", Visibility: "private"})
	if err != nil {
		t.Fatalf("Update owner: %v", err)
	}
	if updated.Name != "新名字" {
		t.Errorf("Name = %q, want 新名字", updated.Name)
	}
	if updated.Visibility != "private" {
		t.Errorf("Visibility = %q, want private", updated.Visibility)
	}
}

// TestTemplateService_Update_PersistsScaffold: the content scaffold fields
// (Writer / Structure / ExampleContent / Category / Tags) set via Update
// must round-trip through the repository. The repository uses db.Save (writes
// ALL columns), so the service MUST copy every patch field onto `existing` —
// forgetting one silently persists the OLD value. This test guards that footgun.
func TestTemplateService_Update_PersistsScaffold(t *testing.T) {
	svc, _, _ := setupTemplateService(t)
	ctx := context.Background()
	ownerID := uuid.New().String()

	created, err := svc.Create(ctx, &model.Template{Name: "脚手架模板", Type: "article"}, ownerID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	patch := &model.Template{
		Writer:         "犀利、接地气",
		Structure:      map[string]any{"text": "钩子 → 论点 → 行动"},
		ExampleContent: map[string]any{"text": "示例正文"},
		Category:       "个人成长",
		Tags:           []string{"干货", "方法论"},
	}
	updated, err := svc.Update(ctx, created.ID, ownerID, patch)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Writer != "犀利、接地气" {
		t.Errorf("Writer = %q, want 犀利、接地气", updated.Writer)
	}
	if got, ok := updated.Structure["text"].(string); !ok || got != "钩子 → 论点 → 行动" {
		t.Errorf("Structure = %v, want {text: 钩子 → 论点 → 行动}", updated.Structure)
	}
	if got, ok := updated.ExampleContent["text"].(string); !ok || got != "示例正文" {
		t.Errorf("ExampleContent = %v, want {text: 示例正文}", updated.ExampleContent)
	}
	if updated.Category != "个人成长" {
		t.Errorf("Category = %q, want 个人成长", updated.Category)
	}
	if len(updated.Tags) != 2 || updated.Tags[0] != "干货" || updated.Tags[1] != "方法论" {
		t.Errorf("Tags = %v, want [干货 方法论]", updated.Tags)
	}

	// Re-fetch from the repository to confirm persistence (not just in-memory).
	refetched, err := svc.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if refetched.Writer != "犀利、接地气" {
		t.Errorf("persisted Writer = %q, want 犀利、接地气", refetched.Writer)
	}
	if got, ok := refetched.Structure["text"].(string); !ok || got != "钩子 → 论点 → 行动" {
		t.Errorf("persisted Structure = %v", refetched.Structure)
	}
}

// TestTemplateService_Update_PersistsAuthorFields: the runtime author/writer
// fields set via Update must round-trip through the repository.
func TestTemplateService_Update_PersistsAuthorFields(t *testing.T) {
	svc, _, _ := setupTemplateService(t)
	ctx := context.Background()
	ownerID := uuid.New().String()

	created, err := svc.Create(ctx, &model.Template{Name: "作者模板", Type: "article"}, ownerID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	patch := &model.Template{
		Author: "老李",
		Writer: "dan-koe",
	}
	updated, err := svc.Update(ctx, created.ID, ownerID, patch)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Author != "老李" {
		t.Errorf("Author = %q, want 老李", updated.Author)
	}
	if updated.Writer != "dan-koe" {
		t.Errorf("Writer = %q, want dan-koe", updated.Writer)
	}

	// Re-fetch to confirm persistence (not just in-memory).
	refetched, err := svc.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if refetched.Author != "老李" {
		t.Errorf("persisted Author = %q, want 老李", refetched.Author)
	}
	if refetched.Writer != "dan-koe" {
		t.Errorf("persisted Writer = %q", refetched.Writer)
	}
}

func TestTemplateService_Update_EmptyVisualStylePatchDoesNotClearExisting(t *testing.T) {
	svc, _, _ := setupTemplateService(t)
	ctx := context.Background()
	ownerID := uuid.New().String()

	created, err := svc.Create(ctx, &model.Template{
		Name:        "视觉模板",
		Type:        "article",
		VisualStyle: "暖色生活摄影",
	}, ownerID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := svc.Update(ctx, created.ID, ownerID, &model.Template{Theme: "autumn-warm"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.VisualStyle != "暖色生活摄影" {
		t.Fatalf("VisualStyle = %q, want existing style preserved", updated.VisualStyle)
	}
}

func TestTemplateService_UpdatePatch_AllowsClearingRuntimeFields(t *testing.T) {
	svc, _, _ := setupTemplateService(t)
	ctx := context.Background()
	ownerID := uuid.New().String()

	created, err := svc.Create(ctx, &model.Template{
		Name:        "可清空模板",
		Type:        "article",
		VisualStyle: "暖色生活摄影",
		Writer:      "dan-koe",
		Author:      "老李",
		Theme:       "autumn-warm",
	}, ownerID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	empty := ""
	updated, err := svc.UpdatePatch(ctx, created.ID, ownerID, TemplatePatch{
		Writer: &empty,
		Author: &empty,
		Theme:  &empty,
	})
	if err != nil {
		t.Fatalf("UpdatePatch: %v", err)
	}
	if updated.Writer != "" || updated.Author != "" || updated.Theme != "" {
		t.Fatalf("Writer/Author/Theme = %q/%q/%q, want all cleared", updated.Writer, updated.Author, updated.Theme)
	}
	if updated.VisualStyle != "暖色生活摄影" {
		t.Fatalf("VisualStyle = %q, want omitted style preserved", updated.VisualStyle)
	}
}

// TestTemplateService_Update_PersistsTheme: the 公众号 排版样式 (Theme) set via
// Update must round-trip through the repository. Same footgun guard as the
// scaffold/author tests: the repo writes ALL columns, so the service
// must copy patch.Theme onto existing — forgetting it silently keeps the OLD
// theme, which is exactly the "编辑后保存无效" bug.
func TestTemplateService_Update_PersistsTheme(t *testing.T) {
	svc, _, _ := setupTemplateService(t)
	ctx := context.Background()
	ownerID := uuid.New().String()

	created, err := svc.Create(ctx, &model.Template{Name: "排版模板", Type: "article"}, ownerID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	patch := &model.Template{
		Theme: "autumn-warm",
	}
	updated, err := svc.Update(ctx, created.ID, ownerID, patch)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Theme != "autumn-warm" {
		t.Errorf("Theme = %q, want autumn-warm", updated.Theme)
	}

	// Re-fetch to confirm persistence (not just in-memory).
	refetched, err := svc.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if refetched.Theme != "autumn-warm" {
		t.Errorf("persisted Theme = %q, want autumn-warm", refetched.Theme)
	}
}

func TestTemplateService_Delete_OwnerOnly(t *testing.T) {
	svc, _, _ := setupTemplateService(t)
	ctx := context.Background()
	ownerID := uuid.New().String()
	otherID := uuid.New().String()

	created, err := svc.Create(ctx, &model.Template{Name: "待删", Type: "poster"}, ownerID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Non-owner delete fails.
	if err := svc.Delete(ctx, created.ID, otherID); err == nil {
		t.Fatalf("expected error for non-owner delete")
	}

	// Owner delete succeeds.
	if err := svc.Delete(ctx, created.ID, ownerID); err != nil {
		t.Fatalf("Delete owner: %v", err)
	}
	if _, err := svc.GetByID(ctx, created.ID); err == nil {
		t.Fatalf("expected error fetching deleted template")
	}
}

func TestTemplateService_List_ScopeVisibility(t *testing.T) {
	svc, _, _ := setupTemplateService(t)
	ctx := context.Background()
	alice := uuid.New().String()
	bob := uuid.New().String()

	// Alice owns a public + a private template.
	pub, err := svc.Create(ctx, &model.Template{Name: "alice-pub", Type: "seednote", Visibility: "public"}, alice)
	if err != nil {
		t.Fatalf("Create pub: %v", err)
	}
	priv, err := svc.Create(ctx, &model.Template{Name: "alice-priv", Type: "seednote", Visibility: "private"}, alice)
	if err != nil {
		t.Fatalf("Create priv: %v", err)
	}
	// Bob owns a private template.
	bobPriv, err := svc.Create(ctx, &model.Template{Name: "bob-priv", Type: "seednote", Visibility: "private"}, bob)
	if err != nil {
		t.Fatalf("Create bobPriv: %v", err)
	}
	_ = pub
	_ = priv

	// scope=mine: alice sees only her own (both private + public).
	mine, _, err := svc.List(ctx, "", "", "", alice, "mine", 0, 100)
	if err != nil {
		t.Fatalf("List mine: %v", err)
	}
	if len(mine) != 2 {
		t.Errorf("alice mine: got %d templates, want 2", len(mine))
	}

	// scope=public: alice sees only public (her own public; bob's private excluded).
	pubOnly, _, err := svc.List(ctx, "", "", "", alice, "public", 0, 100)
	if err != nil {
		t.Fatalf("List public: %v", err)
	}
	if len(pubOnly) != 1 {
		t.Errorf("alice public: got %d templates, want 1", len(pubOnly))
	}
	if pubOnly[0].Visibility != "public" {
		t.Errorf("expected only public template, got %q", pubOnly[0].Visibility)
	}

	// scope=all: alice sees her 2 + any other public (no other public here, so 2).
	all, _, err := svc.List(ctx, "", "", "", alice, "all", 0, 100)
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("alice all: got %d templates, want 2", len(all))
	}

	// bob can see only his own private in scope=mine, but in scope=all he should
	// see his private + alice's public (2), NOT alice's private.
	bobAll, _, err := svc.List(ctx, "", "", "", bob, "all", 0, 100)
	if err != nil {
		t.Fatalf("bob List all: %v", err)
	}
	if len(bobAll) != 2 {
		t.Errorf("bob all: got %d templates, want 2 (his private + alice public)", len(bobAll))
	}
	for _, tmpl := range bobAll {
		if tmpl.UserID != bob && tmpl.Visibility != "public" {
			t.Errorf("bob should not see other users' private templates; got %q owned by %q", tmpl.Name, tmpl.UserID)
		}
	}

	// Unauthenticated (empty userID) only sees public regardless of scope.
	anon, _, err := svc.List(ctx, "", "", "", "", "all", 0, 100)
	if err != nil {
		t.Fatalf("anon List: %v", err)
	}
	if len(anon) != 1 {
		t.Errorf("anon all: got %d, want 1 (only the one public)", len(anon))
	}

	_ = bobPriv
}
