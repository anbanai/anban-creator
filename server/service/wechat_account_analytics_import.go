package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type WechatAccountAnalyticsImportService struct {
	repo  repository.Repository
	store storage.Provider
	now   func() time.Time
}

func NewWechatAccountAnalyticsImportService(repo repository.Repository, store storage.Provider) *WechatAccountAnalyticsImportService {
	return &WechatAccountAnalyticsImportService{repo: repo, store: store, now: time.Now}
}

type WechatAccountAnalyticsImportRequest struct {
	UserID               string     `json:"-"`
	ProjectID            string     `json:"-"`
	UploadID             string     `json:"upload_id"`
	IdempotencyKey       string     `json:"idempotency_key"`
	Timezone             string     `json:"timezone,omitempty"`
	DataAsOfAt           *time.Time `json:"data_as_of_at,omitempty"`
	ClientFileModifiedAt *time.Time `json:"client_file_modified_at,omitempty"`
}
type WechatAccountAnalyticsImportPreview struct {
	Source           string                            `json:"source"`
	ReportType       string                            `json:"report_type"`
	SupportedFields  []string                          `json:"supported_fields"`
	DateFrom         string                            `json:"date_from,omitempty"`
	DateTo           string                            `json:"date_to,omitempty"`
	FileName         string                            `json:"file_name"`
	ParserVersion    string                            `json:"parser_version"`
	Rows             []ParsedWechatAccountAnalyticsRow `json:"rows"`
	Sources          []ParsedWechatTrafficSourceRow    `json:"sources"`
	UnknownHeaders   []string                          `json:"unknown_headers,omitempty"`
	DuplicateDates   []string                          `json:"duplicate_dates,omitempty"`
	DuplicateSources []string                          `json:"duplicate_sources,omitempty"`
	Errors           []string                          `json:"errors,omitempty"`
}
type WechatAccountAnalyticsImportSummary struct {
	Revision int64                              `json:"revision"`
	Batch    *model.WechatAccountAnalyticsBatch `json:"batch"`
}

var ErrWechatAccountAnalyticsImportInvalid = errors.New("invalid WeChat account analytics import")

func normalizeWechatAccountAnalyticsIdempotencyKey(value string) (string, error) {
	key := strings.TrimSpace(value)
	if key == "" {
		return "", fmt.Errorf("%w: idempotency_key is required", ErrWechatAccountAnalyticsImportInvalid)
	}
	if len([]byte(key)) > 128 {
		return "", fmt.Errorf("%w: idempotency_key must be at most 128 bytes", ErrWechatAccountAnalyticsImportInvalid)
	}
	return key, nil
}

func (s *WechatAccountAnalyticsImportService) readWorkbook(ctx context.Context, req WechatAccountAnalyticsImportRequest) (*model.Project, *model.Asset, *ParsedWechatAccountAnalyticsWorkbook, []byte, error) {
	if s.repo == nil || s.store == nil {
		return nil, nil, nil, nil, ErrWechatAccountAnalyticsUnavailable
	}
	project, err := s.repo.Projects().FindByID(ctx, strings.TrimSpace(req.ProjectID))
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if project.UserID != req.UserID {
		return nil, nil, nil, nil, errors.New("project does not belong to user")
	}
	if project.Platform != model.PlatformWechat {
		return nil, nil, nil, nil, errors.New("WeChat account analytics requires a WeChat project")
	}
	asset, err := s.repo.Assets().FindOwnedByID(ctx, strings.TrimSpace(req.UploadID), req.UserID)
	if err != nil {
		if session, sessionErr := s.repo.UploadSessions().FindByID(ctx, strings.TrimSpace(req.UploadID)); sessionErr == nil && session.UserID == req.UserID && session.Purpose == DirectUploadPurposeWechatAnalyticsImport {
			finalStore, ok := s.store.(DirectUploadFinalizationStorage)
			if !ok {
				return nil, nil, nil, nil, fmt.Errorf("%w: storage does not support upload finalization", ErrWechatAccountAnalyticsUnavailable)
			}
			asset, err = FinalizeUploadSession(ctx, finalStore, s.repo, FinalizeUploadRequest{SessionID: req.UploadID, UserID: req.UserID, AllowedPurposes: []string{DirectUploadPurposeWechatAnalyticsImport}})
		}
	}
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("find import asset: %w", err)
	}
	name := strings.ToLower(asset.FileName)
	if asset.Purpose != DirectUploadPurposeWechatAnalyticsImport || (!strings.HasSuffix(name, ".xls") && !strings.HasSuffix(name, ".xlsx")) {
		return nil, nil, nil, nil, errors.New("upload is not a WeChat account .xls/.xlsx import")
	}
	data, err := storage.ReadObject(ctx, s.store, asset.StorageKey, MaxWechatAnalyticsImportBytes)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("%w: read import workbook: %v", ErrWechatAccountAnalyticsUnavailable, err)
	}
	loc, err := time.LoadLocation(defaultWechatAnalyticsImportTimezone(req.Timezone))
	if err != nil {
		return nil, nil, nil, nil, err
	}
	parsed, err := ParseWechatAccountAnalyticsWorkbook(data, loc)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return project, asset, parsed, data, nil
}

