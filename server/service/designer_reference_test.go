package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appconfig "github.com/anbanai/anban-creator/app/config"
	appimage "github.com/anbanai/anban-creator/app/image"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type designerReferenceStore struct {
	data      map[string][]byte
	uploaded  []string
	read      []string
	readLimit []int64
	deleted   []string
	readErr   error
}

type designerReferenceErrorRepository struct {
	err error
}

func (r designerReferenceErrorRepository) Create(context.Context, *model.DesignerReference) error {
	return r.err
}

func (r designerReferenceErrorRepository) FindByIDAndUserID(context.Context, string, string) (*model.DesignerReference, error) {
	return nil, r.err
}

func (s *designerReferenceStore) Name() string { return "designer-reference-test" }
func (s *designerReferenceStore) Upload(_ context.Context, key string, reader io.Reader, contentType string) (*storage.UploadResult, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if s.data == nil {
		s.data = map[string][]byte{}
	}
	s.data[key] = append([]byte(nil), data...)
	s.uploaded = append(s.uploaded, key)
	return &storage.UploadResult{Key: key, URL: "https://storage.test/" + key, Size: int64(len(data)), MimeType: contentType}, nil
}
func (*designerReferenceStore) UploadFile(context.Context, string, string, string) (*storage.UploadResult, error) {
	return nil, errors.New("not implemented")
}
func (*designerReferenceStore) UploadURL(context.Context, string, string, int) (string, error) {
	return "", errors.New("not implemented")
}
func (s *designerReferenceStore) GetURL(key string) string { return "https://storage.test/" + key }
func (s *designerReferenceStore) Read(_ context.Context, key string) ([]byte, error) {
	data, ok := s.data[key]
	if !ok {
		return nil, storage.ErrObjectNotFound
	}
	return append([]byte(nil), data...), nil
}
func (s *designerReferenceStore) ReadObject(ctx context.Context, key string, maxBytes int64) ([]byte, error) {
	s.read = append(s.read, key)
	s.readLimit = append(s.readLimit, maxBytes)
	if s.readErr != nil {
		return nil, s.readErr
	}
	data, err := s.Read(ctx, key)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, storage.ErrObjectExceedsMaxSize
	}
	return data, nil
}
func (s *designerReferenceStore) Delete(_ context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	delete(s.data, key)
	return nil
}
func (*designerReferenceStore) DownloadURL(context.Context, string, int) (string, error) {
	return "", errors.New("not implemented")
}
func (*designerReferenceStore) HasCustomDomain() bool  { return true }
func (*designerReferenceStore) IsOwnedURL(string) bool { return true }

type designerReferenceProvider struct {
	generate func(*appimage.GenerateOptions) (*appimage.GenerateResult, error)
}

func (designerReferenceProvider) Name() string { return "reference-test" }
func (p designerReferenceProvider) Generate(_ context.Context, _ string, opts *appimage.GenerateOptions) (*appimage.GenerateResult, error) {
	return p.generate(opts)
}
func (designerReferenceProvider) Capabilities() *appimage.ProviderCapabilities {
	return &appimage.ProviderCapabilities{}
}

func setupDesignerReferenceService(t *testing.T, store storage.Provider) (*DesignerService, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	logger := zerolog.New(io.Discard)
	return NewDesignerService(db, nil, nil, nil, store, &logger), db
}

