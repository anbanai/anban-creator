package service

import (
	"context"
	"crypto/sha256"
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
	"gorm.io/gorm"
)

var (
	ErrContentReuseRunStopped   = errors.New("content reuse run is not running")
	ErrContentAssetNotFound     = errors.New("content asset not found")
	ErrContentReuseConflict     = errors.New("content reuse request conflicts with an existing run")
	ErrInvalidReuseRunState     = errors.New("invalid content reuse run state transition")
	ErrInvalidContentAssetState = errors.New("invalid content asset state transition")
)

const contentReuseMaxRetries = 3

type ContentAssetService struct {
	repo              repository.Repository
	enqueuer          ContentReuseEnqueuer
	taskSvc           *TaskService
	billingCatalogSvc *BillingCatalogService
	billingWalletSvc  *BillingWalletService
}

type ContentAssetListParams struct {
	UserID    string
	Status    string
	Query     string
	Platform  string
	Source    string
	Lifecycle string
	ProjectID string
	Offset    int
	Limit     int
}

type ContentReuseEnqueuer interface {
	EnqueueUnique(taskType string, payload []byte, uniqueKey string) (bool, error)
}

func NewContentAssetService(repo repository.Repository) *ContentAssetService {
	return &ContentAssetService{repo: repo}
}

func (s *ContentAssetService) SetEnqueuer(enqueuer ContentReuseEnqueuer) {
	if s != nil {
		s.enqueuer = enqueuer
	}
}

func (s *ContentAssetService) SetTaskService(taskSvc *TaskService) {
	if s != nil {
		s.taskSvc = taskSvc
	}
}

func (s *ContentAssetService) SetBillingCatalogService(catalog *BillingCatalogService) {
	if s != nil {
		s.billingCatalogSvc = catalog
	}
}

func (s *ContentAssetService) SetBillingWalletService(wallet *BillingWalletService) {
	if s != nil {
		s.billingWalletSvc = wallet
	}
}

type CreateContentAssetParams struct {
	UserID         string
	Title          string
	CoreQuestion   string
	Audience       string
	SemanticKernel string
	Lifecycle      string
	Status         string
}

type IngestContentAssetParams struct {
	CreateContentAssetParams
	Evidence []model.ContentAssetEvidence
}

type UpdateContentAssetParams struct {
	UserID         string
	AssetID        string
	Title          *string
	CoreQuestion   *string
	Audience       *string
	SemanticKernel *string
	Lifecycle      *string
	Status         *string
}

type ContentAssetDetail struct {
	Asset    *model.ContentAsset                `json:"asset"`
	Evidence []*model.ContentAssetEvidence      `json:"evidence"`
	Links    []*model.ContentAssetLink          `json:"links"`
	Scores   []*model.ContentAssetScoreSnapshot `json:"scores"`
}

type ContentAssetLineage struct {
	*ContentAssetDetail
	Links               []*model.ContentAssetLink         `json:"links"`
	Summary             ContentAssetLineageSummary        `json:"summary"`
	PlatformPerformance []ContentAssetPlatformPerformance `json:"platform_performance"`
	LatestRuns          []*model.ContentReuseRun          `json:"latest_runs"`
}

type ContentAssetLineageSummary struct {
	OriginCount     int   `json:"origin_count"`
	DerivativeCount int   `json:"derivative_count"`
	RunCount        int   `json:"run_count"`
	PublishedCount  int   `json:"published_count"`
	CostCredits     int64 `json:"cost_credits"`
	ReservedCredits int64 `json:"reserved_credits"`
}

type ContentAssetPlatformPerformance struct {
	Platform          string                        `json:"platform"`
	ProjectID         string                        `json:"project_id"`
	ContentCount      int                           `json:"content_count"`
	PublishedCount    int                           `json:"published_count"`
	AnalyticsRevision int64                         `json:"analytics_revision,omitempty"`
	DataAsOf          *time.Time                    `json:"data_as_of,omitempty"`
	Metrics           map[string]any                `json:"metrics,omitempty"`
	Contents          []ContentAssetPlatformContent `json:"contents,omitempty"`
	Missing           []string                      `json:"missing,omitempty"`
}

type ContentAssetPlatformContent struct {
	Kind      string         `json:"kind"`
	ID        string         `json:"id"`
	Published bool           `json:"published"`
	Metrics   map[string]any `json:"metrics,omitempty"`
	DataAsOf  *time.Time     `json:"data_as_of,omitempty"`
}

// ContentAssetRecommendation is a Server-computed dashboard summary. The
// browser receives the immutable score snapshot and current run state instead
// of deriving recommendation claims from raw platform metrics.
type ContentAssetRecommendation struct {
	Asset                *model.ContentAsset              `json:"asset"`
	Score                *model.ContentAssetScoreSnapshot `json:"score,omitempty"`
	Run                  *model.ContentReuseRun           `json:"run,omitempty"`
	RecommendedPlatform  string                           `json:"recommended_platform,omitempty"`
	RecommendationReason string                           `json:"recommendation_reason,omitempty"`
	EstimatedCostCredits int64                            `json:"estimated_cost_credits"`
	EvidenceAsOf         *time.Time                       `json:"evidence_as_of,omitempty"`
	AutomationStatus     string                           `json:"automation_status,omitempty"`
	AutomationStage      string                           `json:"automation_stage,omitempty"`
}

// contentReusePublicationPolicy is the immutable publication portion of a
// reuse run's policy snapshot. A reuse run must never consult the user's
// current policy after admission; doing so would make an already admitted run
// change behavior halfway through production.
type contentReusePublicationPolicySnapshot struct {
	DraftEnabled   bool                            `json:"draft_enabled"`
	PublishEnabled bool                            `json:"publish_enabled"`
	PublishWindows []contentAutopilotPublishWindow `json:"publish_windows"`
}

func contentReusePublicationPolicy(ctx context.Context, repo repository.Repository, task *model.Task) (contentReusePublicationPolicySnapshot, error) {
	if task == nil || strings.TrimSpace(task.ReuseRunID) == "" {
		return contentReusePublicationPolicySnapshot{DraftEnabled: true, PublishEnabled: true}, nil
	}
	run, err := repo.ContentAssets().FindReuseRunByID(ctx, strings.TrimSpace(task.ReuseRunID))
	if err != nil {
		return contentReusePublicationPolicySnapshot{}, fmt.Errorf("load content reuse policy snapshot: %w", err)
	}
	if task.UserID != run.UserID || task.ProjectID != run.TargetProjectID || (task.ReuseStageID == "" && run.TaskID != "" && task.ID != run.TaskID) {
		return contentReusePublicationPolicySnapshot{}, fmt.Errorf("content reuse task binding mismatch")
	}
	if run.Status != model.ContentReuseRunStatusRunning {
		return contentReusePublicationPolicySnapshot{}, ErrContentReuseRunStopped
	}
	if len(run.PolicySnapshot) == 0 || string(run.PolicySnapshot) == "null" {
		return contentReusePublicationPolicySnapshot{}, fmt.Errorf("content reuse policy snapshot is missing")
	}
	var policy contentReusePublicationPolicySnapshot
	if err := json.Unmarshal(run.PolicySnapshot, &policy); err != nil {
		return contentReusePublicationPolicySnapshot{}, fmt.Errorf("decode content reuse policy snapshot: %w", err)
	}
	return policy, nil
}

func contentReusePublishWindow(now time.Time, windows []contentAutopilotPublishWindow) (allowed bool, next *time.Time) {
	if len(windows) == 0 {
		return false, nil
	}
	for _, window := range windows {
		location, err := time.LoadLocation(strings.TrimSpace(window.Timezone))
		if err != nil {
			continue
		}
		localNow := now.In(location)
		start, startErr := time.ParseInLocation("15:04", window.Start, location)
		end, endErr := time.ParseInLocation("15:04", window.End, location)
		if startErr != nil || endErr != nil || !start.Before(end) {
			continue
		}
		start = time.Date(localNow.Year(), localNow.Month(), localNow.Day(), start.Hour(), start.Minute(), 0, 0, location)
		end = time.Date(localNow.Year(), localNow.Month(), localNow.Day(), end.Hour(), end.Minute(), 0, 0, location)
		if !localNow.Before(start) && localNow.Before(end) {
			return true, nil
		}
		candidate := start
		if !localNow.Before(start) {
			candidate = start.Add(24 * time.Hour)
		}
		candidateUTC := candidate.UTC()
		if next == nil || candidateUTC.Before(*next) {
			next = &candidateUTC
		}
	}
	return false, next
}

type CreateReuseRunParams struct {
	expectedPolicyRevision int
	scoredRecommendation   *model.ContentAssetScoreSnapshot
	deferDispatch          bool
	UserID                 string
	AssetID                string
	SourceContentKind      string
	SourceContentID        string
	TargetProjectID        string
	TargetPlatform         string
	TargetAgentID          string
	IdempotencyKey         string
	ScoreSnapshot          map[string]any
	PolicySnapshot         map[string]any
}

type LinkContentAssetParams struct {
	UserID      string
	AssetID     string
	ProjectID   string
	ContentKind string
	ContentID   string
	Platform    string
	Relation    string
	Angle       string
}

func (s *ContentAssetService) Create(ctx context.Context, p CreateContentAssetParams) (*model.ContentAsset, error) {
	title := strings.TrimSpace(p.Title)
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if p.Status != "" && p.Status != model.ContentAssetStatusCandidate {
		return nil, fmt.Errorf("new content assets must start as candidate")
	}
	asset := model.NewContentAsset(p.UserID, title)
	asset.CoreQuestion, asset.Audience, asset.SemanticKernel = strings.TrimSpace(p.CoreQuestion), strings.TrimSpace(p.Audience), strings.TrimSpace(p.SemanticKernel)
	if p.Lifecycle != "" {
		lifecycle := strings.TrimSpace(p.Lifecycle)
		if !validContentAssetLifecycle(lifecycle) {
			return nil, fmt.Errorf("invalid content asset lifecycle %q", lifecycle)
		}
		asset.Lifecycle = lifecycle
	}
	if err := s.repo.ContentAssets().Create(ctx, asset); err != nil {
		return nil, fmt.Errorf("create content asset: %w", err)
	}
	return asset, nil
}