func (s *WechatAccountAnalyticsImportService) Preview(ctx context.Context, req WechatAccountAnalyticsImportRequest) (*WechatAccountAnalyticsImportPreview, error) {
	_, asset, parsed, _, err := s.readWorkbook(ctx, req)
	if err != nil {
		return nil, err
	}
	return &WechatAccountAnalyticsImportPreview{Source: parsed.Source, ReportType: parsed.ReportType, SupportedFields: parsed.SupportedFields, DateFrom: parsed.DateFrom, DateTo: parsed.DateTo, FileName: asset.FileName, ParserVersion: WechatAccountAnalyticsImportParserVersion, Rows: parsed.Rows, Sources: parsed.Sources, UnknownHeaders: parsed.UnknownHeaders, DuplicateDates: parsed.DuplicateDates, DuplicateSources: parsed.DuplicateSources, Errors: parsed.Errors}, nil
}

func (s *WechatAccountAnalyticsImportService) Import(ctx context.Context, req WechatAccountAnalyticsImportRequest) (*WechatAccountAnalyticsImportSummary, error) {
	key, keyErr := normalizeWechatAccountAnalyticsIdempotencyKey(req.IdempotencyKey)
	if keyErr != nil || req.DataAsOfAt == nil || req.DataAsOfAt.IsZero() || req.DataAsOfAt.Year() < 2000 || req.DataAsOfAt.Year() > 9999 {
		if keyErr != nil {
			return nil, keyErr
		}
		return nil, fmt.Errorf("%w: data_as_of_at must be a valid date between years 2000 and 9999", ErrWechatAccountAnalyticsImportInvalid)
	}
	req.IdempotencyKey = key
	project, asset, parsed, data, err := s.readWorkbook(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(parsed.Errors) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrWechatAccountAnalyticsImportInvalid, strings.Join(parsed.Errors, "; "))
	}
	if len(parsed.DuplicateDates) > 0 {
		return nil, fmt.Errorf("%w: duplicate stat dates: %s", ErrWechatAccountAnalyticsImportInvalid, strings.Join(parsed.DuplicateDates, ", "))
	}
	if len(parsed.DuplicateSources) > 0 {
		return nil, fmt.Errorf("%w: duplicate traffic source facts: %s", ErrWechatAccountAnalyticsImportInvalid, strings.Join(parsed.DuplicateSources, ", "))
	}
	requestHash := sha256.Sum256(marshalWechatAccountImportFingerprint(req, data))
	hash := hex.EncodeToString(requestHash[:])
	var summary *WechatAccountAnalyticsImportSummary
	err = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		analyticsRepo := tx.WechatAccountAnalytics()
		state, err := tx.Analytics().LockProject(ctx, project.ID)
		if err != nil {
			return err
		}
		if existing, findErr := analyticsRepo.FindBatchByIdempotency(ctx, project.ID, req.IdempotencyKey); findErr == nil {
			if existing.RequestHash != hash {
				return repository.ErrAnalyticsIdempotencyConflict
			}
			summary = &WechatAccountAnalyticsImportSummary{Revision: state.Revision, Batch: existing}
			return nil
		} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}
		now := s.now().UTC()
		effective := req.DataAsOfAt.UTC()
		raw, _ := json.Marshal(map[string]any{"report_type": parsed.ReportType, "supported_fields": parsed.SupportedFields, "date_from": parsed.DateFrom, "date_to": parsed.DateTo, "unknown_headers": parsed.UnknownHeaders, "duplicate_dates": parsed.DuplicateDates, "duplicate_sources": parsed.DuplicateSources})
		batch := &model.WechatAccountAnalyticsBatch{ID: uuid.NewString(), UserID: req.UserID, ProjectID: project.ID, AssetID: asset.ID, FileName: asset.FileName, SHA256: hex.EncodeToString(sha256Bytes(data)), Source: parsed.Source, Timezone: defaultWechatAnalyticsImportTimezone(req.Timezone), ParserVersion: WechatAccountAnalyticsImportParserVersion, Status: model.WechatAccountAnalyticsBatchCompleted, IdempotencyKey: req.IdempotencyKey, RequestHash: hash, RawProvenance: string(raw), ReceivedAt: now, EffectiveAt: effective, DataAsOfAt: effective, CreatedAt: now, UpdatedAt: now}
		if err := analyticsRepo.CreateBatch(ctx, batch); err != nil {
			return err
		}
		days := make([]*model.WechatAccountAnalyticsDay, 0, len(parsed.Rows))
		for _, row := range parsed.Rows {
			days = append(days, &model.WechatAccountAnalyticsDay{ID: uuid.NewString(), ProjectID: project.ID, BatchID: batch.ID, StatDate: row.StatDate, ReadUsers: row.ReadUsers, ShareUsers: row.ShareUsers, OriginalReadUsers: row.OriginalReadUsers, LikeUsers: row.LikeUsers, CollectionUsers: row.CollectionUsers, CommentCount: row.CommentCount, FollowerGainCount: row.FollowerGainCount, FollowerCancelCount: row.FollowerCancelCount, FollowerNetGainCount: row.FollowerNetGainCount, PublishedArticles: row.PublishedArticles, Source: parsed.Source, RawData: marshalWechatAccountRaw(row.Raw), CreatedAt: now, UpdatedAt: now})
		}
		if err := analyticsRepo.CreateDays(ctx, days); err != nil {
			return err
		}
		sources := make([]*model.WechatTrafficSourceDay, 0, len(parsed.Sources))
		for _, row := range parsed.Sources {
			sources = append(sources, &model.WechatTrafficSourceDay{ID: uuid.NewString(), ProjectID: project.ID, BatchID: batch.ID, StatDate: row.StatDate, Source: row.Source, OriginalLabel: row.OriginalLabel, ReadUsers: row.ReadUsers, RawData: marshalWechatAccountRaw(row.Raw), CreatedAt: now, UpdatedAt: now})
		}
		if err := analyticsRepo.CreateSources(ctx, sources); err != nil {
			return err
		}
		if err := tx.Analytics().AdvanceRevision(ctx, state); err != nil {
			return err
		}
		summary = &WechatAccountAnalyticsImportSummary{Revision: state.Revision, Batch: batch}
		return nil
	})
	return summary, err
}

