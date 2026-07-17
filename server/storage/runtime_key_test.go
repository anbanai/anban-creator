package storage

import (
	"errors"
	"testing"
)

func TestParseRuntimeStorageKeyValidatesDirectUploadIdentity(t *testing.T) {
	parsed, err := ParseRuntimeStorageKey("uploads/finalized/user-1/upload-1/input.png")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Key != "uploads/finalized/user-1/upload-1/input.png" || parsed.FinalizedUpload == nil || parsed.FinalizedUpload.UserID != "user-1" || parsed.FinalizedUpload.UploadID != "upload-1" || parsed.FinalizedUpload.FileName != "input.png" {
		t.Fatalf("parsed key = %#v", parsed)
	}
	if err := parsed.ValidateFinalizedUploadIdentity("user-1", "upload-1"); err != nil {
		t.Fatalf("matching identity: %v", err)
	}
	if err := parsed.ValidateFinalizedUploadIdentity("user-2", "upload-1"); !errors.Is(err, ErrFinalizedUploadIdentityMismatch) {
		t.Fatalf("user mismatch error = %v", err)
	}
	if err := parsed.ValidateFinalizedUploadIdentity("user-1", "upload-2"); !errors.Is(err, ErrFinalizedUploadIdentityMismatch) {
		t.Fatalf("upload mismatch error = %v", err)
	}
}

func TestParseRuntimeStorageKeyRejectsMutableAndMalformedDirectUploadKeys(t *testing.T) {
	tests := []struct {
		key     string
		wantErr error
	}{
		{key: "uploads/pending/user-1/upload-1/input.png", wantErr: ErrPendingDirectUploadRuntimeKey},
		{key: "uploads/pending", wantErr: ErrPendingDirectUploadRuntimeKey},
		{key: "uploads/finalized/user-1/upload-1/../input.png", wantErr: ErrInvalidRuntimeStorageKey},
		{key: `uploads/finalized/user-1/upload-1\input.png`, wantErr: ErrInvalidRuntimeStorageKey},
		{key: "/uploads/finalized/user-1/upload-1/input.png", wantErr: ErrInvalidRuntimeStorageKey},
		{key: "uploads/finalized/user-1/upload-1", wantErr: ErrInvalidFinalizedUploadKey},
		{key: "uploads/finalized//upload-1/input.png", wantErr: ErrInvalidRuntimeStorageKey},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if _, err := ParseRuntimeStorageKey(tt.key); !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseRuntimeStorageKeyAllowsCleanNonDirectInternalKey(t *testing.T) {
	parsed, err := ParseRuntimeStorageKey("uploads/users/user-1/projects/project-1/tasks/task-1/input.png")
	if err != nil || parsed.FinalizedUpload != nil {
		t.Fatalf("non-direct key = %#v, %v", parsed, err)
	}
}
