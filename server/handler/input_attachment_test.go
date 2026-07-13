package handler

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/service"
)

func TestValidateInputAttachmentsNormalizesAndFinalizes(t *testing.T) {
	const publicURL = "https://cdn.test/uploads/pending/user-1/upload-1/product.png"
	pending := &aiEntryPendingRepo{upload: &model.PendingUpload{
		ID:          "upload-1",
		UserID:      "user-1",
		Purpose:     service.DirectUploadPurposeAIEntryAttachment,
		Key:         "uploads/pending/user-1/upload-1/product.png",
		PublicURL:   publicURL,
		FileName:    "product.png",
		ContentType: "image/png",
		Status:      model.PendingUploadStatusPending,
		ExpiresAt:   time.Now().Add(time.Hour),
	}}

	got, err := validateInputAttachments(context.Background(), pending, "user-1", []model.EntryAttachment{{
		Type:        " image ",
		URL:         " " + publicURL + " ",
		FileName:    " product.png ",
		ContentType: " image/png ",
		UploadID:    " attacker-upload ",
		Key:         " uploads/pending/victim/attacker-upload/secret.png ",
		Instruction: "  保持包装和 Logo  ",
	}}, InputAttachmentValidationOptions{MaxCount: 16, AllowedTypes: map[string]bool{"image": true}})

	if err != nil {
		t.Fatalf("validate attachments: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("attachments = %#v, want one", got)
	}
	if got[0].Type != "image" {
		t.Fatalf("type = %q, want image", got[0].Type)
	}
	if got[0].URL != publicURL {
		t.Fatalf("url = %q, want %q", got[0].URL, publicURL)
	}
	if got[0].FileName != "product.png" {
		t.Fatalf("file name = %q", got[0].FileName)
	}
	if got[0].ContentType != "image/png" {
		t.Fatalf("content type = %q", got[0].ContentType)
	}
	if got[0].UploadID != "" || got[0].Key != "" {
		t.Fatalf("client storage authority persisted: %#v", got[0])
	}
	if got[0].Instruction != "保持包装和 Logo" {
		t.Fatalf("instruction = %q", got[0].Instruction)
	}
	if len(pending.finalizedIDs) != 1 || pending.finalizedIDs[0] != "upload-1" {
		t.Fatalf("finalized IDs = %#v, want upload-1", pending.finalizedIDs)
	}
}

func TestValidateInputAttachmentsAcceptsInstructionAt1000CodePoints(t *testing.T) {
	instruction := strings.Repeat("图", 1000)
	got, err := validateInputAttachments(context.Background(), nil, "user-1", []model.EntryAttachment{{
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
	_, err := validateInputAttachments(context.Background(), nil, "user-1", []model.EntryAttachment{{
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
	_, err := validateInputAttachments(context.Background(), nil, "user-1", images,
		InputAttachmentValidationOptions{MaxCount: 16, AllowedTypes: map[string]bool{"image": true}})
	if err == nil || err.Error() != "at most 16 attachments are allowed" {
		t.Fatalf("count error = %v", err)
	}

	_, err = validateInputAttachments(context.Background(), nil, "user-1", []model.EntryAttachment{{
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
			_, err := validateInputAttachments(context.Background(), nil, "user-1", []model.EntryAttachment{tt.attachment},
				InputAttachmentValidationOptions{MaxCount: 16, AllowedTypes: map[string]bool{"image": true}})
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