func (s *ContentAssetService) Ingest(ctx context.Context, p IngestContentAssetParams) (*ContentAssetDetail, error) {
	var asset *model.ContentAsset
	err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		service := NewContentAssetService(tx)
		var err error
		asset, err = service.Create(ctx, p.CreateContentAssetParams)
		if err != nil {
			return err
		}
		for _, evidence := range p.Evidence {
			evidence.ID = uuid.NewString()
			evidence.AssetID, evidence.UserID = asset.ID, p.UserID
			evidence.CreatedAt = time.Time{}
			// Imported claims are candidates; only Server validation can elevate
			// their quality. In particular, caller metrics are not analytics facts.
			evidence.Quality = "unverified"
			if evidence.SourceType == "" {
				evidence.SourceType = model.ContentAssetEvidenceExternal
			}
			switch evidence.SourceType {
			case model.ContentAssetEvidencePublished:
				projectID, err := service.validateLinkedContent(ctx, LinkContentAssetParams{UserID: p.UserID, ContentKind: evidence.SourceKind, ContentID: evidence.SourceID, ProjectID: evidence.ProjectID})
				if err != nil {
					return fmt.Errorf("validate internal evidence: %w", err)
				}
				project, err := tx.Projects().FindByID(ctx, projectID)
				if err != nil || project.UserID != p.UserID {
					return fmt.Errorf("evidence project does not belong to user")
				}
				evidence.ProjectID, evidence.Platform = projectID, project.Platform
			case model.ContentAssetEvidenceExternal, model.ContentAssetEvidenceImported:
				evidence.ProjectID = ""
			default:
				return fmt.Errorf("unsupported evidence source type %q", evidence.SourceType)
			}
			if err := tx.ContentAssets().CreateEvidence(ctx, &evidence); err != nil {
				return fmt.Errorf("create content asset evidence: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, p.UserID, asset.ID)
}

func (s *ContentAssetService) List(ctx context.Context, userID, status, query string, offset, limit int) ([]*model.ContentAsset, int64, error) {
	return s.repo.ContentAssets().List(ctx, userID, strings.TrimSpace(status), strings.TrimSpace(query), offset, limit)
}

func (s *ContentAssetService) ListFiltered(ctx context.Context, p ContentAssetListParams) ([]*model.ContentAsset, int64, error) {
	if p.Offset < 0 {
		p.Offset = 0
	}
	if p.Limit <= 0 || p.Limit > 100 {
		p.Limit = 50
	}
	return s.repo.ContentAssets().ListWithFilters(ctx, repository.ContentAssetListFilter{UserID: p.UserID, Status: p.Status, Query: p.Query, Platform: p.Platform, Source: p.Source, Lifecycle: p.Lifecycle, ProjectID: p.ProjectID, Offset: p.Offset, Limit: p.Limit})
}

func (s *ContentAssetService) ListReuseRuns(ctx context.Context, f repository.ContentReuseRunListFilter) ([]*model.ContentReuseRun, int64, error) {
	if f.Offset < 0 {
		f.Offset = 0
	}
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 50
	}
	return s.repo.ContentAssets().ListReuseRuns(ctx, f)
}

func (s *ContentAssetService) ListRecommendations(ctx context.Context, userID string, limit int) ([]*ContentAssetRecommendation, error) {
	if limit <= 0 || limit > 50 {
		limit = 5
	}
	assets, _, err := s.repo.ContentAssets().List(ctx, userID, "", "", 0, 100)
	if err != nil {
		return nil, fmt.Errorf("list content asset recommendations: %w", err)
	}
	items := make([]*ContentAssetRecommendation, 0, limit)
	for _, asset := range assets {
		if asset == nil || (asset.Status != model.ContentAssetStatusResearched && asset.Status != model.ContentAssetStatusValidated && asset.Status != model.ContentAssetStatusReady && asset.Status != model.ContentAssetStatusInProgress) {
			continue
		}
		scores, scoreErr := s.repo.ContentAssets().ListScoreSnapshots(ctx, userID, asset.ID)
		if scoreErr != nil {
			return nil, fmt.Errorf("list score snapshots for asset %s: %w", asset.ID, scoreErr)
		}
		var score *model.ContentAssetScoreSnapshot
		seenTarget := map[string]bool{}
		for _, candidate := range scores {
			if candidate == nil || (candidate.Status != "scored" && candidate.Status != "exploratory") {
				continue
			}
			project, projectErr := s.repo.Projects().FindByID(ctx, candidate.ProjectID)
			if projectErr != nil || project.UserID != userID || project.Platform != candidate.Platform {
				continue
			}
			family, familyErr := model.AnalyticsMetricFamilyForPlatform(project.Platform)
			if familyErr == nil {
				state, stateErr := s.repo.Analytics().WithMetricFamily(family).State(ctx, project.ID)
				if stateErr != nil || state == nil || state.Status != "ready" || state.Revision != candidate.AnalyticsRevision {
					continue
				}
			}
			key := candidate.ProjectID + ":" + candidate.Platform
			if seenTarget[key] {
				continue
			}
			seenTarget[key] = true
			// Snapshots are newest-first. Select the current revision per target
			// first, then compare only normalized local scores across targets.
			if score == nil || candidate.Score > score.Score || (candidate.Score == score.Score && candidate.CreatedAt.After(score.CreatedAt)) {
				score = candidate
			}
		}
		run, runErr := s.repo.ContentAssets().FindLatestReuseRun(ctx, userID, asset.ID)
		if runErr != nil && !errors.Is(runErr, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("find latest reuse run for asset %s: %w", asset.ID, runErr)
		}
		item := &ContentAssetRecommendation{Asset: asset, Score: score, Run: run}
		if score != nil {
			item.RecommendedPlatform = score.Platform
			item.EvidenceAsOf = score.DataAsOf
			var snapshot struct {
				RecommendationReason string `json:"recommendation_reason"`
				EstimatedCostCredits int64  `json:"estimated_cost_credits"`
			}
			if json.Unmarshal(score.Snapshot, &snapshot) == nil {
				item.RecommendationReason = snapshot.RecommendationReason
				item.EstimatedCostCredits = snapshot.EstimatedCostCredits
			}
		}
		if run != nil {
			item.AutomationStatus = run.Status
			item.AutomationStage = run.Stage
			if run.BudgetReserved > 0 {
				item.EstimatedCostCredits = run.BudgetReserved
			} else if run.CostCredits > 0 {
				item.EstimatedCostCredits = run.CostCredits
			}
		}
		if item.RecommendationReason == "" {
			item.RecommendationReason = "已验证选题，可继续评估跨平台复用"
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i].Score, items[j].Score
		if left == nil && right == nil {
			return items[i].Asset.UpdatedAt.After(items[j].Asset.UpdatedAt)
		}
		if left == nil {
			return false
		}
		if right == nil {
			return true
		}
		if left.Score == right.Score {
			return left.CreatedAt.After(right.CreatedAt)
		}
		return left.Score > right.Score
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *ContentAssetService) LinkContent(ctx context.Context, p LinkContentAssetParams) (*model.ContentAssetLink, error) {
	if _, err := s.repo.ContentAssets().FindByID(ctx, p.UserID, p.AssetID); err != nil {
		return nil, ErrContentAssetNotFound
	}
	projectID := strings.TrimSpace(p.ProjectID)
	if projectID != "" {
		project, err := s.repo.Projects().FindByID(ctx, projectID)
		if err != nil || project.UserID != p.UserID {
			return nil, fmt.Errorf("project does not belong to user")
		}
	}
	if p.ContentKind == "" || p.ContentID == "" {
		return nil, fmt.Errorf("content_kind and content_id are required")
	}
	contentProjectID, err := s.validateLinkedContent(ctx, p)
	if err != nil {
		return nil, err
	}
	if projectID != "" && contentProjectID != "" && projectID != contentProjectID {
		return nil, fmt.Errorf("content does not belong to project")
	}
	if projectID == "" {
		projectID = contentProjectID
	}
	relation := p.Relation
	if relation == "" {
		relation = model.ContentAssetLinkDerivative
	}
	if existing, err := s.repo.ContentAssets().FindLink(ctx, p.UserID, p.AssetID, p.ContentKind, p.ContentID, relation); err == nil {
		return existing, nil
	}
	link := &model.ContentAssetLink{ID: uuid.NewString(), AssetID: p.AssetID, UserID: p.UserID, ProjectID: projectID, ContentKind: p.ContentKind, ContentID: p.ContentID, Platform: p.Platform, Relation: relation, Angle: p.Angle}
	if err := s.repo.ContentAssets().CreateLink(ctx, link); err != nil {
		return nil, fmt.Errorf("link content asset: %w", err)
	}
	return link, nil
}

func (s *ContentAssetService) Get(ctx context.Context, userID, assetID string) (*ContentAssetDetail, error) {
	asset, err := s.repo.ContentAssets().FindByID(ctx, userID, assetID)
	if err != nil {
		return nil, ErrContentAssetNotFound
	}
	evidence, err := s.repo.ContentAssets().ListEvidence(ctx, userID, assetID)
	if err != nil {
		return nil, fmt.Errorf("list content asset evidence: %w", err)
	}
	links, err := s.repo.ContentAssets().ListLinks(ctx, userID, assetID)
	if err != nil {
		return nil, fmt.Errorf("list content asset links: %w", err)
	}
	scores, err := s.repo.ContentAssets().ListScoreSnapshots(ctx, userID, assetID)
	if err != nil {
		return nil, fmt.Errorf("list content asset score snapshots: %w", err)
	}
	return &ContentAssetDetail{Asset: asset, Evidence: evidence, Links: links, Scores: scores}, nil
}

func (s *ContentAssetService) Update(ctx context.Context, p UpdateContentAssetParams) (*model.ContentAsset, error) {
	if p.Status != nil {
		return nil, fmt.Errorf("asset status is server-managed")
	}
	asset, err := s.repo.ContentAssets().FindByID(ctx, p.UserID, p.AssetID)
	if err != nil {
		return nil, ErrContentAssetNotFound
	}
	if p.Title != nil {
		asset.Title = strings.TrimSpace(*p.Title)
	}
	if p.CoreQuestion != nil {
		asset.CoreQuestion = strings.TrimSpace(*p.CoreQuestion)
	}
	if p.Audience != nil {
		asset.Audience = strings.TrimSpace(*p.Audience)
	}
	if p.SemanticKernel != nil {
		asset.SemanticKernel = strings.TrimSpace(*p.SemanticKernel)
	}
	if p.Lifecycle != nil {
		lifecycle := strings.TrimSpace(*p.Lifecycle)
		if !validContentAssetLifecycle(lifecycle) {
			return nil, fmt.Errorf("invalid content asset lifecycle %q", lifecycle)
		}
		asset.Lifecycle = lifecycle
	}
	if asset.Title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if err := s.repo.ContentAssets().Update(ctx, asset); err != nil {
		return nil, fmt.Errorf("update content asset: %w", err)
	}
	return asset, nil
}

func (s *ContentAssetService) ApplyResearchStatus(ctx context.Context, userID, assetID, status, evidenceQuality string) (*model.ContentAsset, error) {
	asset, err := s.repo.ContentAssets().FindByID(ctx, userID, assetID)
	if err != nil {
		return nil, ErrContentAssetNotFound
	}
	allowed := map[string]map[string]bool{
		model.ContentAssetStatusCandidate:  {model.ContentAssetStatusResearched: true, model.ContentAssetStatusArchived: true},
		model.ContentAssetStatusResearched: {model.ContentAssetStatusValidated: true, model.ContentAssetStatusArchived: true},
		model.ContentAssetStatusValidated:  {model.ContentAssetStatusReady: true, model.ContentAssetStatusExhausted: true, model.ContentAssetStatusArchived: true},
		model.ContentAssetStatusReady:      {model.ContentAssetStatusInProgress: true, model.ContentAssetStatusExhausted: true, model.ContentAssetStatusArchived: true},
		model.ContentAssetStatusInProgress: {model.ContentAssetStatusReady: true, model.ContentAssetStatusExhausted: true, model.ContentAssetStatusArchived: true},
	}
	if !allowed[asset.Status][status] {
		return nil, ErrInvalidContentAssetState
	}
	asset.Status = status
	asset.EvidenceQuality = strings.TrimSpace(evidenceQuality)
	if err := s.repo.ContentAssets().Update(ctx, asset); err != nil {
		return nil, fmt.Errorf("update researched content asset: %w", err)
	}
	return asset, nil
}

func validContentAssetLifecycle(value string) bool {
	switch value {
	case model.ContentAssetLifecycleEvergreen, model.ContentAssetLifecycleSeasonal, model.ContentAssetLifecycleExpiring:
		return true
	default:
		return false
	}
}

func (s *ContentAssetService) validateLinkedContent(ctx context.Context, p LinkContentAssetParams) (string, error) {
	switch p.ContentKind {
	case "task", "video_task":
		task, err := s.repo.Tasks().FindByID(ctx, p.ContentID)
		if err != nil || task.UserID != p.UserID {
			return "", fmt.Errorf("task does not belong to user")
		}
		project, err := s.repo.Projects().FindByID(ctx, task.ProjectID)
		if err != nil || project.UserID != p.UserID {
			return "", fmt.Errorf("task project does not belong to user")
		}
		return task.ProjectID, nil
	case "wechat_publication":
		publication, err := s.repo.WechatPublications().FindByID(ctx, p.ContentID)
		if err != nil || publication.UserID != p.UserID {
			return "", fmt.Errorf("publication does not belong to user")
		}
		return publication.ProjectID, nil
	case "seednote_post":
		if strings.TrimSpace(p.ProjectID) == "" {
			return "", fmt.Errorf("project_id is required for seednote posts")
		}
		post, err := s.repo.SeednotePosts().FindByID(ctx, p.ProjectID, p.ContentID)
		if err != nil || post.UserID != p.UserID {
			return "", fmt.Errorf("seednote post does not belong to user")
		}
		return post.ProjectID, nil
	default:
		return "", fmt.Errorf("unsupported content_kind %q", p.ContentKind)
	}
}

func (s *ContentAssetService) CreateReuseRun(ctx context.Context, p CreateReuseRunParams) (*model.ContentReuseRun, error) {
	run, _, err := s.createReuseRun(ctx, p, false)
	return run, err
}

func (s *ContentAssetService) createAutopilotReuseRun(ctx context.Context, p CreateReuseRunParams) (*model.ContentReuseRun, bool, error) {
	var run *model.ContentReuseRun
	var created bool
	err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		policy, err := tx.ContentAssets().LockPolicy(ctx, p.UserID)
		if err != nil {
			return err
		}
		if !policy.Enabled || policy.Revision != p.expectedPolicyRevision {
			return ErrContentReuseConflict
		}
		projects, err := decodePolicyStrings(policy.ProjectIDs)
		if err != nil {
			return err
		}
		platforms, err := decodePolicyStrings(policy.Platforms)
		if err != nil {
			return err
		}
		if !stringSet(projects)[p.TargetProjectID] || !stringSet(platforms)[p.TargetPlatform] {
			return ErrContentReuseConflict
		}
		score := p.scoredRecommendation
		if score == nil || score.AssetID != p.AssetID || score.ProjectID != p.TargetProjectID || score.UserID != p.UserID || score.Platform != p.TargetPlatform {
			return ErrContentReuseConflict
		}
		if !autopilotSnapshotEligible(score, policy, p.TargetPlatform, score.AnalyticsRevision, time.Now().UTC()) {
			return ErrContentReuseConflict
		}
		service := *s
		service.repo = tx
		p.deferDispatch = true
		run, created, err = service.createReuseRun(ctx, p, true)
		return err
	})
	if err != nil {
		return nil, false, err
	}
	if created {
		if err := s.enqueueReuseRun(ctx, run); err != nil {
			_ = s.failReuseRun(ctx, run, "queue_unavailable", err)
			return nil, false, fmt.Errorf("enqueue content reuse run: %w", err)
		}
	}
	return run, created, nil
}

func (s *ContentAssetService) createReuseRun(ctx context.Context, p CreateReuseRunParams, serverScored bool) (*model.ContentReuseRun, bool, error) {
	if strings.TrimSpace(p.TargetProjectID) == "" || strings.TrimSpace(p.TargetPlatform) == "" {
		return nil, false, fmt.Errorf("target_project_id and target_platform are required")
	}
	if s.enqueuer == nil {
		return nil, false, fmt.Errorf("content reuse queue unavailable")
	}
	asset, err := s.repo.ContentAssets().FindByID(ctx, p.UserID, p.AssetID)
	if err != nil {
		return nil, false, ErrContentAssetNotFound
	}
	project, err := s.repo.Projects().FindByID(ctx, p.TargetProjectID)
	if err != nil || project.UserID != p.UserID {
		return nil, false, fmt.Errorf("target project does not belong to user")
	}
	projectPlatform := strings.TrimSpace(project.Platform)
	requestedPlatform := strings.TrimSpace(p.TargetPlatform)
	if projectPlatform == "" || requestedPlatform != projectPlatform {
		return nil, false, fmt.Errorf("target platform must match target project platform")
	}
	policy, err := s.repo.ContentAssets().GetPolicy(ctx, p.UserID)
	if err != nil {
		return nil, false, fmt.Errorf("load content autopilot policy: %w", err)
	}
	if policy.Enabled && asset.Status != model.ContentAssetStatusValidated && asset.Status != model.ContentAssetStatusReady {
		return nil, false, fmt.Errorf("content asset must be validated before automatic production")
	}
	if p.ScoreSnapshot != nil && !serverScored {
		return nil, false, fmt.Errorf("score_snapshot is server-owned")
	}
	if (strings.TrimSpace(p.SourceContentKind) == "") != (strings.TrimSpace(p.SourceContentID) == "") {
		return nil, false, fmt.Errorf("source_content_kind and source_content_id must be provided together")
	}
	if strings.TrimSpace(p.SourceContentKind) != "" {
		links, linkErr := s.repo.ContentAssets().ListLinks(ctx, p.UserID, asset.ID)
		if linkErr != nil {
			return nil, false, fmt.Errorf("load source content links: %w", linkErr)
		}
		found := false
		for _, link := range links {
			if link.ContentKind == strings.TrimSpace(p.SourceContentKind) && link.ContentID == strings.TrimSpace(p.SourceContentID) {
				found = true
				break
			}
		}
		if !found {
			return nil, false, fmt.Errorf("source content is not linked to this asset")
		}
	}
	key := strings.TrimSpace(p.IdempotencyKey)
	if key == "" || len([]rune(key)) > 150 {
		return nil, false, fmt.Errorf("idempotency_key is required and must be at most 150 characters")
	}
	if key != "" {
		fingerprint := contentReuseRequestFingerprint(p, asset.ID, project.ID)
		if existing, findErr := s.repo.ContentAssets().FindReuseRunByIdempotency(ctx, p.UserID, key); findErr == nil {
			existingFingerprint := existing.RequestFingerprint
			if existingFingerprint == "" {
				existingFingerprint = contentReuseRunFingerprint(existing)
			}
			if existingFingerprint != fingerprint {
				return nil, false, ErrContentReuseConflict
			}
			return existing, false, nil
		} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return nil, false, findErr
		}
	}
	run := model.NewContentReuseRun(p.UserID, asset.ID, project.ID, strings.TrimSpace(p.TargetPlatform))
	run.SourceContentKind, run.SourceContentID, run.TargetAgentID = strings.TrimSpace(p.SourceContentKind), strings.TrimSpace(p.SourceContentID), strings.TrimSpace(p.TargetAgentID)
	if key != "" {
		run.IdempotencyKey = key
		run.RequestFingerprint = contentReuseRequestFingerprint(p, asset.ID, project.ID)
	}
	if p.ScoreSnapshot != nil {
		run.ScoreSnapshot = mustContentAssetJSON(p.ScoreSnapshot)
	}
	run.PolicySnapshot = mustContentAssetJSON(policy)
	if err := s.repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
		return nil, false, fmt.Errorf("create content reuse run: %w", err)
	}
	if p.deferDispatch {
		return run, true, nil
	}
	if enqueueErr := s.enqueueReuseRun(ctx, run); enqueueErr != nil {
		_ = s.failReuseRun(ctx, run, "queue_unavailable", enqueueErr)
		return nil, false, fmt.Errorf("enqueue content reuse run: %w", enqueueErr)
	}
	return run, true, nil
}

