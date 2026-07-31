package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/service"
)

func TestInputAttachmentServiceErrorClassifiesObjectMetadataMismatch(t *testing.T) {
	err := inputAttachmentServiceError(service.ErrUploadSessionObjectInvalid)
	if !errors.Is(err, errInputAttachmentValidation) {
		t.Fatalf("error = %v, want input attachment validation error", err)
	}
}

func TestValidateInputAttachmentsRequiresUploadIDForGenericExistingKey(t *testing.T) {
	_, err := validateInputAttachments(context.Background(), nil, nil, "user-1", []model.EntryAttachment{{
		Type:        "image",
		Key:         "uploads/users/user-1/projects/project-1/tasks/task-1/inputs/source.png",
		FileName:    "source.png",
		ContentType: "image/png",
	}}, InputAttachmentValidationOptions{MaxCount: 16, AllowedTypes: allAgentAttachmentTypes})
	if err == nil || err.Error() != "attachment 1: storage attachment requires upload_id and key" {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateInputAttachmentsAcceptsOwnedAssetAndPreservesOrder(t *testing.T) {
	repo := uploadRepositoryFromSessions(t)
	ownedReference := &model.Asset{
		ID:          "owned-reference",
		UserID:      "user-1",
		Purpose:     service.DirectUploadPurposeTaskReference,
		StorageKey:  "assets/users/user-1/owned-reference/reference.png",
		FileName:    "reference.png",
		ContentType: "image/png",
		Size:        2048,
		ETag:        "etag-owned-reference",
	}
	if err := repo.Assets().Create(t.Context(), ownedReference); err != nil {
		t.Fatalf("create owned asset: %v", err)
	}

	got, err := validateInputAttachments(t.Context(), nil, repo, "user-1", []model.EntryAttachment{
		{Type: "text", Text: "first", FileName: "brief.txt"},
		{
			AssetID:     ownedReference.ID,
			Type:        "video",
			FileName:    "forged.mp4",
			ContentType: "video/mp4",
			Size:        1,
			Instruction: " use this composition ",
		},
	}, InputAttachmentValidationOptions{
		AllowedTypes:         allAgentAttachmentTypes,
		AllowedAssetPurposes: []string{service.DirectUploadPurposeTaskReference},
	})
	if err != nil {
		t.Fatalf("validate asset attachment: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("attachments = %#v, want two", got)
	}
	if got[0].FileName != "brief.txt" {
		t.Fatalf("first attachment = %#v, want brief.txt first", got[0])
	}
	if got[1].AssetID != ownedReference.ID || got[1].Type != "image" ||
		got[1].FileName != ownedReference.FileName || got[1].ContentType != ownedReference.ContentType || got[1].Size != ownedReference.Size {
		t.Fatalf("asset attachment = %#v, want server-owned metadata from %#v", got[1], ownedReference)
	}
	if got[1].URL != "" || got[1].Key != "" || got[1].UploadID != "" {
		t.Fatalf("asset attachment alternate identities = %#v, want asset_id only", got[1])
	}
	if got[1].Instruction != "use this composition" {
		t.Fatalf("instruction = %q, want normalized caller instruction", got[1].Instruction)
	}
	roundTripped, err := validateInputAttachments(t.Context(), nil, repo, "user-1", got, InputAttachmentValidationOptions{
		AllowedTypes:         allAgentAttachmentTypes,
		AllowedAssetPurposes: []string{service.DirectUploadPurposeTaskReference},
	})
	if err != nil {
		t.Fatalf("round-trip normalized asset attachment: %v", err)
	}
	if len(roundTripped) != 2 || roundTripped[1] != got[1] {
		t.Fatalf("round-trip attachments = %#v, want %#v", roundTripped, got)
	}
}

func TestValidateInputAttachmentsRejectsInvalidAssetIdentity(t *testing.T) {
	repo := uploadRepositoryFromSessions(t)
	assets := []*model.Asset{
		{
			ID: "foreign-reference", UserID: "user-2", Purpose: service.DirectUploadPurposeTaskReference,
			StorageKey: "assets/users/user-2/foreign-reference/reference.png", FileName: "reference.png",
			ContentType: "image/png", Size: 1024, ETag: "etag-foreign-reference",
		},
		{
			ID: "document-reference", UserID: "user-1", Purpose: service.DirectUploadPurposeTaskReference,
			StorageKey: "assets/users/user-1/document-reference/brief.pdf", FileName: "brief.pdf",
			ContentType: "application/pdf", Size: 1024, ETag: "etag-document-reference",
		},
		{
			ID: "project-reference", UserID: "user-1", Purpose: service.DirectUploadPurposeProjectReference,
			StorageKey: "assets/users/user-1/project-reference/reference.png", FileName: "reference.png",
			ContentType: "image/png", Size: 1024, ETag: "etag-project-reference",
		},
		{
			ID: "owned-reference", UserID: "user-1", Purpose: service.DirectUploadPurposeTaskReference,
			StorageKey: "assets/users/user-1/owned-reference/reference.png", FileName: "reference.png",
			ContentType: "image/png", Size: 1024, ETag: "etag-owned-reference",
		},
	}
	for _, asset := range assets {
		if err := repo.Assets().Create(t.Context(), asset); err != nil {
			t.Fatalf("create asset %s: %v", asset.ID, err)
		}
	}

	tests := []struct {
		name       string
		attachment model.EntryAttachment
		wantErr    string
	}{
		{name: "another user's asset", attachment: model.EntryAttachment{AssetID: "foreign-reference"}, wantErr: "reference asset identity is forbidden"},
		{name: "missing asset", attachment: model.EntryAttachment{AssetID: "missing-reference"}, wantErr: "reference asset identity is forbidden"},
		{name: "non-image asset", attachment: model.EntryAttachment{AssetID: "document-reference"}, wantErr: "reference asset metadata is invalid"},
		{name: "disallowed purpose", attachment: model.EntryAttachment{AssetID: "project-reference"}, wantErr: "reference asset purpose is not allowed"},
		{name: "asset id with url", attachment: model.EntryAttachment{AssetID: "owned-reference", URL: "/api/v1/files/forged.png"}, wantErr: "asset_id cannot be combined with url, key, or upload_id"},
		{name: "asset id with key", attachment: model.EntryAttachment{AssetID: "owned-reference", Key: "assets/users/user-1/forged.png"}, wantErr: "asset_id cannot be combined with url, key, or upload_id"},
		{name: "asset id with upload id", attachment: model.EntryAttachment{AssetID: "owned-reference", UploadID: "upload-1"}, wantErr: "asset_id cannot be combined with url, key, or upload_id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateInputAttachments(t.Context(), nil, repo, "user-1", []model.EntryAttachment{tt.attachment}, InputAttachmentValidationOptions{
				AllowedTypes:         allAgentAttachmentTypes,
				AllowedAssetPurposes: []string{service.DirectUploadPurposeTaskReference},
			})
			if err == nil || !errors.Is(err, errInputAttachmentValidation) || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want input attachment validation error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateInputAttachmentsNormalizesAndFinalizes(t *testing.T) {
	const key = "uploads/pending/user-1/upload-1/product.png"
	session := &model.UploadSession{
		ID:          "upload-1",
		UserID:      "user-1",
		Purpose:     service.DirectUploadPurposeAIEntryAttachment,
		StagingKey:  key,
		FileName:    "repository-product.png",
		ContentType: "image/png",
		Size:        2048,
		Status:      model.UploadSessionPending,
		ExpiresAt:   time.Now().Add(time.Hour),
	}

	uploadRepo := uploadRepositoryFromSessions(t, session)
	got, err := validateInputAttachments(context.Background(), uploadSessionStatStore(uploadRepo.UploadSessions()), uploadRepo, "user-1", []model.EntryAttachment{{
		Type:        " image ",
		URL:         " https://attacker.example/forged.exe?signature=secret ",
		FileName:    " forged.exe ",
		ContentType: " application/x-msdownload ",
		Size:        49 * 1024 * 1024,
		UploadID:    " upload-1 ",
		Key:         " " + key + " ",
		Instruction: "  保持包装和 Logo  ",
	}}, InputAttachmentValidationOptions{MaxCount: 16, AllowedTypes: allAgentAttachmentTypes})

	if err != nil {
		t.Fatalf("validate attachments: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("attachments = %#v, want one", got)
	}
	if got[0].Type != "image" {
		t.Fatalf("type = %q, want image", got[0].Type)
	}
	if got[0].URL != "" {
		t.Fatalf("url = %q, want empty key-first URL", got[0].URL)
	}
	if got[0].FileName != "repository-product.png" {
		t.Fatalf("file name = %q", got[0].FileName)
	}
	if got[0].ContentType != "image/png" {
		t.Fatalf("content type = %q", got[0].ContentType)
	}
	if got[0].Size != 2048 || got[0].UploadID != "upload-1" || got[0].Key != "assets/users/user-1/upload-1/repository-product.png" {
		t.Fatalf("repository storage metadata not persisted: %#v", got[0])
	}
	if got[0].Instruction != "保持包装和 Logo" {
		t.Fatalf("instruction = %q", got[0].Instruction)
	}
	assertFinalizedAsset(t, uploadRepo, "upload-1", "assets/users/user-1/upload-1/repository-product.png")
}

func TestValidateInputAttachmentsDoesNotFinalizeBeforeWholeCollectionValidates(t *testing.T) {
	const key = "uploads/pending/user-1/upload-1/product.png"
	session := &model.UploadSession{
		ID: "upload-1", UserID: "user-1", Purpose: service.DirectUploadPurposeAIEntryAttachment,
		StagingKey: key, FileName: "product.png", ContentType: "image/png", Size: 2048,
		Status: model.UploadSessionPending, ExpiresAt: time.Now().Add(time.Hour),
	}
	uploadRepo := uploadRepositoryFromSessions(t, session)

	_, err := validateInputAttachments(context.Background(), nil, uploadRepo, "user-1", []model.EntryAttachment{
		{UploadID: "upload-1", Key: key, Instruction: "use the product"},
		{Type: "image", URL: "https://attacker.example/invalid.png", FileName: "invalid.png", ContentType: "image/png"},
	}, InputAttachmentValidationOptions{MaxCount: 16, AllowedTypes: allAgentAttachmentTypes})
	if err == nil {
		t.Fatal("invalid later attachment was accepted")
	}
	found, findErr := uploadRepo.UploadSessions().FindByID(t.Context(), session.ID)
	if findErr != nil || found.Status != model.UploadSessionPending || found.AssetID != "" {
		t.Fatalf("first upload session changed before collection validation completed: %#v, %v", found, findErr)
	}
}

func TestValidateInputAttachmentsAcceptsInstructionAt1000CodePoints(t *testing.T) {
	instruction := strings.Repeat("图", 1000)
	got, err := validateInputAttachments(context.Background(), nil, nil, "user-1", []model.EntryAttachment{{
		Type:        "image",
		URL:         "/api/v1/files/product.png",
		FileName:    "product.png",
		ContentType: "image/png",
		Instruction: instruction,
	}}, InputAttachmentValidationOptions{MaxCount: 16, AllowedTypes: map[string]bool{"image": true}})

	if err != nil {
		t.Fatalf("validate attachments: %v", err)
	}
	if got[0].Instruction != instruction {
		t.Fatalf("instruction length = %d runes, want 1000", len([]rune(got[0].Instruction)))
	}
}

func TestValidateInputAttachmentsRejectsInstructionOver1000CodePoints(t *testing.T) {
	_, err := validateInputAttachments(context.Background(), nil, nil, "user-1", []model.EntryAttachment{{
		Type:        "image",
		URL:         "/api/v1/files/product.png",
		FileName:    "product.png",
		ContentType: "image/png",
		Instruction: strings.Repeat("图", 1001),
	}}, InputAttachmentValidationOptions{MaxCount: 16, AllowedTypes: map[string]bool{"image": true}})

	if err == nil || err.Error() != "attachment instruction must not exceed 1000 characters" {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateInputAttachmentsRejectsCountAndDisallowedType(t *testing.T) {
	images := make([]model.EntryAttachment, 17)
	for i := range images {
		images[i] = model.EntryAttachment{
			Type:        "image",
			URL:         fmt.Sprintf("/api/v1/files/%d.png", i),
			ContentType: "image/png",
		}
	}
	_, err := validateInputAttachments(context.Background(), nil, nil, "user-1", images,
		InputAttachmentValidationOptions{MaxCount: 16, AllowedTypes: map[string]bool{"image": true}})
	if err == nil || err.Error() != "at most 16 attachments are allowed" {
		t.Fatalf("count error = %v", err)
	}

	_, err = validateInputAttachments(context.Background(), nil, nil, "user-1", []model.EntryAttachment{{
		Type: "video", URL: "/api/v1/files/demo.mp4", ContentType: "video/mp4",
	}}, InputAttachmentValidationOptions{MaxCount: 16, AllowedTypes: map[string]bool{"image": true}})
	if err == nil || err.Error() != "attachment type video is not allowed" {
		t.Fatalf("type error = %v", err)
	}
}

func TestValidateInputAttachmentsRejectsInvalidMetadataAndURL(t *testing.T) {
	tests := []struct {
		name       string
		attachment model.EntryAttachment
		wantErr    string
	}{
		{
			name: "metadata",
			attachment: model.EntryAttachment{
				Type: "image", URL: "/api/v1/files/tool.exe", FileName: "tool.exe", ContentType: "application/x-msdownload",
			},
			wantErr: "attachment 1: attachment tool.exe has unsupported content type or extension",
		},
		{
			name: "url",
			attachment: model.EntryAttachment{
				Type: "image", URL: "file:///etc/passwd", FileName: "product.png", ContentType: "image/png",
			},
			wantErr: "attachment URLs must be internal file URLs or registered pending-upload URLs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateInputAttachments(context.Background(), nil, nil, "user-1", []model.EntryAttachment{tt.attachment},
				InputAttachmentValidationOptions{MaxCount: 16, AllowedTypes: map[string]bool{"image": true}})
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateInputAttachmentsRejectsTypeLimitsAndCrossTenantUpload(t *testing.T) {
	tests := []struct {
		name       string
		attachment model.EntryAttachment
		userID     string
		session    *model.UploadSession
		wantErr    string
	}{
		{
			name:       "media over 50 MiB",
			attachment: model.EntryAttachment{Type: "video", URL: "/api/v1/files/demo.mp4", FileName: "demo.mp4", ContentType: "video/mp4", Size: 50*1024*1024 + 1},
			userID:     "user-1", wantErr: "50 MB limit",
		},
		{
			name:       "document over 25 MiB",
			attachment: model.EntryAttachment{Type: "document", URL: "/api/v1/files/brief.pdf", FileName: "brief.pdf", ContentType: "application/pdf", Size: 25*1024*1024 + 1},
			userID:     "user-1", wantErr: "25 MB limit",
		},
		{
			name:       "text over 25 MiB",
			attachment: model.EntryAttachment{Type: "text", URL: "/api/v1/files/notes.txt", FileName: "notes.txt", ContentType: "text/plain", Size: 25*1024*1024 + 1},
			userID:     "user-1", wantErr: "25 MB limit",
		},
		{
			name:       "cross tenant upload session",
			attachment: model.EntryAttachment{UploadID: "upload-1", Key: "uploads/pending/owner/upload-1/product.png"},
			userID:     "other-user",
			session: &model.UploadSession{
				ID: "upload-1", UserID: "owner", Purpose: service.DirectUploadPurposeAIEntryAttachment,
				StagingKey: "uploads/pending/owner/upload-1/product.png", FileName: "product.png", ContentType: "image/png", Size: 1024,
				Status: model.UploadSessionPending, ExpiresAt: time.Now().Add(time.Hour),
			},
			wantErr: "upload session access denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uploadRepo := uploadRepositoryFromSessions(t, tt.session)
			_, err := validateInputAttachments(context.Background(), nil, uploadRepo, tt.userID, []model.EntryAttachment{tt.attachment},
				InputAttachmentValidationOptions{MaxCount: 16, AllowedTypes: allAgentAttachmentTypes})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateInputAttachmentsAppliesCallSpecificLimitToVerifiedUploads(t *testing.T) {
	session := &model.UploadSession{
		ID: "resume-video", UserID: "user-1", Purpose: service.DirectUploadPurposeAIEntryAttachment,
		StagingKey: "uploads/pending/user-1/resume-video/demo.mp4", FileName: "demo.mp4", ContentType: "video/mp4",
		Size: 25*1024*1024 + 1, Status: model.UploadSessionPending, ExpiresAt: time.Now().Add(time.Hour),
	}
	_, err := validateInputAttachments(context.Background(), nil, uploadRepositoryFromSessions(t, session), "user-1", []model.EntryAttachment{{
		UploadID: session.ID,
		Key:      session.StagingKey,
	}}, InputAttachmentValidationOptions{
		MaxCount:     maxAgentInputAttachments,
		MaxBytes:     maxTaskResumeFileBytes,
		AllowedTypes: allAgentAttachmentTypes,
	})
	if err == nil || !strings.Contains(err.Error(), "25 MB limit") {
		t.Fatalf("error = %v, want call-specific 25 MB limit", err)
	}
}

type handlerAttachmentRouteCase struct {
	name        string
	attachments string
}

func handlerAttachmentRouteRejectionCases(foreignUploadID, foreignKey string) []handlerAttachmentRouteCase {
	tooMany := make([]model.EntryAttachment, 17)
	for i := range tooMany {
		tooMany[i] = model.EntryAttachment{Type: "image", URL: fmt.Sprintf("/api/v1/files/%d.png", i), FileName: fmt.Sprintf("%d.png", i), ContentType: "image/png"}
	}
	tooManyJSON, _ := json.Marshal(tooMany)
	foreignJSON, _ := json.Marshal([]model.EntryAttachment{{UploadID: foreignUploadID, Key: foreignKey}})
	return []handlerAttachmentRouteCase{
		{name: "mime extension conflict", attachments: `[{"type":"image","url":"/api/v1/files/brief.pdf","file_name":"brief.pdf","content_type":"application/pdf"}]`},
		{name: "application ogg mime on mp3 extension", attachments: `[{"type":"audio","url":"/api/v1/files/voice.mp3","file_name":"voice.mp3","content_type":"application/ogg"}]`},
		{name: "application csv mime on txt extension", attachments: `[{"type":"text","url":"/api/v1/files/notes.txt","file_name":"notes.txt","content_type":"application/csv"}]`},
		{name: "audio ogg mime on mp3 extension", attachments: `[{"type":"audio","url":"/api/v1/files/voice.mp3","file_name":"voice.mp3","content_type":"audio/ogg"}]`},
		{name: "audio mpeg mime on ogg extension", attachments: `[{"type":"audio","url":"/api/v1/files/voice.ogg","file_name":"voice.ogg","content_type":"audio/mpeg"}]`},
		{name: "text csv mime on txt extension", attachments: `[{"type":"text","url":"/api/v1/files/notes.txt","file_name":"notes.txt","content_type":"text/csv"}]`},
		{name: "seventeen attachments", attachments: string(tooManyJSON)},
		{name: "media over 50 MiB", attachments: `[{"type":"video","url":"/api/v1/files/demo.mp4","file_name":"demo.mp4","content_type":"video/mp4","size":52428801}]`},
		{name: "document over 25 MiB", attachments: `[{"type":"document","url":"/api/v1/files/brief.pdf","file_name":"brief.pdf","content_type":"application/pdf","size":26214401}]`},
		{name: "text over 25 MiB", attachments: `[{"type":"text","url":"/api/v1/files/notes.txt","file_name":"notes.txt","content_type":"text/plain","size":26214401}]`},
		{name: "foreign tenant upload", attachments: string(foreignJSON)},
	}
}

const fiveTypeHandlerAttachmentsJSON = `[
	{"type":"image","url":"/api/v1/files/product.png","file_name":"product.png","content_type":"image/png"},
	{"type":"audio","url":"/api/v1/files/voice.ogg","file_name":"voice.ogg","content_type":"application/ogg"},
	{"type":"video","url":"/api/v1/files/demo.mp4","file_name":"demo.mp4","content_type":"video/mp4"},
	{"type":"document","url":"/api/v1/files/brief.pdf","file_name":"brief.pdf","content_type":"application/pdf"},
	{"type":"text","url":"/api/v1/files/notes.csv","file_name":"notes.csv","content_type":"application/csv"}
]`
