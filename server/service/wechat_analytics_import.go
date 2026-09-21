package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type WechatAnalyticsImportService struct {
	repo  repository.Repository
	store storage.Provider
	now   func() time.Time
}

func NewWechatAnalyticsImportService(repo repository.Repository, store storage.Provider) *WechatAnalyticsImportService {
	return &WechatAnalyticsImportService{repo: repo, store: store, now: time.Now}
}

type WechatAnalyticsImportRequest struct {
	UserID               string     `json:"-"`
	ProjectID            string     `json:"-"`
	UploadID             string     `json:"upload_id"`
	DataAsOfAt           *time.Time `json:"data_as_of_at,omitempty"`
	Timezone             string     `json:"timezone,omitempty"`
	ClientFileModifiedAt *time.Time `json:"client_file_modified_at,omitempty"`
}

type WechatAnalyticsImportSummary struct {
	Batch *model.WechatAnalyticsImportBatch `json:"batch"`
	Rows  []*model.WechatAnalyticsImportRow `json:"rows,omitempty"`
}

type WechatAnalyticsImportReceipt struct {
	BatchID       string `json:"batch_id"`
	SourceFile    string `json:"source_file"`
	TotalRows     int    `json:"total_rows"`
	MatchedRows   int    `json:"matched_rows"`
	AmbiguousRows int    `json:"ambiguous_rows"`
	UnmatchedRows int    `json:"unmatched_rows"`
	InvalidRows   int    `json:"invalid_rows"`
	ParserVersion string `json:"parser_version"`
}

func (s *WechatAnalyticsImportSummary) Receipt() *WechatAnalyticsImportReceipt {
	if s == nil || s.Batch == nil {
		return nil
	}
	return &WechatAnalyticsImportReceipt{
		BatchID: s.Batch.ID, SourceFile: s.Batch.FileName, TotalRows: s.Batch.TotalRows,
		MatchedRows: s.Batch.MatchedRows, AmbiguousRows: s.Batch.ReviewRows,
		UnmatchedRows: s.Batch.UnmatchedRows, InvalidRows: s.Batch.InvalidRows,
		ParserVersion: s.Batch.ParserVersion,
	}
}

type WechatAnalyticsFieldMapping struct {
	ExcelField    string `json:"excel_field"`
	InternalField string `json:"internal_field"`
}

type WechatAnalyticsImportPreview struct {
	Source        string                        `json:"source"`
	FileName      string                        `json:"file_name"`
	TotalRows     int                           `json:"total_rows"`
	ParserVersion string                        `json:"parser_version"`
	FieldMapping  []WechatAnalyticsFieldMapping `json:"field_mapping"`
	Rows          []ParsedWechatAnalyticsRow    `json:"rows"`
}

var wechatAnalyticsFieldMapping = []WechatAnalyticsFieldMapping{
	{ExcelField: "数据来源概况", InternalField: "source"},
	{ExcelField: "内容标题", InternalField: "title"},
	{ExcelField: "发表日期", InternalField: "published_date"},
	{ExcelField: "阅读人数", InternalField: "read_users"},
	{ExcelField: "分享人数", InternalField: "share_users"},
	{ExcelField: "阅读后关注人数", InternalField: "read_to_follow_users"},
	{ExcelField: "送达人数", InternalField: "delivered_users"},
	{ExcelField: "送达完成率", InternalField: "delivery_completion_rate"},
	{ExcelField: "阅读完成率", InternalField: "read_completion_rate"},
	{ExcelField: "内容url", InternalField: "article_url"},
}

func (s *WechatAnalyticsImportService) Preview(ctx context.Context, req WechatAnalyticsImportRequest) (*WechatAnalyticsImportPreview, error) {
	_, asset, parsed, _, err := s.readWorkbook(ctx, req)
	if err != nil {
		return nil, err
	}
	rows := parsed.Rows
	if len(rows) > 10 {
		rows = rows[:10]
	}
	return &WechatAnalyticsImportPreview{
		Source: parsed.Source, FileName: asset.FileName, TotalRows: len(parsed.Rows),
		ParserVersion: WechatAnalyticsImportParserVersion,
		FieldMapping:  append([]WechatAnalyticsFieldMapping(nil), wechatAnalyticsFieldMapping...),
		Rows:          rows,
	}, nil
}

