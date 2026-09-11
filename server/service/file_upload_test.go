package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/auth"
	"github.com/anbanai/anban-creator/server/storage"
)

type fileUploadProviderWithoutLength struct{ storage.Provider }

func TestPrepareFileUploadRejectsUnsupportedStorage(t *testing.T) {
	store := &fakeTaskStorage{name: "local"}

	_, err := PrepareFileUpload(context.Background(), store, FileUploadPrepareRequest{
		UserID: "user-1", ProjectID: "project-1", TaskID: "task-1", Size: 1234,
		Purpose:     FileUploadPurposeLiveAudio,
		Filename:    "take.wav",
		ContentType: "audio/wav",
	})
	if err == nil || !strings.Contains(err.Error(), "requires OSS storage") {
		t.Fatalf("PrepareFileUpload error = %v, want unsupported storage rejection", err)
	}
}

func TestPrepareFileUploadRejectsNonAudioMIME(t *testing.T) {
	store := &fakeTaskStorage{name: "oss"}

	_, err := PrepareFileUpload(context.Background(), store, FileUploadPrepareRequest{
		UserID: "user-1", ProjectID: "project-1", TaskID: "task-1", Size: 1234,
		Purpose:     FileUploadPurposeLiveAudio,
		Filename:    "take.png",
		ContentType: "image/png",
	})
	if err == nil || !strings.Contains(err.Error(), "audio content_type is required") {
		t.Fatalf("PrepareFileUpload error = %v, want non-audio MIME rejection", err)
	}
}