func sha256Bytes(data []byte) []byte { h := sha256.Sum256(data); return h[:] }

type wechatAccountImportFingerprint struct {
	UploadID             string `json:"upload_id"`
	IdempotencyKey       string `json:"idempotency_key"`
	Timezone             string `json:"timezone"`
	DataAsOfAt           string `json:"data_as_of_at"`
	ClientFileModifiedAt string `json:"client_file_modified_at,omitempty"`
	FileSHA256           string `json:"file_sha256"`
}

func marshalWechatAccountImportFingerprint(req WechatAccountAnalyticsImportRequest, data []byte) []byte {
	fingerprint := wechatAccountImportFingerprint{
		UploadID:       strings.TrimSpace(req.UploadID),
		IdempotencyKey: strings.TrimSpace(req.IdempotencyKey),
		Timezone:       defaultWechatAnalyticsImportTimezone(req.Timezone),
		FileSHA256:     hex.EncodeToString(sha256Bytes(data)),
	}
	if req.DataAsOfAt != nil {
		fingerprint.DataAsOfAt = req.DataAsOfAt.UTC().Format(time.RFC3339Nano)
	}
	if req.ClientFileModifiedAt != nil {
		fingerprint.ClientFileModifiedAt = req.ClientFileModifiedAt.UTC().Format(time.RFC3339Nano)
	}
	encoded, _ := json.Marshal(fingerprint)
	return encoded
}