func (s *WechatAnalyticsImportService) Import(ctx context.Context, req WechatAnalyticsImportRequest) (*WechatAnalyticsImportSummary, error) {
	project, asset, parsed, data, err := s.readWorkbook(ctx, req)
	if err != nil {
		return nil, err
	}
	location, _ := time.LoadLocation(defaultWechatAnalyticsImportTimezone(req.Timezone))
	now := s.now()
	dataAsOf := now
	if req.ClientFileModifiedAt != nil {
		dataAsOf = req.ClientFileModifiedAt.In(location)
	}
	if req.DataAsOfAt != nil {
		dataAsOf = req.DataAsOfAt.In(location)
	}
	hash := sha256.Sum256(data)
	batch := &model.WechatAnalyticsImportBatch{
		ID: uuid.NewString(), UserID: req.UserID, ProjectID: project.ID, AssetID: asset.ID,
		FileName: asset.FileName, ContentType: asset.ContentType, FileSize: int64(len(data)),
		SHA256: hex.EncodeToString(hash[:]), Source: parsed.Source, ReceivedAt: now,
		DataAsOfAt: dataAsOf, Timezone: defaultWechatAnalyticsImportTimezone(req.Timezone),
		ParserVersion: WechatAnalyticsImportParserVersion, Status: model.WechatAnalyticsImportBatchProcessing,
		TotalRows: len(parsed.Rows),
	}
	rows := make([]*model.WechatAnalyticsImportRow, 0, len(parsed.Rows))
	err = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		if err := tx.WechatAnalyticsImports().LockProject(ctx, project.ID); err != nil {
			return err
		}
		publications, err := tx.WechatPublications().ListByProject(ctx, project.ID)
		if err != nil {
			return fmt.Errorf("list WeChat publications: %w", err)
		}
		for _, parsedRow := range parsed.Rows {
			row := &model.WechatAnalyticsImportRow{
				ID: uuid.NewString(), BatchID: batch.ID, ProjectID: project.ID, SourceRow: parsedRow.SourceRow,
				RawData: marshalWechatRaw(parsedRow), Source: parsedRow.Source, Title: parsedRow.Title,
				NormalizedTitle: parsedRow.NormalizedTitle, PublishedDate: parsedRow.PublishedDate,
				ArticleURL: parsedRow.ArticleURL, ReadUsers: parsedRow.ReadUsers, ShareUsers: parsedRow.ShareUsers,
				ReadToFollowUsers: parsedRow.ReadToFollowUsers, DeliveredUsers: parsedRow.DeliveredUsers,
				DeliveryCompletionRate: parsedRow.DeliveryCompletionRate, ReadCompletionRate: parsedRow.ReadCompletionRate,
				ParseError: parsedRow.ParseError, MatchStatus: model.WechatAnalyticsImportRowUnmatched,
			}
			if parsedRow.ParseError != "" {
				row.MatchStatus = model.WechatAnalyticsImportRowInvalid
				batch.InvalidRows++
			} else {
				match, ambiguous := matchWechatAnalyticsPublication(parsedRow, publications)
				switch {
				case match != nil:
					row.PublicationID = match.ID
					row.MatchStatus = model.WechatAnalyticsImportRowMatched
					batch.MatchedRows++
				case ambiguous:
					row.MatchStatus = model.WechatAnalyticsImportRowNeedsReview
					batch.ReviewRows++
				default:
					batch.UnmatchedRows++
				}
			}
			rows = append(rows, row)
		}
		if batch.ReviewRows > 0 || batch.UnmatchedRows > 0 || batch.InvalidRows > 0 {
			batch.Status = model.WechatAnalyticsImportBatchNeedsReview
		} else {
			batch.Status = model.WechatAnalyticsImportBatchCompleted
		}
		if err := tx.WechatAnalyticsImports().CreateBatch(ctx, batch); err != nil {
			return err
		}
		if err := tx.WechatAnalyticsImports().CreateRows(ctx, rows); err != nil {
			return err
		}
		for _, row := range rows {
			if row.MatchStatus != model.WechatAnalyticsImportRowMatched || row.PublicationID == "" {
				continue
			}
			if err := tx.WechatAnalyticsImports().CreateSnapshot(ctx, snapshotFromWechatImport(batch, row, now)); err != nil {
				return err
			}
			if err := tx.WechatPublications().RecordImportedAnalytics(ctx, project.ID, row.PublicationID, batch.ID, row.ArticleURL); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("persist WeChat analytics import: %w", err)
	}
	return &WechatAnalyticsImportSummary{Batch: batch, Rows: rows}, nil
}