func (s *ContentAssetService) enqueueReuseRun(ctx context.Context, run *model.ContentReuseRun) error {
	if s.enqueuer == nil {
		return fmt.Errorf("content reuse queue unavailable")
	}
	payload, err := json.Marshal(map[string]string{"run_id": run.ID})
	if err != nil {
		return err
	}
	key := "content-reuse:" + run.ID
	// Each persisted recovery/resume version is a fresh transport attempt.
	// Business idempotency remains the run's uniquely bound task, so an archived
	// Asynq job cannot permanently monopolize the dispatch identity.
	if run.Version > 0 {
		key = fmt.Sprintf("%s:v%d", key, run.Version)
	}
	_, err = s.enqueuer.EnqueueUnique("content:reuse", payload, key)
	return err
}

func (s *ContentAssetService) GetReuseRun(ctx context.Context, userID, runID string) (*model.ContentReuseRun, error) {
	run, err := s.repo.ContentAssets().FindReuseRun(ctx, userID, runID)
	if err != nil {
		return nil, ErrContentAssetNotFound
	}
	// A run detail must be reconstructable from durable Server state. Load the
	// known workflow stages and append-only audit trail here rather than making
	// the browser infer progress from the current run label.
	stageNames := []string{
		model.ContentReuseStageResearch,
		model.ContentReuseStageScoring,
		model.ContentReuseStageBrief,
		model.ContentReuseStageProduce,
		model.ContentReuseStageMaterials,
		model.ContentReuseStageRender,
		model.ContentReuseStageReview,
		model.ContentReuseStageSchedule,
		model.ContentReuseStagePublish,
		model.ContentReuseStageObserve,
	}
	run.StageExecutions = make([]*model.ContentReuseStageExecution, 0, len(stageNames))
	for _, stageName := range stageNames {
		stage, stageErr := s.repo.ContentAssets().FindReuseStage(ctx, run.ID, stageName)
		if stageErr == nil {
			run.StageExecutions = append(run.StageExecutions, stage)
			continue
		}
		if !errors.Is(stageErr, gorm.ErrRecordNotFound) {
			return nil, stageErr
		}
	}
	run.AuditEvents, err = s.repo.ContentAssets().ListReuseAudit(ctx, userID, run.ID)
	if err != nil {
		return nil, err
	}
	return run, nil
}

