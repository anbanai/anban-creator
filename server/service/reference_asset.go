package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
)

var (
	ErrReferenceImageSelectionInvalid       = errors.New("reference image selection is invalid")
	ErrReferenceAssetForbidden              = errors.New("reference asset identity is forbidden")
	ErrReferenceAssetPurposeMismatch        = errors.New("reference asset purpose is not allowed")
	ErrReferenceAssetConcurrentFinalization = errors.New("reference asset finalization is in progress")
	ErrReferenceAssetExpired                = errors.New("reference asset upload has expired")
	ErrReferenceAssetInvalidMetadata        = errors.New("reference asset metadata is invalid")
	ErrReferenceAssetUnavailable            = errors.New("reference asset dependency is unavailable")
	errReferenceAssetEmptyDownloadURL       = errors.New("storage signer returned an empty download URL")
)

// ReferenceImageSelection identifies either an immutable asset or an upload
// session that must be finalized before a business record is written.
type ReferenceImageSelection struct {
	AssetID         string `json:"asset_id,omitempty"`
	UploadSessionID string `json:"upload_session_id,omitempty"`
}

func (s ReferenceImageSelection) Validate() error {
	hasAsset := strings.TrimSpace(s.AssetID) != ""
	hasSession := strings.TrimSpace(s.UploadSessionID) != ""
	if hasAsset == hasSession {
		return ErrReferenceImageSelectionInvalid
	}
	return nil
}

type ReferenceAssetService struct {
	repo  repository.Repository
	store storage.Provider
	now   func() time.Time
}

func NewReferenceAssetService(repo repository.Repository, store storage.Provider, now func() time.Time) *ReferenceAssetService {
	if now == nil {
		now = time.Now
	}
	return &ReferenceAssetService{repo: repo, store: store, now: now}
}

func (s *ReferenceAssetService) ResolveSelection(ctx context.Context, userID string, in ReferenceImageSelection, allowed []string) (string, error) {
	if err := in.Validate(); err != nil {
		return "", err
	}
	userID = strings.TrimSpace(userID)
	if assetID := strings.TrimSpace(in.AssetID); assetID != "" {
		asset, err := s.RequireOwned(ctx, userID, assetID, allowed)
		if err != nil {
			return "", err
		}
		return asset.ID, nil
	}
	if s == nil || s.repo == nil {
		return "", ErrReferenceAssetUnavailable
	}

	sessionID := strings.TrimSpace(in.UploadSessionID)
	session, err := s.repo.UploadSessions().FindByID(ctx, sessionID)
	if err != nil {
		if errors.Is(err, model.ErrUploadSessionNotFound) {
			return "", ErrReferenceAssetForbidden
		}
		return "", fmt.Errorf("%w: find upload session: %w", ErrReferenceAssetUnavailable, err)
	}
	if session.UserID != userID {
		return "", ErrReferenceAssetForbidden
	}
	if !directUploadPurposeAllowed(session.Purpose, allowed) {
		return "", ErrReferenceAssetPurposeMismatch
	}
	if strings.TrimSpace(session.StagingKey) == "" {
		return "", ErrReferenceAssetInvalidMetadata
	}
	if err := validateReferenceImageFileMetadata(session.FileName, session.ContentType, session.Size); err != nil {
		return "", err
	}

	finalStore, ok := s.store.(DirectUploadFinalizationStorage)
	if !ok || finalStore == nil {
		return "", ErrReferenceAssetUnavailable
	}
	asset, err := FinalizeUploadSession(ctx, finalStore, s.repo, FinalizeUploadRequest{
		SessionID: session.ID, UserID: userID, AllowedPurposes: allowed, Now: s.now(),
	})
	if err != nil {
		return "", mapReferenceFinalizationError(err)
	}
	if err := validateOwnedReferenceAsset(asset, userID, allowed); err != nil {
		return "", err
	}
	return asset.ID, nil
}

