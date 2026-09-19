package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/platform"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/seednote"
	"github.com/anbanai/anban-creator/server/storage"
)

func uploadRepositoryFromSessions(t *testing.T, sessions ...*model.UploadSession) repository.Repository {
	t.Helper()
	repo := repository.New(setupTaskHandlerTestDB(t))
	seedUploadSessions(t, repo, sessions...)
	return repo
}

func seedUploadSessions(t *testing.T, repo repository.Repository, sessions ...*model.UploadSession) {
	t.Helper()
	for _, session := range sessions {
		if session == nil {
			continue
		}
		if session.Status == model.UploadSessionFinalized && session.AssetID == "" {
			session.AssetID = session.ID
		}
		if session.Status == model.UploadSessionFinalized && session.FinalizationETag == "" {
			session.FinalizationETag = "etag-" + session.ID
		}
		if err := repo.UploadSessions().Create(t.Context(), session); err != nil {
			t.Fatalf("create upload session %s: %v", session.ID, err)
		}
		if session.Status == model.UploadSessionFinalized {
			if err := repo.Assets().Create(t.Context(), &model.Asset{
				ID: session.AssetID, UserID: session.UserID, Purpose: session.Purpose,
				StorageKey: path.Join("assets/users", session.UserID, session.ID, session.FileName),
				FileName:   session.FileName, ContentType: session.ContentType, Size: session.Size, ETag: session.FinalizationETag,
			}); err != nil {
				t.Fatalf("create asset %s: %v", session.ID, err)
			}
		}
	}
}

func assertFinalizedAsset(t *testing.T, repo repository.Repository, id, wantKey string) {
	t.Helper()
	session, err := repo.UploadSessions().FindByID(t.Context(), id)
	if err != nil || session.Status != model.UploadSessionFinalized || session.AssetID != id || session.FinalizationETag == "" {
		t.Fatalf("finalized upload session %s = %#v, %v", id, session, err)
	}
	asset, err := repo.Assets().FindByID(t.Context(), id)
	if err != nil || asset.StorageKey != wantKey || asset.ETag != session.FinalizationETag {
		t.Fatalf("finalized asset %s = %#v, %v; want key %q", id, asset, err, wantKey)
	}
}

type fakeStorageProvider struct {
	data        map[string][]byte
	read        []string
	uploaded    []string
	readErr     error
	readMax     []int64
	sessionRepo repository.UploadSessionRepository
	statErr     error
	statInfo    *storage.ObjectInfo
	objects     map[string]*storage.ObjectInfo
}

var _ storage.Provider = (*fakeStorageProvider)(nil)

func (f *fakeStorageProvider) Name() string { return "fake" }

func (f *fakeStorageProvider) Upload(_ context.Context, key string, reader io.Reader, contentType string) (*storage.UploadResult, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if f.data == nil {
		f.data = map[string][]byte{}
	}
	f.data[key] = data
	f.uploaded = append(f.uploaded, key)
	return &storage.UploadResult{Key: key, URL: f.GetURL(key), Size: int64(len(data)), MimeType: contentType}, nil
}

func (f *fakeStorageProvider) UploadFile(context.Context, string, string, string) (*storage.UploadResult, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeStorageProvider) UploadURL(context.Context, string, string, int) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (f *fakeStorageProvider) GetURL(key string) string { return "/api/v1/files/" + key }

func (f *fakeStorageProvider) Read(_ context.Context, key string) ([]byte, error) {
	f.read = append(f.read, key)
	if f.readErr != nil {
		return nil, f.readErr
	}
	if data, ok := f.data[key]; ok {
		return data, nil
	}
	return nil, fmt.Errorf("not found")
}

func (f *fakeStorageProvider) ReadObject(ctx context.Context, key string, maxBytes int64) ([]byte, error) {
	f.readMax = append(f.readMax, maxBytes)
	data, err := f.Read(ctx, key)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, storage.ErrObjectExceedsMaxSize
	}
	return data, nil
}

