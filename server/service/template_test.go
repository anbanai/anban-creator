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

func TestTemplateService_Create_WritesVisualOnly(t *testing.T) {
	svc, _, _ := setupTemplateService(t)
	ctx := context.Background()
	ownerID := uuid.New().String()

	tmpl := &model.Template{
		Name:           "视觉模板",
		Type:           "article",
		VisualStyle:    "暖色生活摄影",
		Writer:         "犀利、接地气",
		Theme:          "autumn-warm",
		Author:         "老李",
		Structure:      map[string]any{"text": "钩子 → 论点 → 行动"},
		ExampleContent: map[string]any{"text": "示例正文"},
		Category:       "个人成长",
		Tags:           []string{"干货", "方法论"},
	}
	tmpl.SetEcommerce(model.EcommerceTemplateDefaults{
		DefaultSelectedModules: map[string]int{"product_poster": 1},
		TargetPlatform:         "tmall",
		BrandBrief:             "品牌说明",
		ImageModelKey:          "openai-gpt-image",
	})

	created, err := svc.Create(ctx, tmpl, ownerID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.VisualStyle != "暖色生活摄影" {
		t.Errorf("VisualStyle = %q, want 暖色生活摄影", created.VisualStyle)
	}
	if created.Category != "个人成长" {
		t.Errorf("Category = %q, want 个人成长", created.Category)
	}
	if len(created.Tags) != 2 || created.Tags[0] != "干货" || created.Tags[1] != "方法论" {
		t.Errorf("Tags = %v, want [干货 方法论]", created.Tags)
	}
	if created.Writer != "" || created.Theme != "" || created.Author != "" {
		t.Fatalf("Writer/Theme/Author = %q/%q/%q, want empty visual-only fields", created.Writer, created.Theme, created.Author)
	}
	if created.Structure != nil || created.ExampleContent != nil {
		t.Fatalf("Structure/ExampleContent = %v/%v, want nil visual-only fields", created.Structure, created.ExampleContent)
	}
	if ec := created.Ecommerce.Data(); len(ec.DefaultSelectedModules) > 0 || ec.TargetPlatform != "" || ec.BrandBrief != "" || ec.ImageModelKey != "" {
		t.Fatalf("Ecommerce = %+v, want empty visual-only payload", ec)
	}

	refetched, err := svc.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if refetched.Writer != "" || refetched.Theme != "" || refetched.Author != "" || refetched.Structure != nil || refetched.ExampleContent != nil {
		t.Fatalf("persisted legacy fields = writer:%q theme:%q author:%q structure:%v example:%v, want empty",
			refetched.Writer, refetched.Theme, refetched.Author, refetched.Structure, refetched.ExampleContent)
	}
}

func TestTemplateService_Update_IgnoresLegacyBusinessFields(t *testing.T) {
	svc, repo, _ := setupTemplateService(t)
	ctx := context.Background()
	ownerID := uuid.New().String()

	legacy := &model.Template{
		ID:             uuid.NewString(),
		UserID:         ownerID,
		Name:           "旧模板",
		Type:           "article",
		VisualStyle:    "旧视觉",
		Writer:         "legacy-writer",
		Theme:          "legacy-theme",
		Author:         "legacy-author",
		Structure:      map[string]any{"text": "legacy-structure"},
		ExampleContent: map[string]any{"text": "legacy-example"},
		Category:       "旧分类",
		Tags:           []string{"旧标签"},
	}
	legacy.SetEcommerce(model.EcommerceTemplateDefaults{TargetPlatform: "legacy-platform"})
	if err := repo.Templates().Create(ctx, legacy); err != nil {
		t.Fatalf("create legacy template: %v", err)
	}

	updated, err := svc.Update(ctx, legacy.ID, ownerID, &model.Template{
		Name:           "新模板",
		VisualStyle:    "新视觉",
		Writer:         "new-writer",
		Theme:          "new-theme",
		Author:         "new-author",
		Structure:      map[string]any{"text": "new-structure"},
		ExampleContent: map[string]any{"text": "new-example"},
		Category:       "新分类",
		Tags:           []string{"新标签"},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "新模板" || updated.VisualStyle != "新视觉" || updated.Category != "新分类" {
		t.Fatalf("updated visual metadata = name:%q visual:%q category:%q", updated.Name, updated.VisualStyle, updated.Category)
	}
	if len(updated.Tags) != 1 || updated.Tags[0] != "新标签" {
		t.Fatalf("Tags = %v, want [新标签]", updated.Tags)
	}
	if updated.Writer != "legacy-writer" || updated.Theme != "legacy-theme" || updated.Author != "legacy-author" {
		t.Fatalf("legacy writer/theme/author = %q/%q/%q, want preserved legacy values", updated.Writer, updated.Theme, updated.Author)
	}
	if got := updated.Structure["text"]; got != "legacy-structure" {
		t.Fatalf("Structure = %v, want preserved legacy-structure", updated.Structure)
	}
	if got := updated.ExampleContent["text"]; got != "legacy-example" {
		t.Fatalf("ExampleContent = %v, want preserved legacy-example", updated.ExampleContent)
	}
}

func TestTemplateService_Update_LegacyRuntimePatchDoesNotClearVisualStyle(t *testing.T) {
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
	if updated.Theme != "" {
		t.Fatalf("Theme = %q, want ignored legacy runtime field", updated.Theme)
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