func (s *WechatAccountAnalyticsImportService) ListBatches(ctx context.Context, userID, projectID string, offset, limit int) ([]*model.WechatAccountAnalyticsBatch, int64, error) {
	project, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return nil, 0, err
	}
	if project.UserID != userID {
		return nil, 0, errors.New("project does not belong to user")
	}
	if project.Platform != model.PlatformWechat {
		return nil, 0, errors.New("WeChat account analytics requires a WeChat project")
	}
	return s.repo.WechatAccountAnalytics().ListBatches(ctx, projectID, offset, limit)
}
func (s *WechatAccountAnalyticsImportService) GetBatch(ctx context.Context, userID, projectID, batchID string) (*WechatAccountAnalyticsImportSummary, error) {
	project, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if project.UserID != userID {
		return nil, errors.New("project does not belong to user")
	}
	if project.Platform != model.PlatformWechat {
		return nil, errors.New("WeChat account analytics requires a WeChat project")
	}
	batch, err := s.repo.WechatAccountAnalytics().FindBatchByID(ctx, projectID, batchID)
	if err != nil {
		return nil, err
	}
	if batch.UserID != userID {
		return nil, errors.New("import does not belong to user")
	}
	revision, err := s.repo.WechatAccountAnalytics().CurrentRevision(ctx, projectID)
	return &WechatAccountAnalyticsImportSummary{Revision: revision, Batch: batch}, err
}
func (s *WechatAccountAnalyticsImportService) Revoke(ctx context.Context, userID, projectID, batchID string) (*WechatAccountAnalyticsImportSummary, error) {
	project, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if project.UserID != userID {
		return nil, errors.New("project does not belong to user")
	}
	if project.Platform != model.PlatformWechat {
		return nil, errors.New("WeChat account analytics requires a WeChat project")
	}
	var out *WechatAccountAnalyticsImportSummary
	err = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		state, err := tx.Analytics().LockProject(ctx, projectID)
		if err != nil {
			return err
		}
		if existing, findErr := tx.WechatAccountAnalytics().FindBatchByID(ctx, projectID, batchID); findErr != nil {
			return findErr
		} else if existing.RevokedAt != nil {
			out = &WechatAccountAnalyticsImportSummary{Revision: state.Revision, Batch: existing}
			return nil
		}
		if err := tx.WechatAccountAnalytics().RevokeBatch(ctx, projectID, batchID, s.now().UTC()); err != nil {
			return err
		}
		if err := tx.Analytics().AdvanceRevision(ctx, state); err != nil {
			return err
		}
		batch, err := tx.WechatAccountAnalytics().FindBatchByID(ctx, projectID, batchID)
		if err != nil {
			return err
		}
		out = &WechatAccountAnalyticsImportSummary{Revision: state.Revision, Batch: batch}
		return nil
	})
	return out, err
}
