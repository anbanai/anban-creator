package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDesignerReferenceRepositoryPersistsOwnedImmutableMapping(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	repo := NewDesignerReferenceRepository(db)
	ref := &model.DesignerReference{
		ID: uuid.NewString(), UserID: "user-1", StorageKey: "uploads/finalized/user-1/upload-1/reference.png",
		FileName: "reference.png", ContentType: "image/png", Size: 123,
	}
	if err := repo.Create(context.Background(), ref); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := repo.FindByIDAndUserID(context.Background(), ref.ID, ref.UserID)
	if err != nil {
		t.Fatalf("FindByIDAndUserID: %v", err)
	}
	if got.StorageKey != ref.StorageKey || got.FileName != ref.FileName || got.ContentType != ref.ContentType || got.Size != ref.Size || got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatalf("mapping = %#v, want %#v with timestamps", got, ref)
	}
	if _, err := repo.FindByIDAndUserID(context.Background(), ref.ID, "user-2"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-user lookup error = %v, want record not found", err)
	}
}