func (s *WechatAnalyticsImportService) readWorkbook(ctx context.Context, req WechatAnalyticsImportRequest) (*model.Project, *model.Asset, *ParsedWechatAnalyticsWorkbook, []byte, error) {
	if s.repo == nil || s.store == nil {
		return nil, nil, nil, nil, errors.New("WeChat analytics import unavailable")
	}
	project, err := s.repo.Projects().FindByID(ctx, strings.TrimSpace(req.ProjectID))
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("find project: %w", err)
	}
	if project.UserID != req.UserID {
		return nil, nil, nil, nil, errors.New("project does not belong to user")
	}
	if project.Platform != model.PlatformArticle {
		return nil, nil, nil, nil, errors.New("project is not a WeChat article project")
	}
	asset, err := s.repo.Assets().FindOwnedByID(ctx, strings.TrimSpace(req.UploadID), req.UserID)
	if err != nil {
		if session, sessionErr := s.repo.UploadSessions().FindByID(ctx, strings.TrimSpace(req.UploadID)); sessionErr == nil && session.UserID == req.UserID && session.Purpose == DirectUploadPurposeWechatAnalyticsImport {
			finalStore, ok := s.store.(DirectUploadFinalizationStorage)
			if !ok {
				return nil, nil, nil, nil, errors.New("storage does not support upload finalization")
			}
			asset, err = FinalizeUploadSession(ctx, finalStore, s.repo, FinalizeUploadRequest{SessionID: req.UploadID, UserID: req.UserID, AllowedPurposes: []string{DirectUploadPurposeWechatAnalyticsImport}})
		}
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("find import asset: %w", err)
		}
	}
	filename := strings.ToLower(asset.FileName)
	if asset.Purpose != DirectUploadPurposeWechatAnalyticsImport || (!strings.HasSuffix(filename, ".xls") && !strings.HasSuffix(filename, ".xlsx")) {
		return nil, nil, nil, nil, errors.New("upload is not a WeChat .xls/.xlsx import")
	}
	data, err := storage.ReadObject(ctx, s.store, asset.StorageKey, MaxWechatAnalyticsImportBytes)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("read import asset: %w", err)
	}
	location, err := time.LoadLocation(defaultWechatAnalyticsImportTimezone(req.Timezone))
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("invalid timezone: %w", err)
	}
	parsed, err := ParseWechatAnalyticsWorkbook(data, location)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return project, asset, parsed, data, nil
}

func defaultWechatAnalyticsImportTimezone(value string) string {
	if strings.TrimSpace(value) == "" {
		return "Asia/Shanghai"
	}
	return strings.TrimSpace(value)
}

func matchWechatAnalyticsPublication(row ParsedWechatAnalyticsRow, publications []*model.WechatPublication) (*model.WechatPublication, bool) {
	if row.ArticleURL != "" {
		var matches []*model.WechatPublication
		for _, publication := range publications {
			if normalizeWechatArticleURLForMatch(publication.ArticleURL) == row.ArticleURL {
				matches = append(matches, publication)
			}
		}
		if len(matches) == 1 {
			return matches[0], false
		}
		if len(matches) > 1 {
			return nil, true
		}
	}
	var matches []*model.WechatPublication
	if row.PublishedDate == nil {
		return nil, false
	}
	for _, publication := range publications {
		if normalizeWechatAnalyticsTitle(publication.DraftTitle) != row.NormalizedTitle {
			continue
		}
		if publication.PublishedAt == nil || publication.PublishedAt.In(row.PublishedDate.Location()).Format("2006-01-02") != row.PublishedDate.Format("2006-01-02") {
			continue
		}
		matches = append(matches, publication)
	}
	if len(matches) == 1 {
		return matches[0], false
	}
	return nil, len(matches) > 1
}

func normalizeWechatArticleURLForMatch(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return value
	}
	parsed.Fragment = ""
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	return parsed.String()
}