func TestDesignerReferenceRegistrationIsDurableWithoutPermanentTempFiles(t *testing.T) {
	store := &designerReferenceStore{data: map[string][]byte{}}
	svc, db := setupDesignerReferenceService(t, store)
	ctx := context.Background()
	userID := uuid.NewString()

	for i := 0; i < 2; i++ {
		fileID, err := svc.UploadReference(ctx, userID, "reference.png", "image/png", []byte(fmt.Sprintf("image-%d", i)))
		if err != nil {
			t.Fatalf("UploadReference %d: %v", i, err)
		}
		if _, err := uuid.Parse(fileID); err != nil {
			t.Fatalf("file_id = %q: %v", fileID, err)
		}
		matches, err := filepath.Glob(filepath.Join(os.TempDir(), "anban-creator_ref_"+fileID+"_*"))
		if err != nil || len(matches) != 0 {
			t.Fatalf("registration left permanent temp files: %#v err=%v", matches, err)
		}
		ref, err := repository.NewDesignerReferenceRepository(db).FindByIDAndUserID(ctx, fileID, userID)
		if err != nil {
			t.Fatalf("find mapping: %v", err)
		}
		wantPrefix := userID + "/designer/references/" + fileID + "/"
		if !strings.HasPrefix(ref.StorageKey, wantPrefix) || ref.FileName != "reference.png" || ref.ContentType != "image/png" || ref.Size != int64(len(fmt.Sprintf("image-%d", i))) {
			t.Fatalf("mapping = %#v", ref)
		}
	}
	if len(store.uploaded) != 2 {
		t.Fatalf("multipart registrations uploaded %d objects, want exactly one each", len(store.uploaded))
	}

	store.uploaded = nil
	storedKey := "uploads/finalized/" + userID + "/direct/reference.png"
	store.data[storedKey] = []byte("direct")
	fileID, err := svc.RegisterStoredReference(ctx, userID, storedKey, "direct.png", "image/png", int64(len("direct")))
	if err != nil {
		t.Fatalf("RegisterStoredReference: %v", err)
	}
	if len(store.uploaded) != 0 {
		t.Fatalf("direct registration copied bytes to %#v", store.uploaded)
	}
	ref, err := repository.NewDesignerReferenceRepository(db).FindByIDAndUserID(ctx, fileID, userID)
	if err != nil || ref.StorageKey != storedKey {
		t.Fatalf("direct mapping = %#v err=%v", ref, err)
	}
}

