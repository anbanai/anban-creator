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
	"unicode"

	appwechat "github.com/anbanai/anban-creator/app/wechat"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

var (
	ErrWechatPublicationNotFound             = errors.New("WeChat publication not found")
	ErrWechatPublicationForbidden            = errors.New("WeChat publication does not belong to user")
	ErrWechatPublicationProjectMismatch      = errors.New("WeChat publication task and project do not match")
	ErrWechatPublicationConflict             = errors.New("WeChat publication state changed concurrently")
	ErrWechatPublicationModeConflict         = errors.New("WeChat publication action is not allowed by project mode")
	ErrWechatPublicationPending              = errors.New("WeChat publication outcome is pending reconciliation")
	ErrWechatPublicationRateLimited          = errors.New("WeChat publication reconciliation is rate limited")
	ErrWechatPublicationArticleNotFound      = errors.New("published article was not found in the configured WeChat account")
	ErrWechatPublicationInvalidPayload       = errors.New("invalid WeChat draft payload")
	ErrWechatPublicationDraftUnsupported     = errors.New("WeChat account does not support the draft API")
	ErrWechatPublicationDraftRejected        = errors.New("WeChat definitively rejected draft creation")
	ErrWechatPublicationDraftFailed          = errors.New("WeChat draft outcome could not be confirmed")
	ErrWechatPublicationSchedulerUnavailable = errors.New("WeChat publication scheduler is unavailable")
	errWechatPublicationVersionChanged       = errors.New("WeChat publication version changed")
)

const (
	wechatPublicationClaimLease = 2 * time.Minute
	wechatReconcileRateLimit    = time.Minute
	wechatProviderPageSize      = int64(20)
	wechatRecoveryDispatchLease = 15 * time.Minute

	WechatPublicationPollTaskType      = "wechat:publication_poll"
	WechatPublicationReconcileTaskType = "wechat:publication_reconcile"
)

// WechatPublicationAPI is the atomic official-account capability surface used
// by the lifecycle. The DataCube endpoint belongs to the analytics service.
type WechatPublicationAPI interface {
	AddDraft(context.Context, appwechat.DraftAddRequest) (*appwechat.DraftAddResponse, error)
	BatchGetDrafts(context.Context, appwechat.DraftBatchGetRequest) (*appwechat.DraftBatchGetResponse, error)
	SubmitFreePublish(context.Context, appwechat.FreePublishSubmitRequest) (*appwechat.FreePublishSubmitResponse, error)
	GetFreePublish(context.Context, appwechat.FreePublishGetRequest) (*appwechat.FreePublishGetResponse, error)
	BatchGetFreePublishes(context.Context, appwechat.FreePublishBatchGetRequest) (*appwechat.FreePublishBatchGetResponse, error)
}

type WechatPublicationAPIFactory func(*model.Project) (WechatPublicationAPI, error)

type WechatPublicationService struct {
	repo       repository.Repository
	apiFactory WechatPublicationAPIFactory
	logger     *zerolog.Logger
	enqueuer   TaskEnqueuer
	now        func() time.Time
}

// SetEnqueuer wires durable polling into the server's async worker. The
// publication row remains the source of truth, so enqueue failures never lose
// the pending next_check_at timestamp and can be recovered on the next scan.
func (s *WechatPublicationService) SetEnqueuer(enqueuer TaskEnqueuer) { s.enqueuer = enqueuer }

func (s *WechatPublicationService) enqueuePoll(publicationID string, at *time.Time) {
	if s.enqueuer == nil || publicationID == "" || at == nil {
		return
	}
	delay := at.Sub(s.now())
	if delay < 0 {
		delay = 0
	}
	payload, _ := json.Marshal(map[string]string{"publication_id": publicationID})
	_ = s.enqueuer.EnqueueIn(WechatPublicationPollTaskType, payload, delay)
}

func (s *WechatPublicationService) enqueueReconcile(projectID string, at *time.Time) {
	if s.enqueuer == nil || projectID == "" || at == nil {
		return
	}
	delay := at.Sub(s.now())
	if delay < 0 {
		delay = 0
	}
	payload, _ := json.Marshal(map[string]string{"project_id": projectID})
	_ = s.enqueuer.EnqueueIn(WechatPublicationReconcileTaskType, payload, delay)
}

func NewWechatPublicationService(repo repository.Repository, factory WechatPublicationAPIFactory, logger *zerolog.Logger) *WechatPublicationService {
	if factory == nil {
		publisher := NewPublishingService(repo, logger)
		factory = func(project *model.Project) (WechatPublicationAPI, error) {
			cfg, err := publisher.buildAppConfig(project)
			if err != nil {
				return nil, err
			}
			return appwechat.NewService(cfg, logger).OfficialAPI(), nil
		}
	}
	return &WechatPublicationService{repo: repo, apiFactory: factory, logger: logger, now: time.Now}
}