func snapshotFromWechatImport(batch *model.WechatAnalyticsImportBatch, row *model.WechatAnalyticsImportRow, importedAt time.Time) *model.WechatAnalyticsSnapshot {
	return &model.WechatAnalyticsSnapshot{ID: uuid.NewString(), ProjectID: batch.ProjectID, PublicationID: row.PublicationID, BatchID: batch.ID, ImportRowID: row.ID, Source: row.Source, DataAsOfAt: batch.DataAsOfAt, ImportedAt: importedAt, ReadUsers: row.ReadUsers, ShareUsers: row.ShareUsers, ReadToFollowUsers: row.ReadToFollowUsers, DeliveredUsers: row.DeliveredUsers, DeliveryCompletionRate: row.DeliveryCompletionRate, ReadCompletionRate: row.ReadCompletionRate, RawData: row.RawData}
}

type WechatAnalyticsResolveAction struct {
	RowID         string `json:"row_id"`
	Action        string `json:"action"`
	PublicationID string `json:"publication_id,omitempty"`
}

type WechatAnalyticsArticleView struct {
	Publication *model.WechatPublication         `json:"publication"`
	Latest      *model.WechatAnalyticsSnapshot   `json:"latest,omitempty"`
	Snapshots   []*model.WechatAnalyticsSnapshot `json:"snapshots,omitempty"`
}

func (s *WechatAnalyticsImportService) ListArticles(ctx context.Context, userID, projectID string) ([]WechatAnalyticsArticleView, error) {
	project, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if project.UserID != userID {
		return nil, errors.New("project does not belong to user")
	}
	publications, err := s.repo.WechatPublications().ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	views := make([]WechatAnalyticsArticleView, 0, len(publications))
	for _, publication := range publications {
		snapshots, snapshotErr := s.analyticsSnapshotsForPublication(ctx, publication)
		if snapshotErr != nil {
			return nil, snapshotErr
		}
		var latest *model.WechatAnalyticsSnapshot
		if len(snapshots) > 0 {
			latest = snapshots[0]
		}
		if publication.ArticleURL == "" {
			publication.ArticleURL = historicalWechatArticleURL(snapshots)
		}
		views = append(views, WechatAnalyticsArticleView{Publication: publication, Latest: latest, Snapshots: snapshots})
	}
	return views, nil
}

// Older imports retained their URLs only in the immutable observations. Expose
// an unambiguous URL on reads without changing publication state or stored history.
func historicalWechatArticleURL(snapshots []*model.WechatAnalyticsSnapshot) string {
	var candidate string
	for _, snapshot := range snapshots {
		var raw map[string]string
		if json.Unmarshal([]byte(snapshot.RawData), &raw) != nil {
			continue
		}
		articleURL := normalizeWechatArticleURL(raw["内容url"])
		if articleURL == "" {
			continue
		}
		if candidate != "" && candidate != articleURL {
			return ""
		}
		candidate = articleURL
	}
	return candidate
}

type WechatAnalyticsOverview struct {
	Articles       int   `json:"articles"`
	Published      int   `json:"published"`
	WithData       int   `json:"with_data"`
	ReadUsers      int64 `json:"read_users"`
	ShareUsers     int64 `json:"share_users"`
	ReadToFollow   int64 `json:"read_to_follow_users"`
	DeliveredUsers int64 `json:"delivered_users"`
}

func (s *WechatAnalyticsImportService) Overview(ctx context.Context, userID, projectID string) (*WechatAnalyticsOverview, error) {
	articles, err := s.ListArticles(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}
	overview := &WechatAnalyticsOverview{Articles: len(articles)}
	for _, article := range articles {
		if article.Publication.Status == model.WechatPublicationStatusPublished {
			overview.Published++
		}
		if article.Latest == nil {
			continue
		}
		overview.WithData++
		if article.Latest.ReadUsers != nil {
			overview.ReadUsers += *article.Latest.ReadUsers
		}
		if article.Latest.ShareUsers != nil {
			overview.ShareUsers += *article.Latest.ShareUsers
		}
		if article.Latest.ReadToFollowUsers != nil {
			overview.ReadToFollow += *article.Latest.ReadToFollowUsers
		}
		if article.Latest.DeliveredUsers != nil {
			overview.DeliveredUsers += *article.Latest.DeliveredUsers
		}
	}
	return overview, nil
}

