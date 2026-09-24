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
	UserID               string               `json:"-"`
	ProjectID            string               `json:"-"`
	UploadID             string               `json:"upload_id"`
	Selections           []AnalyticsSelection `json:"selections"`
	DataAsOfAt           *time.Time           `json:"data_as_of_at,omitempty"`
	Timezone             string               `json:"timezone,omitempty"`
	ClientFileModifiedAt *time.Time           `json:"client_file_modified_at,omitempty"`
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
	Rows          []WechatAnalyticsPreviewRow   `json:"rows"`
}

type WechatAnalyticsPreviewRow struct {
	ParsedWechatAnalyticsRow
	MatchStatus   string           `json:"match_status"`
	PublicationID string           `json:"publication_id,omitempty"`
	Target        *AnalyticsTarget `json:"target,omitempty"`
	TargetTitle   string           `json:"target_title,omitempty"`
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
	project, asset, parsed, _, err := s.readWorkbook(ctx, req)
	if err != nil {
		return nil, err
	}
	candidates, err := loadAnalyticsCandidates(ctx, s.repo, req.UserID, project.ID)
	if err != nil {
		return nil, err
	}
	rows := make([]WechatAnalyticsPreviewRow, 0, len(parsed.Rows))
	for _, row := range parsed.Rows {
		rows = append(rows, previewWechatCandidateRow(row, candidates))
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
	if len(req.Selections) == 0 {
		return nil, errors.New("请至少选择一行可导入的数据")
	}
	requested := map[int]bool{}
	for _, selection := range req.Selections {
		requested[selection.SourceRow] = true
	}
	selectedRows := make([]ParsedWechatAnalyticsRow, 0, len(req.Selections))
	for _, row := range parsed.Rows {
		if requested[row.SourceRow] {
			selectedRows = append(selectedRows, row)
			delete(requested, row.SourceRow)
		}
	}
	if len(requested) > 0 {
		return nil, errors.New("选择包含不在当前文件中的行，请重新预览")
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
		TotalRows: len(selectedRows),
	}
	rows := make([]*model.WechatAnalyticsImportRow, 0, len(selectedRows))
	err = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		if err := tx.WechatAnalyticsImports().LockProject(ctx, project.ID); err != nil {
			return err
		}
		candidates, err := loadAnalyticsCandidates(ctx, tx, req.UserID, project.ID)
		if err != nil {
			return err
		}
		selected, err := resolveAnalyticsSelections(req.Selections, candidates)
		if err != nil {
			return err
		}
		for _, parsedRow := range selectedRows {
			if parsedRow.ParseError != "" {
				return fmt.Errorf("第 %d 行数据无效：%s", parsedRow.SourceRow, parsedRow.ParseError)
			}
			candidate := selected[parsedRow.SourceRow]
			row := &model.WechatAnalyticsImportRow{
				ID: uuid.NewString(), BatchID: batch.ID, ProjectID: project.ID, SourceRow: parsedRow.SourceRow,
				RawData: marshalWechatRaw(parsedRow), Source: parsedRow.Source, Title: parsedRow.Title,
				NormalizedTitle: parsedRow.NormalizedTitle, PublishedDate: parsedRow.PublishedDate,
				ArticleURL: parsedRow.ArticleURL, ReadUsers: parsedRow.ReadUsers, ShareUsers: parsedRow.ShareUsers,
				ReadToFollowUsers: parsedRow.ReadToFollowUsers, DeliveredUsers: parsedRow.DeliveredUsers,
				DeliveryCompletionRate: parsedRow.DeliveryCompletionRate, ReadCompletionRate: parsedRow.ReadCompletionRate,
				ParseError: parsedRow.ParseError, MatchStatus: model.WechatAnalyticsImportRowUnmatched,
			}
			row.MatchStatus = model.WechatAnalyticsImportRowMatched
			if candidate.Task != nil {
				row.TaskID = candidate.Task.ID
			}
			if candidate.Publication != nil {
				row.PublicationID = candidate.Publication.ID
			}
			switch row.MatchStatus {
			case model.WechatAnalyticsImportRowInvalid:
				batch.InvalidRows++
			case model.WechatAnalyticsImportRowMatched:
				batch.MatchedRows++
			case model.WechatAnalyticsImportRowNeedsReview:
				batch.ReviewRows++
			default:
				batch.UnmatchedRows++
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
			if row.MatchStatus != model.WechatAnalyticsImportRowMatched {
				continue
			}
			if err := tx.WechatAnalyticsImports().CreateSnapshot(ctx, snapshotFromWechatImport(batch, row, now)); err != nil {
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

func previewWechatCandidateRow(row ParsedWechatAnalyticsRow, candidates []AnalyticsCandidate) WechatAnalyticsPreviewRow {
	preview := WechatAnalyticsPreviewRow{ParsedWechatAnalyticsRow: row, MatchStatus: model.WechatAnalyticsImportRowUnmatched}
	if row.ParseError != "" {
		preview.MatchStatus = model.WechatAnalyticsImportRowInvalid
		return preview
	}
	match, ambiguous := matchAnalyticsCandidate(row.Title, row.PublishedDate, row.ArticleURL, candidates)
	if match != nil {
		preview.Target = &match.Target
		preview.TargetTitle = match.Title
		preview.MatchStatus = model.WechatAnalyticsImportRowMatched
		if match.Publication != nil {
			preview.PublicationID = match.Publication.ID
		}
	} else if ambiguous {
		preview.MatchStatus = model.WechatAnalyticsImportRowNeedsReview
	}
	return preview
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
	return &model.WechatAnalyticsSnapshot{ID: uuid.NewString(), ProjectID: batch.ProjectID, PublicationID: row.PublicationID, TaskID: row.TaskID, BatchID: batch.ID, ImportRowID: row.ID, Source: row.Source, DataAsOfAt: batch.DataAsOfAt, ImportedAt: importedAt, ReadUsers: row.ReadUsers, ShareUsers: row.ShareUsers, ReadToFollowUsers: row.ReadToFollowUsers, DeliveredUsers: row.DeliveredUsers, DeliveryCompletionRate: row.DeliveryCompletionRate, ReadCompletionRate: row.ReadCompletionRate, RawData: row.RawData}
}

type AnalyticsTaskIdentity struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}
type WechatAnalyticsArticleView struct {
	URL         string                           `json:"url,omitempty"`
	Target      AnalyticsTarget                  `json:"target"`
	Task        *AnalyticsTaskIdentity           `json:"task,omitempty"`
	ContentType string                           `json:"content_type"`
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
	candidates, err := loadAnalyticsCandidates(ctx, s.repo, userID, projectID)
	if err != nil {
		return nil, err
	}
	views := make([]WechatAnalyticsArticleView, 0, len(candidates))
	for _, candidate := range candidates {
		view := WechatAnalyticsArticleView{Target: candidate.Target, Publication: candidate.Publication, ContentType: candidate.ContentType, URL: candidate.URL}
		if candidate.Task != nil {
			task := candidate.Task
			view.Task = &AnalyticsTaskIdentity{ID: task.ID, Title: candidate.Title, Status: task.Status, CreatedAt: task.CreatedAt}
		}
		var snapshots []*model.WechatAnalyticsSnapshot
		if candidate.Publication != nil {
			snapshots, err = s.analyticsSnapshotsForPublication(ctx, candidate.Publication)
		} else if candidate.Task != nil {
			snapshots, err = s.repo.WechatAnalyticsImports().FindSnapshotsByTaskID(ctx, projectID, candidate.Task.ID)
		}
		if err != nil {
			return nil, err
		}
		if candidate.Publication == nil && len(snapshots) == 0 {
			continue
		}
		view.Snapshots = snapshots
		if len(snapshots) > 0 {
			view.Latest = snapshots[0]
			var raw map[string]string
			if json.Unmarshal([]byte(view.Latest.RawData), &raw) == nil {
				if importedType := wechatAnalyticsContentType(raw); importedType != "unknown" {
					view.ContentType = importedType
				}
			}
		}
		if view.URL == "" {
			view.URL = historicalWechatArticleURL(snapshots)
		}
		if view.Publication != nil && view.Publication.ArticleURL == "" {
			view.Publication.ArticleURL = historicalWechatArticleURL(snapshots)
		}
		views = append(views, view)
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
		if candidate != "" && analyticsPublicIdentity(candidate) != analyticsPublicIdentity(articleURL) {
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
		if article.Publication != nil && article.Publication.Status == model.WechatPublicationStatusPublished {
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
	if errors.Is(err, gorm.ErrRecordNotFound) {
		task, taskErr := s.repo.Tasks().FindByID(ctx, publicationID)
		if taskErr != nil {
			return nil, taskErr
		}
		if task.UserID != userID || task.ProjectID != projectID {
			return nil, gorm.ErrRecordNotFound
		}
		linked, linkedErr := s.repo.WechatPublications().FindByTaskID(ctx, task.ID)
		if linkedErr == nil {
			if linked.ProjectID != projectID || linked.UserID != userID {
				return nil, gorm.ErrRecordNotFound
			}
			return s.analyticsSnapshotsForPublication(ctx, linked)
		}
		if !errors.Is(linkedErr, gorm.ErrRecordNotFound) {
			return nil, linkedErr
		}
		return s.repo.WechatAnalyticsImports().FindSnapshotsByTaskID(ctx, projectID, task.ID)
	}
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
	if publication.TaskID != "" {
		taskSnapshots, taskErr := s.repo.WechatAnalyticsImports().FindSnapshotsByTaskID(ctx, publication.ProjectID, publication.TaskID)
		if taskErr != nil {
			return nil, taskErr
		}
		seen := map[string]bool{}
		for _, v := range imported {
			seen[v.ID] = true
		}
		for _, v := range taskSnapshots {
			if !seen[v.ID] {
				imported = append(imported, v)
			}
		}
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