// WechatContentFingerprint hashes a stable structural rendering of article
// HTML. Attribute ordering and insignificant whitespace do not affect it.
func WechatContentFingerprint(content string) string {
	nodes, err := html.ParseFragment(strings.NewReader(content), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		normalized := strings.Join(strings.Fields(content), " ")
		sum := sha256.Sum256([]byte(normalized))
		return hex.EncodeToString(sum[:])
	}
	var b strings.Builder
	for _, node := range nodes {
		writeNormalizedWechatHTML(&b, node)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func writeNormalizedWechatHTML(b *strings.Builder, node *html.Node) {
	switch node.Type {
	case html.TextNode:
		text := strings.Join(strings.Fields(node.Data), " ")
		if text != "" {
			if strings.TrimLeftFunc(node.Data, unicode.IsSpace) != node.Data {
				b.WriteByte(' ')
			}
			b.WriteString(text)
			if strings.TrimRightFunc(node.Data, unicode.IsSpace) != node.Data {
				b.WriteByte(' ')
			}
		}
	case html.ElementNode:
		b.WriteByte('<')
		b.WriteString(strings.ToLower(node.Data))
		attrs := append([]html.Attribute(nil), node.Attr...)
		sort.Slice(attrs, func(i, j int) bool {
			if attrs[i].Key == attrs[j].Key {
				return attrs[i].Val < attrs[j].Val
			}
			return attrs[i].Key < attrs[j].Key
		})
		for _, attr := range attrs {
			b.WriteByte(' ')
			b.WriteString(strings.ToLower(attr.Key))
			b.WriteByte('=')
			b.WriteString(strings.TrimSpace(attr.Val))
		}
		b.WriteByte('>')
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		writeNormalizedWechatHTML(b, child)
	}
	if node.Type == html.ElementNode {
		b.WriteString("</")
		b.WriteString(strings.ToLower(node.Data))
		b.WriteByte('>')
	}
}

func (s *WechatPublicationService) ownedTaskProject(ctx context.Context, userID, taskID string) (*model.Task, *model.Project, error) {
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, ErrWechatPublicationNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("find task: %w", err)
	}
	if task.UserID != userID {
		return nil, nil, ErrWechatPublicationForbidden
	}
	if task.Type != model.PlatformArticle || task.ProjectID == "" {
		return nil, nil, ErrWechatPublicationModeConflict
	}
	project, err := s.repo.Projects().FindByID(ctx, task.ProjectID)
	if err != nil {
		return nil, nil, fmt.Errorf("find project: %w", err)
	}
	if project.UserID != userID {
		return nil, nil, ErrWechatPublicationForbidden
	}
	return task, project, nil
}

func (s *WechatPublicationService) projectAPI(project *model.Project) (WechatPublicationAPI, error) {
	if s.apiFactory == nil {
		return nil, fmt.Errorf("WeChat publication API unavailable")
	}
	api, err := s.apiFactory(project)
	if err != nil {
		return nil, err
	}
	if api == nil {
		return nil, fmt.Errorf("WeChat publication API unavailable")
	}
	return api, nil
}

func (s *WechatPublicationService) Get(ctx context.Context, userID, taskID string) (*model.WechatPublication, error) {
	if _, _, err := s.ownedTaskProject(ctx, userID, taskID); err != nil {
		return nil, err
	}
	publication, err := s.repo.WechatPublications().FindByTaskID(ctx, taskID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrWechatPublicationNotFound
	}
	return publication, err
}

func firstDraftArticle(request appwechat.DraftAddRequest) (appwechat.DraftArticle, error) {
	if len(request.Articles) != 1 {
		return appwechat.DraftArticle{}, fmt.Errorf("%w: exactly one draft article is required", ErrWechatPublicationInvalidPayload)
	}
	article := normalizeDraftArticle(request.Articles[0])
	if article.Title == "" || strings.TrimSpace(article.Content) == "" {
		return article, fmt.Errorf("%w: draft title and content are required", ErrWechatPublicationInvalidPayload)
	}
	if err := validateContentImageDiversity(article.Content); err != nil {
		return article, fmt.Errorf("%w: %v", ErrWechatPublicationInvalidPayload, err)
	}
	return article, nil
}

func normalizeDraftArticle(article appwechat.DraftArticle) appwechat.DraftArticle {
	article.Title = strings.TrimSpace(article.Title)
	article.Author = strings.TrimSpace(article.Author)
	article.Digest = strings.TrimSpace(article.Digest)
	article.ContentSourceURL = strings.TrimSpace(article.ContentSourceURL)
	article.ThumbMediaID = strings.TrimSpace(article.ThumbMediaID)
	article.URL = strings.TrimSpace(article.URL)
	return article
}

func wechatDraftRequestFingerprint(article appwechat.DraftArticle) string {
	article = normalizeDraftArticle(article)
	article.Content = WechatContentFingerprint(article.Content)
	encoded, _ := json.Marshal(article)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func (s *WechatPublicationService) CreateDraft(ctx context.Context, userID, taskID, projectID string, request appwechat.DraftAddRequest) (*model.WechatPublication, error) {
	task, project, err := s.ownedTaskProject(ctx, userID, taskID)
	if err != nil {
		return nil, err
	}
	if project.ID != projectID {
		return nil, ErrWechatPublicationProjectMismatch
	}
	if project.GetWechatPublishMode() == model.WechatPublishModeDisabled {
		return nil, ErrWechatPublicationModeConflict
	}
	article, err := firstDraftArticle(request)
	if err != nil {
		return nil, err
	}
	request.Articles[0] = article
	fingerprint := WechatContentFingerprint(article.Content)
	requestFingerprint := wechatDraftRequestFingerprint(article)

	existing, findErr := s.repo.WechatPublications().FindByTaskID(ctx, task.ID)
	fresh := errors.Is(findErr, gorm.ErrRecordNotFound)
	claimedForRetry := false
	retryWechatStatusCode := 0
	if findErr != nil && !fresh {
		return nil, findErr
	}
	if !fresh {
		if existing.DraftContentFingerprint != fingerprint || existing.DraftRequestFingerprint != requestFingerprint {
			return existing, ErrWechatPublicationConflict
		}
		if existing.Status == model.WechatPublicationStatusUnsupported && existing.WechatStatusCode != 0 && existing.DraftMediaID == "" {
			retryWechatStatusCode = existing.WechatStatusCode
			now, token := s.now(), uuid.NewString()
			won, retryErr := s.repo.WechatPublications().RetryUnsupportedDraft(ctx, existing.ID, existing.UpdatedAt, now, token)
			if retryErr != nil {
				return existing, retryErr
			}
			if !won {
				return s.finishConcurrentDraftRecovery(ctx, userID, taskID, project, existing.ID)
			}
			existing, findErr = s.repo.WechatPublications().FindByID(ctx, existing.ID)
			if findErr != nil {
				return nil, findErr
			}
			claimedForRetry = true
		}
		if existing.DraftMediaID != "" {
			return s.autoPublishDraft(ctx, userID, taskID, project, existing)
		}
		if !claimedForRetry && existing.ClaimToken != "" && existing.ClaimedAt != nil && existing.ClaimedAt.After(s.now().Add(-wechatPublicationClaimLease)) {
			return existing, ErrWechatPublicationPending
		}
	} else {
		now := s.now()
		existing = &model.WechatPublication{
			ID: uuid.NewString(), TaskID: task.ID, UserID: userID, ProjectID: project.ID,
			DraftTitle: article.Title, DraftAuthor: article.Author, DraftDigest: article.Digest,
			DraftThumbMediaID: article.ThumbMediaID, DraftContentFingerprint: fingerprint, DraftRequestFingerprint: requestFingerprint,
			Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusDrafting,
			DraftCreatedAt: &now, ClaimToken: uuid.NewString(), ClaimedAt: &now,
		}
		if err := s.repo.WechatPublications().Create(ctx, existing); err != nil {
			winner, winnerErr := s.repo.WechatPublications().FindByTaskID(ctx, task.ID)
			if winnerErr != nil {
				return nil, err
			}
			return winner, ErrWechatPublicationPending
		}
	}

	api, err := s.projectAPI(project)
	if err != nil {
		if existing.Status == model.WechatPublicationStatusUnsupported {
			return existing, errors.Join(ErrWechatPublicationDraftUnsupported, err)
		}
		if fresh || claimedForRetry {
			return existing, s.resolveReadOnlyDraftClaimFailure(ctx, existing, err, fresh, retryWechatStatusCode)
		}
		return existing, err
	}
	recovered, err := s.recoverDraftByListing(ctx, api, existing)
	if errors.Is(err, errWechatPublicationVersionChanged) {
		return s.finishConcurrentDraftRecovery(ctx, userID, taskID, project, existing.ID)
	}
	if err != nil {
		if (fresh || claimedForRetry) && !errors.Is(err, ErrWechatPublicationDraftUnsupported) {
			return existing, s.resolveReadOnlyDraftClaimFailure(ctx, existing, err, fresh, retryWechatStatusCode)
		}
		return existing, err
	}
	if recovered {
		return s.finishConcurrentDraftRecovery(ctx, userID, taskID, project, existing.ID)
	}
	if !fresh && !claimedForRetry && existing.Status == model.WechatPublicationStatusDrafting && existing.DraftAddAttemptedAt == nil {
		now, token := s.now(), uuid.NewString()
		won, reclaimErr := s.repo.WechatPublications().ReclaimUnattemptedDraft(
			ctx, existing.ID, existing.UpdatedAt, now.Add(-wechatPublicationClaimLease), now, token,
		)
		if reclaimErr != nil {
			return existing, reclaimErr
		}
		if !won {
			return s.finishConcurrentDraftRecovery(ctx, userID, taskID, project, existing.ID)
		}
		existing, findErr = s.repo.WechatPublications().FindByID(ctx, existing.ID)
		if findErr != nil {
			return nil, findErr
		}
		claimedForRetry = true
	}
	if claimedForRetry && existing.DraftAddAttemptedAt != nil {
		existing.LastError = "此前创建草稿的结果仍未确认，系统只会继续核对，不会重复创建草稿"
		existing.NextCheckAt = nextWechatManualCheck(s.now(), valueOrTime(existing.DraftCreatedAt, existing.CreatedAt))
		if existing.NextCheckAt == nil {
			existing.Status = model.WechatPublicationStatusPublishFailed
			existing.LastError = "此前创建草稿的结果连续 72 小时未能确认"
		}
		won, persistErr := s.repo.WechatPublications().UpdateDraftClaimed(context.WithoutCancel(ctx), existing, existing.ClaimToken)
		if persistErr != nil {
			return existing, fmt.Errorf("persist ambiguous WeChat draft reconciliation: %w", persistErr)
		}
		if !won {
			return s.finishConcurrentDraftRecovery(context.WithoutCancel(ctx), userID, taskID, project, existing.ID)
		}
		existing.ClaimToken, existing.ClaimedAt = "", nil
		s.enqueueReconcile(existing.ProjectID, existing.NextCheckAt)
		if existing.Status == model.WechatPublicationStatusPublishFailed {
			return existing, ErrWechatPublicationDraftFailed
		}
		return existing, ErrWechatPublicationPending
	}
	if !fresh && !claimedForRetry {
		return s.finishConcurrentDraftRecovery(ctx, userID, taskID, project, existing.ID)
	}

	draftAddAttemptedAt := s.now()
	draftRecoveryAt := draftAddAttemptedAt.Add(10 * time.Minute)
	won, err := s.repo.WechatPublications().MarkDraftAddAttempted(ctx, existing.ID, existing.ClaimToken, draftAddAttemptedAt, draftRecoveryAt)
	if err != nil {
		return existing, err
	}
	if !won {
		return s.finishConcurrentDraftRecovery(ctx, userID, taskID, project, existing.ID)
	}
	existing.DraftAddAttemptedAt = &draftAddAttemptedAt
	existing.DraftCreatedAt = &draftAddAttemptedAt
	existing.NextCheckAt = &draftRecoveryAt
	response, err := api.AddDraft(ctx, request)
	draftClaimToken := existing.ClaimToken
	if err != nil {
		if code, definitive := definitiveWechatPublicationError(err); definitive {
			existing.Status, existing.WechatStatusCode, existing.LastError, existing.NextCheckAt = model.WechatPublicationStatusUnsupported, code, safeWechatPublicationError(err), nil
			existing.DraftAddAttemptedAt = nil
		} else {
			existing.LastError = safeWechatPublicationError(err)
			existing.NextCheckAt = nextWechatManualCheck(s.now(), valueOrTime(existing.DraftCreatedAt, existing.CreatedAt))
		}
		won, persistErr := s.repo.WechatPublications().UpdateDraftClaimed(context.WithoutCancel(ctx), existing, draftClaimToken)
		if persistErr != nil {
			return existing, fmt.Errorf("persist ambiguous WeChat draft outcome: %w", persistErr)
		}
		if !won {
			return s.finishConcurrentDraftRecovery(context.WithoutCancel(ctx), userID, taskID, project, existing.ID)
		}
		existing.ClaimToken, existing.ClaimedAt = "", nil
		s.enqueueReconcile(existing.ProjectID, existing.NextCheckAt)
		if existing.Status == model.WechatPublicationStatusUnsupported {
			if existing.WechatStatusCode == 48001 {
				return existing, errors.Join(ErrWechatPublicationDraftUnsupported, err)
			}
			return existing, errors.Join(ErrWechatPublicationDraftRejected, err)
		}
		return existing, err
	}
	if response == nil || strings.TrimSpace(response.MediaID) == "" {
		existing.LastError = "WeChat draft response omitted media_id"
		existing.NextCheckAt = nextWechatManualCheck(s.now(), valueOrTime(existing.DraftCreatedAt, existing.CreatedAt))
		won, persistErr := s.repo.WechatPublications().UpdateDraftClaimed(context.WithoutCancel(ctx), existing, draftClaimToken)
		if persistErr != nil {
			return existing, fmt.Errorf("persist ambiguous WeChat draft response: %w", persistErr)
		}
		if !won {
			return s.finishConcurrentDraftRecovery(context.WithoutCancel(ctx), userID, taskID, project, existing.ID)
		}
		existing.ClaimToken, existing.ClaimedAt = "", nil
		return existing, ErrWechatPublicationPending
	}
	now := s.now()
	existing.DraftMediaID, existing.Status, existing.LastError = response.MediaID, model.WechatPublicationStatusDrafted, ""
	existing.DraftCreatedAt = &now
	existing.NextCheckAt = nextWechatManualCheck(now, now)
	won, err = s.repo.WechatPublications().UpdateDraftClaimed(ctx, existing, draftClaimToken)
	if err != nil {
		return nil, err
	}
	if !won {
		return s.finishConcurrentDraftRecovery(ctx, userID, taskID, project, existing.ID)
	}
	existing.ClaimToken, existing.ClaimedAt = "", nil
	s.enqueueReconcile(existing.ProjectID, existing.NextCheckAt)
	return s.autoPublishDraft(ctx, userID, taskID, project, existing)
}

func (s *WechatPublicationService) resolveReadOnlyDraftClaimFailure(ctx context.Context, publication *model.WechatPublication, cause error, fresh bool, retryWechatStatusCode int) error {
	var won bool
	var err error
	if fresh {
		won, err = s.repo.WechatPublications().DeleteDraftClaimed(context.WithoutCancel(ctx), publication.ID, publication.ClaimToken)
	} else {
		won, err = s.repo.WechatPublications().RestoreUnsupportedDraftClaimed(
			context.WithoutCancel(ctx), publication.ID, publication.ClaimToken,
			retryWechatStatusCode, "微信配置修复后的状态检查暂时失败："+safeWechatPublicationError(cause),
		)
	}
	if err != nil {
		return fmt.Errorf("resolve unsubmitted WeChat draft intent: %v; original error: %w", err, cause)
	}
	if !won {
		return errors.Join(cause, ErrWechatPublicationConflict)
	}
	return cause
}

func (s *WechatPublicationService) finishConcurrentDraftRecovery(ctx context.Context, userID, taskID string, project *model.Project, publicationID string) (*model.WechatPublication, error) {
	latest, err := s.repo.WechatPublications().FindByID(ctx, publicationID)
	if err != nil {
		return nil, err
	}
	if latest.DraftMediaID == "" {
		if latest.Status == model.WechatPublicationStatusUnsupported {
			if latest.WechatStatusCode == 48001 {
				return latest, ErrWechatPublicationDraftUnsupported
			}
			return latest, ErrWechatPublicationDraftRejected
		}
		if latest.Status == model.WechatPublicationStatusPublishFailed {
			return latest, ErrWechatPublicationDraftFailed
		}
		return latest, ErrWechatPublicationPending
	}
	if latest.Status == model.WechatPublicationStatusPublishing {
		s.enqueuePoll(latest.ID, latest.NextCheckAt)
	} else {
		s.enqueueReconcile(latest.ProjectID, latest.NextCheckAt)
	}
	return s.autoPublishDraft(ctx, userID, taskID, project, latest)
}

func (s *WechatPublicationService) autoPublishDraft(ctx context.Context, userID, taskID string, project *model.Project, publication *model.WechatPublication) (*model.WechatPublication, error) {
	if project.GetWechatPublishMode() != model.WechatPublishModeAPIConfirmed || publication.Status != model.WechatPublicationStatusDrafted {
		return publication, nil
	}
	published, err := s.Publish(ctx, userID, taskID)
	if err == nil {
		return published, nil
	}
	return s.deferAutomaticPublish(ctx, publication, err)
}

func (s *WechatPublicationService) deferAutomaticPublish(ctx context.Context, publication *model.WechatPublication, cause error) (*model.WechatPublication, error) {
	latest, err := s.repo.WechatPublications().FindByID(context.WithoutCancel(ctx), publication.ID)
	if err != nil {
		return publication, cause
	}
	if latest.Status == model.WechatPublicationStatusDrafted && !hasWechatSubmissionEvidence(latest) {
		expectedStatus, expectedUpdatedAt := latest.Status, latest.UpdatedAt
		if latest.NextCheckAt == nil {
			next := s.now().Add(10 * time.Minute)
			latest.NextCheckAt = &next
		}
		latest.LastError = "automatic formal publish deferred: " + safeWechatPublicationError(cause)
		won, updateErr := s.repo.WechatPublications().UpdateReconciliation(context.WithoutCancel(ctx), latest, expectedStatus, expectedUpdatedAt)
		if updateErr != nil {
			return latest, updateErr
		}
		if !won {
			concurrent, outcomeErr := s.reloadWechatPublicationAfterCASLoss(context.WithoutCancel(ctx), latest.ID)
			if outcomeErr != nil && !errors.Is(outcomeErr, ErrWechatPublicationPending) && !errors.Is(outcomeErr, ErrWechatPublicationConflict) {
				return concurrent, outcomeErr
			}
			latest = concurrent
		}
	}
	if latest.Status == model.WechatPublicationStatusDrafted {
		s.enqueueReconcile(latest.ProjectID, latest.NextCheckAt)
	} else {
		s.enqueuePublicationNext(latest)
	}
	return latest, nil
}

func (s *WechatPublicationService) recoverDraftByListing(ctx context.Context, api WechatPublicationAPI, publication *model.WechatPublication) (bool, error) {
	items, err := listAllWechatDrafts(ctx, api)
	if err != nil {
		if code, ok := publicationWechatErrCode(err); ok && code == 48001 {
			expectedStatus, expectedUpdatedAt := publication.Status, publication.UpdatedAt
			publication.Status, publication.WechatStatusCode, publication.LastError = model.WechatPublicationStatusUnsupported, code, safeWechatPublicationError(err)
			publication.NextCheckAt = nil
			won, persistErr := s.repo.WechatPublications().UpdateReconciliation(context.WithoutCancel(ctx), publication, expectedStatus, expectedUpdatedAt)
			if persistErr != nil {
				return false, fmt.Errorf("persist unsupported WeChat draft state: %w", persistErr)
			}
			if !won {
				return false, errWechatPublicationVersionChanged
			}
			return false, errors.Join(ErrWechatPublicationDraftUnsupported, err)
		}
		return false, err
	}
	return s.recoverDraftFromItems(ctx, publication, items)
}

func (s *WechatPublicationService) recoverDraftFromItems(ctx context.Context, publication *model.WechatPublication, items []appwechat.DraftBatchItem) (bool, error) {
	matched, ok := exactWechatDraftMatch(publication, items)
	if !ok {
		return false, nil
	}
	lastCheckedAt := s.now()
	nextCheckAt := nextWechatManualCheck(lastCheckedAt, valueOrTime(publication.DraftCreatedAt, publication.CreatedAt))
	won, err := s.repo.WechatPublications().TransitionDraftRecovered(
		ctx, publication.ID, publication.UpdatedAt, matched.MediaID, nextCheckAt, &lastCheckedAt,
	)
	if err != nil {
		return false, err
	}
	if !won {
		return false, errWechatPublicationVersionChanged
	}
	publication.DraftMediaID = matched.MediaID
	publication.Status = model.WechatPublicationStatusDrafted
	publication.LastError = ""
	publication.NextCheckAt = nextCheckAt
	publication.LastCheckedAt = &lastCheckedAt
	publication.ClaimToken, publication.ClaimedAt = "", nil
	return true, nil
}

func exactWechatDraftMatch(publication *model.WechatPublication, items []appwechat.DraftBatchItem) (appwechat.DraftBatchItem, bool) {
	matches := make([]appwechat.DraftBatchItem, 0, 1)
	createdAt := valueOrTime(publication.DraftCreatedAt, publication.CreatedAt)
	for _, item := range items {
		if item.UpdateTime <= 0 || item.UpdateTime < createdAt.Unix() || len(item.Content.NewsItems) != 1 {
			continue
		}
		article := normalizeDraftArticle(item.Content.NewsItems[0])
		if WechatContentFingerprint(article.Content) == publication.DraftContentFingerprint &&
			wechatDraftRequestFingerprint(article) == publication.DraftRequestFingerprint {
			matches = append(matches, item)
		}
	}
	if len(matches) != 1 {
		return appwechat.DraftBatchItem{}, false
	}
	return matches[0], true
}

func listAllWechatDrafts(ctx context.Context, api WechatPublicationAPI) ([]appwechat.DraftBatchItem, error) {
	var items []appwechat.DraftBatchItem
	for offset := int64(0); ; {
		response, err := api.BatchGetDrafts(ctx, appwechat.DraftBatchGetRequest{Offset: offset, Count: wechatProviderPageSize, NoContent: false})
		if err != nil {
			return nil, err
		}
		if response == nil {
			return nil, errors.New("empty WeChat draft list response")
		}
		items = append(items, response.Items...)
		pageCount := response.ItemCount
		if pageCount <= 0 {
			pageCount = int64(len(response.Items))
		}
		if response.TotalCount <= offset+pageCount {
			return items, nil
		}
		if pageCount <= 0 {
			return nil, errors.New("WeChat draft pagination made no progress")
		}
		offset += pageCount
	}
}

func listAllWechatPublishes(ctx context.Context, api WechatPublicationAPI) ([]publishedWechatArticle, error) {
	var articles []publishedWechatArticle
	for offset := int64(0); ; {
		response, err := api.BatchGetFreePublishes(ctx, appwechat.FreePublishBatchGetRequest{Offset: offset, Count: wechatProviderPageSize, NoContent: false})
		if err != nil {
			return nil, err
		}
		if response == nil {
			return nil, errors.New("empty WeChat published list response")
		}
		articles = append(articles, publishedArticles(response)...)
		pageCount := response.ItemCount
		if pageCount <= 0 {
			pageCount = int64(len(response.Items))
		}
		if response.TotalCount <= offset+pageCount {
			return articles, nil
		}
		if pageCount <= 0 {
			return nil, errors.New("WeChat published pagination made no progress")
		}
		offset += pageCount
	}
}

func (s *WechatPublicationService) Publish(ctx context.Context, userID, taskID string) (*model.WechatPublication, error) {
	_, project, err := s.ownedTaskProject(ctx, userID, taskID)
	if err != nil {
		return nil, err
	}
	if project.GetWechatPublishMode() == model.WechatPublishModeDisabled {
		return nil, ErrWechatPublicationModeConflict
	}
	if s.enqueuer == nil {
		return nil, ErrWechatPublicationSchedulerUnavailable
	}
	publication, err := s.repo.WechatPublications().FindByTaskID(ctx, taskID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrWechatPublicationNotFound
	}
	if err != nil {
		return nil, err
	}
	if publication.Status == model.WechatPublicationStatusPublished || publication.Status == model.WechatPublicationStatusUnsupported {
		return publication, nil
	}
	api, err := s.projectAPI(project)
	if err != nil {
		return publication, err
	}
	articles, err := listAllWechatPublishes(ctx, api)
	if err != nil {
		return s.handleFormalPublishProviderError(ctx, publication, err)
	}
	matches, candidates, err := s.matchPublished(ctx, publication, articles)
	if err != nil {
		return publication, err
	}
	if len(matches) == 1 {
		return s.bindPublished(ctx, publication, matches[0], reconciledWechatPublicationSource(publication))
	}
	if len(matches) > 1 {
		candidates = append(candidates, candidateViews(matches, "exact")...)
	}
	if len(candidates) > 0 {
		encoded, marshalErr := json.Marshal(dedupeWechatCandidates(candidates))
		if marshalErr != nil {
			return publication, marshalErr
		}
		source := reconciledWechatPublicationSource(publication)
		nextCheckAt := nextWechatManualCheck(s.now(), wechatFormalReconciliationAnchor(publication))
		won, err := s.repo.WechatPublications().TransitionToNeedsSelection(
			ctx, publication.ID, publication.Status, publication.UpdatedAt, source, encoded, nextCheckAt,
		)
		if err != nil {
			return publication, err
		}
		if !won {
			return s.reloadWechatPublicationAfterCASLoss(ctx, publication.ID)
		}
		publication.Status, publication.Source = model.WechatPublicationStatusNeedsSelection, source
		publication.Candidates, publication.NextCheckAt = datatypes.JSON(encoded), nextCheckAt
		return publication, ErrWechatPublicationPending
	}

	drafts, err := listAllWechatDrafts(ctx, api)
	if err != nil {
		return s.handleFormalPublishProviderError(ctx, publication, err)
	}
	if publication.DraftMediaID == "" {
		recovered, recoverErr := s.recoverDraftFromItems(ctx, publication, drafts)
		if errors.Is(recoverErr, errWechatPublicationVersionChanged) {
			return s.reloadWechatPublicationAfterCASLoss(ctx, publication.ID)
		}
		if recoverErr != nil {
			return publication, recoverErr
		}
		if !recovered {
			return publication, ErrWechatPublicationPending
		}
	}
	draftExists := false
	for _, item := range drafts {
		if item.MediaID == publication.DraftMediaID {
			draftExists = true
			break
		}
	}
	if !draftExists {
		lastError := "WeChat draft disappeared before publish submission"
		nextCheckAt := nextWechatManualCheck(s.now(), wechatFormalReconciliationAnchor(publication))
		if nextCheckAt == nil {
			return s.failExpiredFormalReconciliation(ctx, publication, "微信草稿已不存在，且连续 72 小时未识别到正式发布结果")
		}
		won, err := s.repo.WechatPublications().TransitionToPublishSubmitting(
			ctx, publication.ID, publication.Status, publication.UpdatedAt, lastError, nextCheckAt,
		)
		if err != nil {
			return publication, err
		}
		if !won {
			return s.reloadWechatPublicationAfterCASLoss(ctx, publication.ID)
		}
		publication.Status, publication.LastError = model.WechatPublicationStatusPublishSubmitting, lastError
		publication.NextCheckAt = nextCheckAt
		s.enqueueReconcile(publication.ProjectID, nextCheckAt)
		return publication, ErrWechatPublicationPending
	}
	if publication.Status == model.WechatPublicationStatusDrafted && hasWechatSubmissionEvidence(publication) {
		nextCheckAt := nextWechatManualCheck(s.now(), wechatFormalReconciliationAnchor(publication))
		if nextCheckAt == nil {
			return s.failExpiredFormalReconciliation(ctx, publication, "微信正式发布提交后连续 72 小时未能确认结果")
		}
		won, err := s.repo.WechatPublications().TransitionToPublishSubmitting(
			ctx, publication.ID, publication.Status, publication.UpdatedAt,
			"WeChat publish may already have been accepted; reconciling before any retry", nextCheckAt,
		)
		if err != nil {
			return publication, err
		}
		if !won {
			return s.reloadWechatPublicationAfterCASLoss(ctx, publication.ID)
		}
		publication.Status = model.WechatPublicationStatusPublishSubmitting
		publication.NextCheckAt = nextCheckAt
		s.enqueueReconcile(publication.ProjectID, nextCheckAt)
		return publication, ErrWechatPublicationPending
	}
	if publication.Status != model.WechatPublicationStatusDrafted {
		return publication, ErrWechatPublicationConflict
	}
	now, token := s.now(), uuid.NewString()
	nextCheckAt := nextWechatManualCheck(now, now)
	won, err := s.repo.WechatPublications().ClaimPublish(ctx, publication.ID, token, now, now.Add(-wechatPublicationClaimLease), nextCheckAt)
	if err != nil {
		return publication, err
	}
	if !won {
		return publication, ErrWechatPublicationConflict
	}
	publication.SubmitAttemptedAt = &now
	response, submitErr := api.SubmitFreePublish(ctx, appwechat.FreePublishSubmitRequest{MediaID: publication.DraftMediaID})
	publication.Status = model.WechatPublicationStatusPublishSubmitting
	if submitErr != nil {
		publication.LastError = safeWechatPublicationError(submitErr)
		publication.NextCheckAt = nextWechatManualCheck(now, wechatFormalReconciliationAnchor(publication))
		if code, definitive := definitiveWechatPublicationError(submitErr); definitive {
			publication.WechatStatusCode = code
			publication.Status, publication.NextCheckAt = model.WechatPublicationStatusUnsupported, nil
			publication.SubmitAttemptedAt = nil
		} else if code, ok := publicationWechatErrCode(submitErr); ok {
			publication.WechatStatusCode = code
		}
		won, persistErr := s.repo.WechatPublications().UpdateClaimed(context.WithoutCancel(ctx), publication, token)
		if persistErr != nil {
			return publication, fmt.Errorf("persist ambiguous WeChat publish outcome: %w", persistErr)
		}
		if !won {
			return publication, ErrWechatPublicationConflict
		}
		if publication.Status == model.WechatPublicationStatusUnsupported {
			return s.repo.WechatPublications().FindByID(context.WithoutCancel(ctx), publication.ID)
		}
		if _, ok := publicationWechatErrCode(submitErr); !ok {
			s.enqueueReconcile(publication.ProjectID, publication.NextCheckAt)
			return publication, ErrWechatPublicationPending
		}
		return publication, submitErr
	}
	if response == nil || strings.TrimSpace(response.PublishID) == "" {
		if response != nil {
			publication.MsgDataID = response.MsgDataID
		}
		publication.LastError = "WeChat publish response omitted publish_id"
		publication.NextCheckAt = nextWechatManualCheck(now, wechatFormalReconciliationAnchor(publication))
		won, persistErr := s.repo.WechatPublications().UpdateClaimed(context.WithoutCancel(ctx), publication, token)
		if persistErr != nil {
			return publication, fmt.Errorf("persist ambiguous WeChat publish response: %w", persistErr)
		}
		if !won {
			return publication, ErrWechatPublicationConflict
		}
		s.enqueueReconcile(publication.ProjectID, publication.NextCheckAt)
		return publication, ErrWechatPublicationPending
	}
	publication.Status, publication.PublishID, publication.MsgDataID = model.WechatPublicationStatusPublishing, response.PublishID, response.MsgDataID
	publication.NextCheckAt, publication.LastError = nextWechatAPICheck(now, 0), ""
	won, err = s.repo.WechatPublications().UpdateClaimed(ctx, publication, token)
	if err != nil {
		return publication, err
	}
	if !won {
		return publication, ErrWechatPublicationConflict
	}
	s.enqueuePoll(publication.ID, publication.NextCheckAt)
	return s.repo.WechatPublications().FindByID(ctx, publication.ID)
}

func (s *WechatPublicationService) handleFormalPublishProviderError(ctx context.Context, publication *model.WechatPublication, cause error) (*model.WechatPublication, error) {
	code, definitive := definitiveWechatPublicationError(cause)
	if !definitive {
		return publication, cause
	}
	expectedStatus, expectedUpdatedAt := publication.Status, publication.UpdatedAt
	publication.Status = model.WechatPublicationStatusUnsupported
	publication.WechatStatusCode = code
	publication.LastError = safeWechatPublicationError(cause)
	publication.NextCheckAt = nil
	won, err := s.repo.WechatPublications().UpdateReconciliation(context.WithoutCancel(ctx), publication, expectedStatus, expectedUpdatedAt)
	if err != nil {
		return publication, err
	}
	if !won {
		return s.reloadWechatPublicationAfterCASLoss(ctx, publication.ID)
	}
	return s.repo.WechatPublications().FindByID(context.WithoutCancel(ctx), publication.ID)
}

// RetryPublish retries a formal publish that WeChat definitively rejected.
// Accepted or ambiguous submissions resume observation and are never submitted
// again.
func (s *WechatPublicationService) RetryPublish(ctx context.Context, userID, taskID string) (*model.WechatPublication, error) {
	_, project, err := s.ownedTaskProject(ctx, userID, taskID)
	if err != nil {
		return nil, err
	}
	if project.GetWechatPublishMode() == model.WechatPublishModeDisabled {
		return nil, ErrWechatPublicationModeConflict
	}
	if s.enqueuer == nil {
		return nil, ErrWechatPublicationSchedulerUnavailable
	}
	publication, err := s.repo.WechatPublications().FindByTaskID(ctx, taskID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrWechatPublicationNotFound
	}
	if err != nil {
		return nil, err
	}
	if publication.Status != model.WechatPublicationStatusUnsupported || publication.WechatStatusCode == 0 || publication.DraftMediaID == "" {
		return publication, ErrWechatPublicationConflict
	}
	now := s.now()
	targetStatus := model.WechatPublicationStatusDrafted
	nextCheckAt := &now
	if publication.PublishID != "" {
		targetStatus = model.WechatPublicationStatusPublishing
		nextCheckAt = nextWechatAPICheck(now, 0)
	} else if hasWechatSubmissionEvidence(publication) {
		targetStatus = model.WechatPublicationStatusPublishSubmitting
	}
	won, err := s.repo.WechatPublications().RetryUnsupportedPublish(ctx, publication.ID, publication.UpdatedAt, targetStatus, nextCheckAt)
	if err != nil {
		return publication, err
	}
	if !won {
		return s.reloadWechatPublicationAfterCASLoss(ctx, publication.ID)
	}
	publication, err = s.repo.WechatPublications().FindByID(ctx, publication.ID)
	if err != nil {
		return nil, err
	}
	if targetStatus == model.WechatPublicationStatusPublishing {
		s.enqueuePoll(publication.ID, publication.NextCheckAt)
		return publication, nil
	}
	if targetStatus == model.WechatPublicationStatusPublishSubmitting {
		s.enqueueReconcile(publication.ProjectID, publication.NextCheckAt)
		return publication, nil
	}
	s.enqueueReconcile(publication.ProjectID, publication.NextCheckAt)
	return s.Publish(ctx, userID, taskID)
}

func (s *WechatPublicationService) reloadWechatPublicationAfterCASLoss(ctx context.Context, publicationID string) (*model.WechatPublication, error) {
	latest, err := s.repo.WechatPublications().FindByID(ctx, publicationID)
	if err != nil {
		return nil, fmt.Errorf("reload WeChat publication after CAS loss: %w", err)
	}
	switch latest.Status {
	case model.WechatPublicationStatusPublished:
		return latest, nil
	case model.WechatPublicationStatusDrafting, model.WechatPublicationStatusPublishSubmitting,
		model.WechatPublicationStatusPublishing, model.WechatPublicationStatusNeedsSelection:
		return latest, ErrWechatPublicationPending
	default:
		return latest, ErrWechatPublicationConflict
	}
}

func (s *WechatPublicationService) Poll(ctx context.Context, publicationID string) (*model.WechatPublication, error) {
	publication, err := s.repo.WechatPublications().FindByID(ctx, publicationID)
	if err != nil {
		return nil, err
	}
	if publication.Status == model.WechatPublicationStatusPublishSubmitting && publication.PublishID == "" {
		if err := s.ReconcileProject(ctx, publication.ProjectID); err != nil {
			return publication, err
		}
		latest, err := s.repo.WechatPublications().FindByID(ctx, publication.ID)
		if err != nil {
			return publication, err
		}
		if latest.Status == model.WechatPublicationStatusPublished {
			return latest, nil
		}
		return latest, ErrWechatPublicationPending
	}
	if publication.Status != model.WechatPublicationStatusPublishing || publication.PublishID == "" {
		return publication, ErrWechatPublicationConflict
	}
	expectedStatus, expectedUpdatedAt := publication.Status, publication.UpdatedAt
	project, err := s.repo.Projects().FindByID(ctx, publication.ProjectID)
	if err != nil {
		return publication, err
	}
	api, err := s.projectAPI(project)
	if err != nil {
		return publication, err
	}
	response, getErr := api.GetFreePublish(ctx, appwechat.FreePublishGetRequest{PublishID: publication.PublishID})
	now := s.now()
	publication.LastCheckedAt = &now
	publication.CheckAttempts++
	if getErr != nil {
		publication.LastError = safeWechatPublicationError(getErr)
		s.scheduleNextPollOrReconcile(publication, now)
		if code, ok := publicationWechatErrCode(getErr); ok && code == 48001 {
			publication.Status, publication.WechatStatusCode, publication.NextCheckAt = model.WechatPublicationStatusUnsupported, code, nil
		}
		won, persistErr := s.repo.WechatPublications().UpdateReconciliation(context.WithoutCancel(ctx), publication, expectedStatus, expectedUpdatedAt)
		if persistErr != nil {
			return publication, fmt.Errorf("persist WeChat publish poll failure: %w", persistErr)
		}
		if !won {
			return s.reloadWechatPublicationAfterCASLoss(context.WithoutCancel(ctx), publication.ID)
		}
		s.enqueuePublicationNext(publication)
		return publication, errors.Join(ErrWechatPublicationPending, getErr)
	}
	if response == nil {
		emptyErr := errors.New("empty WeChat publish status response")
		publication.LastError = emptyErr.Error()
		s.scheduleNextPollOrReconcile(publication, now)
		won, err := s.repo.WechatPublications().UpdateReconciliation(context.WithoutCancel(ctx), publication, expectedStatus, expectedUpdatedAt)
		if err != nil {
			return publication, fmt.Errorf("persist empty WeChat publish status response: %w", err)
		}
		if !won {
			return s.reloadWechatPublicationAfterCASLoss(context.WithoutCancel(ctx), publication.ID)
		}
		s.enqueuePublicationNext(publication)
		return publication, errors.Join(ErrWechatPublicationPending, emptyErr)
	}
	publication.WechatStatusCode = response.PublishStatus
	var pendingErr error
	switch response.PublishStatus {
	case appwechat.FreePublishStatusSucceeded:
		if response.ArticleID == "" || len(response.ArticleDetail.Items) == 0 || strings.TrimSpace(response.ArticleDetail.Items[0].ArticleURL) == "" {
			publication.LastError = "published response omitted article metadata"
			s.scheduleNextPollOrReconcile(publication, now)
			pendingErr = errors.New(publication.LastError)
			break
		}
		articles, listErr := listAllWechatPublishes(ctx, api)
		if listErr != nil {
			publication.LastError = "resolve authoritative WeChat publication timestamp: " + safeWechatPublicationError(listErr)
			s.scheduleNextPollOrReconcile(publication, now)
			pendingErr = listErr
			break
		}
		var authoritative *publishedWechatArticle
		for i := range articles {
			if articles[i].ArticleID == response.ArticleID {
				authoritative = &articles[i]
				break
			}
		}
		if authoritative == nil {
			publication.LastError = "published article is not yet visible in WeChat batch results"
			s.scheduleNextPollOrReconcile(publication, now)
			pendingErr = errors.New(publication.LastError)
			break
		}
		return s.bindPublished(ctx, publication, *authoritative, model.WechatPublicationSourceAnbanAPI)
	case appwechat.FreePublishStatusPublishing:
		publication.Status, publication.LastError = model.WechatPublicationStatusPublishing, ""
		s.scheduleNextPollOrReconcile(publication, now)
	case appwechat.FreePublishStatusOriginalFailed, appwechat.FreePublishStatusFailed, appwechat.FreePublishStatusAuditRejected,
		appwechat.FreePublishStatusUserDeleted, appwechat.FreePublishStatusSystemBanned:
		publication.Status, publication.NextCheckAt = model.WechatPublicationStatusPublishFailed, nil
		publication.LastError = wechatPublishStatusMessage(response.PublishStatus)
	default:
		publication.Status, publication.NextCheckAt, publication.LastError = model.WechatPublicationStatusPublishFailed, nil, fmt.Sprintf("unknown WeChat publish status %d", response.PublishStatus)
	}
	won, err := s.repo.WechatPublications().UpdateReconciliation(ctx, publication, expectedStatus, expectedUpdatedAt)
	if err != nil {
		return nil, err
	}
	if !won {
		return s.reloadWechatPublicationAfterCASLoss(ctx, publication.ID)
	}
	s.enqueuePublicationNext(publication)
	if pendingErr != nil {
		return publication, errors.Join(ErrWechatPublicationPending, pendingErr)
	}
	return publication, nil
}

func (s *WechatPublicationService) failExpiredFormalReconciliation(ctx context.Context, publication *model.WechatPublication, lastError string) (*model.WechatPublication, error) {
	expectedStatus, expectedUpdatedAt := publication.Status, publication.UpdatedAt
	publication.Status = model.WechatPublicationStatusPublishFailed
	publication.NextCheckAt = nil
	publication.LastError = lastError
	won, err := s.repo.WechatPublications().UpdateReconciliation(ctx, publication, expectedStatus, expectedUpdatedAt)
	if err != nil {
		return publication, err
	}
	if !won {
		return s.reloadWechatPublicationAfterCASLoss(ctx, publication.ID)
	}
	return publication, nil
}

func (s *WechatPublicationService) scheduleNextPollOrReconcile(publication *model.WechatPublication, now time.Time) {
	publication.NextCheckAt = nextWechatAPICheck(now, publication.CheckAttempts)
	if publication.NextCheckAt != nil {
		return
	}
	publication.Status = model.WechatPublicationStatusPublishSubmitting
	publication.LastError = "WeChat publish status polling timed out; reconciling the published list"
	publication.NextCheckAt = nextWechatManualCheck(now, wechatFormalReconciliationAnchor(publication))
}

func (s *WechatPublicationService) enqueuePublicationNext(publication *model.WechatPublication) {
	if publication == nil || publication.NextCheckAt == nil {
		return
	}
	if publication.Status == model.WechatPublicationStatusPublishSubmitting {
		s.enqueueReconcile(publication.ProjectID, publication.NextCheckAt)
		return
	}
	s.enqueuePoll(publication.ID, publication.NextCheckAt)
}

// ProcessPoll is the queue-facing form of Poll. Provider failures that were
// durably scheduled are complete queue jobs; retrying them in Asynq would fan
// out duplicate delayed polls.
func (s *WechatPublicationService) ProcessPoll(ctx context.Context, publicationID string) error {
	_, err := s.Poll(ctx, publicationID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if errors.Is(err, ErrWechatPublicationPending) || errors.Is(err, ErrWechatPublicationConflict) {
		return nil
	}
	return err
}

func nextWechatAPICheck(now time.Time, attempts int) *time.Time {
	delays := []time.Duration{5 * time.Second, 15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute, 5 * time.Minute}
	if attempts < len(delays) {
		next := now.Add(delays[attempts])
		return &next
	}
	// The six short checks consume 8m50s. Eleven 10-minute checks stay within
	// the two-hour window; attempt 17 would schedule beyond it.
	if attempts < 17 {
		next := now.Add(10 * time.Minute)
		return &next
	}
	return nil
}

func nextWechatManualCheck(now, draftCreatedAt time.Time) *time.Time {
	age := now.Sub(draftCreatedAt)
	var delay time.Duration
	switch {
	case age < 2*time.Hour:
		delay = 10 * time.Minute
	case age < 24*time.Hour:
		delay = time.Hour
	case age < 72*time.Hour:
		delay = 6 * time.Hour
	default:
		return nil
	}
	next := now.Add(delay)
	cutoff := draftCreatedAt.Add(72 * time.Hour)
	if next.After(cutoff) {
		next = cutoff
	}
	return &next
}

func valueOrTime(value *time.Time, fallback time.Time) time.Time {
	if value != nil {
		return *value
	}
	return fallback
}

func wechatFormalReconciliationAnchor(publication *model.WechatPublication) time.Time {
	if publication != nil && publication.SubmitAttemptedAt != nil {
		return *publication.SubmitAttemptedAt
	}
	if publication == nil {
		return time.Time{}
	}
	return valueOrTime(publication.DraftCreatedAt, publication.CreatedAt)
}

func wechatPublishStatusMessage(code int) string {
	switch code {
	case 2:
		return "originality check failed"
	case 3:
		return "WeChat publication failed"
	case 4:
		return "WeChat publication audit rejected"
	case 5:
		return "published article was deleted by user"
	case 6:
		return "published article was banned by WeChat"
	default:
		return "WeChat publication failed"
	}
}

type WechatPublicationCandidate struct {
	ArticleID    string    `json:"article_id"`
	Title        string    `json:"title"`
	Digest       string    `json:"digest,omitempty"`
	ThumbMediaID string    `json:"thumb_media_id,omitempty"`
	PublishedAt  time.Time `json:"published_at"`
	Match        string    `json:"match"`
}

type publishedWechatArticle struct {
	ArticleID    string
	URL          string
	Title        string
	Digest       string
	ThumbMediaID string
	Content      string
	Index        int
	PublishedAt  time.Time
}

func publishedArticles(response *appwechat.FreePublishBatchGetResponse) []publishedWechatArticle {
	if response == nil {
		return nil
	}
	var result []publishedWechatArticle
	for _, item := range response.Items {
		if item.UpdateTime <= 0 {
			continue
		}
		publishedAt := time.Unix(item.UpdateTime, 0)
		if len(item.Content.NewsItems) == 0 {
			continue
		}
		article := item.Content.NewsItems[0]
		if strings.TrimSpace(item.ArticleID) == "" || strings.TrimSpace(article.URL) == "" {
			continue
		}
		result = append(result, publishedWechatArticle{ArticleID: item.ArticleID, URL: article.URL, Title: strings.TrimSpace(article.Title), Digest: strings.TrimSpace(article.Digest), ThumbMediaID: strings.TrimSpace(article.ThumbMediaID), Content: article.Content, Index: 1, PublishedAt: publishedAt})
	}
	return result
}

func (s *WechatPublicationService) Reconcile(ctx context.Context, userID, taskID string) error {
	_, project, err := s.ownedTaskProject(ctx, userID, taskID)
	if err != nil {
		return err
	}
	publication, err := s.repo.WechatPublications().FindByTaskID(ctx, taskID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrWechatPublicationNotFound
	}
	if err != nil {
		return err
	}
	if project.GetWechatPublishMode() == model.WechatPublishModeDisabled && !hasWechatSubmissionEvidence(publication) {
		return ErrWechatPublicationModeConflict
	}
	if !isWechatPublicationReconcilePending(publication.Status) {
		return ErrWechatPublicationConflict
	}
	claimed, err := s.repo.WechatPublications().ClaimProjectReconcile(ctx, project.ID, wechatReconcileRateLimit)
	if err != nil {
		return err
	}
	if !claimed {
		return ErrWechatPublicationRateLimited
	}
	return s.reconcileProject(ctx, project)
}

// ReconcileProject performs one provider batch call for every pending task in
// the project. Scheduler wiring is intentionally owned by the integration task.
func (s *WechatPublicationService) ReconcileProject(ctx context.Context, projectID string) error {
	project, err := s.repo.Projects().FindByID(ctx, projectID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	claimed, err := s.repo.WechatPublications().ClaimProjectReconcile(ctx, project.ID, wechatReconcileRateLimit)
	if err != nil || !claimed {
		return err
	}
	return s.reconcileProject(ctx, project)
}

func isWechatPublicationReconcilePending(status string) bool {
	switch status {
	case model.WechatPublicationStatusDrafting,
		model.WechatPublicationStatusDrafted,
		model.WechatPublicationStatusPublishSubmitting,
		model.WechatPublicationStatusNeedsSelection:
		return true
	default:
		return false
	}
}

func hasWechatSubmissionEvidence(publication *model.WechatPublication) bool {
	return publication != nil && (publication.SubmitAttemptedAt != nil || publication.PublishID != "" || publication.MsgDataID != "" || publication.MsgID != "")
}

func (s *WechatPublicationService) reconcileProject(ctx context.Context, project *model.Project) error {
	pending, err := s.repo.WechatPublications().FindPendingByProject(ctx, project.ID)
	if err != nil || len(pending) == 0 {
		return err
	}
	if project.GetWechatPublishMode() == model.WechatPublishModeDisabled {
		evidenced := make([]*model.WechatPublication, 0, len(pending))
		for _, publication := range pending {
			if hasWechatSubmissionEvidence(publication) {
				evidenced = append(evidenced, publication)
				continue
			}
			if publication.NextCheckAt == nil {
				continue
			}
			expectedStatus, expectedUpdatedAt := publication.Status, publication.UpdatedAt
			publication.NextCheckAt = nil
			won, updateErr := s.repo.WechatPublications().UpdateReconciliation(ctx, publication, expectedStatus, expectedUpdatedAt)
			if updateErr != nil {
				return updateErr
			}
			if !won {
				if _, outcomeErr := s.reloadWechatPublicationAfterCASLoss(ctx, publication.ID); outcomeErr != nil &&
					!errors.Is(outcomeErr, ErrWechatPublicationPending) && !errors.Is(outcomeErr, ErrWechatPublicationConflict) {
					return outcomeErr
				}
			}
		}
		pending = evidenced
		if len(pending) == 0 {
			return nil
		}
	}
	api, err := s.projectAPI(project)
	if err != nil {
		return err
	}
	hasDrafting := false
	for _, publication := range pending {
		if publication.Status == model.WechatPublicationStatusDrafting && publication.DraftMediaID == "" {
			hasDrafting = true
			break
		}
	}
	if hasDrafting {
		drafts, listErr := listAllWechatDrafts(ctx, api)
		if listErr != nil {
			if persistErr := s.persistDraftReconciliationFailure(ctx, pending, listErr); persistErr != nil {
				return persistErr
			}
			if code, ok := publicationWechatErrCode(listErr); ok && code == 48001 {
				return nil
			}
			return listErr
		}
		now := s.now()
		for _, publication := range pending {
			if publication.Status != model.WechatPublicationStatusDrafting || publication.DraftMediaID != "" {
				continue
			}
			expectedStatus, expectedUpdatedAt := publication.Status, publication.UpdatedAt
			publication.LastCheckedAt = &now
			if matched, ok := exactWechatDraftMatch(publication, drafts); ok {
				nextCheckAt := nextWechatManualCheck(now, valueOrTime(publication.DraftCreatedAt, publication.CreatedAt))
				won, updateErr := s.repo.WechatPublications().TransitionDraftRecovered(
					ctx, publication.ID, expectedUpdatedAt, matched.MediaID, nextCheckAt, &now,
				)
				if updateErr != nil {
					return updateErr
				}
				if !won {
					if _, outcomeErr := s.reloadWechatPublicationAfterCASLoss(ctx, publication.ID); outcomeErr != nil &&
						!errors.Is(outcomeErr, ErrWechatPublicationPending) && !errors.Is(outcomeErr, ErrWechatPublicationConflict) {
						return outcomeErr
					}
				}
				continue
			} else if next := nextWechatManualCheck(now, valueOrTime(publication.DraftCreatedAt, publication.CreatedAt)); next != nil {
				publication.NextCheckAt = next
				publication.LastError = "awaiting exact WeChat draft reconciliation after ambiguous draft/add outcome"
			} else {
				publication.Status, publication.NextCheckAt = model.WechatPublicationStatusPublishFailed, nil
				publication.LastError = "WeChat draft reconciliation timed out after 72 hours; draft/add outcome remains ambiguous"
			}
			won, updateErr := s.repo.WechatPublications().UpdateReconciliation(ctx, publication, expectedStatus, expectedUpdatedAt)
			if updateErr != nil {
				return updateErr
			}
			if !won {
				if _, outcomeErr := s.reloadWechatPublicationAfterCASLoss(ctx, publication.ID); outcomeErr != nil &&
					!errors.Is(outcomeErr, ErrWechatPublicationPending) && !errors.Is(outcomeErr, ErrWechatPublicationConflict) {
					return outcomeErr
				}
			}
		}
		pending, err = s.repo.WechatPublications().FindPendingByProject(ctx, project.ID)
		if err != nil || len(pending) == 0 {
			return err
		}
	}
	articles, err := listAllWechatPublishes(ctx, api)
	if err != nil {
		if code, definitive := definitiveWechatPublicationError(err); definitive {
			for _, publication := range pending {
				expectedStatus, expectedUpdatedAt := publication.Status, publication.UpdatedAt
				publication.Status, publication.WechatStatusCode, publication.NextCheckAt, publication.LastError = model.WechatPublicationStatusUnsupported, code, nil, safeWechatPublicationError(err)
				won, persistErr := s.repo.WechatPublications().UpdateReconciliation(context.WithoutCancel(ctx), publication, expectedStatus, expectedUpdatedAt)
				if persistErr != nil {
					return fmt.Errorf("persist unsupported WeChat reconciliation state: %w", persistErr)
				}
				if !won {
					if _, outcomeErr := s.reloadWechatPublicationAfterCASLoss(context.WithoutCancel(ctx), publication.ID); outcomeErr != nil &&
						!errors.Is(outcomeErr, ErrWechatPublicationPending) && !errors.Is(outcomeErr, ErrWechatPublicationConflict) {
						return outcomeErr
					}
				}
			}
			return nil
		}
		if persistErr := s.persistFormalReconciliationFailure(ctx, project, pending, err); persistErr != nil {
			return persistErr
		}
		return nil
	}
	now := s.now()
	for _, publication := range pending {
		expectedStatus, expectedUpdatedAt := publication.Status, publication.UpdatedAt
		publication.LastCheckedAt = &now
		matches, candidates, matchErr := s.matchPublished(ctx, publication, articles)
		if matchErr != nil {
			return matchErr
		}
		if len(matches) == 1 {
			if _, bindErr := s.bindPublished(ctx, publication, matches[0], reconciledWechatPublicationSource(publication)); bindErr != nil {
				if errors.Is(bindErr, ErrWechatPublicationPending) {
					continue
				}
				return bindErr
			}
			continue
		}
		publication.NextCheckAt = nextWechatManualCheck(now, wechatFormalReconciliationAnchor(publication))
		if len(matches) > 1 {
			candidates = append(candidates, candidateViews(matches, "exact")...)
		}
		if len(candidates) > 0 {
			publication.Status = model.WechatPublicationStatusNeedsSelection
			encoded, marshalErr := json.Marshal(dedupeWechatCandidates(candidates))
			if marshalErr != nil {
				return marshalErr
			}
			publication.Candidates = datatypes.JSON(encoded)
		} else if publication.Status == model.WechatPublicationStatusNeedsSelection {
			publication.Candidates = nil
			if hasWechatSubmissionEvidence(publication) {
				publication.Status = model.WechatPublicationStatusPublishSubmitting
			} else {
				publication.Status = model.WechatPublicationStatusDrafted
			}
		}
		if publication.NextCheckAt == nil && publication.Status == model.WechatPublicationStatusPublishSubmitting {
			publication.Status = model.WechatPublicationStatusPublishFailed
			publication.LastError = "微信正式发布提交后连续 72 小时未能确认结果"
		}
		won, err := s.repo.WechatPublications().UpdateReconciliation(ctx, publication, expectedStatus, expectedUpdatedAt)
		if err != nil {
			return err
		}
		if !won {
			if _, outcomeErr := s.reloadWechatPublicationAfterCASLoss(ctx, publication.ID); outcomeErr != nil &&
				!errors.Is(outcomeErr, ErrWechatPublicationPending) && !errors.Is(outcomeErr, ErrWechatPublicationConflict) {
				return outcomeErr
			}
			continue
		}
		if project.GetWechatPublishMode() == model.WechatPublishModeAPIConfirmed &&
			publication.Status == model.WechatPublicationStatusDrafted && !hasWechatSubmissionEvidence(publication) {
			automatic, automaticErr := s.autoPublishDraft(ctx, publication.UserID, publication.TaskID, project, publication)
			if automaticErr != nil {
				return automaticErr
			}
			*publication = *automatic
		}
	}
	var nextCheckAt *time.Time
	for _, publication := range pending {
		if publication.NextCheckAt != nil && (nextCheckAt == nil || publication.NextCheckAt.Before(*nextCheckAt)) {
			next := *publication.NextCheckAt
			nextCheckAt = &next
		}
	}
	s.enqueueReconcile(project.ID, nextCheckAt)
	return nil
}

func (s *WechatPublicationService) persistDraftReconciliationFailure(ctx context.Context, pending []*model.WechatPublication, cause error) error {
	now := s.now()
	for _, publication := range pending {
		if publication.Status != model.WechatPublicationStatusDrafting || publication.DraftMediaID != "" {
			continue
		}
		expectedStatus, expectedUpdatedAt := publication.Status, publication.UpdatedAt
		publication.LastCheckedAt = &now
		if code, ok := publicationWechatErrCode(cause); ok && code == 48001 {
			publication.Status, publication.WechatStatusCode, publication.NextCheckAt = model.WechatPublicationStatusUnsupported, code, nil
			publication.LastError = safeWechatPublicationError(cause)
		} else if next := nextWechatManualCheck(now, valueOrTime(publication.DraftCreatedAt, publication.CreatedAt)); next != nil {
			publication.NextCheckAt = next
			publication.LastError = "WeChat draft reconciliation failed: " + safeWechatPublicationError(cause)
		} else {
			publication.Status, publication.NextCheckAt = model.WechatPublicationStatusPublishFailed, nil
			publication.LastError = "WeChat draft reconciliation timed out after 72 hours: " + safeWechatPublicationError(cause)
		}
		won, err := s.repo.WechatPublications().UpdateReconciliation(context.WithoutCancel(ctx), publication, expectedStatus, expectedUpdatedAt)
		if err != nil {
			return fmt.Errorf("persist WeChat draft reconciliation failure: %w", err)
		}
		if !won {
			if _, outcomeErr := s.reloadWechatPublicationAfterCASLoss(context.WithoutCancel(ctx), publication.ID); outcomeErr != nil &&
				!errors.Is(outcomeErr, ErrWechatPublicationPending) && !errors.Is(outcomeErr, ErrWechatPublicationConflict) {
				return outcomeErr
			}
		}
	}
	return nil
}

func (s *WechatPublicationService) persistFormalReconciliationFailure(ctx context.Context, project *model.Project, pending []*model.WechatPublication, cause error) error {
	now := s.now()
	var earliestNextCheck *time.Time
	for _, publication := range pending {
		expectedStatus, expectedUpdatedAt := publication.Status, publication.UpdatedAt
		publication.LastCheckedAt = &now
		publication.CheckAttempts++
		if next := nextWechatManualCheck(now, wechatFormalReconciliationAnchor(publication)); next != nil {
			publication.NextCheckAt = next
			publication.LastError = "微信正式发布状态确认失败：" + safeWechatPublicationError(cause)
			if earliestNextCheck == nil || next.Before(*earliestNextCheck) {
				candidate := *next
				earliestNextCheck = &candidate
			}
		} else {
			publication.NextCheckAt = nil
			if publication.Status != model.WechatPublicationStatusDrafted || hasWechatSubmissionEvidence(publication) || project.GetWechatPublishMode() == model.WechatPublishModeAPIConfirmed {
				publication.Status = model.WechatPublicationStatusPublishFailed
				publication.LastError = "微信正式发布状态连续 72 小时无法确认：" + safeWechatPublicationError(cause)
			} else {
				publication.LastError = "微信后台发布状态检查已停止，可继续手动发布：" + safeWechatPublicationError(cause)
			}
		}
		won, err := s.repo.WechatPublications().UpdateReconciliation(context.WithoutCancel(ctx), publication, expectedStatus, expectedUpdatedAt)
		if err != nil {
			return fmt.Errorf("persist WeChat formal publication reconciliation failure: %w", err)
		}
		if !won {
			if _, outcomeErr := s.reloadWechatPublicationAfterCASLoss(context.WithoutCancel(ctx), publication.ID); outcomeErr != nil &&
				!errors.Is(outcomeErr, ErrWechatPublicationPending) && !errors.Is(outcomeErr, ErrWechatPublicationConflict) {
				return outcomeErr
			}
		}
	}
	s.enqueueReconcile(pending[0].ProjectID, earliestNextCheck)
	return nil
}

// RecoverDue re-enqueues publication work from the database when a delayed
// Redis task was lost. Moving next_check_at forward is a dispatch lease: the
// worker's normal state transition replaces it with the authoritative cadence.
func (s *WechatPublicationService) RecoverDue(ctx context.Context, limit int) error {
	if s.enqueuer == nil {
		return nil
	}
	now := s.now()
	publications, err := s.repo.WechatPublications().FindDue(ctx, now, limit)
	if err != nil {
		return err
	}
	reconciles := make(map[string]bool)
	for _, publication := range publications {
		isPoll := publication.Status == model.WechatPublicationStatusPublishing && publication.PublishID != ""
		if !isPoll && reconciles[publication.ProjectID] {
			continue
		}
		leaseUntil := now.Add(wechatRecoveryDispatchLease)
		claimed, err := s.repo.WechatPublications().ClaimDueDispatch(ctx, publication.ID, publication.UpdatedAt, now, leaseUntil)
		if err != nil {
			return err
		}
		if !claimed {
			continue
		}
		payload := map[string]string{"project_id": publication.ProjectID}
		taskType := WechatPublicationReconcileTaskType
		if isPoll {
			payload, taskType = map[string]string{"publication_id": publication.ID}, WechatPublicationPollTaskType
		} else {
			reconciles[publication.ProjectID] = true
		}
		encoded, _ := json.Marshal(payload)
		if err := s.enqueuer.Enqueue(taskType, encoded); err != nil {
			if _, releaseErr := s.repo.WechatPublications().ReleaseDueDispatch(ctx, publication.ID, leaseUntil, now); releaseErr != nil {
				return fmt.Errorf("enqueue WeChat publication recovery: %v; release recovery lease: %w", err, releaseErr)
			}
			return err
		}
	}
	return nil
}

func (s *WechatPublicationService) matchPublished(ctx context.Context, publication *model.WechatPublication, articles []publishedWechatArticle) ([]publishedWechatArticle, []WechatPublicationCandidate, error) {
	var fingerprint, metadata []publishedWechatArticle
	var weak []WechatPublicationCandidate
	for _, article := range articles {
		if article.ArticleID == "" || article.PublishedAt.Before(valueOrTime(publication.DraftCreatedAt, publication.CreatedAt)) {
			continue
		}
		if bound, err := s.repo.WechatPublications().FindByArticleID(ctx, publication.ProjectID, article.ArticleID); err == nil {
			if bound.ID != publication.ID {
				continue
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, err
		}
		if WechatContentFingerprint(article.Content) == publication.DraftContentFingerprint {
			fingerprint = append(fingerprint, article)
			continue
		}
		if article.Title == publication.DraftTitle && article.Digest == publication.DraftDigest && article.ThumbMediaID == publication.DraftThumbMediaID {
			metadata = append(metadata, article)
			continue
		}
		if article.Title == publication.DraftTitle || (publication.DraftDigest != "" && article.Digest == publication.DraftDigest) || (publication.DraftThumbMediaID != "" && article.ThumbMediaID == publication.DraftThumbMediaID) {
			weak = append(weak, candidateView(article, "weak"))
		}
	}
	exact := dedupePublishedWechatArticles(append(fingerprint, metadata...))
	return exact, weak, nil
}

func reconciledWechatPublicationSource(publication *model.WechatPublication) string {
	if publication.Source == model.WechatPublicationSourceAnbanAPI &&
		(publication.SubmitAttemptedAt != nil || publication.PublishID != "" || publication.MsgDataID != "") {
		return model.WechatPublicationSourceAnbanAPI
	}
	return model.WechatPublicationSourceWechatConsole
}

func dedupePublishedWechatArticles(articles []publishedWechatArticle) []publishedWechatArticle {
	seen := make(map[string]bool, len(articles))
	result := make([]publishedWechatArticle, 0, len(articles))
	for _, article := range articles {
		key := fmt.Sprintf("%s:%d", article.ArticleID, article.Index)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, article)
	}
	return result
}

func candidateView(article publishedWechatArticle, match string) WechatPublicationCandidate {
	return WechatPublicationCandidate{ArticleID: article.ArticleID, Title: article.Title, Digest: article.Digest, ThumbMediaID: article.ThumbMediaID, PublishedAt: article.PublishedAt, Match: match}
}
func candidateViews(articles []publishedWechatArticle, match string) []WechatPublicationCandidate {
	result := make([]WechatPublicationCandidate, 0, len(articles))
	for _, article := range articles {
		result = append(result, candidateView(article, match))
	}
	return result
}
func dedupeWechatCandidates(candidates []WechatPublicationCandidate) []WechatPublicationCandidate {
	seen := map[string]bool{}
	result := make([]WechatPublicationCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		key := fmt.Sprintf("%s:%d", candidate.ArticleID, candidate.PublishedAt.Unix())
		if !seen[key] {
			seen[key] = true
			result = append(result, candidate)
		}
	}
	return result
}

func (s *WechatPublicationService) Select(ctx context.Context, userID, taskID, articleID string) (*model.WechatPublication, error) {
	_, project, err := s.ownedTaskProject(ctx, userID, taskID)
	if err != nil {
		return nil, err
	}
	publication, err := s.repo.WechatPublications().FindByTaskID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if publication.Status != model.WechatPublicationStatusNeedsSelection {
		return publication, ErrWechatPublicationConflict
	}
	articleID = strings.TrimSpace(articleID)
	if articleID == "" {
		return publication, ErrWechatPublicationArticleNotFound
	}
	var storedCandidates []WechatPublicationCandidate
	if err := json.Unmarshal(publication.Candidates, &storedCandidates); err != nil {
		return publication, fmt.Errorf("decode stored WeChat publication candidates: %w", err)
	}
	isCandidate := false
	for _, candidate := range storedCandidates {
		if candidate.ArticleID == articleID {
			isCandidate = true
			break
		}
	}
	if !isCandidate {
		return publication, ErrWechatPublicationArticleNotFound
	}
	api, err := s.projectAPI(project)
	if err != nil {
		return publication, err
	}
	articles, err := listAllWechatPublishes(ctx, api)
	if err != nil {
		return publication, err
	}
	var selected *publishedWechatArticle
	for _, article := range articles {
		if article.ArticleID == articleID && !article.PublishedAt.Before(valueOrTime(publication.DraftCreatedAt, publication.CreatedAt)) {
			copy := article
			selected = &copy
			break
		}
	}
	if selected == nil {
		return publication, ErrWechatPublicationArticleNotFound
	}
	if bound, err := s.repo.WechatPublications().FindByArticleID(ctx, publication.ProjectID, articleID); err == nil {
		if bound.ID != publication.ID {
			return publication, ErrWechatPublicationConflict
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return publication, err
	}
	return s.bindPublished(ctx, publication, *selected, reconciledWechatPublicationSource(publication))
}

func (s *WechatPublicationService) bindPublished(ctx context.Context, publication *model.WechatPublication, article publishedWechatArticle, source string) (*model.WechatPublication, error) {
	expectedStatus, expectedUpdatedAt := publication.Status, publication.UpdatedAt
	if article.Index <= 0 {
		article.Index = 1
	}
	publication.Status, publication.Source = model.WechatPublicationStatusPublished, source
	publication.ArticleID, publication.ArticleURL, publication.ArticleIndex = article.ArticleID, article.URL, 1
	publication.PublishedAt, publication.NextCheckAt, publication.LastError, publication.Candidates = &article.PublishedAt, nil, "", nil
	if publication.MsgDataID != "" {
		publication.MsgID = publication.MsgDataID + "_1"
	}
	trackingID := ""
	err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		claimed, err := tx.WechatPublications().ClaimArticleBinding(ctx, publication.ProjectID, article.ArticleID, publication.ID)
		if err != nil {
			return err
		}
		if !claimed {
			if bound, findErr := tx.WechatPublications().FindByArticleID(ctx, publication.ProjectID, article.ArticleID); findErr == nil && bound.ID == publication.ID {
				// Idempotent retry of the same binding.
			} else if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
				return findErr
			} else if findErr != nil {
				latest, reloadErr := tx.WechatPublications().FindByID(ctx, publication.ID)
				if reloadErr != nil {
					return reloadErr
				}
				if latest.Status == model.WechatPublicationStatusPublished {
					return errWechatPublicationVersionChanged
				}
				return ErrWechatPublicationConflict
			} else {
				return ErrWechatPublicationConflict
			}
		}
		won, err := tx.WechatPublications().TransitionToPublished(ctx, publication, expectedStatus, expectedUpdatedAt)
		if err != nil {
			return err
		}
		if !won {
			return errWechatPublicationVersionChanged
		}
		if tracking, err := tx.WechatTrackings().FindByTaskID(ctx, publication.TaskID); err == nil {
			trackingID = tracking.ID
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		now := s.now()
		next := now
		expiresAt := article.PublishedAt.Add(model.WechatTrackingWindow)
		trackingID = uuid.NewString()
		return tx.WechatTrackings().Create(ctx, &model.WechatArticleTracking{
			ID: trackingID, TaskID: publication.TaskID, UserID: publication.UserID, ProjectID: publication.ProjectID,
			PublicationID: publication.ID, Source: publication.Source, Status: model.WechatTrackingStatusWaitingData,
			ArticleID: article.ArticleID, MsgDataID: publication.MsgDataID, MsgID: publication.MsgID, ArticleURL: article.URL,
			PublishedAt: article.PublishedAt, ExpiresAt: expiresAt, NextFetchAt: &next,
		})
	})
	if errors.Is(err, errWechatPublicationVersionChanged) {
		return s.reloadWechatPublicationAfterCASLoss(ctx, publication.ID)
	}
	if err != nil {
		return publication, err
	}
	if s.enqueuer != nil && trackingID != "" {
		payload, _ := json.Marshal(map[string]string{"tracking_id": trackingID})
		if err := s.enqueuer.Enqueue(WechatCaptureMetricsTaskType, payload); err != nil && s.logger != nil {
			s.logger.Warn().Err(err).Str("tracking_id", trackingID).Msg("enqueue initial WeChat analytics capture failed; database recovery will retry when due")
		}
	}
	return publication, nil
}

func coalesceString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func publicationWechatErrCode(err error) (int, bool) {
	var apiErr *appwechat.WechatAPIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrCode, true
	}
	if parsed := appwechat.ParseWechatError(err); parsed != nil {
		return parsed.ErrCode, true
	}
	return 0, false
}

func definitiveWechatPublicationError(err error) (int, bool) {
	var apiErr *appwechat.WechatAPIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrCode, !apiErr.Retryable
	}
	if parsed := appwechat.ParseWechatError(err); parsed != nil {
		return parsed.ErrCode, !parsed.Retryable
	}
	return 0, false
}

func safeWechatPublicationError(err error) string {
	code, ok := publicationWechatErrCode(err)
	if !ok {
		return "微信接口暂时不可用，请稍后重试"
	}
	switch code {
	case 40001, 40125:
		return fmt.Sprintf("微信凭证无效，请检查 AppID 和 AppSecret（错误码 %d）", code)
	case 40164:
		return fmt.Sprintf("服务器 IP 不在微信公众号白名单中（错误码 %d）", code)
	case 42001:
		return fmt.Sprintf("微信 access_token 已过期，请稍后重试（错误码 %d）", code)
	case 45009:
		return fmt.Sprintf("微信接口调用频率已超限，请稍后重试（错误码 %d）", code)
	case 48001:
		return "当前公众号没有所需接口权限（错误码 48001）"
	case -1:
		return "微信系统繁忙，请稍后重试（错误码 -1）"
	default:
		return fmt.Sprintf("微信 API 请求失败（错误码 %d）", code)
	}
}