func (s *WechatAnalyticsImportService) Resolve(ctx context.Context, userID, projectID, batchID string, actions []WechatAnalyticsResolveAction) (*WechatAnalyticsImportSummary, error) {
	batch, err := s.repo.WechatAnalyticsImports().FindBatchByID(ctx, projectID, batchID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, gorm.ErrRecordNotFound
	}
	if err != nil {
		return nil, err
	}
	if batch.UserID != userID {
		return nil, errors.New("import does not belong to user")
	}
	publications, err := s.repo.WechatPublications().ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*model.WechatPublication, len(publications))
	for _, publication := range publications {
		byID[publication.ID] = publication
	}
	now := s.now()
	err = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		if err := tx.WechatAnalyticsImports().LockProject(ctx, projectID); err != nil {
			return err
		}
		if err := tx.WechatAnalyticsImports().LockBatch(ctx, projectID, batchID); err != nil {
			return err
		}
		locked, err := tx.WechatAnalyticsImports().FindBatchByID(ctx, projectID, batchID)
		if err != nil {
			return err
		}
		if locked.RevokedAt != nil {
			return ErrAnalyticsImportRevoked
		}
		batch = locked
		for _, action := range actions {
			row, findErr := tx.WechatAnalyticsImports().FindRowByID(ctx, projectID, batchID, action.RowID)
			if findErr != nil {
				return findErr
			}
			if row.MatchStatus == model.WechatAnalyticsImportRowInvalid {
				continue
			}
			switch action.Action {
			case "link_existing":
				if byID[action.PublicationID] == nil {
					return fmt.Errorf("publication %q not found", action.PublicationID)
				}
				row.PublicationID = action.PublicationID
				row.MatchStatus = model.WechatAnalyticsImportRowMatched
			case "skip":
				row.PublicationID = ""
				row.MatchStatus = model.WechatAnalyticsImportRowUnmatched
			default:
				return fmt.Errorf("unsupported resolve action %q", action.Action)
			}
			if err := tx.WechatAnalyticsImports().UpdateRow(ctx, row); err != nil {
				return err
			}
			if row.MatchStatus == model.WechatAnalyticsImportRowMatched {
				if err := tx.WechatAnalyticsImports().CreateSnapshot(ctx, snapshotFromWechatImport(batch, row, now)); err != nil {
					return err
				}
				if err := tx.WechatPublications().RecordImportedAnalytics(ctx, projectID, row.PublicationID, batch.ID, normalizeWechatArticleURL(row.ArticleURL)); err != nil {
					return err
				}
			}
		}
		rows, err := tx.WechatAnalyticsImports().FindRowsByBatchID(ctx, projectID, batchID)
		if err != nil {
			return err
		}
		batch.MatchedRows, batch.ReviewRows, batch.UnmatchedRows = 0, 0, 0
		for _, row := range rows {
			switch row.MatchStatus {
			case model.WechatAnalyticsImportRowMatched:
				batch.MatchedRows++
			case model.WechatAnalyticsImportRowNeedsReview:
				batch.ReviewRows++
			case model.WechatAnalyticsImportRowUnmatched:
				batch.UnmatchedRows++
			}
		}
		if batch.ReviewRows == 0 && batch.UnmatchedRows == 0 && batch.InvalidRows == 0 {
			batch.Status = model.WechatAnalyticsImportBatchCompleted
		} else {
			batch.Status = model.WechatAnalyticsImportBatchNeedsReview
		}
		return tx.WechatAnalyticsImports().UpdateBatch(ctx, batch)
	})
	if err != nil {
		return nil, err
	}
	return s.GetBatch(ctx, userID, projectID, batchID)
}

func (s *WechatAnalyticsImportService) GetBatch(ctx context.Context, userID, projectID, batchID string) (*WechatAnalyticsImportSummary, error) {
	batch, err := s.repo.WechatAnalyticsImports().FindBatchByID(ctx, projectID, batchID)
	if err != nil {
		return nil, err
	}
	if batch.UserID != userID {
		return nil, errors.New("import does not belong to user")
	}
	rows, err := s.repo.WechatAnalyticsImports().FindRowsByBatchID(ctx, projectID, batchID)
	if err != nil {
		return nil, err
	}
	return &WechatAnalyticsImportSummary{Batch: batch, Rows: rows}, nil
}