func (s *ContentAssetService) ProcessReuseRun(ctx context.Context, runID string) error {
	if s == nil || s.taskSvc == nil {
		return fmt.Errorf("content reuse task service unavailable")
	}
	run, err := s.repo.ContentAssets().FindReuseRunByID(ctx, runID)
	if err != nil {
		return err
	}
	if run.Status == model.ContentReuseRunStatusCancelled || run.Status == model.ContentReuseRunStatusCompleted || run.Status == model.ContentReuseRunStatusPaused || run.Status == model.ContentReuseRunStatusFailed {
		return nil
	}
	if run.NextRunAt != nil && run.NextRunAt.After(time.Now().UTC()) {
		return nil
	}
	if run.Status != model.ContentReuseRunStatusRunning {
		claimed, err := s.repo.ContentAssets().ClaimReuseRun(ctx, run.ID)
		if err != nil {
			return err
		}
		if !claimed {
			return nil
		}
		run, err = s.repo.ContentAssets().FindReuseRunByID(ctx, run.ID)
		if err != nil {
			return err
		}
		if run.Status != model.ContentReuseRunStatusRunning {
			return nil
		}
	}
	// Explicit stage bindings take precedence over the legacy one-task path.
	// A queued stage with no task is durable admission intent; do not bypass it
	// by creating a full platform task before its accepted inputs exist.
	if stage, stageErr := s.repo.ContentAssets().FindReuseStage(ctx, run.ID, run.Stage); stageErr == nil {
		if reuseStageTaskID(stage) == "" {
			if !s.contentReusePlanningEnabled(run) || run.Stage != model.ContentReuseStageResearch {
				return nil
			}
			input := map[string]any{
				"schema_version":     "1.0",
				"task_kind":          model.TaskTypeContentResearch,
				"candidates":         []map[string]any{{"asset_id": run.AssetID, "title": assetTitleForRun(ctx, s.repo, run)}},
				"accepted_artifacts": []any{},
			}
			if _, ensureErr := s.EnsureReuseStage(ctx, run.UserID, run.ID, model.ContentReuseStageResearch, input); ensureErr != nil {
				return ensureErr
			}
			tasks, createErr := s.CreatePlanningStageTask(ctx, run.UserID, run.ID, stage.ID, model.TaskTypeContentResearch, input)
			if createErr != nil {
				return createErr
			}
			for _, planningTask := range tasks {
				if err := s.taskSvc.EnqueueExecution(ctx, planningTask, nil); err != nil {
					return err
				}
			}
			return nil
		}
		stageTask, taskErr := s.repo.Tasks().FindByReuseStage(ctx, run.UserID, stage.ID)
		if taskErr != nil {
			return taskErr
		}
		if stage.Status == model.ContentStageCompleted && stage.Stage == model.ContentReuseStageRender {
			return s.finalizeRenderedReuseTask(ctx, stageTask)
		}
		if stageTask.Status == model.TaskStatusPending {
			if err := s.taskSvc.EnqueueExecution(ctx, stageTask, nil); err != nil {
				return err
			}
		}
		return s.FinalizeReuseTask(ctx, stageTask)
	} else if !errors.Is(stageErr, gorm.ErrRecordNotFound) {
		return stageErr
	}
	if s.contentReusePlanningEnabled(run) && run.Stage == model.ContentReuseStageResearch {
		input := map[string]any{
			"schema_version":     "1.0",
			"task_kind":          model.TaskTypeContentResearch,
			"candidates":         []map[string]any{{"asset_id": run.AssetID, "title": assetTitleForRun(ctx, s.repo, run)}},
			"accepted_artifacts": []any{},
		}
		stage, ensureErr := s.EnsureReuseStage(ctx, run.UserID, run.ID, model.ContentReuseStageResearch, input)
		if ensureErr != nil {
			return ensureErr
		}
		tasks, createErr := s.CreatePlanningStageTask(ctx, run.UserID, run.ID, stage.ID, model.TaskTypeContentResearch, input)
		if createErr != nil {
			return createErr
		}
		for _, planningTask := range tasks {
			if err := s.taskSvc.EnqueueExecution(ctx, planningTask, nil); err != nil {
				return err
			}
		}
		return nil
	}
	existing, taskErr := s.repo.Tasks().FindByReuseRun(ctx, run.UserID, run.ID)
	if taskErr == nil {
		if run.TaskID != "" && run.TaskID != existing.ID {
			return fmt.Errorf("content reuse task binding mismatch")
		}
		if run.TaskID == "" {
			run.TaskID = existing.ID
			if err := s.repo.ContentAssets().UpdateReuseRun(ctx, run); err != nil {
				return err
			}
		}
		if existing.Status == model.TaskStatusPending {
			if err := s.taskSvc.EnqueueExecution(ctx, existing, nil); err != nil {
				return err
			}
		}
		if err := s.taskSvc.resumeReuseDraftFinalization(ctx, existing); err != nil {
			return err
		}
		return s.FinalizeReuseTask(ctx, existing)
	}
	if !errors.Is(taskErr, gorm.ErrRecordNotFound) {
		return taskErr
	}
	if run.TaskID != "" {
		return fmt.Errorf("bound content reuse task is missing")
	}
	asset, err := s.repo.ContentAssets().FindByID(ctx, run.UserID, run.AssetID)
	if err != nil {
		return s.failReuseRun(ctx, run, "asset_not_found", err)
	}
	project, err := s.repo.Projects().FindByID(ctx, run.TargetProjectID)
	if err != nil || project.UserID != run.UserID {
		if err == nil {
			err = fmt.Errorf("target project does not belong to user")
		}
		return s.failReuseRun(ctx, run, "project_not_found", err)
	}
	agentID, channel, taskKind, ok := contentReuseIdentity(project.Platform)
	if !ok {
		return s.failReuseRun(ctx, run, "unsupported_platform", fmt.Errorf("unsupported target platform %q", project.Platform))
	}
	stageRank := map[string]int{
		model.ContentReuseStageResearch: 1,
		model.ContentReuseStageScoring:  2,
		model.ContentReuseStageBrief:    3,
		model.ContentReuseStageProduce:  4,
		model.ContentReuseStageReview:   5,
		model.ContentReuseStageSchedule: 6,
		model.ContentReuseStagePublish:  7,
		model.ContentReuseStageObserve:  8,
	}
	advanceStage := func(stage string) error {
		if stageRank[stage] <= stageRank[run.Stage] {
			return nil
		}
		run.Stage = stage
		return s.repo.ContentAssets().UpdateReuseRun(ctx, run)
	}
	if err := advanceStage(model.ContentReuseStageScoring); err != nil {
		return s.failReuseRun(ctx, run, "stage_persistence_failed", err)
	}
	if err := advanceStage(model.ContentReuseStageBrief); err != nil {
		return s.failReuseRun(ctx, run, "stage_persistence_failed", err)
	}
	if err := advanceStage(model.ContentReuseStageProduce); err != nil {
		return s.failReuseRun(ctx, run, "stage_persistence_failed", err)
	}
	createParams := CreateManualParams{UserID: run.UserID, ProjectID: run.TargetProjectID, ExecutionProfile: "effective", AgentID: agentID, Channel: channel, TaskKind: taskKind, Prompt: fmt.Sprintf("跨平台复用运行。选题资产：%s。核心问题：%s。受众：%s。语义核心：%s。目标平台：%s。来源内容：%s/%s。请使用 content-reuse Brief 约束，保留证据边界并生成该平台独立表达版本。复用运行 ID：%s。", asset.Title, asset.CoreQuestion, asset.Audience, asset.SemanticKernel, run.TargetPlatform, run.SourceContentKind, run.SourceContentID, run.ID), ContentAssetID: run.AssetID, SourceContentRef: strings.Trim(strings.Join([]string{run.SourceContentKind, run.SourceContentID}, ":"), ":"), ReuseRunID: run.ID}
	if project.Platform == model.PlatformMontage {
		createParams.MontageInput = &model.MontageInput{Brief: fmt.Sprintf("围绕选题资产“%s”制作平台化口播视频。核心问题：%s。受众：%s。", asset.Title, asset.CoreQuestion, asset.Audience)}
	}
	policy := model.NewContentAutopilotPolicy(run.UserID)
	if len(run.PolicySnapshot) > 0 && string(run.PolicySnapshot) != "null" {
		if err := json.Unmarshal(run.PolicySnapshot, policy); err != nil {
			return s.failReuseRun(ctx, run, "policy_load_failed", err)
		}
	} else if policy, err = s.repo.ContentAssets().GetPolicy(ctx, run.UserID); err != nil {
		return s.failReuseRun(ctx, run, "policy_load_failed", err)
	}
	if policy.Enabled {
		if policy.DailyBudgetCredits <= 0 || policy.MaxRunCostCredits <= 0 {
			return s.failReuseRun(ctx, run, "budget_not_configured", fmt.Errorf("autopilot daily and per-run budgets must be positive"))
		}
		if s.billingCatalogSvc == nil {
			return s.failReuseRun(ctx, run, "billing_unavailable", fmt.Errorf("content autopilot billing is unavailable"))
		}
		taskType := contentReuseBillingTaskType(project.Platform)
		if taskType == "" {
			return s.failReuseRun(ctx, run, "unsupported_billing_platform", fmt.Errorf("billing is not configured for target platform %q", project.Platform))
		}
		fingerprint := billingFingerprint("content-reuse-task", run.ID, taskType, "effective")
		quote, quoteErr := s.billingCatalogSvc.CreateTaskQuote(ctx, TaskQuoteRequest{UserID: run.UserID, TaskType: taskType, ExecutionProfile: "effective", RequestFingerprint: fingerprint, IdempotencyScope: "content-reuse-quote", IdempotencyKey: fmt.Sprintf("%s:v%d", run.ID, run.Version)})
		if quoteErr != nil {
			return s.failReuseRun(ctx, run, "billing_quote_failed", quoteErr)
		}
		if quote.PriceCredits > policy.MaxRunCostCredits {
			return s.failReuseRun(ctx, run, "run_cost_exceeded", fmt.Errorf("quoted cost %d exceeds per-run limit %d", quote.PriceCredits, policy.MaxRunCostCredits))
		}
		reserved, reserveErr := s.repo.ContentAssets().ReserveReuseRunBudget(ctx, run.ID, quote.PriceCredits, policy.DailyBudgetCredits, time.Now().UTC())
		if reserveErr != nil {
			return s.failReuseRun(ctx, run, "budget_reservation_failed", reserveErr)
		}
		if !reserved {
			return s.failReuseRun(ctx, run, "daily_budget_exceeded", fmt.Errorf("daily autopilot budget is exhausted"))
		}
		run, err = s.repo.ContentAssets().FindReuseRunByID(ctx, run.ID)
		if err != nil {
			return err
		}
		if run.Status != model.ContentReuseRunStatusRunning {
			return nil
		}
		run.BudgetReserved = quote.PriceCredits
		run.BillingQuoteID, run.BillingFingerprint = quote.ID, fingerprint
		createParams.BillingQuoteID, createParams.BillingRequestFingerprint = quote.ID, fingerprint
		if err := s.repo.ContentAssets().UpdateReuseRun(ctx, run); err != nil {
			return s.failReuseRun(ctx, run, "budget_persistence_failed", err)
		}
	}
	tasks, err := s.taskSvc.CreateManual(ctx, createParams)
	if err != nil {
		// The unique reuse key is the durable idempotency boundary. A concurrent
		// worker may have created the task after the preflight lookup above; in
		// that case reconcile the winner instead of creating a second task.
		if existing, findErr := s.repo.Tasks().FindByReuseRun(ctx, run.UserID, run.ID); findErr == nil {
			return s.FinalizeReuseTask(ctx, existing)
		}
		return s.failReuseRun(ctx, run, "task_creation_failed", err)
	}
	for _, task := range tasks {
		_, _ = s.LinkContent(ctx, LinkContentAssetParams{UserID: run.UserID, AssetID: run.AssetID, ProjectID: run.TargetProjectID, ContentKind: "task", ContentID: task.ID, Platform: run.TargetPlatform, Relation: model.ContentAssetLinkDerivative, Angle: run.TargetPlatform})
	}

	return nil
}

