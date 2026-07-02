package service

import (
	"context"
	"strings"
	"testing"
)

func TestPrepareFileUploadRejectsUnsupportedStorage(t *testing.T) {
	store := &fakeAudioASRStorage{name: "local"}

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
	store := &fakeAudioASRStorage{name: "oss"}

	_, err := PrepareFileUpload(context.Background(), store, FileUploadPrepareRequest{
		Purpose:     FileUploadPurposeVideoAudio,
		Filename:    "take.png",
		ContentType: "image/png",
	})
	if err == nil || !strings.Contains(err.Error(), "audio content_type is required") {
		t.Fatalf("PrepareFileUpload error = %v, want non-audio MIME rejection", err)
	}
}

func TestPrepareFileUploadAcceptsLiveAudioPurpose(t *testing.T) {
	store := &fakeAudioASRStorage{name: "oss"}

	result, err := PrepareFileUpload(context.Background(), store, FileUploadPrepareRequest{
		Purpose:     FileUploadPurposeLiveAudio,
		Filename:    "live.mp3",
		ContentType: "audio/mpeg",
	})
	if err != nil {
		t.Fatalf("PrepareFileUpload: %v", err)
	}
	if !strings.HasPrefix(result.Key, "uploads/live-audio/") {
		t.Fatalf("Key = %q, want live audio prefix", result.Key)
	}
	if result.Method != "PUT" || result.UploadURL == "" || result.DownloadURL == "" {
		t.Fatalf("prepared upload missing direct-upload fields: %#v", result)
	}
	if got := store.uploadContentType; got != "audio/mpeg" {
		t.Fatalf("UploadURL content type = %q, want audio/mpeg", got)
	}
}
