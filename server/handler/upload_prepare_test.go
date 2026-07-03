package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
)

type handlerDirectUploadStore struct{}

func (handlerDirectUploadStore) Name() string { return "oss" }
func (handlerDirectUploadStore) Upload(context.Context, string, io.Reader, string) (*storage.UploadResult, error) {
	return nil, fmt.Errorf("not implemented")
}
func (handlerDirectUploadStore) UploadFile(context.Context, string, string, string) (*storage.UploadResult, error) {
	return nil, fmt.Errorf("not implemented")
}
func (handlerDirectUploadStore) UploadURL(_ context.Context, key string, _ string, _ int) (string, error) {
	return "https://oss-upload.example.com/" + key, nil
}
func (handlerDirectUploadStore) GetURL(key string) string { return "https://cdn.example.com/" + key }
func (handlerDirectUploadStore) Read(context.Context, string) ([]byte, error) {
	return nil, fmt.Errorf("not implemented")
}
func (handlerDirectUploadStore) Delete(context.Context, string) error { return nil }
func (handlerDirectUploadStore) DownloadURL(_ context.Context, key string, _ int) (string, error) {
	return "https://oss-download.example.com/" + key, nil
}
func (handlerDirectUploadStore) HasCustomDomain() bool  { return true }
func (handlerDirectUploadStore) IsOwnedURL(string) bool { return false }

type handlerPendingUploadRepo struct {
	created *model.PendingUpload
}

func (r *handlerPendingUploadRepo) CreatePendingUpload(_ context.Context, upload *model.PendingUpload) error {
	cp := *upload
	r.created = &cp
	return nil
}
func (r *handlerPendingUploadRepo) FindPendingUploadByID(context.Context, string) (*model.PendingUpload, error) {
	return nil, service.ErrPendingUploadNotFound
}
func (r *handlerPendingUploadRepo) FinalizePendingUploads(context.Context, []string, time.Time) error {
	return nil
}
func (r *handlerPendingUploadRepo) FindExpiredPendingUploads(context.Context, time.Time, int) ([]*model.PendingUpload, error) {
	return nil, nil
}
func (r *handlerPendingUploadRepo) MarkPendingUploadExpired(context.Context, string, time.Time) error {
	return nil
}

func TestUploadPrepareReturnsDirectUploadCredentials(t *testing.T) {
	logger := zerolog.New(io.Discard)
	store := handlerDirectUploadStore{}
	repo := &handlerPendingUploadRepo{}
	h := NewUploadHandler(store, repo, service.DirectUploadConfig{
		Storage: config.StorageConfig{
			Provider:       "oss",
			Endpoint:       "oss-cn-hangzhou.aliyuncs.com",
			BucketName:     "bucket",
			Region:         "oss-cn-hangzhou",
			STSRoleArn:     "acs:ram::1:role/upload",
			STSSessionName: "studio-upload",
		},
		CredentialIssuer: service.StaticUploadCredentialIssuer(func(_ context.Context, _ service.UploadCredentialRequest) (*service.UploadCredential, error) {
			return &service.UploadCredential{
				AccessKeyID:     "sts-ak",
				AccessKeySecret: "sts-secret",
				SecurityToken:   "sts-token",
				ExpiresAt:       time.Now().Add(15 * time.Minute),
			}, nil
		}),
	}, &logger)

	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return c.Next()
	})
	app.Post("/uploads/prepare", h.Prepare)

	body := bytes.NewBufferString(`{"purpose":"video_reference","filename":"test.mp4","content_type":"video/mp4","size":1234}`)
	req := httptest.NewRequest(http.MethodPost, "/uploads/prepare", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var decoded struct {
		Data struct {
			UploadID         string    `json:"upload_id"`
			Key              string    `json:"key"`
			PublicURL        string    `json:"public_url"`
			STSAccessKeyID   string    `json:"sts_access_key_id"`
			STSSecurityToken string    `json:"sts_security_token"`
			ExpiresAt        time.Time `json:"expires_at"`
			MaxSize          int64     `json:"max_size"`
			Method           string    `json:"method"`
			Bucket           string    `json:"bucket"`
			Region           string    `json:"region"`
			Endpoint         string    `json:"endpoint"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Data.UploadID == "" || decoded.Data.Key == "" || decoded.Data.PublicURL == "" {
		t.Fatalf("missing upload fields: %+v", decoded.Data)
	}
	if decoded.Data.Method != "PUT" || decoded.Data.MaxSize != 50*1024*1024 {
		t.Fatalf("method/max_size = %q/%d", decoded.Data.Method, decoded.Data.MaxSize)
	}
	if !strings.HasPrefix(decoded.Data.Key, "uploads/pending/user-1/") {
		t.Fatalf("key = %q, want user pending prefix", decoded.Data.Key)
	}
}