func contentReusePlanningEnabled(run *model.ContentReuseRun) bool {
	if run == nil || len(run.PolicySnapshot) == 0 || string(run.PolicySnapshot) == "null" {
		return false
	}
	var snapshot struct {
		Enabled bool `json:"enabled"`
	}
	return json.Unmarshal(run.PolicySnapshot, &snapshot) == nil && snapshot.Enabled
}

func (s *ContentAssetService) contentReusePlanningEnabled(run *model.ContentReuseRun) bool {
	return contentReusePlanningEnabled(run) && s != nil && s.taskSvc != nil && s.taskSvc.ContentPlanningAvailable() == nil
}

func assetTitleForRun(ctx context.Context, repo repository.Repository, run *model.ContentReuseRun) string {
	asset, err := repo.ContentAssets().FindByID(ctx, run.UserID, run.AssetID)
	if err != nil || asset == nil {
		return "选题资产"
	}
	return asset.Title
}

func (s *ContentAssetService) failReuseRun(ctx context.Context, run *model.ContentReuseRun, code string, err error) error {
	run.RetryCount++
	retryable := code == "queue_unavailable" || code == "policy_load_failed" || code == "billing_unavailable" || code == "billing_quote_failed" || code == "budget_persistence_failed" || code == "task_creation_failed" || code == "stage_persistence_failed"
	if retryable && run.RetryCount < contentReuseMaxRetries {
		run.Status = model.ContentReuseRunStatusQueued
		next := time.Now().UTC().Add(time.Minute * time.Duration(1<<uint(run.RetryCount-1)))
		run.NextRunAt = &next
	} else if retryable && contentReuseFailureMode(run) == model.ContentAutopilotFailurePause {
		run.Status = model.ContentReuseRunStatusPaused
		run.NextRunAt = nil
	} else {
		run.Status = model.ContentReuseRunStatusFailed
		run.NextRunAt = nil
	}
	run.BudgetReserved = 0
	run.ErrorCode = code
	run.ErrorMessage = err.Error()
	_ = s.repo.ContentAssets().UpdateReuseRun(ctx, run)
	return err
}

func contentReuseFailureMode(run *model.ContentReuseRun) string {
	if run != nil && len(run.PolicySnapshot) > 0 && string(run.PolicySnapshot) != "null" {
		var snapshot struct {
			FailureMode string `json:"failure_mode"`
		}
		if json.Unmarshal(run.PolicySnapshot, &snapshot) == nil && snapshot.FailureMode == model.ContentAutopilotFailurePause {
			return model.ContentAutopilotFailurePause
		}
	}
	return model.ContentAutopilotFailureSkip
}

func contentReuseBillingTaskType(platform string) string {
	switch platform {
	case model.PlatformWechat:
		return model.TaskTypeWechatArticle
	case model.PlatformSeednote:
		return model.PlatformSeednote
	case model.PlatformHypit:
		return model.PlatformHypit
	case model.PlatformMontage:
		return model.PlatformMontage
	case model.PlatformWhiteboardAnimation:
		return model.PlatformWhiteboardAnimation
	default:
		return ""
	}
}

