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

// IsReferenceAssetError reports whether err belongs to the public reference asset error contract.
func IsReferenceAssetError(err error) bool {
	return errors.Is(err, ErrReferenceImageSelectionInvalid) ||
		errors.Is(err, ErrReferenceAssetPurposeMismatch) ||
		errors.Is(err, ErrReferenceAssetInvalidMetadata) ||
		errors.Is(err, ErrReferenceAssetForbidden) ||
		errors.Is(err, ErrReferenceAssetConcurrentFinalization) ||
		errors.Is(err, ErrReferenceAssetExpired) ||
		errors.Is(err, ErrReferenceAssetUnavailable)
}

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

func taskReferenceAssetID(task *model.Task) string {
	if task == nil {
		return ""
	}
	return strings.TrimSpace(task.ReferenceImageAssetID)
}

func projectStyleReferenceAssetID(task *model.Task) string {
	if task == nil || task.SkipReferenceImage {
		return ""
	}
	return strings.TrimSpace(task.ProjectSnapshot.Data().ReferenceImageAssetID)
}

func resolveTaskReferenceAsset(ctx context.Context, repo repository.Repository, task *model.Task) (*model.Asset, error) {
	return resolveOwnedReferenceAsset(ctx, repo, task, taskReferenceAssetID(task), []string{
		DirectUploadPurposeTaskReference,
		DirectUploadPurposeAIEntryAttachment,
	}, "task reference")
}

func resolveProjectStyleReferenceAsset(ctx context.Context, repo repository.Repository, task *model.Task) (*model.Asset, error) {
	return resolveOwnedReferenceAsset(ctx, repo, task, projectStyleReferenceAssetID(task), []string{
		DirectUploadPurposeProjectReference,
	}, "project style reference")
}

func resolveOwnedReferenceAsset(ctx context.Context, repo repository.Repository, task *model.Task, id string, allowed []string, role string) (*model.Asset, error) {
	if id == "" {
		return nil, nil
	}
	if repo == nil || task == nil {
		return nil, ErrReferenceAssetUnavailable
	}
	asset, err := NewReferenceAssetService(repo, nil, nil).RequireOwned(ctx, task.UserID, id, allowed)
	if err != nil {
		return nil, fmt.Errorf("resolve %s asset: %w", role, err)
	}
	return asset, nil
}

func resolveRuntimeReferenceAssets(ctx context.Context, repo repository.Repository, task *model.Task) (*model.Asset, *model.Asset, error) {
	taskAsset, err := resolveTaskReferenceAsset(ctx, repo, task)
	if err != nil {
		return nil, nil, err
	}
	projectStyleAsset, err := resolveProjectStyleReferenceAsset(ctx, repo, task)
	if err != nil {
		return nil, nil, err
	}
	return taskAsset, projectStyleAsset, nil
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
	if err := validateReferenceImageFileMetadata(session.Purpose, session.FileName, session.ContentType, session.Size); err != nil {
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
	return s.requireOwned(ctx, userID, assetID, allowed, validateOwnedReferenceAsset)
}

func (s *ReferenceAssetService) RequireOwnedAttachment(ctx context.Context, userID, assetID string, allowed []string) (*model.Asset, error) {
	return s.requireOwned(ctx, userID, assetID, allowed, validateOwnedAttachmentAsset)
}

func (s *ReferenceAssetService) requireOwned(ctx context.Context, userID, assetID string, allowed []string, validate func(*model.Asset, string, []string) error) (*model.Asset, error) {
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
	if err := validate(asset, userID, allowed); err != nil {
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
	if err := validateOwnedAttachmentAsset(asset, userID, allowed); err != nil {
		return err
	}
	if ClassifyDirectUploadFile(asset.ContentType, strings.ToLower(filepath.Ext(strings.TrimSpace(asset.FileName)))) != "image" {
		return ErrReferenceAssetInvalidMetadata
	}
	return nil
}

func validateOwnedAttachmentAsset(asset *model.Asset, userID string, allowed []string) error {
	if asset == nil || asset.UserID != userID {
		return ErrReferenceAssetForbidden
	}
	if !directUploadPurposeAllowed(asset.Purpose, allowed) {
		return ErrReferenceAssetPurposeMismatch
	}
	if strings.TrimSpace(asset.StorageKey) == "" || strings.TrimSpace(asset.ETag) == "" {
		return ErrReferenceAssetInvalidMetadata
	}
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(asset.FileName)))
	policy, ok := directUploadPolicies[asset.Purpose]
	if !ok || !policy.validate(asset.ContentType, ext) {
		return ErrReferenceAssetInvalidMetadata
	}
	maxSize := policy.maxSize
	if policy.maxSizeFor != nil {
		maxSize = policy.maxSizeFor(asset.ContentType, ext)
	}
	if asset.Size <= 0 || maxSize <= 0 || asset.Size > maxSize {
		return ErrReferenceAssetInvalidMetadata
	}
	return nil
}

func validateReferenceImageFileMetadata(purpose, fileName, contentType string, size int64) error {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(fileName)))
	policy, ok := directUploadPolicies[purpose]
	if !ok || !isDirectUploadImage(contentType, ext) || !policy.validate(contentType, ext) {
		return ErrReferenceAssetInvalidMetadata
	}
	maxSize := policy.maxSize
	if policy.maxSizeFor != nil {
		maxSize = policy.maxSizeFor(contentType, ext)
	}
	if size <= 0 || maxSize <= 0 || size > maxSize {
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
