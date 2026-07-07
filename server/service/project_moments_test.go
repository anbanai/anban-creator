package service

import (
	"context"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func TestProjectServiceCreateAcceptsMomentsAndDefaultsRatio(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	svc := NewProjectService(repo, &logger)

	project, err := svc.Create(context.Background(), uuid.NewString(), &model.Project{
		Platform:     model.PlatformMoments,
		Name:         "私域朋友圈",
		Instructions: "面向私域成交的轻运营内容",
	})
	if err != nil {
		t.Fatalf("Create moments project: %v", err)
	}
	if project.Platform != model.PlatformMoments {
		t.Fatalf("platform = %q, want moments", project.Platform)
	}
	if project.ImageRatio != "3:4" {
		t.Fatalf("image ratio = %q, want 3:4", project.ImageRatio)
	}
}
