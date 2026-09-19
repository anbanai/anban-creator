package service

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func setupTemplateTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	// Use a unique table suffix per test to avoid cache=shared pollution.
	if err := db.AutoMigrate(&model.Template{}, &model.User{}); err != nil {
		t.Fatalf("failed to migrate templates: %v", err)
	}
	// Clean slate.
	db.Exec("DELETE FROM templates")
	return db
}

func createTemplateTestUser(t *testing.T, repo repository.Repository, id string, isAdmin bool) {
	t.Helper()
	if err := repo.Users().Create(t.Context(), &model.User{
		ID:         id,
		Email:      id + "@example.com",
		Password:   "test",
		InviteCode: strings.ToUpper(strings.ReplaceAll(id, "-", ""))[:8],
		IsAdmin:    isAdmin,
	}); err != nil {
		t.Fatalf("create test user %s: %v", id, err)
	}
}

func TestTemplateServiceCreateRequiresDatabaseAdminAndSeednoteCategory(t *testing.T) {
	svc, repo, _ := setupTemplateService(t)
	ctx := context.Background()
	createTemplateTestUser(t, repo, "regular-user", false)
	createTemplateTestUser(t, repo, "admin-user", true)

	valid := func() *model.Template {
		return &model.Template{
			Name:     "晨光留白",
			Type:     model.TemplateTypeSeednote,
			Category: model.SeednoteTemplateCategoryProduct,
			Prompt:   "暖色晨光，封面标题居中",
		}
	}

	if _, err := svc.Create(ctx, valid(), "regular-user"); !errors.Is(err, ErrTemplateForbidden) {
		t.Fatalf("regular Create error = %v, want ErrTemplateForbidden", err)
	}

	invalidType := valid()
	invalidType.Type = model.TemplateTypeArticle
	if _, err := svc.Create(ctx, invalidType, "admin-user"); !errors.Is(err, ErrTemplateTypeInvalid) {
		t.Fatalf("article Create error = %v, want ErrTemplateTypeInvalid", err)
	}

	invalidCategory := valid()
	invalidCategory.Category = "其他"
	if _, err := svc.Create(ctx, invalidCategory, "admin-user"); !errors.Is(err, ErrTemplateCategoryInvalid) {
		t.Fatalf("invalid category Create error = %v, want ErrTemplateCategoryInvalid", err)
	}

	created, err := svc.Create(ctx, valid(), "admin-user")
	if err != nil {
		t.Fatalf("admin Create valid template: %v", err)
	}
	if created.UserID != "admin-user" || created.Type != model.TemplateTypeSeednote {
		t.Fatalf("created template = %+v", created)
	}
}

func TestTemplateServiceRejectsBlankPromptOnCreateAndUpdate(t *testing.T) {
	svc, repo, _ := setupTemplateService(t)
	ctx := context.Background()
	createTemplateTestUser(t, repo, "admin-prompt-user", true)

	if _, err := svc.Create(ctx, &model.Template{
		Name: "无内容模板", Type: model.TemplateTypeSeednote,
		Category: model.SeednoteTemplateCategoryProduct, Prompt: " \n\t ",
	}, "admin-prompt-user"); err == nil {
		t.Fatal("Create blank prompt error = nil, want validation error")
	}

	existing := &model.Template{
		ID: "prompt-update-template", UserID: "admin-prompt-user", Name: "现有模板",
		Type: model.TemplateTypeSeednote, Category: model.SeednoteTemplateCategoryProduct,
		Prompt: "原有视觉 Prompt", Visibility: "public", IsActive: true,
	}
	if err := repo.Templates().Create(ctx, existing); err != nil {
		t.Fatalf("create existing template: %v", err)
	}
	blankPrompt := "  "
	if _, err := svc.UpdatePatch(ctx, existing.ID, "admin-prompt-user", TemplatePatch{Prompt: &blankPrompt}); err == nil {
		t.Fatal("UpdatePatch blank prompt error = nil, want validation error")
	}
	persisted, err := repo.Templates().FindByID(ctx, existing.ID)
	if err != nil {
		t.Fatalf("find existing template: %v", err)
	}
	if persisted.Prompt != "原有视觉 Prompt" {
		t.Fatalf("prompt = %q, want unchanged original prompt", persisted.Prompt)
	}
}