func (f *fakeStorageProvider) StatObject(ctx context.Context, key string) (*storage.ObjectInfo, error) {
	if f.statErr != nil {
		return nil, f.statErr
	}
	if f.statInfo != nil {
		info := *f.statInfo
		return &info, nil
	}
	if info := f.objects[key]; info != nil {
		copy := *info
		return &copy, nil
	}
	parts := strings.Split(strings.Trim(key, "/"), "/")
	if f.sessionRepo != nil && len(parts) >= 4 && parts[0] == "uploads" && parts[1] == "pending" {
		session, err := f.sessionRepo.FindByID(ctx, parts[3])
		if err != nil || session.StagingKey != key {
			return nil, storage.ErrObjectNotFound
		}
		return &storage.ObjectInfo{Key: key, Size: session.Size, ContentType: session.ContentType, ETag: "etag-" + session.ID}, nil
	}
	return nil, storage.ErrObjectNotFound
}

func (f *fakeStorageProvider) PromoteObject(ctx context.Context, sourceKey, finalKey, expectedETag string) (*storage.ObjectInfo, error) {
	if f.objects[finalKey] != nil {
		return nil, storage.ErrObjectAlreadyExists
	}
	info, err := f.StatObject(ctx, sourceKey)
	if err != nil {
		return nil, err
	}
	if info.ETag != expectedETag {
		return nil, storage.ErrPromotionPreconditionFailed
	}
	if f.objects == nil {
		f.objects = map[string]*storage.ObjectInfo{}
	}
	copy := *info
	copy.Key = finalKey
	f.objects[finalKey] = &copy
	if data, ok := f.data[sourceKey]; ok {
		if f.data == nil {
			f.data = map[string][]byte{}
		}
		f.data[finalKey] = append([]byte(nil), data...)
	}
	return &copy, nil
}

func uploadSessionStatStore(repo repository.UploadSessionRepository) *fakeStorageProvider {
	return &fakeStorageProvider{sessionRepo: repo}
}

func (f *fakeStorageProvider) Delete(context.Context, string) error { return nil }

func (f *fakeStorageProvider) DownloadURL(_ context.Context, key string, _ int) (string, error) {
	return f.GetURL(key), nil
}

func (f *fakeStorageProvider) HasCustomDomain() bool { return false }

// IsOwnedURL is a plumbing fake: it accepts its Local/CDN fixture URLs and a
// fixed OSS hostname so handler tests can exercise each storage routing branch.
// It does NOT replicate the real SSRF defenses (subdomain spoofing, scheme
// rejection, case-insensitive host match) — those are exercised against the
// production provider in server/storage/owned_url_test.go.
func (f *fakeStorageProvider) IsOwnedURL(rawURL string) bool {
	return strings.HasPrefix(rawURL, "/api/v1/files/") ||
		strings.HasPrefix(rawURL, "https://cdn.example.com/") ||
		strings.HasPrefix(rawURL, "https://fake-bucket.oss-cn-hangzhou.aliyuncs.com/")
}

func TestProjectFetchProfilePreservesProviderFieldsWithoutLLMEnrichment(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/user/profile" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(seednote.APIResponse[seednote.UserProfile]{
			Success: true,
			Data: seednote.UserProfile{UserBasicInfo: seednote.UserBasicInfo{
				Nickname: "原始昵称",
				Desc:     "原始简介",
				Avatar:   "https://cdn.example.com/avatar.png",
				RedID:    "red-1",
			}},
		})
	}))
	defer upstream.Close()

	h := NewProjectHandler(nil, testProjectLogger(t))
	h.SetSeednoteClient(seednote.NewClient(upstream.URL, time.Second))
	app := fiber.New()
	app.Post("/fetch", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.FetchProfile(c)
	})

	resp, err := app.Test(httptestJSON("POST", "/fetch", `{"platform":"seednote","profile_url":"https://www.xiaohongshu.com/user/profile/user-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var envelope struct {
		Data platform.PlatformProfile `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Name != "原始昵称" || envelope.Data.RawData["desc"] != "原始简介" {
		t.Fatalf("provider profile changed: %#v", envelope.Data)
	}
	if _, exists := envelope.Data.RawData["analysis"]; exists {
		t.Fatal("hidden LLM analysis remains")
	}
}

func TestProjectFetchProfileRejectsNonSeednoteAutoFetch(t *testing.T) {
	h := NewProjectHandler(nil, testProjectLogger(t))
	app := fiber.New()
	app.Post("/fetch", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.FetchProfile(c)
	})

	resp, err := app.Test(httptestJSON("POST", "/fetch", `{"platform":"article","profile_url":"https://mp.weixin.qq.com/test"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
}

func httptestJSON(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func testProjectLogger(t *testing.T) *zerolog.Logger {
	t.Helper()
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	return &logger
}

func tinyPNG() []byte {
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xde,
	}
}