func (s *ContentAssetService) FinalizeReuseTask(ctx context.Context, task *model.Task) error {
	if task == nil || strings.TrimSpace(task.ReuseRunID) == "" {
		return nil
	}
	if strings.TrimSpace(task.ReuseStageID) != "" {
		return s.finalizeReuseStageTask(ctx, task)
	}
	run, err := s.repo.ContentAssets().FindReuseRunByID(ctx, task.ReuseRunID)
	if err != nil {
		return err
	}
	if task.UserID != run.UserID || task.ProjectID != run.TargetProjectID || (task.ReuseStageID == "" && run.TaskID != "" && task.ID != run.TaskID) {
		return fmt.Errorf("task does not belong to content reuse run")
	}
	terminalTask := task.Status == model.TaskStatusCompleted || task.Status == model.TaskStatusFailed || task.Status == model.TaskStatusCancelled
	if terminalTask {
		run.CostCredits = task.BillingPriceCredits
		run.BudgetReserved = 0
		totals, err := s.repo.Billing().ListTaskChargeTotals(ctx, task.UserID, []string{task.ID})
		if err != nil && !strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return err
		}
		if len(totals) > 0 {
			run.CostCredits = totals[0].TotalCredits
			if totals[0].TaskChargeCount == 0 {
				run.CostCredits += task.BillingPriceCredits
			}
		}
		if task.BillingChargeID != nil && ShouldReverseTaskCharge(task.BillingTerminalReason, false) {
			if _, err := s.repo.Billing().FindReversal(ctx, *task.BillingChargeID); err == nil {
				if len(totals) == 0 || totals[0].TaskChargeCount == 0 {
					run.CostCredits -= task.BillingPriceCredits
				}
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		if _, err := s.repo.Tasks().FindByID(ctx, task.ID); err == nil {
			walletCost, walletErr := s.repo.ContentAssets().ReuseRunWalletCost(ctx, run)
			if walletErr != nil && !strings.Contains(strings.ToLower(walletErr.Error()), "no such table") {
				return walletErr
			}
			if walletErr == nil {
				run.CostCredits = walletCost
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	if run.Status == model.ContentReuseRunStatusCancelled || run.Status == model.ContentReuseRunStatusPaused || run.Status == model.ContentReuseRunStatusCompleted {
		if terminalTask {
			return s.repo.ContentAssets().UpdateReuseRun(ctx, run)
		}
		return nil
	}
	if task.Status == model.TaskStatusCompleted {
		if task.Outcome == nil {
			run.Stage = model.ContentReuseStageReview
			return s.repo.ContentAssets().UpdateReuseRun(ctx, run)
		}
		if task.Outcome.Review.Status != model.TaskReviewPassed {
			run.Status = model.ContentReuseRunStatusPaused
			run.Stage = model.ContentReuseStageReview
			run.ErrorCode = "review_not_ready"
			run.ErrorMessage = "内容质量检查尚未通过 Server readiness"
			return s.repo.ContentAssets().UpdateReuseRun(ctx, run)
		}
		var policy contentReusePublicationPolicySnapshot
		if len(run.PolicySnapshot) > 0 && string(run.PolicySnapshot) != "null" {
			if err := json.Unmarshal(run.PolicySnapshot, &policy); err != nil {
				return fmt.Errorf("decode reuse policy: %w", err)
			}
		}
		run.BudgetReserved = 0
		if policy.PublishEnabled {
			ready, err := s.reconcileReusePublication(ctx, run, task)
			if err != nil {
				return err
			}
			if !ready {
				return s.repo.ContentAssets().UpdateReuseRun(ctx, run)
			}
		} else if task.Outcome.Publication.Status == model.TaskPublicationAmbiguous || task.Outcome.Publication.Status == model.TaskPublicationBlocked || task.Outcome.Publication.Status == model.TaskPublicationFailed {
			run.Status = model.ContentReuseRunStatusPaused
			run.Stage = model.ContentReuseStagePublish
			run.ErrorCode = "publication_not_ready"
			run.ErrorMessage = "外部发布状态尚未通过 Server reconciliation"
			return s.repo.ContentAssets().UpdateReuseRun(ctx, run)
		}
		run.Status = model.ContentReuseRunStatusCompleted
		run.Stage = model.ContentReuseStageObserve
		run.BudgetReserved = 0
		run.ErrorCode, run.ErrorMessage = "", ""
	} else if task.Status == model.TaskStatusFailed || task.Status == model.TaskStatusCancelled {
		run.Status = model.ContentReuseRunStatusFailed
		run.BudgetReserved = 0
		run.ErrorCode = "task_" + task.Status
		run.ErrorMessage = task.ErrorMessage
		// TaskService owns terminal billing settlement. The reuse finalizer only
		// reports the run outcome; it must not enqueue a second reversal with a
		// different idempotency key after task finalization already settled it.
		if task.BillingChargeID != nil && strings.TrimSpace(*task.BillingChargeID) != "" &&
			ShouldReverseTaskCharge(task.BillingTerminalReason, false) {
			chargeID := strings.TrimSpace(*task.BillingChargeID)
			if _, reversalErr := s.repo.Billing().FindReversal(ctx, chargeID); reversalErr != nil && !errors.Is(reversalErr, gorm.ErrRecordNotFound) {
				run.ErrorCode = "billing_reversal_pending"
				run.ErrorMessage = reversalErr.Error()
			}
		}
	}
	return s.repo.ContentAssets().UpdateReuseRun(ctx, run)
}

func (s *ContentAssetService) finalizeReuseStageTask(ctx context.Context, task *model.Task) error {
	if task == nil || task.ReuseStageID == "" {
		return nil
	}
	executionID := ""
	if task.CurrentExecutionID != nil {
		executionID = strings.TrimSpace(*task.CurrentExecutionID)
	}
	if executionID == "" {
		return fmt.Errorf("stage task execution is missing")
	}
	status := model.ContentStageFailed
	if task.Status == model.TaskStatusCompleted {
		status = model.ContentStageCompleted
	}
	if task.Status != model.TaskStatusCompleted && task.Status != model.TaskStatusFailed && task.Status != model.TaskStatusCancelled {
		return nil
	}
	files, err := s.repo.TaskFiles().FindByExecutionID(ctx, executionID)
	if err != nil {
		return err
	}
	refs := make([]ContentStageArtifact, 0, len(files))
	for _, file := range files {
		if file == nil || file.State != model.TaskFileStateDelivered || file.FileSize <= 0 || !strings.HasPrefix(file.FilePath, "output/") {
			continue
		}
		refs = append(refs, ContentStageArtifact{TaskID: task.ID, ExecutionID: executionID, Path: file.FilePath, ContentSHA256: file.ContentHash})
	}
	if status == model.ContentStageCompleted && len(refs) == 0 {
		return fmt.Errorf("completed stage has no delivered artifacts")
	}
	reason := task.ErrorMessage
	if reason == "" && status == model.ContentStageCompleted {
		reason = "stage_execution_completed"
	}
	if err := s.CompleteReuseStage(ctx, task.UserID, task.ReuseStageID, executionID, refs, status, reason); err != nil {
		return err
	}
	if status == model.ContentStageCompleted {
		stage, stageErr := s.repo.ContentAssets().FindReuseStageByID(ctx, task.ReuseStageID)
		if stageErr != nil {
			return stageErr
		}
		if stage.Stage == model.ContentReuseStageRender {
			return s.finalizeRenderedReuseTask(ctx, task)
		}
		return s.advancePlanningStage(ctx, task.UserID, task.ReuseStageID)
	}
	return nil
}

func (s *ContentAssetService) finalizeRenderedReuseTask(ctx context.Context, task *model.Task) error {
	run, err := s.repo.ContentAssets().FindReuseRunByID(ctx, task.ReuseRunID)
	if err != nil {
		return err
	}
	if task.Status == model.TaskStatusFailed || task.Status == model.TaskStatusCancelled {
		run.Status = model.ContentReuseRunStatusFailed
		run.Stage = model.ContentReuseStageRender
		run.ErrorCode = "render_" + task.Status
		run.ErrorMessage = task.ErrorMessage
		run.BudgetReserved = 0
		return s.repo.ContentAssets().UpdateReuseRun(ctx, run)
	}
	if task.Status != model.TaskStatusCompleted {
		return nil
	}
	if task.CurrentExecutionID == nil || strings.TrimSpace(*task.CurrentExecutionID) == "" {
		return fmt.Errorf("completed render task execution is missing")
	}
	executionID := strings.TrimSpace(*task.CurrentExecutionID)
	if task.Outcome == nil || task.Outcome.Review.Status != model.TaskReviewPassed {
		run.Status = model.ContentReuseRunStatusPaused
		run.Stage = model.ContentReuseStageReview
		run.ErrorCode = "review_not_ready"
		run.ErrorMessage = "渲染产物尚未通过 Server readiness"
		return s.repo.ContentAssets().UpdateReuseRun(ctx, run)
	}
	// Persist the deterministic scheduling checkpoint before consulting the
	// publisher. This makes a paused run explainable and lets recovery resume
	// from the publish boundary without rerendering the paid artifact.
	if err := s.completeWorkflowStage(ctx, run, model.ContentReuseStageSchedule, executionID, "schedule_ready", nil); err != nil {
		return err
	}
	if run, err = s.repo.ContentAssets().FindReuseRunByID(ctx, run.ID); err != nil {
		return err
	}
	var policy contentReusePublicationPolicySnapshot
	if len(run.PolicySnapshot) > 0 && string(run.PolicySnapshot) != "null" {
		if err := json.Unmarshal(run.PolicySnapshot, &policy); err != nil {
			return err
		}
	}
	if policy.PublishEnabled {
		ready, err := s.reconcileReusePublication(ctx, run, task)
		if err != nil {
			return err
		}
		if !ready {
			return s.repo.ContentAssets().UpdateReuseRun(ctx, run)
		}
	}
	if err := s.completeWorkflowStage(ctx, run, model.ContentReuseStagePublish, executionID, "publication_reconciled", nil); err != nil {
		return err
	}
	if run, err = s.repo.ContentAssets().FindReuseRunByID(ctx, run.ID); err != nil {
		return err
	}
	if err := s.completeWorkflowStage(ctx, run, model.ContentReuseStageObserve, executionID, "observation_started", nil); err != nil {
		return err
	}
	if run, err = s.repo.ContentAssets().FindReuseRunByID(ctx, run.ID); err != nil {
		return err
	}
	run.Status = model.ContentReuseRunStatusCompleted
	run.Stage = model.ContentReuseStageObserve
	run.BudgetReserved = 0
	run.ErrorCode, run.ErrorMessage = "", ""
	return s.repo.ContentAssets().UpdateReuseRun(ctx, run)
}

// completeWorkflowStage records non-paid deterministic checkpoints. It is
// intentionally called while the run is still running because the repository
// transition CAS protects stage and run state together.
func (s *ContentAssetService) completeWorkflowStage(ctx context.Context, run *model.ContentReuseRun, name, executionID, reason string, artifacts []ContentStageArtifact) error {
	if run == nil || run.Status != model.ContentReuseRunStatusRunning {
		return ErrContentReuseRunStopped
	}
	input := map[string]any{"schema_version": "1.0", "stage": name, "execution_id": strings.TrimSpace(executionID)}
	stage, err := s.EnsureReuseStage(ctx, run.UserID, run.ID, name, input)
	if err != nil {
		return err
	}
	if stage.Status == model.ContentStageCompleted {
		return nil
	}
	if stage.Status != model.ContentStageQueued {
		return fmt.Errorf("workflow stage %s is not queued", name)
	}
	stage.Status = model.ContentStageCompleted
	stage.AcceptedExecutionID = strings.TrimSpace(executionID)
	if len(artifacts) > 0 {
		stage.ArtifactRefs, err = json.Marshal(artifacts)
		if err != nil {
			return err
		}
	}
	stage.RecoveryPoint = name
	event := &model.ContentReuseAuditEvent{UserID: run.UserID, RunID: run.ID, StageExecutionID: stage.ID, EntityKind: "stage", EntityID: stage.ID, FromState: model.ContentStageQueued, ToState: model.ContentStageCompleted, ExecutionID: stage.AcceptedExecutionID, PolicyRevision: contentStagePolicyRevision(run.PolicySnapshot), ReasonCode: reason}
	return s.repo.ContentAssets().TransitionReuseStage(ctx, stage, event)
}

func (s *ContentAssetService) advancePlanningStage(ctx context.Context, userID, completedStageID string) error {
	completed, err := s.repo.ContentAssets().FindReuseStageByID(ctx, completedStageID)
	if err != nil {
		return err
	}
	run, err := s.repo.ContentAssets().FindReuseRunByID(ctx, completed.RunID)
	if err != nil {
		return err
	}
	if run.UserID != userID {
		return fmt.Errorf("content reuse stage is not owned by user")
	}
	if !contentReusePlanningEnabled(run) {
		return nil
	}
	var nextStage, nextKind string
	switch completed.Stage {
	case model.ContentReuseStageResearch:
		nextStage, nextKind = model.ContentReuseStageScoring, model.TaskTypeContentValidation
	case model.ContentReuseStageScoring:
		nextStage, nextKind = model.ContentReuseStageProduce, model.TaskTypeContentScript
	case model.ContentReuseStageProduce:
		return s.advanceProductionStages(ctx, userID, run, completed)
	default:
		return nil
	}
	var previous []ContentStageArtifact
	if len(completed.ArtifactRefs) == 0 || json.Unmarshal(completed.ArtifactRefs, &previous) != nil || len(previous) == 0 {
		return fmt.Errorf("completed planning stage has no accepted artifacts")
	}
	// The script stage consumes both the original research evidence and the
	// validation output. Accepted artifacts are immutable references, so joining
	// them here does not copy or revalidate another metric store.
	if completed.Stage == model.ContentReuseStageScoring {
		research, researchErr := s.repo.ContentAssets().FindReuseStage(ctx, run.ID, model.ContentReuseStageResearch)
		if researchErr != nil {
			return researchErr
		}
		var researchArtifacts []ContentStageArtifact
		if len(research.ArtifactRefs) == 0 || json.Unmarshal(research.ArtifactRefs, &researchArtifacts) != nil || len(researchArtifacts) == 0 {
			return fmt.Errorf("completed research stage has no accepted artifacts")
		}
		previous = append(researchArtifacts, previous...)
	}
	accepted := make([]map[string]any, 0, len(previous))
	for _, ref := range previous {
		accepted = append(accepted, map[string]any{"task_id": ref.TaskID, "execution_id": ref.ExecutionID, "path": ref.Path, "content_sha256": ref.ContentSHA256})
	}
	if nextKind == model.TaskTypeContentScript {
		// Keep the research evidence alongside the validated article/review.
		// The validation task itself is responsible for producing the latter.
	}
	input := map[string]any{"schema_version": "1.0", "task_kind": nextKind, "candidates": []any{}, "accepted_artifacts": accepted, "target_requirements": map[string]any{"platform": run.TargetPlatform}}
	stage, err := s.EnsureReuseStage(ctx, run.UserID, run.ID, nextStage, input)
	if err != nil {
		return err
	}
	if stage.Status != model.ContentStageQueued || reuseStageTaskID(stage) != "" {
		return nil
	}
	run.Stage = nextStage
	if err := s.repo.ContentAssets().UpdateReuseRun(ctx, run); err != nil {
		return err
	}
	tasks, err := s.CreatePlanningStageTask(ctx, run.UserID, run.ID, stage.ID, nextKind, input)
	if err != nil {
		return err
	}
	for _, planningTask := range tasks {
		if err := s.taskSvc.EnqueueExecution(ctx, planningTask, nil); err != nil {
			return err
		}
	}
	return nil
}

// advanceProductionStages turns an accepted script into durable materials and
// render checkpoints. Materials is intentionally server-completed from the
// immutable script artifacts; rendering is the only subsequent paid task.
func (s *ContentAssetService) advanceProductionStages(ctx context.Context, userID string, run *model.ContentReuseRun, scriptStage *model.ContentReuseStageExecution) error {
	if run == nil || scriptStage == nil || run.UserID != userID {
		return fmt.Errorf("content production stage is not owned")
	}
	if !isVideoProductionPlatform(run.TargetPlatform) {
		run.Stage = model.ContentReuseStageReview
		return s.repo.ContentAssets().UpdateReuseRun(ctx, run)
	}
	var scriptArtifacts []ContentStageArtifact
	if len(scriptStage.ArtifactRefs) == 0 || json.Unmarshal(scriptStage.ArtifactRefs, &scriptArtifacts) != nil || len(scriptArtifacts) == 0 {
		return fmt.Errorf("script stage has no accepted artifacts")
	}
	accepted := make([]map[string]any, 0, len(scriptArtifacts))
	var spokenScript, subtitles string
	for _, ref := range scriptArtifacts {
		accepted = append(accepted, map[string]any{"task_id": ref.TaskID, "execution_id": ref.ExecutionID, "path": ref.Path, "content_sha256": ref.ContentSHA256})
		if ref.Path != "output/video-script.md" && ref.Path != "output/video-subtitles.srt" && ref.Path != "output/video-script.json" {
			continue
		}
		_, file, err := planningAcceptedArtifact(ctx, s.repo, s.taskSvc.store, run.UserID, run.TargetProjectID, model.PlanningArtifactRef{TaskID: ref.TaskID, ExecutionID: ref.ExecutionID, Path: ref.Path, ContentSHA256: ref.ContentSHA256})
		if err != nil {
			return err
		}
		body, err := storage.ReadObject(ctx, s.taskSvc.store, file.OSSKey, 2<<20)
		if err != nil {
			return err
		}
		switch ref.Path {
		case "output/video-script.md":
			spokenScript = string(body)
		case "output/video-subtitles.srt":
			subtitles = string(body)
		case "output/video-script.json":
			var script model.PlanningVideoScript
			if err := json.Unmarshal(body, &script); err != nil {
				return fmt.Errorf("decode accepted video script: %w", err)
			}
			if strings.TrimSpace(spokenScript) == "" {
				spokenScript = script.SpokenText
			}
		}
	}
	materialsInput := map[string]any{"schema_version": "1.0", "stage": model.ContentReuseStageMaterials, "accepted_artifacts": accepted, "target_requirements": map[string]any{"platform": run.TargetPlatform}}
	materials, err := s.EnsureReuseStage(ctx, run.UserID, run.ID, model.ContentReuseStageMaterials, materialsInput)
	if err != nil {
		return err
	}
	if materials.Status == model.ContentStageQueued {
		materials.Status = model.ContentStageCompleted
		materials.AcceptedExecutionID = scriptArtifacts[0].ExecutionID
		materials.ArtifactRefs = scriptStage.ArtifactRefs
		materials.RecoveryPoint = model.ContentReuseStageMaterials
		event := &model.ContentReuseAuditEvent{UserID: run.UserID, RunID: run.ID, StageExecutionID: materials.ID, EntityKind: "stage", EntityID: materials.ID, FromState: model.ContentStageQueued, ToState: model.ContentStageCompleted, ExecutionID: materials.AcceptedExecutionID, ReasonCode: "script_materials_accepted"}
		if err := s.repo.ContentAssets().TransitionReuseStage(ctx, materials, event); err != nil {
			return err
		}
	}
	run, err = s.repo.ContentAssets().FindReuseRunByID(ctx, run.ID)
	if err != nil {
		return err
	}
	renderInput := map[string]any{"schema_version": "1.0", "stage": model.ContentReuseStageRender, "accepted_artifacts": accepted, "target_requirements": map[string]any{"platform": run.TargetPlatform}}
	render, err := s.EnsureReuseStage(ctx, run.UserID, run.ID, model.ContentReuseStageRender, renderInput)
	if err != nil {
		return err
	}
	run.Stage = model.ContentReuseStageRender
	if err := s.repo.ContentAssets().UpdateReuseRun(ctx, run); err != nil {
		return err
	}
	if reuseStageTaskID(render) != "" || render.Status != model.ContentStageQueued {
		return nil
	}
	project, err := s.repo.Projects().FindByID(ctx, run.TargetProjectID)
	if err != nil || project.UserID != run.UserID {
		return fmt.Errorf("render project is not owned")
	}
	agentID, channel, taskKind, ok := contentReuseIdentity(project.Platform)
	if !ok {
		return fmt.Errorf("unsupported render platform %q", project.Platform)
	}
	if project.Platform == model.PlatformWhiteboardAnimation {
		if strings.TrimSpace(subtitles) == "" || !validGeneratedSRT(subtitles) {
			run.Status = model.ContentReuseRunStatusPaused
			run.ErrorCode = "render_input_missing_srt"
			run.ErrorMessage = "白板动画渲染需要由脚本阶段提供唯一有效的 SRT 输入"
			return s.repo.ContentAssets().UpdateReuseRun(ctx, run)
		}
	}
	if strings.TrimSpace(spokenScript) == "" {
		return fmt.Errorf("accepted video script markdown is missing")
	}
	params := CreateManualParams{UserID: run.UserID, ProjectID: run.TargetProjectID, ExecutionProfile: "effective", AgentID: agentID, Channel: channel, TaskKind: taskKind, Prompt: fmt.Sprintf("依据已验收脚本渲染选题资产 %s。仅使用已验收输入，不重复生成脚本。", run.AssetID), ContentAssetID: run.AssetID, ReuseRunID: run.ID, ReuseStageID: render.ID, SourceContentRef: strings.Trim(strings.Join([]string{run.SourceContentKind, run.SourceContentID}, ":"), ":")}
	if project.Platform == model.PlatformMontage {
		params.MontageInput = &model.MontageInput{Brief: "依据以下已验收口播脚本制作视频，不得重写脚本：\n\n" + spokenScript, PipelineKey: "cinematic"}
		params.InputAttachments = []model.EntryAttachment{{Type: "text", FileName: "accepted-video-script.md", ContentType: "text/markdown", Text: spokenScript, Role: "script"}}
	}
	if project.Platform == model.PlatformHypit {
		run.Status = model.ContentReuseRunStatusPaused
		run.ErrorCode = "render_reference_missing"
		run.ErrorMessage = "Hypit 渲染需要已有参考视频素材；已保留验收脚本，不会重复生成脚本或发起无素材渲染"
		return s.repo.ContentAssets().UpdateReuseRun(ctx, run)
	}
	if project.Platform == model.PlatformWhiteboardAnimation {
		params.InputAttachments = []model.EntryAttachment{{Type: "text", FileName: "accepted-video-subtitles.srt", ContentType: "application/x-subrip", Text: subtitles, Role: "subtitle"}}
	}
	tasks, err := s.taskSvc.CreateManual(ctx, params)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if err := s.taskSvc.EnqueueExecution(ctx, task, nil); err != nil {
			return err
		}
	}
	return nil
}

func validGeneratedSRT(value string) bool {
	blocks := strings.Split(strings.TrimSpace(strings.ReplaceAll(value, "\r\n", "\n")), "\n\n")
	if len(blocks) == 0 {
		return false
	}
	for i, block := range blocks {
		lines := strings.Split(strings.TrimSpace(block), "\n")
		if len(lines) < 3 || strings.TrimSpace(lines[0]) != fmt.Sprint(i+1) || !strings.Contains(lines[1], " --> ") || strings.TrimSpace(strings.Join(lines[2:], "")) == "" {
			return false
		}
	}
	return true
}

func contentReuseIdentity(platform string) (string, string, string, bool) {
	switch platform {
	case model.PlatformWechat:
		return model.AgentIDArticle, model.ChannelArticle, model.TaskKindContentGeneration, true
	case model.PlatformSeednote:
		return model.AgentIDSeednote, model.ChannelSeednote, model.TaskKindContentGeneration, true
	case model.PlatformHypit:
		return model.AgentIDHypit, model.ChannelHypit, model.AgentIDHypit, true
	case model.PlatformMontage:
		return model.AgentIDMontage, model.ChannelMontage, model.PlatformMontage, true
	case model.PlatformWhiteboardAnimation:
		return model.AgentIDWhiteboard, model.ChannelWhiteboard, model.PlatformWhiteboardAnimation, true
	case model.ChannelWechatPicture:
		return model.AgentIDWechatPicture, model.ChannelWechatPicture, model.TaskKindContentGeneration, true
	default:
		return "", "", "", false
	}
}

func (s *ContentAssetService) SetReuseRunStatus(ctx context.Context, userID, runID, status string) (*model.ContentReuseRun, error) {
	run, err := s.GetReuseRun(ctx, userID, runID)
	if err != nil {
		return nil, err
	}
	status = strings.TrimSpace(status)
	valid := map[string]bool{
		model.ContentReuseRunStatusQueued:    true,
		model.ContentReuseRunStatusPaused:    true,
		model.ContentReuseRunStatusCancelled: true,
	}
	if !valid[status] || run.Status == model.ContentReuseRunStatusCompleted || run.Status == model.ContentReuseRunStatusCancelled {
		return nil, ErrInvalidReuseRunState
	}
	if status == model.ContentReuseRunStatusQueued && run.Status != model.ContentReuseRunStatusPaused && run.Status != model.ContentReuseRunStatusQueued {
		return nil, ErrInvalidReuseRunState
	}
	if status == model.ContentReuseRunStatusQueued && run.ErrorCode == "operation_reconciliation_required" {
		resolved, reconcileErr := s.repo.ContentAssets().ReconcileReuseOperationHold(ctx, userID, run.ID)
		if reconcileErr != nil {
			return nil, reconcileErr
		}
		if !resolved {
			return nil, ErrInvalidReuseRunState
		}
		run, err = s.GetReuseRun(ctx, userID, runID)
		if err != nil {
			return nil, err
		}
	}
	if status == model.ContentReuseRunStatusPaused && run.Status != model.ContentReuseRunStatusQueued && run.Status != model.ContentReuseRunStatusRunning {
		return nil, ErrInvalidReuseRunState
	}
	if status == model.ContentReuseRunStatusCancelled {
		task, err := s.repo.Tasks().FindByReuseRun(ctx, userID, run.ID)
		if errors.Is(err, gorm.ErrRecordNotFound) && run.TaskID == "" {
			run.BudgetReserved = 0
		} else if err != nil {
			return nil, err
		} else {
			run.TaskID = task.ID
		}
	}
	if status == model.ContentReuseRunStatusQueued {
		run.NextRunAt = nil
		run.ErrorCode, run.ErrorMessage = "", ""
	}
	run.Status = status
	if err := s.repo.ContentAssets().UpdateReuseRun(ctx, run); err != nil {
		return nil, fmt.Errorf("update content reuse run: %w", err)
	}
	if status == model.ContentReuseRunStatusQueued {
		if err := s.enqueueReuseRun(ctx, run); err != nil {
			run.Status = model.ContentReuseRunStatusPaused
			run.ErrorCode = "queue_unavailable"
			run.ErrorMessage = err.Error()
			_ = s.repo.ContentAssets().UpdateReuseRun(ctx, run)
			return nil, fmt.Errorf("enqueue content reuse run: %w", err)
		}
	}
	return run, nil
}

func (s *ContentAssetService) GetPolicy(ctx context.Context, userID string) (*model.ContentAutopilotPolicy, error) {
	return s.repo.ContentAssets().GetPolicy(ctx, userID)
}

func (s *ContentAssetService) UpsertPolicy(ctx context.Context, policy *model.ContentAutopilotPolicy) (*model.ContentAutopilotPolicy, error) {
	if policy == nil || policy.UserID == "" {
		return nil, fmt.Errorf("user_id is required")
	}
	if policy.FailureMode == "" {
		policy.FailureMode = model.ContentAutopilotFailurePause
	}
	if policy.FailureMode != model.ContentAutopilotFailurePause && policy.FailureMode != model.ContentAutopilotFailureSkip {
		return nil, fmt.Errorf("failure_mode must be pause or skip")
	}
	if policy.DailyBudgetCredits < 0 || policy.MaxRunCostCredits < 0 {
		return nil, fmt.Errorf("budget credits must be non-negative")
	}
	if policy.MinRecommendationScore < 0 || policy.MinRecommendationScore > 100 {
		return nil, fmt.Errorf("min_recommendation_score must be between 0 and 100")
	}
	if policy.MinVideoConfidence < 0 || policy.MinVideoConfidence > 100 {
		return nil, fmt.Errorf("min_video_confidence must be between 0 and 100")
	}
	platformScores, err := decodePlatformScoreOverrides(policy.PlatformMinRecommendationScores)
	if err != nil {
		return nil, err
	}
	projectIDs, err := decodePolicyStrings(policy.ProjectIDs)
	if err != nil {
		return nil, fmt.Errorf("project_ids must be a string array")
	}
	platforms, err := decodePolicyStrings(policy.Platforms)
	if err != nil {
		return nil, fmt.Errorf("platforms must be a string array")
	}
	platformSet := stringSet(platforms)
	for _, platform := range platforms {
		if !model.IsProjectPlatform(platform) {
			return nil, fmt.Errorf("unsupported autopilot platform %q", platform)
		}
	}
	for _, projectID := range projectIDs {
		project, findErr := s.repo.Projects().FindByID(ctx, projectID)
		if findErr != nil || project.UserID != policy.UserID {
			return nil, fmt.Errorf("autopilot project %q does not belong to user", projectID)
		}
		if len(platformSet) > 0 && !platformSet[project.Platform] {
			return nil, fmt.Errorf("autopilot project platform %q is not enabled", project.Platform)
		}
	}
	for platform := range platformScores {
		if !model.IsProjectPlatform(platform) {
			return nil, fmt.Errorf("unsupported platform score override %q", platform)
		}
	}
	if policy.PublishEnabled {
		if !policy.DraftEnabled {
			return nil, fmt.Errorf("automatic publishing requires draft creation to be enabled")
		}
		if err := validateContentAutopilotPublishWindows(policy.PublishWindows); err != nil {
			return nil, err
		}
	}
	if err := s.repo.ContentAssets().UpsertPolicy(ctx, policy); err != nil {
		return nil, fmt.Errorf("save content autopilot policy: %w", err)
	}
	return policy, nil
}

func decodePlatformScoreOverrides(raw []byte) (map[string]int, error) {
	if len(strings.TrimSpace(string(raw))) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return map[string]int{}, nil
	}
	var values map[string]int
	if err := json.Unmarshal(raw, &values); err != nil || values == nil {
		return nil, fmt.Errorf("platform_min_recommendation_scores must be an object of integers")
	}
	for platform, score := range values {
		if !model.IsProjectPlatform(platform) {
			return nil, fmt.Errorf("unsupported platform score override %q", platform)
		}
		if score < 0 || score > 100 {
			return nil, fmt.Errorf("platform score override for %q must be between 0 and 100", platform)
		}
	}
	return values, nil
}

type contentAutopilotPublishWindow struct {
	Start    string `json:"start"`
	End      string `json:"end"`
	Timezone string `json:"timezone"`
}

func validateContentAutopilotPublishWindows(raw []byte) error {
	var windows []contentAutopilotPublishWindow
	if err := json.Unmarshal(raw, &windows); err != nil || len(windows) == 0 {
		return fmt.Errorf("automatic publishing requires at least one valid publish window")
	}
	for _, window := range windows {
		start, startErr := time.Parse("15:04", window.Start)
		end, endErr := time.Parse("15:04", window.End)
		if startErr != nil || endErr != nil || !start.Before(end) {
			return fmt.Errorf("publish windows must use start/end HH:MM and have start before end")
		}
		if _, err := time.LoadLocation(strings.TrimSpace(window.Timezone)); err != nil {
			return fmt.Errorf("publish window timezone is invalid")
		}
	}
	return nil
}

func mustContentAssetJSON(value any) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}

func contentReuseRequestFingerprint(p CreateReuseRunParams, assetID, projectID string) string {
	payload := struct {
		AssetID           string         `json:"asset_id"`
		SourceContentKind string         `json:"source_content_kind"`
		SourceContentID   string         `json:"source_content_id"`
		TargetProjectID   string         `json:"target_project_id"`
		TargetPlatform    string         `json:"target_platform"`
		TargetAgentID     string         `json:"target_agent_id"`
		ScoreSnapshot     map[string]any `json:"score_snapshot,omitempty"`
		PolicySnapshot    map[string]any `json:"policy_snapshot,omitempty"`
	}{assetID, p.SourceContentKind, p.SourceContentID, projectID, p.TargetPlatform, p.TargetAgentID, p.ScoreSnapshot, p.PolicySnapshot}
	encoded, _ := json.Marshal(payload)
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", digest[:])
}

func contentReuseRunFingerprint(run *model.ContentReuseRun) string {
	var scoreSnapshot, policySnapshot map[string]any
	if len(run.ScoreSnapshot) > 0 {
		_ = json.Unmarshal(run.ScoreSnapshot, &scoreSnapshot)
	}
	if len(run.PolicySnapshot) > 0 {
		_ = json.Unmarshal(run.PolicySnapshot, &policySnapshot)
	}
	return contentReuseRequestFingerprint(CreateReuseRunParams{
		SourceContentKind: run.SourceContentKind,
		SourceContentID:   run.SourceContentID,
		TargetProjectID:   run.TargetProjectID,
		TargetPlatform:    run.TargetPlatform,
		TargetAgentID:     run.TargetAgentID,
		ScoreSnapshot:     scoreSnapshot,
		PolicySnapshot:    policySnapshot,
	}, run.AssetID, run.TargetProjectID)
}