func TestDesignerReferenceCrossInstanceMaterializationAndCleanup(t *testing.T) {
	store := &designerReferenceStore{data: map[string][]byte{}}
	svcA, db := setupDesignerReferenceService(t, store)
	ctx := context.Background()
	userID := uuid.NewString()
	refID, err := svcA.UploadReference(ctx, userID, "reference.png", "image/png", []byte("reference-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	maskID, err := svcA.UploadReference(ctx, userID, "mask.png", "image/png", []byte("mask-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	legacyTemps, _ := filepath.Glob(filepath.Join(os.TempDir(), "anban-creator_ref_*"))
	for _, temp := range legacyTemps {
		if strings.Contains(temp, refID) || strings.Contains(temp, maskID) {
			_ = os.Remove(temp)
		}
	}

	refJSON, _ := json.Marshal([]string{refID})
	genID := uuid.NewString()
	if err := db.Create(&model.ImageGeneration{
		ID: genID, UserID: userID, ProjectID: "default", Prompt: "edit", Provider: "openai", Model: "test",
		N: 1, Status: model.ImageGenerationStatusGenerating, ReferenceFiles: string(refJSON), MaskFileID: maskID,
	}).Error; err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	svcB := NewDesignerService(db, nil, nil, nil, store, &logger)
	var materialized []string
	svcB.providerFactory = func(*appconfig.ImageAPI, *zerolog.Logger) (appimage.Provider, error) {
		return designerReferenceProvider{generate: func(opts *appimage.GenerateOptions) (*appimage.GenerateResult, error) {
			materialized = append(materialized, opts.RefImagePaths...)
			materialized = append(materialized, opts.MaskPath)
			if len(opts.RefImagePaths) != 1 || string(mustReadDesignerTemp(t, opts.RefImagePaths[0])) != "reference-bytes" || string(mustReadDesignerTemp(t, opts.MaskPath)) != "mask-bytes" {
				t.Fatalf("materialized options = %#v", opts)
			}
			return &appimage.GenerateResult{}, nil
		}}, nil
	}
	svcB.ExecuteGeneration(ctx, genID)
	if len(materialized) != 2 {
		t.Fatalf("materialized paths = %#v", materialized)
	}
	for _, path := range materialized {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("temp path still exists after success: %s err=%v", path, err)
		}
	}
	if len(store.read) != 2 || store.read[0] == store.read[1] || store.readLimit[0] != maxDesignerReferenceBytes || store.readLimit[1] != maxDesignerReferenceBytes {
		t.Fatalf("bounded reads keys=%#v limits=%#v", store.read, store.readLimit)
	}
}

func TestDesignerReferenceRejectsUnownedOrMalformedIDsBeforeProvider(t *testing.T) {
	store := &designerReferenceStore{data: map[string][]byte{}}
	svc, db := setupDesignerReferenceService(t, store)
	ctx := context.Background()
	ownerID := uuid.NewString()
	otherID := uuid.NewString()
	ownedRef, err := svc.UploadReference(ctx, ownerID, "reference.png", "image/png", []byte("owner"))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		userID string
		fileID string
	}{
		{name: "wildcard", userID: ownerID, fileID: "*"},
		{name: "malformed", userID: ownerID, fileID: "not-a-uuid"},
		{name: "not found", userID: ownerID, fileID: uuid.NewString()},
		{name: "cross user exact id", userID: otherID, fileID: ownedRef},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refJSON, _ := json.Marshal([]string{tt.fileID})
			genID := uuid.NewString()
			if err := db.Create(&model.ImageGeneration{
				ID: genID, UserID: tt.userID, ProjectID: "default", Prompt: "edit", Provider: "openai", Model: "test",
				N: 1, Status: model.ImageGenerationStatusGenerating, ReferenceFiles: string(refJSON),
			}).Error; err != nil {
				t.Fatal(err)
			}
			providerCalls := 0
			svc.providerFactory = func(*appconfig.ImageAPI, *zerolog.Logger) (appimage.Provider, error) {
				providerCalls++
				return designerReferenceProvider{generate: func(*appimage.GenerateOptions) (*appimage.GenerateResult, error) {
					return &appimage.GenerateResult{}, nil
				}}, nil
			}
			svc.ExecuteGeneration(ctx, genID)
			if providerCalls != 0 {
				t.Fatalf("provider created %d times for rejected file_id %q", providerCalls, tt.fileID)
			}
			var gen model.ImageGeneration
			if err := db.First(&gen, "id = ?", genID).Error; err != nil {
				t.Fatal(err)
			}
			if gen.Status != model.ImageGenerationStatusFailed {
				t.Fatalf("generation status = %q, want failed", gen.Status)
			}
		})
	}
}

func TestDesignerReferenceRejectsMalformedPersistedContractBeforeProvider(t *testing.T) {
	store := &designerReferenceStore{data: map[string][]byte{}}
	svc, db := setupDesignerReferenceService(t, store)
	ctx := context.Background()
	genID := uuid.NewString()
	if err := db.Create(&model.ImageGeneration{
		ID: genID, UserID: uuid.NewString(), ProjectID: "default", Prompt: "edit", Provider: "openai", Model: "test",
		N: 1, Status: model.ImageGenerationStatusGenerating, ReferenceFiles: `{"broken"`,
	}).Error; err != nil {
		t.Fatal(err)
	}
	providerCalls := 0
	svc.providerFactory = func(*appconfig.ImageAPI, *zerolog.Logger) (appimage.Provider, error) {
		providerCalls++
		return designerReferenceProvider{generate: func(*appimage.GenerateOptions) (*appimage.GenerateResult, error) {
			return &appimage.GenerateResult{}, nil
		}}, nil
	}

	svc.ExecuteGeneration(ctx, genID)
	if providerCalls != 0 {
		t.Fatalf("provider created %d times for malformed persisted reference contract", providerCalls)
	}
	var gen model.ImageGeneration
	if err := db.First(&gen, "id = ?", genID).Error; err != nil {
		t.Fatal(err)
	}
	if gen.Status != model.ImageGenerationStatusFailed || gen.Error != "designer reference is invalid or unavailable" {
		t.Fatalf("generation status=%q error=%q; want generic reference failure", gen.Status, gen.Error)
	}
}