func (s *ReferenceAssetService) RequireOwned(ctx context.Context, userID, assetID string, allowed []string) (*model.Asset, error) {
	if s == nil || s.repo == nil {
		return nil, ErrReferenceAssetUnavailable
	}
	userID = strings.TrimSpace(userID)
	assetID = strings.TrimSpace(assetID)
	if userID == "" || assetID == "" {
		return nil, ErrReferenceAssetForbidden
	}
	asset, err := s.repo.Assets().FindOwnedByID(ctx, assetID, userID)
	if err != nil {
		if errors.Is(err, model.ErrAssetNotFound) {
			return nil, ErrReferenceAssetForbidden
		}
		return nil, fmt.Errorf("%w: find asset: %w", ErrReferenceAssetUnavailable, err)
	}
	if err := validateOwnedReferenceAsset(asset, userID, allowed); err != nil {
		return nil, err
	}
	return asset, nil
}

func (s *ReferenceAssetService) Present(ctx context.Context, userID, assetID string, allowed []string) (*model.AssetView, error) {
	if strings.TrimSpace(assetID) == "" {
		return nil, nil
	}
	asset, err := s.RequireOwned(ctx, userID, assetID, allowed)
	if err != nil {
		return nil, err
	}
	if s.store == nil {
		return nil, ErrReferenceAssetUnavailable
	}
	now := s.now()
	downloadURL, err := s.store.DownloadURL(ctx, asset.StorageKey, DefaultSignedURLTTL)
	if err != nil {
		return nil, fmt.Errorf("%w: sign asset download: %w", ErrReferenceAssetUnavailable, err)
	}
	if strings.TrimSpace(downloadURL) == "" {
		return nil, fmt.Errorf("%w: sign asset download: %w", ErrReferenceAssetUnavailable, errReferenceAssetEmptyDownloadURL)
	}
	return &model.AssetView{
		AssetID: asset.ID, FileName: asset.FileName, ContentType: asset.ContentType, Size: asset.Size,
		DownloadURL: downloadURL, DownloadExpiresAt: now.Add(time.Duration(DefaultSignedURLTTL) * time.Second),
	}, nil
}

func validateOwnedReferenceAsset(asset *model.Asset, userID string, allowed []string) error {
	if asset == nil || asset.UserID != userID {
		return ErrReferenceAssetForbidden
	}
	if !directUploadPurposeAllowed(asset.Purpose, allowed) {
		return ErrReferenceAssetPurposeMismatch
	}
	if strings.TrimSpace(asset.StorageKey) == "" || strings.TrimSpace(asset.ETag) == "" {
		return ErrReferenceAssetInvalidMetadata
	}
	return validateReferenceImageFileMetadata(asset.FileName, asset.ContentType, asset.Size)
}

func validateReferenceImageFileMetadata(fileName, contentType string, size int64) error {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(fileName)))
	if size <= 0 || size > maxUploadImageBytes || !isDirectUploadImage(contentType, ext) {
		return ErrReferenceAssetInvalidMetadata
	}
	return nil
}

func mapReferenceFinalizationError(err error) error {
	referenceErr := ErrReferenceAssetUnavailable
	switch {
	case errors.Is(err, ErrUploadSessionAccessDenied):
		referenceErr = ErrReferenceAssetForbidden
	case errors.Is(err, ErrUploadSessionExpired):
		referenceErr = ErrReferenceAssetExpired
	case errors.Is(err, ErrUploadSessionStateConflict):
		referenceErr = ErrReferenceAssetConcurrentFinalization
	case errors.Is(err, ErrUploadSessionObjectInvalid):
		referenceErr = ErrReferenceAssetInvalidMetadata
	case errors.Is(err, ErrUploadSessionUnavailable):
		referenceErr = ErrReferenceAssetUnavailable
	}
	return fmt.Errorf("%w: finalize upload session: %w", referenceErr, err)
}
