package mcp

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type userMediaMCPStorage struct{}

func (userMediaMCPStorage) Name() string { return "test" }
func (userMediaMCPStorage) Upload(context.Context, string, io.Reader, string) (*storage.UploadResult, error) {
	return nil, nil
}
func (userMediaMCPStorage) UploadFile(context.Context, string, string, string) (*storage.UploadResult, error) {
	return nil, nil
}
func (userMediaMCPStorage) UploadURL(context.Context, string, string, int) (string, error) {
	return "", nil
}
func (userMediaMCPStorage) GetURL(string) string                         { return "" }
func (userMediaMCPStorage) Read(context.Context, string) ([]byte, error) { return nil, nil }
func (userMediaMCPStorage) Delete(context.Context, string) error         { return nil }
func (userMediaMCPStorage) DownloadURL(_ context.Context, key string, _ int) (string, error) {
	return "https://cdn.example/" + key, nil
}
func (userMediaMCPStorage) HasCustomDomain() bool  { return false }
func (userMediaMCPStorage) IsOwnedURL(string) bool { return false }

func TestSearchUserMediaAssetsUsesAuthenticatedIdentityAndReadyOnlyRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Asset{}, &model.UserMediaAsset{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	seed := func(userID, id, status string) {
		assetID := uuid.NewString()
		if err := repo.Assets().Create(t.Context(), &model.Asset{ID: assetID, UserID: userID, Purpose: service.DirectUploadPurposeMediaLibrary, StorageKey: id + ".png", FileName: id + ".png", ContentType: "image/png", Size: 1, ETag: id}); err != nil {
			t.Fatal(err)
		}
		if err := repo.UserMediaAssets().Create(t.Context(), &model.UserMediaAsset{ID: id, UserID: userID, AssetID: assetID, MediaType: "image", Status: status, Description: "产品素材"}); err != nil {
			t.Fatal(err)
		}
	}
	seed("user-1", "available", model.UserMediaAssetStatusAvailable)
	seed("user-1", "queued", model.UserMediaAssetStatusQueued)
	seed("user-2", "foreign", model.UserMediaAssetStatusAvailable)

	old := svcs
	svcs = &Services{UserMediaSvc: service.NewUserMediaLibraryService(repo, userMediaMCPStorage{}, nil)}
	t.Cleanup(func() { svcs = old })
	ctx := withMCPExecutionIdentity(context.Background(), "user-1", "project-1", "task-1", "execution-1")
	result, err := searchUserMediaAssetsHandler(ctx, &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"query":"产品"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.IsError {
		t.Fatalf("result = %#v", result)
	}
	text := toolResultText(result)
	if !strings.Contains(text, `"asset_id":"available"`) || strings.Contains(text, `"asset_id":"queued"`) || strings.Contains(text, `"asset_id":"foreign"`) {
		t.Fatalf("filtered result = %s", text)
	}

	spoofed, err := searchUserMediaAssetsHandler(ctx, &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"project_id":"foreign-project"}`)}})
	if err != nil || spoofed == nil || !spoofed.IsError || !strings.Contains(toolResultText(spoofed), "project_id is derived") {
		t.Fatalf("project spoof result = %#v, %v", spoofed, err)
	}
}