func TestDesignerReferenceTempFilesRemovedAfterProviderError(t *testing.T) {
	store := &designerReferenceStore{data: map[string][]byte{}}
	svc, db := setupDesignerReferenceService(t, store)
	ctx := context.Background()
	userID := uuid.NewString()
	fileID, err := svc.UploadReference(ctx, userID, "reference.png", "image/png", []byte("reference"))
	if err != nil {
		t.Fatal(err)
	}
	refJSON, _ := json.Marshal([]string{fileID})
	genID := uuid.NewString()
	if err := db.Create(&model.ImageGeneration{
		ID: genID, UserID: userID, ProjectID: "default", Prompt: "edit", Provider: "openai", Model: "test",
		N: 1, Status: model.ImageGenerationStatusGenerating, ReferenceFiles: string(refJSON),
	}).Error; err != nil {
		t.Fatal(err)
	}
	var tempPath string
	svc.providerFactory = func(*appconfig.ImageAPI, *zerolog.Logger) (appimage.Provider, error) {
		return designerReferenceProvider{generate: func(opts *appimage.GenerateOptions) (*appimage.GenerateResult, error) {
			tempPath = opts.RefImagePaths[0]
			if _, err := os.Stat(tempPath); err != nil {
				t.Fatalf("temp unavailable during provider call: %v", err)
			}
			return nil, errors.New("provider failed")
		}}, nil
	}
	svc.ExecuteGeneration(ctx, genID)
	if tempPath == "" {
		t.Fatal("provider did not receive temp path")
	}
	if _, err := os.Stat(tempPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temp path still exists after provider error: %s err=%v", tempPath, err)
	}
}

func TestDesignerReferenceBackendErrorsAreRedacted(t *testing.T) {
	store := &designerReferenceStore{data: map[string][]byte{}}
	svc, db := setupDesignerReferenceService(t, store)
	ctx := context.Background()
	userID := uuid.NewString()
	fileID, err := svc.UploadReference(ctx, userID, "reference.png", "image/png", []byte("reference"))
	if err != nil {
		t.Fatal(err)
	}
	refJSON, _ := json.Marshal([]string{fileID})
	genID := uuid.NewString()
	if err := db.Create(&model.ImageGeneration{
		ID: genID, UserID: userID, ProjectID: "default", Prompt: "edit", Provider: "openai", Model: "test",
		N: 1, Status: model.ImageGenerationStatusGenerating, ReferenceFiles: string(refJSON),
	}).Error; err != nil {
		t.Fatal(err)
	}
	const backendSecret = "OSS endpoint secret: request signature"
	store.readErr = errors.New(backendSecret)
	providerCalls := 0
	svc.providerFactory = func(*appconfig.ImageAPI, *zerolog.Logger) (appimage.Provider, error) {
		providerCalls++
		return nil, errors.New("must not be called")
	}
	svc.ExecuteGeneration(ctx, genID)
	if providerCalls != 0 {
		t.Fatalf("provider created %d times after storage failure", providerCalls)
	}
	var gen model.ImageGeneration
	if err := db.First(&gen, "id = ?", genID).Error; err != nil {
		t.Fatal(err)
	}
	if gen.Status != model.ImageGenerationStatusFailed || strings.Contains(gen.Error, backendSecret) {
		t.Fatalf("generation leaked backend error: status=%q error=%q", gen.Status, gen.Error)
	}
}

