package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
)

var (
	ErrUserMediaNotFound       = errors.New("user media asset not found")
	ErrUserMediaProjectDenied  = errors.New("user media project is not owned by user")
	ErrUserMediaInvalidUpload  = errors.New("user media upload is invalid")
	ErrUserMediaNotRetryable   = errors.New("user media analysis is not retryable")
	ErrUserMediaAlreadyRemoved = errors.New("user media asset is already removed")
)

const userMediaPreviewTTL = 5 * 60

type UserMediaLibraryService struct {
	repo     repository.Repository
	store    storage.Provider
	analysis *ImageAnalysisService
	now      func() time.Time
}

func NewUserMediaLibraryService(repo repository.Repository, store storage.Provider, analysis *ImageAnalysisService) *UserMediaLibraryService {
	return &UserMediaLibraryService{repo: repo, store: store, analysis: analysis, now: time.Now}
}

type UserMediaListRequest struct {
	UserID    string
	Query     string
	MediaType string
	ProjectID string
	Status    string
	Offset    int
	Limit     int
}

type UserMediaCreateRequest struct {
	UserID          string
	UploadSessionID string
	ProjectID       string
	UsageNotes      string
}

type UserMediaUpdateRequest struct {
	UserID     string
	ProjectID  *string
	UsageNotes *string
}

