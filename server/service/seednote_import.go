package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
	"github.com/google/uuid"
)

type SeednoteImportService struct {
	repo  repository.Repository
	store storage.Provider
	now   func() time.Time
}

func NewSeednoteImportService(repo repository.Repository, store storage.Provider) *SeednoteImportService {
	return &SeednoteImportService{repo: repo, store: store, now: time.Now}
}

type SeednoteImportRequest struct {
	Selections           []AnalyticsSelection `json:"selections"`
	UserID               string
	ProjectID            string
	UploadID             string     `json:"upload_id"`
	DataAsOfAt           *time.Time `json:"data_as_of_at,omitempty"`
	Timezone             string     `json:"timezone,omitempty"`
	ClientFileModifiedAt *time.Time `json:"client_file_modified_at,omitempty"`
}

type SeednoteImportSummary struct {
	Batch *model.SeednoteImportBatch `json:"batch"`
	Rows  []*model.SeednoteImportRow `json:"rows,omitempty"`
}

func (s *SeednoteImportService) loadWorkbook(ctx context.Context, req SeednoteImportRequest) (*model.Asset, []byte, *ParsedSeednoteWorkbook, *time.Location, error) {
	if s.repo == nil || s.store == nil {
		return nil, nil, nil, nil, errors.New("seednote import unavailable")
	}
	if err := s.checkProject(ctx, req.UserID, req.ProjectID); err != nil {
		return nil, nil, nil, nil, err
	}
	asset, err := s.repo.Assets().FindOwnedByID(ctx, strings.TrimSpace(req.UploadID), req.UserID)
	if err != nil {
		if session, sessionErr := s.repo.UploadSessions().FindByID(ctx, strings.TrimSpace(req.UploadID)); sessionErr == nil && session.UserID == req.UserID && session.Purpose == DirectUploadPurposeSeednoteImport {
			finalStore, ok := s.store.(DirectUploadFinalizationStorage)
			if !ok {
				return nil, nil, nil, nil, errors.New("storage does not support upload finalization")
			}
			asset, err = FinalizeUploadSession(ctx, finalStore, s.repo, FinalizeUploadRequest{SessionID: req.UploadID, UserID: req.UserID, AllowedPurposes: []string{DirectUploadPurposeSeednoteImport}})
		}
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("find import asset: %w", err)
		}
	}
	if asset.Purpose != DirectUploadPurposeSeednoteImport || !strings.HasSuffix(strings.ToLower(asset.FileName), ".xlsx") {
		return nil, nil, nil, nil, errors.New("upload is not a seednote xlsx import")
	}
	data, err := storage.ReadObject(ctx, s.store, asset.StorageKey, MaxSeednoteImportBytes)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("read import asset: %w", err)
	}
	location, err := time.LoadLocation(defaultSeednoteImportTimezone(req.Timezone))
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("invalid timezone: %w", err)
	}
	parsed, err := ParseSeednoteWorkbook(data, location)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return asset, data, parsed, location, nil
}