func TestDesignerCreateGenerationRejectsInvalidOrUnownedReferencesBeforeSideEffects(t *testing.T) {
	tests := []struct {
		name          string
		referenceID   func(ownerReferenceID string) string
		maskID        func(ownerReferenceID string) string
		mappingUserID func(requestUserID string) string
	}{
		{name: "reference wildcard", referenceID: func(string) string { return "*" }},
		{name: "reference malformed", referenceID: func(string) string { return "not-a-uuid" }},
		{name: "reference missing", referenceID: func(string) string { return uuid.NewString() }},
		{
			name:          "reference cross user",
			referenceID:   func(ownerReferenceID string) string { return ownerReferenceID },
			mappingUserID: func(string) string { return uuid.NewString() },
		},
		{name: "mask malformed", maskID: func(string) string { return "not-a-uuid" }},
		{name: "mask missing", maskID: func(string) string { return uuid.NewString() }},
		{
			name:          "mask cross user",
			maskID:        func(ownerReferenceID string) string { return ownerReferenceID },
			mappingUserID: func(string) string { return uuid.NewString() },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, db := setupDesignerBillingTest(t)
			ctx := context.Background()
			requestUserID := createCreditTestUser(t, repo, 5000)
			mappingUserID := requestUserID
			if tt.mappingUserID != nil {
				mappingUserID = tt.mappingUserID(requestUserID)
			}
			ownerReferenceID := uuid.NewString()
			if err := repository.NewDesignerReferenceRepository(db).Create(ctx, &model.DesignerReference{
				ID: ownerReferenceID, UserID: mappingUserID, StorageKey: mappingUserID + "/designer/reference.png",
				FileName: "reference.png", ContentType: "image/png", Size: 9,
			}); err != nil {
				t.Fatalf("create reference mapping: %v", err)
			}

			req := DesignerGenerateRequest{
				ProjectID: "default", Prompt: "edit", ProviderID: "gpt_image_2",
				Quality: "medium", Size: "1024x1024", N: 1,
			}
			if tt.referenceID != nil {
				req.ReferenceFileIDs = []string{tt.referenceID(ownerReferenceID)}
			}
			if tt.maskID != nil {
				req.MaskFileID = tt.maskID(ownerReferenceID)
			}

			created, err := svc.CreateGenerationRecord(ctx, requestUserID, req)
			if err == nil || created != nil {
				t.Fatalf("CreateGenerationRecord() = %#v, %v; want reference rejection", created, err)
			}
			balance, balanceErr := svc.creditSvc.GetBalance(ctx, requestUserID)
			if balanceErr != nil || balance != 5000 {
				t.Fatalf("credit balance = %d, err=%v; want unchanged 5000", balance, balanceErr)
			}
			var generationCount int64
			if err := db.Model(&model.ImageGeneration{}).Count(&generationCount).Error; err != nil || generationCount != 0 {
				t.Fatalf("generation count = %d, err=%v; want 0", generationCount, err)
			}
			var transactionCount int64
			if err := db.Model(&model.CreditTransaction{}).Count(&transactionCount).Error; err != nil || transactionCount != 0 {
				t.Fatalf("credit transaction count = %d, err=%v; want 0", transactionCount, err)
			}
		})
	}
}