func (s *WechatAnalyticsImportService) GetBatchByID(ctx context.Context, userID, batchID string) (*WechatAnalyticsImportSummary, error) {
	batch, err := s.repo.WechatAnalyticsImports().FindBatchByIDAnyProject(ctx, batchID)
	if err != nil {
		return nil, err
	}
	if batch.UserID != userID {
		return nil, errors.New("import does not belong to user")
	}
	return s.GetBatch(ctx, userID, batch.ProjectID, batch.ID)
}

func (s *WechatAnalyticsImportService) ResolveByID(ctx context.Context, userID, batchID string, actions []WechatAnalyticsResolveAction) (*WechatAnalyticsImportSummary, error) {
	batch, err := s.repo.WechatAnalyticsImports().FindBatchByIDAnyProject(ctx, batchID)
	if err != nil {
		return nil, err
	}
	if batch.UserID != userID {
		return nil, errors.New("import does not belong to user")
	}
	return s.Resolve(ctx, userID, batch.ProjectID, batch.ID, actions)
}

func (s *WechatAnalyticsImportService) ListBatches(ctx context.Context, userID, projectID string, offset, limit int) ([]*model.WechatAnalyticsImportBatch, int64, error) {
	project, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return nil, 0, err
	}
	if project.UserID != userID {
		return nil, 0, errors.New("project does not belong to user")
	}
	return s.repo.WechatAnalyticsImports().ListBatches(ctx, projectID, offset, limit)
}

func (s *WechatAnalyticsImportService) Snapshots(ctx context.Context, userID, projectID, publicationID string) ([]*model.WechatAnalyticsSnapshot, error) {
	project, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if project.UserID != userID {
		return nil, errors.New("project does not belong to user")
	}
	publication, err := s.repo.WechatPublications().FindByID(ctx, publicationID)
	if err != nil {
		return nil, err
	}
	if publication.ProjectID != projectID {
		return nil, gorm.ErrRecordNotFound
	}
	return s.analyticsSnapshotsForPublication(ctx, publication)
}

func (s *WechatAnalyticsImportService) SnapshotsByPublicationID(ctx context.Context, userID, publicationID string) ([]*model.WechatAnalyticsSnapshot, error) {
	publication, err := s.repo.WechatPublications().FindByID(ctx, publicationID)
	if err != nil {
		return nil, err
	}
	if publication.UserID != userID {
		return nil, errors.New("article does not belong to user")
	}
	return s.analyticsSnapshotsForPublication(ctx, publication)
}

func (s *WechatAnalyticsImportService) analyticsSnapshotsForPublication(ctx context.Context, publication *model.WechatPublication) ([]*model.WechatAnalyticsSnapshot, error) {
	imported, err := s.repo.WechatAnalyticsImports().FindSnapshotsByPublicationID(ctx, publication.ProjectID, publication.ID)
	if err != nil {
		return nil, err
	}
	official, err := s.repo.WechatMetricSnapshots().FindByTaskID(ctx, publication.TaskID)
	if err != nil {
		return nil, err
	}
	snapshots := append([]*model.WechatAnalyticsSnapshot(nil), imported...)
	for _, item := range official {
		dataAsOf := item.CapturedAt
		if parsed, parseErr := time.ParseInLocation("2006-01-02", item.StatDate, wechatAnalyticsLocation); parseErr == nil {
			dataAsOf = parsed
		}
		readUsers, shareUsers := int64(item.ReadUsers), int64(item.ShareUsers)
		readToFollowUsers := int64(item.ReadToSubscribeUsers)
		readCompletionRate := item.ReadFinishRate
		snapshots = append(snapshots, &model.WechatAnalyticsSnapshot{
			ID: item.ID, ProjectID: publication.ProjectID, PublicationID: publication.ID,
			Source: "wechat_official_api", DataAsOfAt: dataAsOf, ImportedAt: item.CapturedAt,
			ReadUsers: &readUsers, ShareUsers: &shareUsers, ReadToFollowUsers: &readToFollowUsers,
			ReadCompletionRate: &readCompletionRate,
		})
	}
	sort.SliceStable(snapshots, func(i, j int) bool {
		if snapshots[i].DataAsOfAt.Equal(snapshots[j].DataAsOfAt) {
			if snapshots[i].Source == snapshots[j].Source {
				return snapshots[i].ImportedAt.After(snapshots[j].ImportedAt)
			}
			return snapshots[i].Source == "wechat_official_api"
		}
		return snapshots[i].DataAsOfAt.After(snapshots[j].DataAsOfAt)
	})
	return snapshots, nil
}
