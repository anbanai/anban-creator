package service

import (
	"context"
	"strings"
	"testing"
)

func TestPrepareFileUploadRejectsUnsupportedStorage(t *testing.T) {
	store := &fakeVideoASRStorage{name: "local"}

	_, err := PrepareFileUpload(context.Background(), store, FileUploadPrepareRequest{
		Purpose:     FileUploadPurposeVideoAudio,
		Filename:    "take.wav",
		ContentType: "audio/wav",
	})
	if err == nil || !strings.Contains(err.Error(), "requires OSS storage") {
		t.Fatalf("PrepareFileUpload error = %v, want unsupported storage rejection", err)
	}
}

func TestPrepareFileUploadRejectsNonAudioMIME(t *testing.T) {
	store := &fakeVideoASRStorage{name: "oss"}

	_, err := PrepareFileUpload(context.Background(), store, FileUploadPrepareRequest{
		Purpose:     FileUploadPurposeVideoAudio,
		Filename:    "take.png",
		ContentType: "image/png",
	})
	if err == nil || !strings.Contains(err.Error(), "audio content_type is required") {
		t.Fatalf("PrepareFileUpload error = %v, want non-audio MIME rejection", err)
	}
}