func TestDesignerCreateGenerationPropagatesReferenceRepositoryFailureBeforeSideEffects(t *testing.T) {
	svc, repo, db := setupDesignerBillingTest(t)
	ctx := context.Background()
	userID := createCreditTestUser(t, repo, 5000)
	backendErr := errors.New("database connection sentinel")
	svc.referenceRepo = designerReferenceErrorRepository{err: backendErr}
	providerCalls := 0
	svc.providerFactory = func(*appconfig.ImageAPI, *zerolog.Logger) (appimage.Provider, error) {
		providerCalls++
		return nil, errors.New("provider must not be created")
	}

	created, err := svc.CreateGenerationRecord(ctx, userID, DesignerGenerateRequest{
		ProjectID: "default", Prompt: "edit", ProviderID: "gpt_image_2",
		Quality: "medium", Size: "1024x1024", N: 1,
		ReferenceFileIDs: []string{uuid.NewString()},
	})
	if created != nil || !errors.Is(err, backendErr) || errors.Is(err, ErrDesignerReferenceInvalid) {
		t.Fatalf("CreateGenerationRecord() = %#v, %v; want wrapped infrastructure error", created, err)
	}
	if providerCalls != 0 {
		t.Fatalf("provider created %d times after repository failure", providerCalls)
	}
	balance, balanceErr := svc.creditSvc.GetBalance(ctx, userID)
	if balanceErr != nil || balance != 5000 {
		t.Fatalf("credit balance = %d, err=%v; want unchanged 5000", balance, balanceErr)
	}
	var generationCount int64
	if err := db.Model(&model.ImageGeneration{}).Count(&generationCount).Error; err != nil || generationCount != 0 {
		t.Fatalf("generation count = %d, err=%v; want 0", generationCount, err)
	}
	var transactionCount int64
	if err := db.Model(&model.CreditTransaction{}).Count(&transactionCount).Error; err != nil || transactionCount != 0 {
		t.Fatalf("credit transaction count = %d, err=%v; want 0", transactionCount, err)
	}
}

func TestDesignerUploadReferenceFromLocalURLUsesBoundedRead(t *testing.T) {
	userID := uuid.NewString()
	key := userID + "/designer/result.png"
	store := &designerReferenceStore{data: map[string][]byte{key: []byte("image")}}
	svc, _ := setupDesignerReferenceService(t, store)

	if _, err := svc.UploadReferenceFromURL(context.Background(), userID, "/api/v1/files/"+key); err != nil {
		t.Fatalf("UploadReferenceFromURL: %v", err)
	}
	if len(store.readLimit) != 1 || store.readLimit[0] != maxDesignerReferenceBytes {
		t.Fatalf("bounded read limits = %#v, want [%d]", store.readLimit, maxDesignerReferenceBytes)
	}
}

func TestDesignerUploadReferenceFromRemoteURLRejectsOversizedResponseWithoutSideEffects(t *testing.T) {
	userID := uuid.NewString()
	store := &designerReferenceStore{data: map[string][]byte{}}
	svc, db := setupDesignerReferenceService(t, store)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = io.WriteString(w, strings.Repeat("x", int(maxDesignerReferenceBytes+1)))
	}))
	defer server.Close()

	before, err := filepath.Glob(filepath.Join(os.TempDir(), "anban-creator_download_0_*.png"))
	if err != nil {
		t.Fatal(err)
	}
	beforeSet := make(map[string]struct{}, len(before))
	for _, filePath := range before {
		beforeSet[filePath] = struct{}{}
	}

	_, err = svc.UploadReferenceFromURL(context.Background(), userID, server.URL+"/"+userID+"/designer/result.png")
	if !errors.Is(err, storage.ErrObjectExceedsMaxSize) {
		t.Fatalf("UploadReferenceFromURL error = %v, want oversized response", err)
	}
	if len(store.uploaded) != 0 {
		t.Fatalf("oversized response uploaded objects: %#v", store.uploaded)
	}
	var mappingCount int64
	if err := db.Model(&model.DesignerReference{}).Count(&mappingCount).Error; err != nil || mappingCount != 0 {
		t.Fatalf("designer reference mappings = %d, err=%v; want 0", mappingCount, err)
	}
	after, err := filepath.Glob(filepath.Join(os.TempDir(), "anban-creator_download_0_*.png"))
	if err != nil {
		t.Fatal(err)
	}
	for _, filePath := range after {
		if _, existed := beforeSet[filePath]; !existed {
			t.Fatalf("oversized response left temp file %s", filePath)
		}
	}
}

func mustReadDesignerTemp(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read temp %s: %v", path, err)
	}
	return data
}
