package storage

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

var (
	ErrInvalidRuntimeStorageKey        = errors.New("runtime storage key is invalid")
	ErrPendingDirectUploadRuntimeKey   = errors.New("pending direct-upload key is not finalized")
	ErrInvalidFinalizedUploadKey       = errors.New("finalized direct-upload key is invalid")
	ErrFinalizedUploadIdentityMismatch = errors.New("finalized direct-upload identity mismatch")
)

type FinalizedUploadIdentity struct {
	UserID   string
	UploadID string
	FileName string
}

type RuntimeStorageKey struct {
	Key             string
	FinalizedUpload *FinalizedUploadIdentity
}

func ParseRuntimeStorageKey(raw string) (*RuntimeStorageKey, error) {
	key := strings.TrimSpace(raw)
	if key == "" || strings.Contains(key, "://") || strings.HasPrefix(key, "/") || strings.Contains(key, `\`) {
		return nil, ErrInvalidRuntimeStorageKey
	}
	clean := path.Clean(key)
	if clean != key || clean == "." || strings.HasPrefix(clean, "../") {
		return nil, ErrInvalidRuntimeStorageKey
	}
	if key == "uploads/pending" || strings.HasPrefix(key, "uploads/pending/") {
		return nil, ErrPendingDirectUploadRuntimeKey
	}
	parsed := &RuntimeStorageKey{Key: key}
	if key != "uploads/finalized" && !strings.HasPrefix(key, "uploads/finalized/") {
		return parsed, nil
	}
	parts := strings.Split(key, "/")
	if len(parts) != 5 || parts[0] != "uploads" || parts[1] != "finalized" || parts[2] == "" || parts[3] == "" || parts[4] == "" || path.Base(parts[2]) != parts[2] || path.Base(parts[3]) != parts[3] || path.Base(parts[4]) != parts[4] {
		return nil, ErrInvalidFinalizedUploadKey
	}
	parsed.FinalizedUpload = &FinalizedUploadIdentity{UserID: parts[2], UploadID: parts[3], FileName: parts[4]}
	return parsed, nil
}

func (k *RuntimeStorageKey) ValidateFinalizedUploadIdentity(userID, uploadID string) error {
	if k == nil || k.FinalizedUpload == nil {
		return nil
	}
	userID = strings.TrimSpace(userID)
	uploadID = strings.TrimSpace(uploadID)
	if userID == "" || k.FinalizedUpload.UserID != userID || (uploadID != "" && k.FinalizedUpload.UploadID != uploadID) {
		return fmt.Errorf("%w: key user=%q upload=%q", ErrFinalizedUploadIdentityMismatch, k.FinalizedUpload.UserID, k.FinalizedUpload.UploadID)
	}
	return nil
}
