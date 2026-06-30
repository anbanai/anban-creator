package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/service"
	"github.com/royalrick/anbanwriter/server/storage"
)

type fakeVideoReferenceStorage struct {
	url string
	key string
}

func (f *fakeVideoReferenceStorage) Name() string { return "fake" }

func (f *fakeVideoReferenceStorage) Upload(_ context.Context, key string, reader io.Reader, contentType string) (*storage.UploadResult, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	f.key = key
	return &storage.UploadResult{URL: f.url, Key: key, Size: int64(len(data)), MimeType: contentType}, nil
}

func (f *fakeVideoReferenceStorage) UploadFile(ctx context.Context, key string, filePath string, contentType string) (*storage.UploadResult, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return f.Upload(ctx, key, file, contentType)
}

func (f *fakeVideoReferenceStorage) GetURL(key string) string { return f.url }
func (f *fakeVideoReferenceStorage) Read(context.Context, string) ([]byte, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeVideoReferenceStorage) Delete(context.Context, string) error { return nil }
func (f *fakeVideoReferenceStorage) DownloadURL(context.Context, string, int) (string, error) {
	return f.url, nil
}
func (f *fakeVideoReferenceStorage) HasCustomDomain() bool {
	return strings.HasPrefix(f.url, "https://")
}
func (f *fakeVideoReferenceStorage) IsOwnedURL(string) bool { return false }

func TestDownloadFileRejectsOversizedResponse(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("123456"))
	}))
	defer srv.Close()
	oldTransport := http.DefaultTransport
	http.DefaultTransport = srv.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = oldTransport })

	out := filepath.Join(t.TempDir(), "video.mp4")
	err := downloadFile(context.Background(), srv.URL, out, 5)
	if err == nil || !strings.Contains(err.Error(), "exceeds max size") {
		t.Fatalf("downloadFile error = %v, want max size error", err)
	}
	info, statErr := os.Stat(out)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("stat output: %v", statErr)
	}
	if info != nil && info.Size() <= 5 {
		t.Fatalf("expected oversized partial file, got size %d", info.Size())
	}
}

func TestRegisterVideoReferenceUploadsLocalFileAndRejectsPrivateStorageURL(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	store := &fakeVideoReferenceStorage{url: "/api/v1/files/uploads/video-references/u/ref.png"}
	svcs = &Services{Store: store}

	refPath := filepath.Join(t.TempDir(), "ref.png")
	if err := os.WriteFile(refPath, []byte("fake-png"), 0644); err != nil {
		t.Fatalf("write ref: %v", err)
	}
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"project_id":"p1","type":"image_url","file_path":` + strconv.Quote(refPath) + `}`)},
	}
	result, err := registerVideoReferenceHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("registerVideoReferenceHandler returned error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected local storage URL to be rejected")
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "OSS/CDN") || !strings.Contains(text, "HTTPS") {
		t.Fatalf("unexpected error text: %q", text)
	}
	if !strings.Contains(store.key, "uploads/video-references/") {
		t.Fatalf("storage key = %q, want video reference prefix", store.key)
	}
}

func TestRegisterVideoReferenceRequiresProjectID(t *testing.T) {
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"type":"image_url","url":"https://example.com/ref.png"}`)},
	}
	result, err := registerVideoReferenceHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("registerVideoReferenceHandler returned error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error")
	}
	if text := result.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "project_id is required") {
		t.Fatalf("unexpected error text: %q", text)
	}
}

func TestRegisterVideoReferenceReturnsPublicURL(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	store := &fakeVideoReferenceStorage{url: "https://cdn.example.com/uploads/video-references/u/ref.png"}
	svcs = &Services{Store: store}

	refPath := filepath.Join(t.TempDir(), "ref.png")
	if err := os.WriteFile(refPath, []byte("fake-png"), 0644); err != nil {
		t.Fatalf("write ref: %v", err)
	}
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"project_id":"p1","type":"image_url","file_path":` + strconv.Quote(refPath) + `}`)},
	}
	result, err := registerVideoReferenceHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("registerVideoReferenceHandler returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "https://cdn.example.com/uploads/video-references/u/ref.png") || !strings.Contains(text, "ark_url") {
		t.Fatalf("unexpected result: %s", text)
	}
}

func TestBuildVideoGenerationPlanHandlerValidatesArguments(t *testing.T) {
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"project_id":"p1","prompt":"生成视频","purpose":"unknown"}`)},
	}
	result, err := buildVideoGenerationPlanHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("buildVideoGenerationPlanHandler returned error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error")
	}
	if text := result.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "purpose must be one of") {
		t.Fatalf("unexpected error text: %q", text)
	}
}

func TestBuildVideoGenerationPlanHandlerReturnsPayloadPreview(t *testing.T) {
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"project_id":"p1",
			"prompt":"生成一条咖啡杯种草视频",
			"purpose":"planting",
			"references":[{"type":"image_url","url":"https://example.com/cup.png"}],
			"duration":15,
			"ratio":"9:16",
			"resolution":"1080p"
		}`)},
	}
	result, err := buildVideoGenerationPlanHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("buildVideoGenerationPlanHandler returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	for _, want := range []string{"generation_plan", "sdk_payload_preview", "reference-anchors.md", "shot-plan.md"} {
		if !strings.Contains(text, want) {
			t.Fatalf("payload missing %q: %s", want, text)
		}
	}
}

func TestCreateVideoGenerationTaskHandlerRequiresService(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	svcs = &Services{}
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"project_id":"p1","prompt":"生成视频"}`)},
	}
	result, err := createVideoGenerationTaskHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("createVideoGenerationTaskHandler returned error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error")
	}
	if text := result.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "video service not available") {
		t.Fatalf("unexpected error text: %q", text)
	}
}

func TestCreateVideoGenerationTaskHandlerRejectsInvalidReference(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	svcs = &Services{VideoSvc: service.NewVideoService(&config.VideoAPIConfig{
		Key:     "test-key",
		BaseURL: "https://example.com",
		Model:   "doubao-seedance-2-0",
		Defaults: config.VideoDefaultsConfig{
			Resolution: "1080p",
			Ratio:      "9:16",
			Duration:   15,
		},
	})}

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"project_id":"p1",
			"prompt":"生成视频",
			"references":[{"type":"image_url","url":"http://localhost/a.png"}]
		}`)},
	}
	result, err := createVideoGenerationTaskHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("createVideoGenerationTaskHandler returned error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error")
	}
	if text := result.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "publicly accessible HTTPS") {
		t.Fatalf("unexpected error text: %q", text)
	}
}
