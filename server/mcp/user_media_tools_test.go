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

type userMediaMCPSemantic struct {
	results []storage.SemanticMediaResult
}

func (f userMediaMCPSemantic) Search(context.Context, string, string, string, int) ([]storage.SemanticMediaResult, error) {
	return f.results, nil
}
func (userMediaMCPSemantic) Lookup(context.Context, string, string) (*storage.SemanticMediaResult, error) {
	return nil, nil
}

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

func TestSearchUserMediaAssetsReturnsSemanticVideoContract(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Asset{}, &model.UserMediaAsset{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	assetID := uuid.NewString()
	if err := repo.Assets().Create(t.Context(), &model.Asset{ID: assetID, UserID: "user-1", Purpose: service.DirectUploadPurposeMediaLibrary, StorageKey: "assets/users/user-1/coffee.mp4", FileName: "coffee.mp4", ContentType: "video/mp4", Size: 1, ETag: "coffee"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UserMediaAssets().Create(t.Context(), &model.UserMediaAsset{ID: "media-1", UserID: "user-1", AssetID: assetID, MediaType: "video", Status: model.UserMediaAssetStatusAvailable}); err != nil {
		t.Fatal(err)
	}
	old := svcs
	svc := service.NewUserMediaLibraryService(repo, userMediaMCPStorage{}, nil)
	svc.SetSemanticSearch(userMediaMCPSemantic{results: []storage.SemanticMediaResult{{ObjectKey: "assets/users/user-1/coffee.mp4", Summary: "店员制作咖啡", Description: "视频展示店员在吧台制作咖啡的过程。"}}})
	svcs = &Services{UserMediaSvc: svc}
	t.Cleanup(func() { svcs = old })
	ctx := withMCPExecutionIdentity(context.Background(), "user-1", "project-1", "task-1", "execution-1")
	result, err := searchUserMediaAssetsHandler(ctx, &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"query":"有人在店里制作咖啡","media_type":"video","limit":8}`)}})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("semantic result = %#v, %v", result, err)
	}
	text := toolResultText(result)
	for _, fragment := range []string{`"summary":"店员制作咖啡"`, `"description":"视频展示店员在吧台制作咖啡的过程。"`, `"provider":"oss_ai_content_awareness"`, `"requires_confirmation":true`, `"option_label":"media_asset:media-1"`} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("semantic response missing %s: %s", fragment, text)
		}
	}
}

func TestSearchUserMediaAssetsDoesNotFallbackToDatabaseForSemanticVideos(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Asset{}, &model.UserMediaAsset{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	old := svcs
	svcs = &Services{UserMediaSvc: service.NewUserMediaLibraryService(repo, userMediaMCPStorage{}, nil)}
	t.Cleanup(func() { svcs = old })
	ctx := withMCPExecutionIdentity(context.Background(), "user-1", "", "task-1", "execution-1")
	result, err := searchUserMediaAssetsHandler(ctx, &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"query":"咖啡","media_type":"video"}`)}})
	if err != nil || result == nil || !result.IsError || !strings.Contains(toolResultText(result), "media_search_failed") {
		t.Fatalf("unconfigured semantic search = %#v, %v", result, err)
	}
}

func TestSearchUserMediaAssetsRejectsOrdinaryUserIdentity(t *testing.T) {
	old := svcs
	svcs = &Services{UserMediaSvc: service.NewUserMediaLibraryService(nil, userMediaMCPStorage{}, nil)}
	t.Cleanup(func() { svcs = old })
	ctx := withMCPUserID(context.Background(), "user-1")
	result, err := searchUserMediaAssetsHandler(ctx, &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"query":"咖啡","media_type":"video"}`)}})
	if err != nil || result == nil || !result.IsError || !strings.Contains(toolResultText(result), "identity_required") {
		t.Fatalf("ordinary user identity result = %#v, %v", result, err)
	}
}
