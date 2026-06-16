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
		Name:        "我的种草模板",
		Type:        "seednote",
		ThumbnailURL: "https://example.com/x.png",
		StylePrompt: "暖色调，柔和光线",
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

func TestTemplateService_Create_RejectsEmptyName(t *testing.T) {
	svc, _, _ := setupTemplateService(t)
	_, err := svc.Create(context.Background(), &model.Template{Name: "", Type: "seednote"}, uuid.New().String())
	if err == nil {
		t.Fatalf("expected error for empty name, got nil")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("expected name-related error, got %v", err)
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