func TestTemplateServiceDoesNotReadLegacyPromptAtRuntime(t *testing.T) {
	svc, _, db := setupTemplateService(t)
	ctx := context.Background()

	if err := db.Exec(`ALTER TABLE templates ADD COLUMN style_prompt TEXT`).Error; err != nil {
		t.Fatalf("add legacy prompt column: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO templates
			(id, type, name, category, visibility, is_active, prompt, style_prompt)
		VALUES
			('rolling-legacy-template', 'seednote', '滚动发布尾部写入', '好物种草', 'public', true, '', '旧 Pod 最后写入的视觉版式')
	`).Error; err != nil {
		t.Fatalf("insert rolling legacy template: %v", err)
	}

	templates, _, err := svc.List(ctx, model.TemplateTypeSeednote, "", "", "", "public", 0, 20)
	if err != nil {
		t.Fatalf("list templates: %v", err)
	}
	if len(templates) != 1 {
		t.Fatalf("templates = %d, want 1", len(templates))
	}
	if templates[0].Prompt != "" {
		t.Fatalf("prompt = %q, want canonical field only", templates[0].Prompt)
	}
}

func TestTemplateServiceNeverPersistsBlankName(t *testing.T) {
	svc, repo, _ := setupTemplateService(t)
	ctx := context.Background()
	createTemplateTestUser(t, repo, "admin-name-user", true)

	created, err := svc.Create(ctx, &model.Template{
		Name: "  ", Type: model.TemplateTypeSeednote,
		Category: model.SeednoteTemplateCategoryProduct, Prompt: "清爽留白，标题居中",
	}, "admin-name-user")
	if err != nil {
		t.Fatalf("Create whitespace name: %v", err)
	}
	if strings.TrimSpace(created.Name) == "" {
		t.Fatalf("created name = %q, want derived non-blank name", created.Name)
	}

	blankName := " \n "
	if _, err := svc.UpdatePatch(ctx, created.ID, "admin-name-user", TemplatePatch{Name: &blankName}); err == nil {
		t.Fatal("UpdatePatch blank name error = nil, want validation error")
	}
	persisted, err := repo.Templates().FindByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("find created template: %v", err)
	}
	if persisted.Name != created.Name {
		t.Fatalf("persisted name = %q, want unchanged %q", persisted.Name, created.Name)
	}
}

func TestTemplateServiceListScopesDependOnDatabaseAdmin(t *testing.T) {
	svc, repo, db := setupTemplateService(t)
	ctx := context.Background()
	createTemplateTestUser(t, repo, "regular-list-user", false)
	createTemplateTestUser(t, repo, "admin-list-user", true)

	rows := []*model.Template{
		{ID: "active-public", Name: "active public", Type: model.TemplateTypeSeednote, Category: model.SeednoteTemplateCategoryProduct, Visibility: "public", IsActive: true},
		{ID: "active-private", Name: "active private", Type: model.TemplateTypeSeednote, Category: model.SeednoteTemplateCategoryProduct, Visibility: "private", IsActive: true},
		{ID: "inactive-public", Name: "inactive public", Type: model.TemplateTypeSeednote, Category: model.SeednoteTemplateCategoryProduct, Visibility: "public", IsActive: false},
		{ID: "inactive-private", Name: "inactive private", Type: model.TemplateTypeSeednote, Category: model.SeednoteTemplateCategoryProduct, Visibility: "private", IsActive: false},
	}
	for _, row := range rows {
		if err := repo.Templates().Create(ctx, row); err != nil {
			t.Fatalf("create template %s: %v", row.ID, err)
		}
		if strings.HasPrefix(row.ID, "inactive-") {
			if err := db.Model(&model.Template{}).Where("id = ?", row.ID).UpdateColumn("is_active", false).Error; err != nil {
				t.Fatalf("deactivate template %s: %v", row.ID, err)
			}
		}
	}

	for _, scope := range []string{"all", "public", "private", "inactive"} {
		items, total, err := svc.List(ctx, "", "", "", "regular-list-user", scope, 0, 100)
		if err != nil {
			t.Fatalf("regular List scope=%s: %v", scope, err)
		}
		if total != 1 || len(items) != 1 || items[0].ID != "active-public" {
			t.Errorf("regular scope=%s items=%v total=%d, want active-public only", scope, templateIDs(items), total)
		}
	}

	tests := []struct {
		scope string
		want  []string
	}{
		{scope: "all", want: []string{"active-private", "active-public", "inactive-private", "inactive-public"}},
		{scope: "public", want: []string{"active-public"}},
		{scope: "private", want: []string{"active-private"}},
		{scope: "inactive", want: []string{"inactive-private", "inactive-public"}},
	}
	for _, tt := range tests {
		items, total, err := svc.List(ctx, "", "", "", "admin-list-user", tt.scope, 0, 100)
		if err != nil {
			t.Fatalf("admin List scope=%s: %v", tt.scope, err)
		}
		got := templateIDs(items)
		sort.Strings(got)
		if total != int64(len(tt.want)) || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("admin scope=%s items=%v total=%d, want %v", tt.scope, got, total, tt.want)
		}
	}
}

func templateIDs(templates []*model.Template) []string {
	ids := make([]string, 0, len(templates))
	for _, tmpl := range templates {
		ids = append(ids, tmpl.ID)
	}
	return ids
}

func TestTemplateServiceGetByIDRestrictsOrdinaryUsersAndAllowsDatabaseAdmin(t *testing.T) {
	svc, repo, db := setupTemplateService(t)
	ctx := context.Background()
	createTemplateTestUser(t, repo, "regular-get-user", false)
	createTemplateTestUser(t, repo, "admin-get-user", true)

	public := &model.Template{ID: "get-public", Name: "public", Type: model.TemplateTypeSeednote, Category: model.SeednoteTemplateCategoryProduct, Visibility: "public", IsActive: true}
	private := &model.Template{ID: "get-private", Name: "private", Type: model.TemplateTypeSeednote, Category: model.SeednoteTemplateCategoryProduct, Visibility: "private", IsActive: true}
	inactive := &model.Template{ID: "get-inactive", Name: "inactive", Type: model.TemplateTypeSeednote, Category: model.SeednoteTemplateCategoryProduct, Visibility: "public", IsActive: true}
	for _, row := range []*model.Template{public, private, inactive} {
		if err := repo.Templates().Create(ctx, row); err != nil {
			t.Fatalf("create %s: %v", row.ID, err)
		}
	}
	if err := db.Model(&model.Template{}).Where("id = ?", inactive.ID).UpdateColumn("is_active", false).Error; err != nil {
		t.Fatalf("deactivate template: %v", err)
	}

	if _, err := svc.GetByID(ctx, public.ID, "regular-get-user"); err != nil {
		t.Fatalf("regular public GetByID: %v", err)
	}
	for _, id := range []string{private.ID, inactive.ID} {
		if _, err := svc.GetByID(ctx, id, "regular-get-user"); !errors.Is(err, ErrTemplateNotFound) {
			t.Errorf("regular GetByID(%s) error = %v, want ErrTemplateNotFound", id, err)
		}
		if _, err := svc.GetByID(ctx, id, "admin-get-user"); err != nil {
			t.Errorf("admin GetByID(%s): %v", id, err)
		}
	}
}

func TestTemplateServiceReadsExcludeLegacyTemplateTypes(t *testing.T) {
	svc, repo, _ := setupTemplateService(t)
	ctx := context.Background()
	createTemplateTestUser(t, repo, "regular-seednote-read", false)
	createTemplateTestUser(t, repo, "admin-seednote-read", true)
	seednote := &model.Template{ID: "seednote-read", Name: "seednote", Type: model.TemplateTypeSeednote, Category: model.SeednoteTemplateCategoryProduct, Visibility: "public", IsActive: true}
	legacy := &model.Template{ID: "legacy-article-read", Name: "legacy", Type: model.TemplateTypeArticle, Visibility: "public", IsActive: true}
	invalidCategory := &model.Template{ID: "invalid-category-read", Name: "invalid", Type: model.TemplateTypeSeednote, Category: "生活方式", Visibility: "public", IsActive: true}
	for _, tmpl := range []*model.Template{seednote, legacy, invalidCategory} {
		if err := repo.Templates().Create(ctx, tmpl); err != nil {
			t.Fatalf("create %s: %v", tmpl.ID, err)
		}
	}

	for _, userID := range []string{"regular-seednote-read", "admin-seednote-read"} {
		items, total, err := svc.List(ctx, "", "", "", userID, "all", 0, 100)
		if err != nil {
			t.Fatalf("List user=%s: %v", userID, err)
		}
		if total != 1 || len(items) != 1 || items[0].ID != seednote.ID {
			t.Errorf("List user=%s items=%v total=%d, want seednote only", userID, templateIDs(items), total)
		}
		for _, id := range []string{legacy.ID, invalidCategory.ID} {
			if _, err := svc.GetByID(ctx, id, userID); !errors.Is(err, ErrTemplateNotFound) {
				t.Errorf("GetByID invalid template %s user=%s error=%v, want ErrTemplateNotFound", id, userID, err)
			}
		}
	}

	newPrompt := "不应写入"
	if _, err := svc.UpdatePatch(ctx, invalidCategory.ID, "admin-seednote-read", TemplatePatch{Prompt: &newPrompt}); !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("UpdatePatch invalid category error=%v, want ErrTemplateNotFound", err)
	}
}

func TestTemplateServiceGetRecommendedWithoutProfileReturnsOnlyActivePublicSeednote(t *testing.T) {
	svc, repo, _ := setupTemplateService(t)
	ctx := context.Background()
	templates := []*model.Template{
		{ID: "recommended-public-seednote", Name: "public", Type: model.TemplateTypeSeednote, Category: model.SeednoteTemplateCategoryProduct, Visibility: "public", IsActive: true, SortOrder: 10},
		{ID: "recommended-private-seednote", Name: "private", Type: model.TemplateTypeSeednote, Category: model.SeednoteTemplateCategoryProduct, Visibility: "private", IsActive: true, SortOrder: 30},
		{ID: "recommended-inactive-seednote", Name: "inactive", Type: model.TemplateTypeSeednote, Category: model.SeednoteTemplateCategoryProduct, Visibility: "public", IsActive: false, SortOrder: 20},
		{ID: "recommended-public-article", Name: "article", Type: model.TemplateTypeArticle, Visibility: "public", IsActive: true, SortOrder: 40},
	}
	for _, tmpl := range templates {
		if err := repo.Templates().Create(ctx, tmpl); err != nil {
			t.Fatalf("create %s: %v", tmpl.ID, err)
		}
		if tmpl.ID == "recommended-inactive-seednote" {
			tmpl.IsActive = false
			if err := repo.Templates().Update(ctx, tmpl); err != nil {
				t.Fatalf("deactivate %s: %v", tmpl.ID, err)
			}
		}
	}

	got, err := svc.GetRecommended(ctx, "", nil, 20)
	if err != nil {
		t.Fatalf("GetRecommended: %v", err)
	}
	if len(got) != 1 || got[0].ID != "recommended-public-seednote" {
		t.Fatalf("GetRecommended IDs = %v, want [recommended-public-seednote]", templateIDs(got))
	}
}

func TestTemplateServiceUpdateDeleteRequireDatabaseAdmin(t *testing.T) {
	svc, repo, _ := setupTemplateService(t)
	ctx := context.Background()
	createTemplateTestUser(t, repo, "regular-write-user", false)
	createTemplateTestUser(t, repo, "admin-write-user", true)
	tmpl := &model.Template{
		ID:         "admin-managed-template",
		UserID:     "someone-else",
		Name:       "旧模板",
		Type:       model.TemplateTypeSeednote,
		Category:   model.SeednoteTemplateCategoryProduct,
		Prompt:     "旧提示词",
		Visibility: "public",
		IsActive:   true,
	}
	if err := repo.Templates().Create(ctx, tmpl); err != nil {
		t.Fatalf("create template: %v", err)
	}

	newPrompt := "新视觉与版式提示词"
	if _, err := svc.UpdatePatch(ctx, tmpl.ID, "regular-write-user", TemplatePatch{Prompt: &newPrompt}); !errors.Is(err, ErrTemplateForbidden) {
		t.Fatalf("regular UpdatePatch error = %v, want ErrTemplateForbidden", err)
	}

	invalidCategory := "其他"
	if _, err := svc.UpdatePatch(ctx, tmpl.ID, "admin-write-user", TemplatePatch{Category: &invalidCategory}); !errors.Is(err, ErrTemplateCategoryInvalid) {
		t.Fatalf("invalid category UpdatePatch error = %v, want ErrTemplateCategoryInvalid", err)
	}

	newCategory := model.SeednoteTemplateCategoryBeauty
	private := "private"
	inactive := false
	updated, err := svc.UpdatePatch(ctx, tmpl.ID, "admin-write-user", TemplatePatch{
		Prompt:     &newPrompt,
		Category:   &newCategory,
		Visibility: &private,
		IsActive:   &inactive,
	})
	if err != nil {
		t.Fatalf("admin UpdatePatch: %v", err)
	}
	if updated.Prompt != newPrompt || updated.Category != newCategory || updated.Visibility != private || updated.IsActive {
		t.Fatalf("updated template = %+v", updated)
	}

	if err := svc.Delete(ctx, tmpl.ID, "regular-write-user"); !errors.Is(err, ErrTemplateForbidden) {
		t.Fatalf("regular Delete error = %v, want ErrTemplateForbidden", err)
	}
	if err := svc.Delete(ctx, tmpl.ID, "admin-write-user"); err != nil {
		t.Fatalf("admin Delete: %v", err)
	}
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
	baseRepo := repository.New(db)
	repo := &templateFixtureRepository{Repository: baseRepo, users: &templateFixtureUsers{UserRepository: baseRepo.Users()}}
	logger := zerolog.Nop()
	svc := NewTemplateService(repo, &logger)
	return svc, repo, db
}

type templateFixtureRepository struct {
	repository.Repository
	users repository.UserRepository
}

func (r *templateFixtureRepository) Users() repository.UserRepository { return r.users }

type templateFixtureUsers struct{ repository.UserRepository }

func (r *templateFixtureUsers) FindByID(ctx context.Context, id string) (*model.User, error) {
	user, err := r.UserRepository.FindByID(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &model.User{ID: id, IsAdmin: true}, nil
	}
	return user, err
}

func TestTemplateService_Create_AssignsDefaults(t *testing.T) {
	svc, _, _ := setupTemplateService(t)
	ctx := context.Background()

	tmpl := &model.Template{
		Name: "我的种草模板", Type: "seednote",
		Category: model.SeednoteTemplateCategoryProduct, Prompt: "暖色调，柔和光线",
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

	// name 为空但 prompt 非空 → 从 style 截取前 20 字
	created, err := svc.Create(ctx, &model.Template{
		Name:     "",
		Type:     "seednote",
		Category: model.SeednoteTemplateCategoryProduct,
		Prompt:   "治愈系水彩风格，柔和色调，手绘质感",
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
	_, err := svc.Create(context.Background(), &model.Template{Name: "", Type: "seednote", Category: model.SeednoteTemplateCategoryProduct}, uuid.New().String())
	if err == nil {
		t.Fatalf("expected error when both name and prompt are empty, got nil")
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
		Name: "无标签模板", Prompt: "清爽留白版式",
		Type: model.TemplateTypeSeednote, Category: model.SeednoteTemplateCategoryProduct,
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
		Category:   model.SeednoteTemplateCategoryProduct,
		Prompt:     "清爽留白版式",
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
			got := deriveNameFromPrompt(tc.style)
			if got != tc.want {
				t.Errorf("deriveNameFromPrompt(%q) = %q, want %q", tc.style, got, tc.want)
			}
		})
	}
}

func TestTemplateService_Create_WritesVisualOnly(t *testing.T) {
	svc, _, _ := setupTemplateService(t)
	ctx := context.Background()
	ownerID := uuid.New().String()

	tmpl := &model.Template{
		Name:           "视觉模板",
		Type:           model.TemplateTypeSeednote,
		Prompt:         "暖色生活摄影",
		Writer:         "犀利、接地气",
		Theme:          "autumn-warm",
		Author:         "老李",
		Structure:      map[string]any{"text": "钩子 → 论点 → 行动"},
		ExampleContent: map[string]any{"text": "示例正文"},
		Category:       model.SeednoteTemplateCategoryKnowledge,
		Tags:           []string{"干货", "方法论"},
	}
	tmpl.SetEcommerce(model.EcommerceTemplateDefaults{
		DefaultSelectedModules: map[string]int{"product_poster": 1},
		TargetPlatform:         "tmall",
		BrandBrief:             "品牌说明",
		ImageCapabilityKey:     "openai-gpt-image",
	})

	created, err := svc.Create(ctx, tmpl, ownerID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Prompt != "暖色生活摄影" {
		t.Errorf("Prompt = %q, want 暖色生活摄影", created.Prompt)
	}
	if created.Category != model.SeednoteTemplateCategoryKnowledge {
		t.Errorf("Category = %q, want %s", created.Category, model.SeednoteTemplateCategoryKnowledge)
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
	if ec := created.Ecommerce.Data(); len(ec.DefaultSelectedModules) > 0 || ec.TargetPlatform != "" || ec.BrandBrief != "" || ec.ImageCapabilityKey != "" {
		t.Fatalf("Ecommerce = %+v, want empty visual-only payload", ec)
	}

	refetched, err := svc.GetByID(ctx, created.ID, ownerID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if refetched.Writer != "" || refetched.Theme != "" || refetched.Author != "" || refetched.Structure != nil || refetched.ExampleContent != nil {
		t.Fatalf("persisted legacy fields = writer:%q theme:%q author:%q structure:%v example:%v, want empty",
			refetched.Writer, refetched.Theme, refetched.Author, refetched.Structure, refetched.ExampleContent)
	}
}

func TestTemplateService_Update_IgnoresLegacyBusinessFieldsAndTags(t *testing.T) {
	svc, repo, _ := setupTemplateService(t)
	ctx := context.Background()
	ownerID := uuid.New().String()

	legacy := &model.Template{
		ID:             uuid.NewString(),
		UserID:         ownerID,
		Name:           "旧模板",
		Type:           model.TemplateTypeSeednote,
		Prompt:         "旧视觉",
		Writer:         "legacy-writer",
		Theme:          "legacy-theme",
		Author:         "legacy-author",
		Structure:      map[string]any{"text": "legacy-structure"},
		ExampleContent: map[string]any{"text": "legacy-example"},
		Category:       model.SeednoteTemplateCategoryProduct,
		Tags:           []string{"旧标签"},
	}
	legacy.SetEcommerce(model.EcommerceTemplateDefaults{TargetPlatform: "legacy-platform"})
	if err := repo.Templates().Create(ctx, legacy); err != nil {
		t.Fatalf("create legacy template: %v", err)
	}

	updated, err := svc.Update(ctx, legacy.ID, ownerID, &model.Template{
		Name:           "新模板",
		Prompt:         "新视觉",
		Writer:         "new-writer",
		Theme:          "new-theme",
		Author:         "new-author",
		Structure:      map[string]any{"text": "new-structure"},
		ExampleContent: map[string]any{"text": "new-example"},
		Category:       model.SeednoteTemplateCategoryBeauty,
		Tags:           []string{"新标签"},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "新模板" || updated.Prompt != "新视觉" || updated.Category != model.SeednoteTemplateCategoryBeauty {
		t.Fatalf("updated visual metadata = name:%q visual:%q category:%q", updated.Name, updated.Prompt, updated.Category)
	}
	if len(updated.Tags) != 1 || updated.Tags[0] != "旧标签" {
		t.Fatalf("Tags = %v, want preserved [旧标签]", updated.Tags)
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

func TestTemplateService_Update_LegacyRuntimePatchDoesNotClearPrompt(t *testing.T) {
	svc, _, _ := setupTemplateService(t)
	ctx := context.Background()
	ownerID := uuid.New().String()

	created, err := svc.Create(ctx, &model.Template{
		Name:     "视觉模板",
		Type:     model.TemplateTypeSeednote,
		Category: model.SeednoteTemplateCategoryProduct,
		Prompt:   "暖色生活摄影",
	}, ownerID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := svc.Update(ctx, created.ID, ownerID, &model.Template{Theme: "autumn-warm"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Prompt != "暖色生活摄影" {
		t.Fatalf("Prompt = %q, want existing style preserved", updated.Prompt)
	}
	if updated.Theme != "" {
		t.Fatalf("Theme = %q, want ignored legacy runtime field", updated.Theme)
	}
}