func (s *SeednoteImportService) Import(ctx context.Context, req SeednoteImportRequest) (*SeednoteImportSummary, error) {
	if len(req.Selections) == 0 {
		return nil, errors.New("select at least one content row to import")
	}
	asset, data, parsed, location, err := s.loadWorkbook(ctx, req)
	if err != nil {
		return nil, err
	}

	now := s.now()
	dataAsOf := now
	if parsed.Metadata.Modified != nil {
		dataAsOf = parsed.Metadata.Modified.In(location)
	}
	if parsed.Metadata.Modified == nil && parsed.Metadata.Created != nil {
		dataAsOf = parsed.Metadata.Created.In(location)
	}
	// Browser metadata is only a fallback. The workbook's own timestamps are
	// the source of truth when present, otherwise a re-upload from another
	// browser could silently move the reporting day.
	if parsed.Metadata.Modified == nil && parsed.Metadata.Created == nil && req.ClientFileModifiedAt != nil {
		dataAsOf = req.ClientFileModifiedAt.In(location)
	}
	if req.DataAsOfAt != nil {
		dataAsOf = req.DataAsOfAt.In(location)
	}
	metadataRaw, _ := json.Marshal(parsed.Metadata)
	hash := sha256.Sum256(data)
	batch := &model.SeednoteImportBatch{
		ID: uuid.NewString(), UserID: req.UserID, ProjectID: req.ProjectID, AssetID: asset.ID,
		FileName: asset.FileName, ContentType: asset.ContentType, FileSize: int64(len(data)), SHA256: hex.EncodeToString(hash[:]),
		ReceivedAt: now, ClientModifiedAt: req.ClientFileModifiedAt, SourceCreatedAt: parsed.Metadata.Created, SourceModifiedAt: parsed.Metadata.Modified,
		SourceMetadataJSON: string(metadataRaw), DataAsOfAt: dataAsOf, Timezone: defaultSeednoteImportTimezone(req.Timezone),
		ParserVersion: SeednoteImportParserVersion, Status: model.SeednoteImportBatchStatusProcessing, TotalRows: len(parsed.Rows),
	}
	rows := make([]*model.SeednoteImportRow, 0, len(parsed.Rows))
	for _, parsedRow := range parsed.Rows {
		raw, _ := json.Marshal(parsedRow.Raw)
		row := &model.SeednoteImportRow{ID: uuid.NewString(), BatchID: batch.ID, ProjectID: req.ProjectID, SourceRow: parsedRow.SourceRow, RawData: string(raw), NormalizedTitle: parsedRow.NormalizedTitle, Title: parsedRow.Title, FirstPublishedAt: parsedRow.FirstPublishedAt, Genre: parsedRow.Genre, ExposureCount: parsedRow.Exposure, ViewCount: parsedRow.ViewCount, CoverClickRate: parsedRow.CoverClickRate, LikeCount: parsedRow.LikeCount, CommentCount: parsedRow.CommentCount, CollectCount: parsedRow.CollectCount, FollowerGainCount: parsedRow.FollowerGain, ShareCount: parsedRow.ShareCount, AvgWatchDuration: parsedRow.AvgWatchDuration, BarrageCount: parsedRow.BarrageCount, ParseError: parsedRow.ParseError, MatchStatus: model.SeednoteImportRowStatusNeedsReview}
		if parsedRow.ParseError != "" {
			row.MatchStatus = model.SeednoteImportRowStatusInvalid
			batch.InvalidRows++
		}
		if row.ParseError == "" {
			row.MatchStatus = model.SeednoteImportRowStatusSkipped
		}

		rows = append(rows, row)
	}
	batch.Status = model.SeednoteImportBatchStatusCompleted
	if err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		// Serialize creation of the analytics identity shared by a task.
		if err := tx.WechatAnalyticsImports().LockProject(ctx, req.ProjectID); err != nil {
			return err
		}
		candidates, err := loadAnalyticsCandidates(ctx, tx, req.UserID, req.ProjectID)
		if err != nil {
			return err
		}
		selected, err := resolveAnalyticsSelections(req.Selections, candidates)
		if err != nil {
			return err
		}
		byRow := make(map[int]*model.SeednoteImportRow, len(rows))
		for _, row := range rows {
			byRow[row.SourceRow] = row
		}
		for sourceRow := range selected {
			row := byRow[sourceRow]
			if row == nil {
				return fmt.Errorf("source row %d does not exist", sourceRow)
			}
			if row.ParseError != "" {
				return fmt.Errorf("source row %d is invalid: %s", sourceRow, row.ParseError)
			}
		}
		selectedRows := make([]*model.SeednoteImportRow, 0, len(selected))
		for _, row := range rows {
			if _, ok := selected[row.SourceRow]; ok {
				selectedRows = append(selectedRows, row)
			}
		}
		rows = selectedRows
		batch.TotalRows, batch.InvalidRows = len(rows), 0
		for _, row := range rows {
			candidate, ok := selected[row.SourceRow]
			if !ok {
				continue
			}
			post := candidate.Post
			if post == nil {
				if candidate.Task == nil {
					return errors.New("seednote target must be a same-project task or post")
				}
				post = &model.SeednotePost{ID: uuid.NewString(), UserID: req.UserID, ProjectID: req.ProjectID, TaskID: candidate.Task.ID, FirstPublishedAtBatchID: batch.ID, Title: candidate.Title, NormalizedTitle: NormalizeSeednoteTitle(candidate.Title), FirstPublishedAt: row.FirstPublishedAt, Genre: row.Genre}
				if err := tx.SeednotePosts().Create(ctx, post); err != nil {
					return err
				}
			} else if post.Genre == "" && row.Genre != "" {
				post.Genre = row.Genre
				if err := tx.SeednotePosts().Update(ctx, post); err != nil {
					return err
				}
			}
			row.PostID, row.CandidatePostID = post.ID, post.ID
			row.MatchStatus, row.CandidateConfidence = model.SeednoteImportRowStatusMatched, 1
			batch.ResolvedRows++
			if err := tx.SeednotePostAliases().Create(ctx, &model.SeednotePostAlias{ID: uuid.NewString(), PostID: post.ID, BatchID: batch.ID, NormalizedTitle: row.NormalizedTitle, FirstPublishedAt: row.FirstPublishedAt}); err != nil {
				return err
			}
		}
		if err := tx.SeednoteImports().CreateBatch(ctx, batch); err != nil {
			return err
		}
		if err := tx.SeednoteImports().CreateRows(ctx, rows); err != nil {
			return err
		}
		for _, row := range rows {
			if row.PostID != "" {
				if err := s.createMetricVersion(ctx, tx, batch, row, now); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("persist import: %w", err)
	}
	return &SeednoteImportSummary{Batch: batch, Rows: rows}, nil
}

func defaultSeednoteImportTimezone(value string) string {
	if strings.TrimSpace(value) == "" {
		return "Asia/Shanghai"
	}
	return strings.TrimSpace(value)
}

type SeednoteImportOverview struct {
	Dates         []string                `json:"dates"`
	Series        []SeednoteOverviewPoint `json:"series"`
	Posts         []*model.SeednotePost   `json:"posts"`
	PostSummaries []SeednotePostSummary   `json:"post_summaries"`
}

type SeednotePostSummary struct {
	DataAsOfAt        *time.Time `json:"data_as_of_at"`
	Genre             string     `json:"genre,omitempty"`
	ContentType       string     `json:"content_type,omitempty"`
	TaskID            string     `json:"task_id,omitempty"`
	ID                string     `json:"id"`
	Title             string     `json:"title"`
	NoteID            string     `json:"note_id,omitempty"`
	NoteURL           string     `json:"note_url,omitempty"`
	FirstPublishedAt  *time.Time `json:"first_published_at,omitempty"`
	ExposureCount     *int64     `json:"exposure_count"`
	ViewCount         *int64     `json:"view_count"`
	CoverClickRate    *float64   `json:"cover_click_rate"`
	LikeCount         *int64     `json:"like_count"`
	CommentCount      *int64     `json:"comment_count"`
	CollectCount      *int64     `json:"collect_count"`
	FollowerGainCount *int64     `json:"follower_gain_count"`
	ShareCount        *int64     `json:"share_count"`
	AvgWatchDuration  *float64   `json:"avg_watch_duration"`
	BarrageCount      *int64     `json:"barrage_count"`
}
type SeednoteOverviewPoint struct {
	Date              string   `json:"date"`
	ExposureCount     *int64   `json:"exposure_count"`
	ViewCount         *int64   `json:"view_count"`
	LikeCount         *int64   `json:"like_count"`
	CommentCount      *int64   `json:"comment_count"`
	CollectCount      *int64   `json:"collect_count"`
	FollowerGainCount *int64   `json:"follower_gain_count"`
	ShareCount        *int64   `json:"share_count"`
	BarrageCount      *int64   `json:"barrage_count"`
	CoverClickRate    *float64 `json:"cover_click_rate"`
	AvgWatchDuration  *float64 `json:"avg_watch_duration"`
}

func (s *SeednoteImportService) createMetricVersion(ctx context.Context, tx repository.Repository, batch *model.SeednoteImportBatch, row *model.SeednoteImportRow, now time.Time) error {
	raw := row.RawData
	return tx.SeednoteMetricVersions().Create(ctx, &model.SeednoteMetricVersion{ID: uuid.NewString(), PostID: row.PostID, BatchID: batch.ID, ImportRowID: row.ID, DataAsOfAt: batch.DataAsOfAt, ImportedAt: now, ExposureCount: row.ExposureCount, ViewCount: row.ViewCount, CoverClickRate: row.CoverClickRate, LikeCount: row.LikeCount, CommentCount: row.CommentCount, CollectCount: row.CollectCount, FollowerGainCount: row.FollowerGainCount, ShareCount: row.ShareCount, AvgWatchDuration: row.AvgWatchDuration, BarrageCount: row.BarrageCount, RawData: raw})
}

func (s *SeednoteImportService) ListBatches(ctx context.Context, userID, projectID string, offset, limit int) ([]*model.SeednoteImportBatch, int64, error) {
	if err := s.checkProject(ctx, userID, projectID); err != nil {
		return nil, 0, err
	}
	return s.repo.SeednoteImports().ListBatches(ctx, projectID, offset, limit)
}
func (s *SeednoteImportService) GetBatch(ctx context.Context, userID, projectID, batchID string) (*SeednoteImportSummary, error) {
	if err := s.checkProject(ctx, userID, projectID); err != nil {
		return nil, err
	}
	batch, err := s.repo.SeednoteImports().FindBatchByID(ctx, projectID, batchID)
	if err != nil {
		return nil, err
	}
	rows, err := s.repo.SeednoteImports().FindRowsByBatchID(ctx, projectID, batchID)
	if err != nil {
		return nil, err
	}
	return &SeednoteImportSummary{Batch: batch, Rows: rows}, nil
}
func (s *SeednoteImportService) FileURL(ctx context.Context, userID, projectID, batchID string) (string, time.Time, error) {
	if err := s.checkProject(ctx, userID, projectID); err != nil {
		return "", time.Time{}, err
	}
	batch, err := s.repo.SeednoteImports().FindBatchByID(ctx, projectID, batchID)
	if err != nil {
		return "", time.Time{}, err
	}
	asset, err := s.repo.Assets().FindOwnedByID(ctx, batch.AssetID, userID)
	if err != nil {
		return "", time.Time{}, err
	}
	url, err := s.store.DownloadURL(ctx, asset.StorageKey, DefaultSignedURLTTL)
	if err != nil {
		return "", time.Time{}, err
	}
	return url, s.now().Add(DefaultSignedURLTTL * time.Second), nil
}
func (s *SeednoteImportService) ListPosts(ctx context.Context, userID, projectID, search string, offset, limit int) ([]*model.SeednotePost, int64, error) {
	if err := s.checkProject(ctx, userID, projectID); err != nil {
		return nil, 0, err
	}
	posts, total, err := s.repo.SeednotePosts().ListByProject(ctx, projectID, search, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	if err := s.populatePostPublicIdentities(ctx, userID, projectID, posts); err != nil {
		return nil, 0, err
	}
	return posts, total, nil
}
func (s *SeednoteImportService) GetPost(ctx context.Context, userID, projectID, postID string, from, to *time.Time) (*model.SeednotePost, []*model.SeednoteMetricVersion, error) {
	if err := s.checkProject(ctx, userID, projectID); err != nil {
		return nil, nil, err
	}
	post, err := s.repo.SeednotePosts().FindByID(ctx, projectID, postID)
	if err != nil {
		return nil, nil, err
	}
	versions, err := s.repo.SeednoteMetricVersions().FindByPostID(ctx, postID, from, to)
	if err != nil {
		return nil, nil, err
	}
	if err := s.populatePostPublicIdentities(ctx, userID, projectID, []*model.SeednotePost{post}); err != nil {
		return nil, nil, err
	}
	return post, versions, nil
}

// Imported posts and task tracking retain separate lifecycles. Only an explicit
// task association may supply a missing public identity; titles are not identity.
func (s *SeednoteImportService) populatePostPublicIdentities(ctx context.Context, userID, projectID string, posts []*model.SeednotePost) error {
	var taskIDs []string
	for _, post := range posts {
		post.ContentType = post.Genre
		if post.TaskID != "" && post.NoteURL == "" {
			taskIDs = append(taskIDs, post.TaskID)
		}
	}
	if len(taskIDs) == 0 {
		return nil
	}
	trackings, err := s.repo.SeednoteTrackings().FindByTaskIDs(ctx, userID, projectID, taskIDs)
	if err != nil {
		return err
	}
	byTask := make(map[string]*model.SeednotePostTracking, len(trackings))
	for _, tracking := range trackings {
		byTask[tracking.TaskID] = tracking
	}
	for _, post := range posts {
		tracking := byTask[post.TaskID]
		if tracking == nil || post.NoteURL != "" || (post.NoteID != "" && post.NoteID != tracking.NoteID) {
			continue
		}
		post.NoteID, post.NoteURL = tracking.NoteID, tracking.NoteURL
	}
	return nil
}

// Imported timestamps may be decoded in UTC by the database. Analytics buckets
// use the same Shanghai calendar as the API's date filters, independently of
// the timestamp's retained location.
var seednoteAnalyticsLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

func seednoteAnalyticsDate(at time.Time) string {
	return at.In(seednoteAnalyticsLocation).Format("2006-01-02")
}

func (s *SeednoteImportService) Overview(ctx context.Context, userID, projectID string, from, to *time.Time) (*SeednoteImportOverview, error) {
	if err := s.checkProject(ctx, userID, projectID); err != nil {
		return nil, err
	}
	versions, err := s.repo.SeednoteMetricVersions().FindByProject(ctx, projectID, from, to)
	if err != nil {
		return nil, err
	}
	latest := map[string]*model.SeednoteMetricVersion{}
	for _, version := range versions {
		key := version.PostID + "|" + seednoteAnalyticsDate(version.DataAsOfAt)
		if current := latest[key]; current == nil || version.ImportedAt.After(current.ImportedAt) {
			latest[key] = version
		}
	}
	byDate := map[string]*SeednoteOverviewPoint{}
	postExposure := map[string]int64{}
	rateSums := map[string]float64{}
	rateCounts := map[string]int{}
	durationSums := map[string]float64{}
	durationCounts := map[string]int{}
	for _, version := range latest {
		date := seednoteAnalyticsDate(version.DataAsOfAt)
		point := byDate[date]
		if point == nil {
			point = &SeednoteOverviewPoint{Date: date}
			byDate[date] = point
		}
		addSeednoteOptionalCount(&point.ExposureCount, version.ExposureCount)
		if version.ExposureCount != nil {
			postExposure[version.PostID] += *version.ExposureCount
		}
		addSeednoteOptionalCount(&point.ViewCount, version.ViewCount)
		addSeednoteOptionalCount(&point.LikeCount, version.LikeCount)
		addSeednoteOptionalCount(&point.CommentCount, version.CommentCount)
		addSeednoteOptionalCount(&point.CollectCount, version.CollectCount)
		addSeednoteOptionalCount(&point.FollowerGainCount, version.FollowerGainCount)
		addSeednoteOptionalCount(&point.ShareCount, version.ShareCount)
		addSeednoteOptionalCount(&point.BarrageCount, version.BarrageCount)
		if version.CoverClickRate != nil {
			rateSums[date] += *version.CoverClickRate
			rateCounts[date]++
		}
		if version.AvgWatchDuration != nil {
			durationSums[date] += *version.AvgWatchDuration
			durationCounts[date]++
		}
	}
	for date, point := range byDate {
		if rateCount := rateCounts[date]; rateCount > 0 {
			value := rateSums[date] / float64(rateCount)
			point.CoverClickRate = &value
		}
		if durationCount := durationCounts[date]; durationCount > 0 {
			value := durationSums[date] / float64(durationCount)
			point.AvgWatchDuration = &value
		}
	}
	posts, totalPosts, postsErr := s.repo.SeednotePosts().ListByProject(ctx, projectID, "", 0, 100)
	if postsErr != nil {
		return nil, postsErr
	}
	if totalPosts > int64(len(posts)) {
		posts, _, postsErr = s.repo.SeednotePosts().ListByProject(ctx, projectID, "", 0, int(totalPosts))
		if postsErr != nil {
			return nil, postsErr
		}
	}
	if err := s.populatePostPublicIdentities(ctx, userID, projectID, posts); err != nil {
		return nil, err
	}
	// The overview's post list is used as a ranking in Studio. Rank by the
	// selected range's exposure total, with newest posts as a stable tie-break.
	sort.SliceStable(posts, func(i, j int) bool {
		if postExposure[posts[i].ID] != postExposure[posts[j].ID] {
			return postExposure[posts[i].ID] > postExposure[posts[j].ID]
		}
		return posts[i].CreatedAt.After(posts[j].CreatedAt)
	})
	result := &SeednoteImportOverview{Dates: make([]string, 0, len(byDate)), Series: make([]SeednoteOverviewPoint, 0, len(byDate)), Posts: posts, PostSummaries: aggregateSeednotePostSummaries(posts, versions)}
	for date, point := range byDate {
		result.Dates = append(result.Dates, date)
		result.Series = append(result.Series, *point)
	}
	sort.Slice(result.Series, func(i, j int) bool { return result.Series[i].Date < result.Series[j].Date })
	sort.Strings(result.Dates)
	return result, nil
}

type postSummaryAccumulator struct {
	SeednotePostSummary
	createdAt     time.Time
	hasData       bool
	rateSum       float64
	rateCount     int
	durationSum   float64
	durationCount int
}

func aggregateSeednotePostSummaries(posts []*model.SeednotePost, versions []*model.SeednoteMetricVersion) []SeednotePostSummary {
	latest := map[string]*model.SeednoteMetricVersion{}
	for _, version := range versions {
		key := version.PostID + "|" + seednoteAnalyticsDate(version.DataAsOfAt)
		if current := latest[key]; current == nil || version.ImportedAt.After(current.ImportedAt) {
			latest[key] = version
		}
	}
	byPost := make(map[string]*postSummaryAccumulator, len(posts))
	for _, post := range posts {
		byPost[post.ID] = &postSummaryAccumulator{SeednotePostSummary: SeednotePostSummary{ID: post.ID, Title: post.Title, Genre: post.Genre, ContentType: post.Genre, TaskID: post.TaskID, NoteID: post.NoteID, NoteURL: post.NoteURL, FirstPublishedAt: post.FirstPublishedAt}, createdAt: post.CreatedAt}
	}
	for _, version := range latest {
		item := byPost[version.PostID]
		if item == nil {
			continue
		}
		item.hasData = true
		if item.DataAsOfAt == nil || version.DataAsOfAt.After(*item.DataAsOfAt) {
			value := version.DataAsOfAt
			item.DataAsOfAt = &value
		}
		addSeednoteOptionalCount(&item.ExposureCount, version.ExposureCount)
		addSeednoteOptionalCount(&item.ViewCount, version.ViewCount)
		addSeednoteOptionalCount(&item.LikeCount, version.LikeCount)
		addSeednoteOptionalCount(&item.CommentCount, version.CommentCount)
		addSeednoteOptionalCount(&item.CollectCount, version.CollectCount)
		addSeednoteOptionalCount(&item.FollowerGainCount, version.FollowerGainCount)
		addSeednoteOptionalCount(&item.ShareCount, version.ShareCount)
		addSeednoteOptionalCount(&item.BarrageCount, version.BarrageCount)
		if version.CoverClickRate != nil {
			item.rateSum += *version.CoverClickRate
			item.rateCount++
		}
		if version.AvgWatchDuration != nil {
			item.durationSum += *version.AvgWatchDuration
			item.durationCount++
		}
	}
	result := make([]SeednotePostSummary, 0, len(posts))
	for _, post := range posts {
		item := byPost[post.ID]
		if !item.hasData {
			continue
		}
		if item.rateCount > 0 {
			value := item.rateSum / float64(item.rateCount)
			item.CoverClickRate = &value
		}
		if item.durationCount > 0 {
			value := item.durationSum / float64(item.durationCount)
			item.AvgWatchDuration = &value
		}
		result = append(result, item.SeednotePostSummary)
	}
	sort.SliceStable(result, func(i, j int) bool {
		left, right := result[i].ExposureCount, result[j].ExposureCount
		if (left == nil) != (right == nil) {
			return left != nil
		}
		if left != nil && *left != *right {
			return *left > *right
		}
		return byPost[result[i].ID].createdAt.After(byPost[result[j].ID].createdAt)
	})
	return result
}

func addSeednoteOptionalCount(target **int64, value *int64) {
	if value == nil {
		return
	}
	if *target == nil {
		*target = new(int64)
	}
	**target += *value
}
func (s *SeednoteImportService) checkProject(ctx context.Context, userID, projectID string) error {
	project, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return err
	}
	if project.UserID != userID {
		return errors.New("project does not belong to user")
	}
	if project.Platform != model.PlatformSeednote {
		return errors.New("project is not a seednote project")
	}
	return nil
}