func TestPrepareFileUploadAcceptsLiveAudioPurpose(t *testing.T) {
	store := &fakeTaskStorage{name: "oss"}

	result, err := PrepareFileUpload(context.Background(), store, FileUploadPrepareRequest{
		UserID: "user-1", ProjectID: "project-1", TaskID: "task-1", Size: 1234,
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
	for _, segment := range []string{"user-1", "project-1", "task-1"} {
		if !strings.Contains(result.Key, "/"+segment+"/") {
			t.Fatalf("Key = %q, missing ownership segment %q", result.Key, segment)
		}
	}
	if result.Method != "PUT" || result.UploadURL == "" || result.DownloadURL == "" {
		t.Fatalf("prepared upload missing direct-upload fields: %#v", result)
	}
	if got := store.uploadContentType; got != "audio/mpeg" {
		t.Fatalf("UploadURL content type = %q, want audio/mpeg", got)
	}
	if got := store.uploadContentLength; got != 1234 {
		t.Fatalf("UploadURL content length = %d, want 1234", got)
	}
	if got := result.Headers["Content-Length"]; got != "1234" {
		t.Fatalf("Content-Length header = %q, want 1234", got)
	}
	if result.Size != 1234 || result.MaxSize != maxExecutionLiveAudioBytes {
		t.Fatalf("size bounds = %d/%d, want 1234/%d", result.Size, result.MaxSize, maxExecutionLiveAudioBytes)
	}
}

func TestFileUploadServicePreparesTypedRequest(t *testing.T) {
	store := &fakeTaskStorage{name: "oss"}
	svc := NewFileUploadService(store)

	result, err := svc.Prepare(context.Background(), FileUploadPrepareRequest{
		UserID: "user-1", ProjectID: "project-1", TaskID: "task-1", Size: 1234,
		Purpose:     FileUploadPurposeLiveAudio,
		Filename:    "live.mp3",
		ContentType: "audio/mpeg",
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if !strings.HasPrefix(result.Key, "uploads/live-audio/") || result.Method != "PUT" {
		t.Fatalf("prepared upload = %#v", result)
	}
}

func TestPrepareFileUploadRejectsMissingIdentity(t *testing.T) {
	_, err := PrepareFileUpload(context.Background(), &fakeTaskStorage{name: "oss"}, FileUploadPrepareRequest{
		Purpose: FileUploadPurposeLiveAudio, Filename: "live.mp3", ContentType: "audio/mpeg", Size: 1,
	})
	if err == nil || !strings.Contains(err.Error(), "upload identity is incomplete") {
		t.Fatalf("PrepareFileUpload error = %v, want missing identity rejection", err)
	}
}

func TestPrepareFileUploadRejectsMissingOrOversizedFile(t *testing.T) {
	base := FileUploadPrepareRequest{
		UserID: "user-1", ProjectID: "project-1", TaskID: "task-1",
		Purpose: FileUploadPurposeLiveAudio, Filename: "live.mp3", ContentType: "audio/mpeg",
	}
	for _, tc := range []struct {
		name string
		size int64
		want string
	}{
		{name: "missing", size: 0, want: "file size is required"},
		{name: "oversized", size: maxExecutionLiveAudioBytes + 1, want: "file size exceeds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := base
			req.Size = tc.size
			_, err := PrepareFileUpload(context.Background(), &fakeTaskStorage{name: "oss"}, req)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("PrepareFileUpload error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestPrepareFileUploadRejectsStorageWithoutLengthBinding(t *testing.T) {
	base := &fakeTaskStorage{name: "oss"}
	_, err := PrepareFileUpload(context.Background(), &fileUploadProviderWithoutLength{Provider: base}, FileUploadPrepareRequest{
		UserID: "user-1", ProjectID: "project-1", TaskID: "task-1", Size: 1,
		Purpose: FileUploadPurposeLiveAudio, Filename: "live.mp3", ContentType: "audio/mpeg",
	})
	if err == nil || !strings.Contains(err.Error(), "cannot bind upload size") {
		t.Fatalf("PrepareFileUpload error = %v, want size-binding rejection", err)
	}
}

func TestPrepareFileUploadRejectsUnsafeIdentitySegment(t *testing.T) {
	_, err := PrepareFileUpload(context.Background(), &fakeTaskStorage{name: "oss"}, FileUploadPrepareRequest{
		UserID: "user/escape", ProjectID: "project-1", TaskID: "task-1", Size: 1,
		Purpose: FileUploadPurposeLiveAudio, Filename: "live.mp3", ContentType: "audio/mpeg",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid upload user_id") {
		t.Fatalf("PrepareFileUpload error = %v, want unsafe identity rejection", err)
	}
}

func TestPrepareFileUploadDoesNotEmbedUnsafeFilenameExtension(t *testing.T) {
	store := &fakeTaskStorage{name: "oss"}
	result, err := PrepareFileUpload(context.Background(), store, FileUploadPrepareRequest{
		UserID: "user-1", ProjectID: "project-1", TaskID: "task-1", Size: 1,
		Purpose: FileUploadPurposeLiveAudio, Filename: "audio.mp3\\evil", ContentType: "audio/mpeg",
	})
	if err != nil {
		t.Fatalf("PrepareFileUpload: %v", err)
	}
	if strings.ContainsAny(result.Key, "\\\x00") || !safeUploadExtension(result.Key[strings.LastIndex(result.Key, "."):]) {
		t.Fatalf("Key = %q, want safe MIME-derived extension", result.Key)
	}
}

func TestPrepareFileUploadBoundsSignedURLLifetimeToExecutionCredential(t *testing.T) {
	store := &fakeTaskStorage{name: "oss"}
	maxTTL := int(auth.MaximumExecutionTokenLifetime / time.Second)

	result, err := PrepareFileUpload(context.Background(), store, FileUploadPrepareRequest{
		UserID: "user-1", ProjectID: "project-1", TaskID: "task-1", Size: 1,
		Purpose: FileUploadPurposeLiveAudio, Filename: "live.mp3", ContentType: "audio/mpeg",
		ExpiresSeconds: maxTTL,
	})
	if err != nil {
		t.Fatalf("PrepareFileUpload at maximum TTL: %v", err)
	}
	if result.ExpiresSecond != maxTTL {
		t.Fatalf("ExpiresSecond = %d, want %d", result.ExpiresSecond, maxTTL)
	}

	if _, err := PrepareFileUpload(context.Background(), store, FileUploadPrepareRequest{
		UserID: "user-1", ProjectID: "project-1", TaskID: "task-1", Size: 1,
		Purpose: FileUploadPurposeLiveAudio, Filename: "live.mp3", ContentType: "audio/mpeg",
		ExpiresSeconds: maxTTL + 1,
	}); err == nil || !strings.Contains(err.Error(), "execution limit") {
		t.Fatalf("PrepareFileUpload over maximum TTL error = %v, want execution limit rejection", err)
	}
}
