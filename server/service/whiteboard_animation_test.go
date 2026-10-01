package service

import (
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestValidateWhiteboardAnimationInputsRequiresOneReadableSRT(t *testing.T) {
	valid := model.EntryAttachment{Type: "document", FileName: "captions.srt", UploadID: "upload", Key: "key"}
	for _, test := range []struct {
		name        string
		attachments []model.EntryAttachment
		wantErr     bool
	}{
		{name: "uploaded SRT with optional style reference", attachments: []model.EntryAttachment{valid, {Type: "image", FileName: "style.png", UploadID: "image", Key: "style"}}},
		{name: "inline text SRT", attachments: []model.EntryAttachment{{Type: "text", FileName: "captions.srt", Text: "1\\n00:00:00,000 --> 00:00:01,000\\nHello"}}},
		{name: "missing SRT", attachments: []model.EntryAttachment{{Type: "image", FileName: "style.png", UploadID: "image"}}, wantErr: true},
		{name: "multiple SRT files", attachments: []model.EntryAttachment{valid, valid}, wantErr: true},
		{name: "multiple style references", attachments: []model.EntryAttachment{valid, {Type: "image", FileName: "style-a.png", UploadID: "image-a"}, {Type: "image", FileName: "style-b.png", UploadID: "image-b"}}, wantErr: true},
		{name: "unsupported extra attachment", attachments: []model.EntryAttachment{valid, {Type: "audio", FileName: "voice.mp3", UploadID: "audio"}}, wantErr: true},
		{name: "unsupported type", attachments: []model.EntryAttachment{{Type: "image", FileName: "captions.srt", UploadID: "image"}}, wantErr: true},
		{name: "empty attachment", attachments: []model.EntryAttachment{{Type: "document", FileName: "captions.srt"}}, wantErr: true},
		{name: "extension is case insensitive", attachments: []model.EntryAttachment{{Type: "document", FileName: "CAPTIONS.SRT", AssetID: "asset"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateWhiteboardAnimationInputs(test.attachments)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateWhiteboardAnimationInputs() error = %v, wantErr=%t", err, test.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "SRT") {
				t.Fatalf("error = %q, want actionable SRT validation", err)
			}
		})
	}
}