type UserMediaAssetView struct {
	ID               string     `json:"id"`
	OptionLabel      string     `json:"option_label,omitempty"`
	AssetID          string     `json:"asset_id"`
	FileName         string     `json:"file_name"`
	ContentType      string     `json:"content_type"`
	Size             int64      `json:"size"`
	MediaType        string     `json:"media_type"`
	ProjectID        string     `json:"project_id,omitempty"`
	Status           string     `json:"status"`
	Description      string     `json:"description,omitempty"`
	RecommendedUses  []string   `json:"recommended_uses,omitempty"`
	UsageNotes       string     `json:"usage_notes,omitempty"`
	ErrorCode        string     `json:"error_code,omitempty"`
	ErrorMessage     string     `json:"error_message,omitempty"`
	PreviewURL       string     `json:"preview_url,omitempty"`
	PreviewExpiresAt *time.Time `json:"preview_expires_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type UserMediaListResult struct {
	Items  []*UserMediaAssetView `json:"items"`
	Total  int64                 `json:"total"`
	Offset int                   `json:"offset"`
	Limit  int                   `json:"limit"`
}

func (s *UserMediaLibraryService) Create(ctx context.Context, req UserMediaCreateRequest) (*UserMediaAssetView, error) {
	if s == nil || s.repo == nil || s.store == nil || s.analysis == nil {
		return nil, ErrUserMediaInvalidUpload
	}
	userID := strings.TrimSpace(req.UserID)
	uploadID := strings.TrimSpace(req.UploadSessionID)
	if userID == "" || uploadID == "" {
		return nil, ErrUserMediaInvalidUpload
	}
	projectID, err := s.validateProject(ctx, userID, req.ProjectID)
	if err != nil {
		return nil, err
	}
	finalStore, ok := s.store.(DirectUploadFinalizationStorage)
	if !ok {
		return nil, ErrUploadSessionUnavailable
	}
	asset, err := FinalizeUploadSession(ctx, finalStore, s.repo, FinalizeUploadRequest{
		SessionID: uploadID, UserID: userID, AllowedPurposes: []string{DirectUploadPurposeMediaLibrary}, Now: s.now(),
	})
	if err != nil {
		return nil, err
	}
	mediaType := mediaTypeForContentType(asset.ContentType)
	if mediaType == "" {
		return nil, ErrUserMediaInvalidUpload
	}
	if existing, findErr := s.repo.UserMediaAssets().FindByAssetIDForUser(ctx, userID, asset.ID, true); findErr == nil {
		if existing.RemovedAt != nil {
			return nil, ErrUserMediaAlreadyRemoved
		}
		return s.present(ctx, existing)
	} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return nil, findErr
	}
	media := &model.UserMediaAsset{
		ID: uuid.NewString(), UserID: userID, AssetID: asset.ID, ProjectID: projectID,
		MediaType: mediaType, Status: model.UserMediaAssetStatusQueued, UsageNotes: strings.TrimSpace(req.UsageNotes),
	}
	if err := s.repo.UserMediaAssets().Create(ctx, media); err != nil {
		return nil, err
	}
	if _, err := s.analysis.CreateUserMediaJob(ctx, media); err != nil {
		return nil, err
	}
	return s.present(ctx, media)
}

func (s *UserMediaLibraryService) List(ctx context.Context, req UserMediaListRequest) (*UserMediaListResult, error) {
	if s == nil || s.repo == nil {
		return nil, ErrUserMediaInvalidUpload
	}
	rows, total, err := s.repo.UserMediaAssets().List(ctx, repository.UserMediaAssetListFilter{
		UserID: strings.TrimSpace(req.UserID), Query: strings.TrimSpace(req.Query), MediaType: strings.TrimSpace(req.MediaType),
		ProjectID: strings.TrimSpace(req.ProjectID), Status: strings.TrimSpace(req.Status), Offset: req.Offset, Limit: req.Limit,
	})
	if err != nil {
		return nil, err
	}
	items := make([]*UserMediaAssetView, 0, len(rows))
	for _, row := range rows {
		view, err := s.present(ctx, row)
		if err != nil {
			return nil, err
		}
		items = append(items, view)
	}
	limit := req.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}
	return &UserMediaListResult{Items: items, Total: total, Offset: offset, Limit: limit}, nil
}

func (s *UserMediaLibraryService) Get(ctx context.Context, userID, id string) (*UserMediaAssetView, error) {
	row, err := s.repo.UserMediaAssets().FindByIDForUser(ctx, strings.TrimSpace(userID), strings.TrimSpace(id), false)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserMediaNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.present(ctx, row)
}

func (s *UserMediaLibraryService) Update(ctx context.Context, req UserMediaUpdateRequest, id string) (*UserMediaAssetView, error) {
	row, err := s.repo.UserMediaAssets().FindByIDForUser(ctx, strings.TrimSpace(req.UserID), strings.TrimSpace(id), false)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserMediaNotFound
	}
	if err != nil {
		return nil, err
	}
	if req.ProjectID != nil {
		projectID, err := s.validateProject(ctx, req.UserID, *req.ProjectID)
		if err != nil {
			return nil, err
		}
		row.ProjectID = projectID
	}
	if req.UsageNotes != nil {
		row.UsageNotes = strings.TrimSpace(*req.UsageNotes)
	}
	if err := s.repo.UserMediaAssets().Update(ctx, row); err != nil {
		return nil, err
	}
	return s.present(ctx, row)
}

func (s *UserMediaLibraryService) Retry(ctx context.Context, userID, id string) (*UserMediaAssetView, error) {
	row, err := s.repo.UserMediaAssets().FindByIDForUser(ctx, strings.TrimSpace(userID), strings.TrimSpace(id), false)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserMediaNotFound
	}
	if err != nil {
		return nil, err
	}
	job, err := s.repo.ImageAnalyses().FindBySubject(ctx, model.ImageAnalysisSubjectUserMedia, row.ID, model.ImageAnalysisKindUserMedia)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserMediaNotRetryable
		}
		return nil, err
	}
	if _, err := s.analysis.RetryUserMedia(ctx, userID, job.ID); err != nil {
		if errors.Is(err, ErrImageAnalysisNotRetryable) {
			return nil, ErrUserMediaNotRetryable
		}
		return nil, err
	}
	row, err = s.repo.UserMediaAssets().FindByIDForUser(ctx, userID, id, false)
	if err != nil {
		return nil, err
	}
	return s.present(ctx, row)
}

func (s *UserMediaLibraryService) Remove(ctx context.Context, userID, id string) error {
	row, err := s.repo.UserMediaAssets().FindByIDForUser(ctx, strings.TrimSpace(userID), strings.TrimSpace(id), false)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrUserMediaNotFound
	}
	if err != nil {
		return err
	}
	now := s.now().UTC()
	row.RemovedAt = &now
	return s.repo.UserMediaAssets().Update(ctx, row)
}

func (s *UserMediaLibraryService) SearchCandidates(ctx context.Context, userID, projectID, query, mediaType string, limit int) ([]*UserMediaAssetView, error) {
	rows, err := s.repo.UserMediaAssets().ListReadyCandidates(ctx, strings.TrimSpace(userID), strings.TrimSpace(projectID), strings.TrimSpace(query), strings.TrimSpace(mediaType), limit)
	if err != nil {
		return nil, err
	}
	items := make([]*UserMediaAssetView, 0, len(rows))
	for _, row := range rows {
		view, err := s.present(ctx, row)
		if err != nil {
			return nil, err
		}
		items = append(items, view)
	}
	return items, nil
}

func (s *UserMediaLibraryService) validateProject(ctx context.Context, userID, projectID string) (string, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return "", nil
	}
	project, err := s.repo.Projects().FindByID(ctx, projectID)
	if errors.Is(err, gorm.ErrRecordNotFound) || err != nil && project == nil {
		return "", ErrUserMediaProjectDenied
	}
	if err != nil {
		return "", err
	}
	if project.UserID != strings.TrimSpace(userID) || project.DeletingAt != nil {
		return "", ErrUserMediaProjectDenied
	}
	return projectID, nil
}

func (s *UserMediaLibraryService) present(ctx context.Context, row *model.UserMediaAsset) (*UserMediaAssetView, error) {
	if row == nil {
		return nil, ErrUserMediaNotFound
	}
	asset, err := s.repo.Assets().FindByID(ctx, row.AssetID)
	if err != nil {
		return nil, err
	}
	view := &UserMediaAssetView{
		ID: row.ID, OptionLabel: "media_asset:" + row.ID, AssetID: row.AssetID, FileName: asset.FileName, ContentType: asset.ContentType, Size: asset.Size,
		MediaType: row.MediaType, ProjectID: row.ProjectID, Status: row.Status, Description: row.Description,
		RecommendedUses: append([]string(nil), row.RecommendedUses...), UsageNotes: row.UsageNotes,
		ErrorCode: row.ErrorCode, ErrorMessage: row.ErrorMessage, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if s.store != nil && row.RemovedAt == nil {
		url, err := s.store.DownloadURL(ctx, asset.StorageKey, userMediaPreviewTTL)
		if err != nil {
			return nil, err
		}
		expires := s.now().Add(userMediaPreviewTTL * time.Second)
		view.PreviewURL, view.PreviewExpiresAt = url, &expires
	}
	return view, nil
}

func mediaTypeForContentType(contentType string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "image/") {
		return "image"
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "video/") {
		return "video"
	}
	return ""
}
