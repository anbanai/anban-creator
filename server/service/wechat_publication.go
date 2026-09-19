package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	appwechat "github.com/anbanai/anban-creator/server/app/wechat"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"golang.org/x/text/unicode/norm"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

var (
	ErrWechatPublicationNotFound             = errors.New("WeChat publication not found")
	ErrWechatPublicationForbidden            = errors.New("WeChat publication does not belong to user")
	ErrWechatPublicationProjectMismatch      = errors.New("WeChat publication task and project do not match")
	ErrWechatPublicationExecutionMismatch    = errors.New("WeChat publication does not belong to the current task execution")
	ErrWechatPublicationConflict             = errors.New("WeChat publication state changed concurrently")
	ErrWechatPublicationModeConflict         = errors.New("WeChat publication action is not allowed by project mode")
	ErrWechatPublicationPending              = errors.New("WeChat publication outcome is pending reconciliation")
	ErrWechatPublicationRateLimited          = errors.New("WeChat publication reconciliation is rate limited")
	ErrWechatPublicationArticleNotFound      = errors.New("published article was not found in the configured WeChat account")
	ErrWechatPublicationInvalidPayload       = errors.New("invalid WeChat draft payload")
	ErrWechatPublicationMarketingBlocked     = errors.New("WeChat draft blocked by deterministic marketing review")
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
	repo          repository.Repository
	apiFactory    WechatPublicationAPIFactory
	logger        *zerolog.Logger
	enqueuer      TaskEnqueuer
	now           func() time.Time
	recovery      func(context.Context, string, string) (model.TaskPublicationOutcome, error)
	lifecycleSync func(context.Context, string) (*model.TaskLifecycle, error)
}

// SetEnqueuer wires durable polling into the server's async worker. The
// publication row remains the source of truth, so enqueue failures never lose
// the pending next_check_at timestamp and can be recovered on the next scan.
func (s *WechatPublicationService) SetEnqueuer(enqueuer TaskEnqueuer) { s.enqueuer = enqueuer }

func (s *WechatPublicationService) SetRecovery(recovery func(context.Context, string, string) (model.TaskPublicationOutcome, error)) {
	s.recovery = recovery
}

func (s *WechatPublicationService) SetLifecycleSync(sync func(context.Context, string) (*model.TaskLifecycle, error)) {
	s.lifecycleSync = sync
}

func (s *WechatPublicationService) syncTaskLifecycle(ctx context.Context, taskID string) {
	if s.lifecycleSync == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	if _, err := s.lifecycleSync(context.WithoutCancel(ctx), taskID); err != nil && s.logger != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Msg("sync WeChat task lifecycle")
	}
}

func (s *WechatPublicationService) Recover(ctx context.Context, userID, taskID string) (model.TaskPublicationOutcome, error) {
	if _, _, err := s.ownedTaskProject(ctx, userID, taskID); err != nil {
		return model.TaskPublicationOutcome{}, err
	}
	if s.recovery == nil {
		return model.TaskPublicationOutcome{}, ErrWechatPublicationSchedulerUnavailable
	}
	return s.recovery(ctx, userID, taskID)
}

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
	if err != nil {
		return nil, err
	}
	s.syncTaskLifecycle(ctx, taskID)
	return publication, nil
}

func (s *WechatPublicationService) GetCapabilities(ctx context.Context, userID, projectID string) ([]*model.WechatAccountCapability, error) {
	project, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if project.UserID != userID {
		return nil, ErrWechatPublicationForbidden
	}
	stored, err := s.repo.WechatCapabilities().ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]*model.WechatAccountCapability, len(stored))
	for _, item := range stored {
		byName[item.Capability] = item
	}
	result := make([]*model.WechatAccountCapability, 0, len(model.WechatCapabilities))
	for _, name := range model.WechatCapabilities {
		if item := byName[name]; item != nil {
			result = append(result, item)
		} else {
			result = append(result, &model.WechatAccountCapability{ProjectID: projectID, Capability: name, Status: model.WechatCapabilityUnknown})
		}
	}
	return result, nil
}

func (s *WechatPublicationService) recordCapability(ctx context.Context, projectID, name, status string, code int) {
	now := s.now()
	_ = s.repo.WechatCapabilities().Upsert(context.WithoutCancel(ctx), &model.WechatAccountCapability{ProjectID: projectID, Capability: name, Status: status, LastCheckedAt: &now, LastWechatCode: code})
}

func (s *WechatPublicationService) cachedDeniedFormalPublishCode(ctx context.Context, projectID string) (int, bool, error) {
	capabilities, err := s.repo.WechatCapabilities().ListByProject(ctx, projectID)
	if err != nil {
		return 0, false, err
	}
	for _, capability := range capabilities {
		if capability.Status == model.WechatCapabilityDenied &&
			(capability.Capability == model.WechatCapabilityFreePublishSubmit || capability.Capability == model.WechatCapabilityPublishedArticleQuery) {
			return capability.LastWechatCode, true, nil
		}
	}
	return 0, false, nil
}

func applyWechatManualPublishFallback(publication *model.WechatPublication, code int) {
	publication.Status = model.WechatPublicationStatusAwaitingManual
	publication.Source = model.WechatPublicationSourceWechatConsole
	publication.ManualPublishRequired = true
	publication.WechatStatusCode = code
	publication.LastError = "当前公众号没有正式发布接口权限，请前往公众号后台发布草稿"
	publication.NextCheckAt = nil
}

// BindManualPublication records a user-confirmed publication made from the
// official-account console. The URL is an identity/matching key; it is never
// treated as evidence that the API published the article.
func (s *WechatPublicationService) BindManualPublication(ctx context.Context, userID, taskID, articleURL string) (*model.WechatPublication, error) {
	defer s.syncTaskLifecycle(ctx, taskID)
	_, _, err := s.ownedTaskProject(ctx, userID, taskID)
	if err != nil {
		return nil, err
	}
	rawArticleURL := strings.TrimSpace(articleURL)
	articleURL = normalizeManualWechatURL(rawArticleURL)
	if rawArticleURL != "" && articleURL == "" {
		return nil, ErrWechatPublicationInvalidPayload
	}
	publication, err := s.repo.WechatPublications().FindByTaskID(ctx, taskID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrWechatPublicationNotFound
	}
	if err != nil {
		return nil, err
	}
	if publication.Status == model.WechatPublicationStatusPublished && publication.ArticleURL == articleURL {
		return publication, nil
	}
	if publication.DraftMediaID == "" || (publication.Status != model.WechatPublicationStatusAwaitingManual && publication.Status != model.WechatPublicationStatusDrafted) {
		return publication, ErrWechatPublicationConflict
	}
	now := s.now()
	publication.Source = model.WechatPublicationSourceWechatConsole
	publication.Status = model.WechatPublicationStatusPublished
	publication.ArticleURL = articleURL
	publication.PublishedAt = &now
	publication.ManualPublishRequired = false
	publication.LastError = ""
	publication.NextCheckAt = nil
	publication.WechatStatusCode = 0
	if err := s.repo.WechatPublications().Update(ctx, publication); err != nil {
		return nil, err
	}
	return publication, nil
}

func normalizeManualWechatURL(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	parsed.Fragment = ""
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	return parsed.String()
}

func firstDraftArticle(request appwechat.DraftAddRequest) (appwechat.DraftArticle, error) {
	if len(request.Articles) != 1 {
		return appwechat.DraftArticle{}, fmt.Errorf("%w: exactly one draft article is required", ErrWechatPublicationInvalidPayload)
	}
	article := normalizeDraftArticle(request.Articles[0])
	if article.Title == "" || strings.TrimSpace(article.Content) == "" {
		return article, fmt.Errorf("%w: draft title and content are required", ErrWechatPublicationInvalidPayload)
	}
	if err := validateWechatHTMLFragment("draft article content", []byte(article.Content)); err != nil {
		return article, fmt.Errorf("%w: %v", ErrWechatPublicationInvalidPayload, err)
	}
	if err := validateContentImageDiversity(article.Content); err != nil {
		return article, fmt.Errorf("%w: %v", ErrWechatPublicationInvalidPayload, err)
	}
	return article, nil
}

var wechatPublicationMarketingBlockRules = []struct {
	id      string
	pattern *regexp.Regexp
}{
	{id: "external_url", pattern: regexp.MustCompile(`(?i)https?://[^\s]+`)},
	{id: "contact_phone", pattern: regexp.MustCompile(`(^|[^0-9])1[3-9][0-9]{9}([^0-9]|$)`)},
	{id: "qr_code", pattern: regexp.MustCompile(`二维码|扫码(添加|加群|进群|联系|领取)`)},
	{id: "private_contact", pattern: regexp.MustCompile(`(加|添加)(我)?微信|私信我|联系我|进群|加群`)},
	{id: "keyword_reward", pattern: regexp.MustCompile(`回复(关键词|“[^”]+”|「[^」]+」|[^\s]{1,12})(即可|可)?(领|领取|获取|获得|下载)|(关注|点赞|留言|评论|转发|分享).{0,12}(领|领取|获取|获得|赠送)`)},
	{id: "false_promise", pattern: regexp.MustCompile(`保证(赚钱|盈利|见效)|稳赚不赔|零风险(收益|赚钱)|永久有效`)},
	{id: "medical_efficacy", pattern: regexp.MustCompile(`(根治|治愈|治疗)(癌症|糖尿病|高血压|失眠|抑郁|疾病)|替代(药物|就医|治疗)`)},
}

func wechatPublicationMarketingBlocks(article appwechat.DraftArticle) []string {
	contentText, outboundContent := publicationMarketingScanContent(article.Content)
	scanText := stripPublicationMarketingFormatCharacters(strings.Join([]string{
		article.Title,
		article.Author,
		article.Digest,
		article.ContentSourceURL,
		article.URL,
		contentText,
	}, "\n"))
	blocked := make([]string, 0, len(wechatPublicationMarketingBlockRules))
	seen := make(map[string]struct{}, len(wechatPublicationMarketingBlockRules))
	if outboundContent || strings.TrimSpace(article.ContentSourceURL) != "" || strings.TrimSpace(article.URL) != "" {
		blocked = append(blocked, "external_url")
		seen["external_url"] = struct{}{}
	}
	for _, rule := range wechatPublicationMarketingBlockRules {
		if _, exists := seen[rule.id]; !exists && rule.pattern.MatchString(scanText) {
			blocked = append(blocked, rule.id)
			seen[rule.id] = struct{}{}
		}
	}
	return blocked
}

func stripPublicationMarketingFormatCharacters(value string) string {
	value = norm.NFKC.String(value)
	return strings.Map(func(char rune) rune {
		if unicode.Is(unicode.Cf, char) ||
			unicode.Is(unicode.Properties["Other_Default_Ignorable_Code_Point"], char) ||
			unicode.Is(unicode.Properties["Variation_Selector"], char) {
			return -1
		}
		return char
	}, value)
}

func publicationMarketingScanContent(content string) (string, bool) {
	nodes, err := html.ParseFragment(strings.NewReader(content), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return content, true
	}
	var text strings.Builder
	var attributeEvidence []string
	lastWasBreak := true
	appendText := func(value string) {
		if value == "" {
			return
		}
		text.WriteString(value)
		lastWasBreak = false
	}
	appendBreak := func() {
		if text.Len() > 0 && !lastWasBreak {
			text.WriteByte('\n')
			lastWasBreak = true
		}
	}
	outboundContent := false
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		switch node.Type {
		case html.TextNode:
			appendText(node.Data)
		case html.ElementNode:
			block := isPublicationMarketingBlockElement(node.Data)
			if block {
				appendBreak()
			}
			for _, attribute := range node.Attr {
				name := strings.ToLower(attribute.Key)
				if node.DataAtom == atom.A && name == "href" {
					if value := strings.TrimSpace(attribute.Val); value != "" {
						attributeEvidence = append(attributeEvidence, value)
					}
					if hasOutboundHTMLLink(compactHTMLSecurityValue(attribute.Val)) {
						outboundContent = true
					}
				} else if name == "alt" {
					appendText(attribute.Val)
				} else if name == "style" && hasUnsafeInlineCSS(compactHTMLSecurityValue(attribute.Val)) {
					outboundContent = true
				}
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
			if block {
				appendBreak()
			}
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	for _, node := range nodes {
		walk(node)
	}
	return strings.Join(append([]string{text.String()}, attributeEvidence...), "\n"), outboundContent
}

func (s *WechatPublicationService) validateDraftImageSources(ctx context.Context, task *model.Task, content string) error {
	sources, err := wechatDraftImageSources(content)
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return nil
	}

	registered := make(map[string]struct{})
	if task != nil && task.CurrentExecutionID != nil && strings.TrimSpace(*task.CurrentExecutionID) != "" {
		files, err := s.repo.TaskFiles().FindByExecutionID(ctx, *task.CurrentExecutionID)
		if err != nil {
			return fmt.Errorf("list current execution images: %w", err)
		}
		for _, file := range files {
			if file != nil && file.TaskID == task.ID && strings.TrimSpace(file.WechatURL) != "" {
				registered[strings.TrimSpace(file.WechatURL)] = struct{}{}
			}
		}
	}

	for _, source := range sources {
		parsed, err := url.Parse(source)
		if err != nil || !parsed.IsAbs() ||
			(!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) ||
			parsed.Host == "" || parsed.User != nil {
			return fmt.Errorf("%w: body image source must be an absolute HTTP(S) URL", ErrWechatPublicationInvalidPayload)
		}
		trustedCDN := strings.EqualFold(parsed.Hostname(), "mmbiz.qpic.cn") && parsed.Port() == ""
		_, uploadedByExecution := registered[source]
		if !trustedCDN && !uploadedByExecution {
			return fmt.Errorf("%w: body image source is not a trusted WeChat upload", ErrWechatPublicationInvalidPayload)
		}
	}
	return nil
}

func wechatDraftImageSources(content string) ([]string, error) {
	nodes, err := html.ParseFragment(strings.NewReader(content), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return nil, fmt.Errorf("%w: parse draft image sources", ErrWechatPublicationInvalidPayload)
	}
	var sources []string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.DataAtom == atom.Img {
			for _, attribute := range node.Attr {
				if strings.EqualFold(attribute.Key, "src") {
					sources = append(sources, strings.TrimSpace(attribute.Val))
					break
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	for _, node := range nodes {
		walk(node)
	}
	return sources, nil
}

func isPublicationMarketingBlockElement(tag string) bool {
	switch strings.ToLower(tag) {
	case "address", "article", "aside", "blockquote", "br", "div", "footer", "h1", "h2", "h3", "h4", "h5", "h6",
		"header", "hr", "li", "main", "nav", "ol", "p", "pre", "section", "table", "tbody", "td", "th", "thead", "tr", "ul":
		return true
	default:
		return false
	}
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

func (s *WechatPublicationService) CreateDraft(ctx context.Context, userID, taskID, projectID, executionID string, request appwechat.DraftAddRequest) (publication *model.WechatPublication, resultErr error) {
	defer s.syncTaskLifecycle(ctx, taskID)
	task, project, err := s.ownedTaskProject(ctx, userID, taskID)
	if err != nil {
		return nil, err
	}
	if project.ID != projectID {
		return nil, ErrWechatPublicationProjectMismatch
	}
	executionID = strings.TrimSpace(executionID)
	if executionID == "" || task.CurrentExecutionID == nil || *task.CurrentExecutionID != executionID {
		return nil, ErrWechatPublicationExecutionMismatch
	}
	currentExecution, err := s.repo.TaskExecutions().FindByID(ctx, executionID)
	if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && currentExecution.TaskID != task.ID {
		return nil, ErrWechatPublicationExecutionMismatch
	}
	if err != nil {
		return nil, err
	}
	if currentExecution.DraftDeliveryStatus == model.TaskExecutionDraftDeliveryFailed {
		var previous draftDeliveryEvidence
		if json.Unmarshal(currentExecution.DraftDeliveryResult, &previous) != nil ||
			previous.Code != "create_draft_provider_failure" || previous.Attempt >= 2 {
			return nil, ErrWechatPublicationConflict
		}
	}
	article, err := firstDraftArticle(request)
	if err != nil {
		return nil, err
	}
	if err := s.validateDraftImageSources(ctx, task, article.Content); err != nil {
		return nil, err
	}
	if blocked := wechatPublicationMarketingBlocks(article); len(blocked) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrWechatPublicationMarketingBlocked, strings.Join(blocked, ","))
	}
	request.Articles[0] = article
	fingerprint := WechatContentFingerprint(article.Content)
	requestFingerprint := wechatDraftRequestFingerprint(article)

	existing, findErr := s.repo.WechatPublications().FindByTaskID(ctx, task.ID)
	fresh := errors.Is(findErr, gorm.ErrRecordNotFound)
	claimedForRetry := false
	retryWechatStatusCode := 0
	var api WechatPublicationAPI
	if findErr != nil && !fresh {
		return nil, findErr
	}
	if fresh {
		api, err = s.projectAPI(project)
		if err != nil {
			if recordErr := s.recordDraftPreflightFailure(context.WithoutCancel(ctx), executionID, currentExecution, err); recordErr != nil {
				return nil, errors.Join(err, recordErr)
			}
			return nil, err
		}
	}
	if !fresh {
		if existing.ExecutionID != executionID {
			if existing.DraftContentFingerprint != fingerprint || existing.DraftRequestFingerprint != requestFingerprint {
				return existing, ErrWechatPublicationExecutionMismatch
			}
			priorExecution, executionErr := s.repo.TaskExecutions().FindByID(ctx, existing.ExecutionID)
			if executionErr != nil {
				if errors.Is(executionErr, gorm.ErrRecordNotFound) {
					return existing, ErrWechatPublicationExecutionMismatch
				}
				return existing, executionErr
			}
			if !isTerminalExecution(priorExecution.Status) {
				return existing, ErrWechatPublicationExecutionMismatch
			}
			_, rebindErr := s.repo.WechatPublications().RebindExecution(ctx, existing.ID, existing.ExecutionID, executionID, existing.UpdatedAt)
			if rebindErr != nil {
				return existing, rebindErr
			}
			latest, latestErr := s.repo.WechatPublications().FindByID(ctx, existing.ID)
			if latestErr != nil {
				return existing, latestErr
			}
			if latest.ExecutionID != executionID || latest.DraftContentFingerprint != fingerprint || latest.DraftRequestFingerprint != requestFingerprint {
				return latest, ErrWechatPublicationExecutionMismatch
			}
			existing = latest
		}
		if existing.DraftContentFingerprint != fingerprint || existing.DraftRequestFingerprint != requestFingerprint {
			return existing, ErrWechatPublicationConflict
		}
		if existing.Status == model.WechatPublicationStatusDrafting && existing.DraftAddAttemptedAt != nil && existing.ClaimToken == "" {
			return existing, ErrWechatPublicationPending
		}
		if existing.Status == model.WechatPublicationStatusUnsupported && existing.WechatStatusCode != 0 && existing.DraftMediaID == "" {
			if existing.DraftAddAttempts >= 2 || existing.DraftAddAttemptedAt != nil && existing.DraftRetryAuthorizedAt == nil {
				return existing, ErrWechatPublicationConflict
			}
			retryWechatStatusCode = existing.WechatStatusCode
			now, token := s.now(), uuid.NewString()
			won, retryErr := s.repo.WechatPublications().RetryUnsupportedDraft(ctx, existing.ID, existing.UpdatedAt, now, token)
			if retryErr != nil {
				return existing, retryErr
			}
			if !won {
				return s.finishConcurrentDraftRecovery(ctx, userID, taskID, existing.ID)
			}
			existing, findErr = s.repo.WechatPublications().FindByID(ctx, existing.ID)
			if findErr != nil {
				return nil, findErr
			}
			claimedForRetry = true
		}
		if existing.DraftMediaID != "" {
			return s.autoPublishDraft(ctx, userID, taskID, existing)
		}
		if !claimedForRetry && existing.ClaimToken != "" && existing.ClaimedAt != nil && existing.ClaimedAt.After(s.now().Add(-wechatPublicationClaimLease)) {
			return existing, ErrWechatPublicationPending
		}
	} else {
		now := s.now()
		existing = &model.WechatPublication{
			ID: uuid.NewString(), TaskID: task.ID, ExecutionID: executionID, UserID: userID, ProjectID: project.ID,
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
	// An execution already marked in-flight/ambiguous owns the external-call
	// boundary. Do not even list drafts on a duplicate request; reconciliation
	// is the only safe next step.
	if current, executionErr := s.repo.TaskExecutions().FindByID(ctx, executionID); executionErr == nil {
		switch current.DraftDeliveryStatus {
		case model.TaskExecutionDraftDeliveryInFlight:
			return existing, ErrWechatPublicationPending
		case model.TaskExecutionDraftDeliveryAmbiguous:
			if existing == nil || existing.DraftAddAttemptedAt == nil {
				return existing, ErrWechatPublicationPending
			}
		}
	}
	if api == nil {
		api, err = s.projectAPI(project)
	}
	if err != nil {
		resolvedErr := s.resolveReadOnlyDraftClaimFailure(ctx, existing, err, false, retryWechatStatusCode)
		if recordErr := s.recordDraftPreflightFailure(context.WithoutCancel(ctx), executionID, currentExecution, resolvedErr); recordErr != nil {
			return existing, errors.Join(resolvedErr, recordErr)
		}
		return existing, resolvedErr
	}

	recovered, err := s.recoverDraftByListing(ctx, api, existing)
	if errors.Is(err, errWechatPublicationVersionChanged) {
		return s.finishConcurrentDraftRecovery(ctx, userID, taskID, existing.ID)
	}
	if err != nil {
		if (fresh || claimedForRetry) && !errors.Is(err, ErrWechatPublicationDraftUnsupported) {
			return existing, s.resolveReadOnlyDraftClaimFailure(ctx, existing, err, fresh, retryWechatStatusCode)
		}
		return existing, err
	}
	if recovered {
		return s.finishConcurrentDraftRecovery(ctx, userID, taskID, existing.ID)
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
			return s.finishConcurrentDraftRecovery(ctx, userID, taskID, existing.ID)
		}
		existing, findErr = s.repo.WechatPublications().FindByID(ctx, existing.ID)
		if findErr != nil {
			return nil, findErr
		}
		claimedForRetry = true
	}
	if claimedForRetry && existing.DraftAddAttemptedAt != nil && existing.DraftRetryAuthorizedAt == nil {
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
			concurrent, concurrentErr := s.finishConcurrentDraftRecovery(context.WithoutCancel(ctx), userID, taskID, existing.ID)
			if concurrentErr != nil {
				return concurrent, fmt.Errorf("persist expired ambiguous WeChat draft reconciliation: %w", concurrentErr)
			}
			return concurrent, nil
		}
		existing.ClaimToken, existing.ClaimedAt = "", nil
		s.enqueueReconcile(existing.ProjectID, existing.NextCheckAt)
		if existing.Status == model.WechatPublicationStatusPublishFailed {
			return existing, ErrWechatPublicationDraftFailed
		}
		return existing, ErrWechatPublicationPending
	}
	if !fresh && !claimedForRetry {
		return s.finishConcurrentDraftRecovery(ctx, userID, taskID, existing.ID)
	}

	draftAddAttemptedAt := s.now()
	draftRecoveryAt := draftAddAttemptedAt.Add(10 * time.Minute)
	draftDeliveryAttempt := 0
	var won bool
	err = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		var started bool
		var beginErr error
		draftDeliveryAttempt, started, beginErr = s.beginDraftDeliveryWithRepository(ctx, tx, executionID)
		if beginErr != nil {
			return beginErr
		}
		if !started {
			return ErrWechatPublicationPending
		}
		var markErr error
		won, markErr = tx.WechatPublications().MarkDraftAddAttempted(ctx, existing.ID, existing.ClaimToken, draftAddAttemptedAt, draftRecoveryAt)
		if markErr != nil {
			return markErr
		}
		if !won {
			return ErrWechatPublicationConflict
		}
		return nil
	})
	if err != nil {
		return existing, err
	}
	if existing.DraftAddAttemptedAt == nil {
		existing.DraftAddAttemptedAt = &draftAddAttemptedAt
	}
	existing.DraftAddAttempts++
	existing.DraftRetryAuthorizedAt = nil
	existing.DraftCreatedAt = &draftAddAttemptedAt
	existing.NextCheckAt = &draftRecoveryAt
	defer func() {
		if err := s.finishDraftDelivery(context.WithoutCancel(ctx), executionID, draftDeliveryAttempt, publication, resultErr); err != nil && s.logger != nil {
			s.logger.Error().Err(err).Str("task_id", taskID).Str("execution_id", executionID).Msg("record WeChat draft delivery result")
		}
	}()
	publication = existing
	response, err := api.AddDraft(ctx, request)
	draftClaimToken := existing.ClaimToken
	if err != nil {
		if code, definitive := definitiveWechatPublicationError(err); definitive {
			if code == 48001 {
				s.recordCapability(ctx, existing.ProjectID, model.WechatCapabilityDraftAdd, model.WechatCapabilityDenied, code)
			}
			existing.Status, existing.WechatStatusCode, existing.LastError, existing.NextCheckAt = model.WechatPublicationStatusUnsupported, code, safeWechatPublicationError(err), nil
			// Preserve the first external-call timestamp as immutable audit
			// evidence. A definitive rejection authorizes one bounded retry;
			// transport-ambiguous failures never receive this authorization.
			if existing.DraftAddAttempts < 2 {
				retryAuthorizedAt := s.now()
				existing.DraftRetryAuthorizedAt = &retryAuthorizedAt
			} else {
				existing.DraftRetryAuthorizedAt = nil
			}
		} else {
			s.recordCapability(ctx, existing.ProjectID, model.WechatCapabilityDraftAdd, model.WechatCapabilityTemporarilyUnavailable, 0)
			existing.LastError = safeWechatPublicationError(err)
			existing.NextCheckAt = nextWechatManualCheck(s.now(), valueOrTime(existing.DraftCreatedAt, existing.CreatedAt))
		}
		won, persistErr := s.repo.WechatPublications().UpdateDraftClaimed(context.WithoutCancel(ctx), existing, draftClaimToken)
		if persistErr != nil {
			return existing, fmt.Errorf("persist ambiguous WeChat draft outcome: %w", persistErr)
		}
		if !won {
			return s.finishConcurrentDraftRecovery(context.WithoutCancel(ctx), userID, taskID, existing.ID)
		}
		existing.ClaimToken, existing.ClaimedAt = "", nil
		s.enqueueReconcile(existing.ProjectID, existing.NextCheckAt)
		if existing.Status == model.WechatPublicationStatusUnsupported {
			if existing.WechatStatusCode == 48001 {
				return existing, errors.Join(ErrWechatPublicationDraftUnsupported, err)
			}
			return existing, errors.Join(ErrWechatPublicationDraftRejected, err)
		}
		return existing, errors.Join(ErrWechatPublicationPending, err)
	}
	s.recordCapability(ctx, existing.ProjectID, model.WechatCapabilityDraftAdd, model.WechatCapabilityAvailable, 0)
	if response == nil || strings.TrimSpace(response.MediaID) == "" {
		existing.LastError = "WeChat draft response omitted media_id"
		existing.NextCheckAt = nextWechatManualCheck(s.now(), valueOrTime(existing.DraftCreatedAt, existing.CreatedAt))
		won, persistErr := s.repo.WechatPublications().UpdateDraftClaimed(context.WithoutCancel(ctx), existing, draftClaimToken)
		if persistErr != nil {
			return existing, fmt.Errorf("persist ambiguous WeChat draft response: %w", persistErr)
		}
		if !won {
			return s.finishConcurrentDraftRecovery(context.WithoutCancel(ctx), userID, taskID, existing.ID)
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
		return s.finishConcurrentDraftRecovery(ctx, userID, taskID, existing.ID)
	}
	existing.ClaimToken, existing.ClaimedAt = "", nil
	return s.autoPublishDraft(ctx, userID, taskID, existing)
}

// CreateDraftInteractive resolves the current execution for an explicit,
// user-authenticated MCP request. Managed execution credentials are denied at
// the MCP authorization boundary and the automatic path calls CreateDraft
// internally from the Server finalizer.
func (s *WechatPublicationService) CreateDraftInteractive(ctx context.Context, userID, taskID, projectID string, request appwechat.DraftAddRequest) (*model.WechatPublication, error) {
	task, project, err := s.ownedTaskProject(ctx, userID, taskID)
	if err != nil {
		return nil, err
	}
	if project.ID != strings.TrimSpace(projectID) {
		return nil, ErrWechatPublicationProjectMismatch
	}
	if task.CurrentExecutionID == nil || strings.TrimSpace(*task.CurrentExecutionID) == "" {
		return nil, ErrWechatPublicationExecutionMismatch
	}
	return s.CreateDraft(ctx, userID, taskID, project.ID, strings.TrimSpace(*task.CurrentExecutionID), request)
}

type draftDeliveryEvidence struct {
	Source     string `json:"source"`
	Status     string `json:"status"`
	Code       string `json:"code,omitempty"`
	Attempt    int    `json:"attempt,omitempty"`
	Attempted  bool   `json:"attempted"`
	Action     string `json:"action,omitempty"`
	OccurredAt string `json:"occurred_at,omitempty"`
}

func (s *WechatPublicationService) beginDraftDelivery(ctx context.Context, executionID string) (int, bool, error) {
	return s.beginDraftDeliveryWithRepository(ctx, s.repo, executionID)
}

func (s *WechatPublicationService) recordDraftPreflightFailure(ctx context.Context, executionID string, execution *model.TaskExecution, _ error) error {
	from, attempt := "", 1
	if execution != nil && execution.DraftDeliveryStatus == model.TaskExecutionDraftDeliveryFailed {
		var previous draftDeliveryEvidence
		if json.Unmarshal(execution.DraftDeliveryResult, &previous) != nil ||
			previous.Code != "create_draft_provider_failure" || previous.Attempt >= 2 {
			return ErrWechatPublicationConflict
		}
		from, attempt = model.TaskExecutionDraftDeliveryFailed, previous.Attempt+1
	}
	evidence, err := json.Marshal(draftDeliveryEvidence{
		Source: "create_draft", Status: model.TaskExecutionDraftDeliveryFailed,
		Code: "create_draft_provider_failure", Attempt: attempt, Attempted: false,
		Action: "retry_draft", OccurredAt: s.now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	won, err := s.repo.TaskExecutions().TransitionDraftDelivery(ctx, executionID, from, model.TaskExecutionDraftDeliveryFailed, evidence)
	if err != nil {
		return err
	}
	if !won {
		return ErrWechatPublicationConflict
	}
	return nil
}

func (s *WechatPublicationService) beginDraftDeliveryWithRepository(ctx context.Context, repo repository.Repository, executionID string) (int, bool, error) {
	evidence, err := json.Marshal(draftDeliveryEvidence{Source: "create_draft", Status: model.TaskExecutionDraftDeliveryInFlight, Attempt: 1})
	if err != nil {
		return 0, false, err
	}
	started, err := repo.TaskExecutions().TransitionDraftDelivery(ctx, executionID, "", model.TaskExecutionDraftDeliveryInFlight, evidence)
	if err != nil || started {
		return 1, started, err
	}
	current, err := repo.TaskExecutions().FindByID(ctx, executionID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, ErrWechatPublicationExecutionMismatch
	}
	if err != nil {
		return 0, false, err
	}
	switch current.DraftDeliveryStatus {
	case model.TaskExecutionDraftDeliverySucceeded:
		return 0, false, nil
	case model.TaskExecutionDraftDeliveryInFlight, model.TaskExecutionDraftDeliveryAmbiguous:
		return 0, false, ErrWechatPublicationPending
	case model.TaskExecutionDraftDeliveryFailed:
		var previous draftDeliveryEvidence
		if json.Unmarshal(current.DraftDeliveryResult, &previous) != nil ||
			(previous.Code != "create_draft_provider_failure" && previous.Code != "create_draft_unsupported") || previous.Attempt >= 2 {
			return 0, false, ErrWechatPublicationConflict
		}
	default:
		return 0, false, ErrWechatPublicationConflict
	}
	evidence, err = json.Marshal(draftDeliveryEvidence{Source: "create_draft", Status: model.TaskExecutionDraftDeliveryInFlight, Attempt: 2})
	if err != nil {
		return 0, false, err
	}
	started, err = repo.TaskExecutions().TransitionDraftDelivery(ctx, executionID, model.TaskExecutionDraftDeliveryFailed, model.TaskExecutionDraftDeliveryInFlight, evidence)
	if err != nil {
		return 0, false, err
	}
	if !started {
		return 0, false, ErrWechatPublicationPending
	}
	return 2, true, nil
}

func (s *WechatPublicationService) finishDraftDelivery(ctx context.Context, executionID string, attempt int, publication *model.WechatPublication, cause error) error {
	status := model.TaskExecutionDraftDeliveryFailed
	code := wechatDraftDeliveryFailureCode(cause)
	attempted := publication != nil && publication.DraftAddAttemptedAt != nil
	action := "retry_draft"
	if cause == nil || publication != nil && publication.DraftMediaID != "" {
		status = model.TaskExecutionDraftDeliverySucceeded
		code = ""
		action = ""
	} else if errors.Is(cause, ErrWechatPublicationPending) {
		if attempted {
			status = model.TaskExecutionDraftDeliveryAmbiguous
			action = "check_wechat"
		} else {
			status = model.TaskExecutionDraftDeliveryBlocked
		}
	} else if errors.Is(cause, ErrWechatPublicationDraftRejected) || errors.Is(cause, ErrWechatPublicationDraftUnsupported) {
		action = "retry_draft"
	} else if attempted {
		action = "check_wechat"
	}
	evidence, err := json.Marshal(draftDeliveryEvidence{
		Source:     "create_draft",
		Status:     status,
		Code:       code,
		Attempt:    attempt,
		Attempted:  attempted,
		Action:     action,
		OccurredAt: s.now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	won, err := s.repo.TaskExecutions().TransitionDraftDelivery(ctx, executionID, model.TaskExecutionDraftDeliveryInFlight, status, evidence)
	if err != nil || won {
		return err
	}
	return fmt.Errorf("record draft delivery result: state changed concurrently")
}

func wechatDraftDeliveryFailureCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrWechatPublicationInvalidPayload):
		return "create_draft_invalid_payload"
	case errors.Is(err, ErrWechatPublicationMarketingBlocked):
		return "create_draft_marketing_blocked"
	case errors.Is(err, ErrWechatPublicationNotFound):
		return "create_draft_not_found"
	case errors.Is(err, ErrWechatPublicationProjectMismatch):
		return "create_draft_project_mismatch"
	case errors.Is(err, ErrWechatPublicationExecutionMismatch):
		return "create_draft_execution_mismatch"
	case errors.Is(err, ErrWechatPublicationForbidden):
		return "create_draft_forbidden"
	case errors.Is(err, ErrWechatPublicationModeConflict):
		return "create_draft_disabled"
	case errors.Is(err, ErrWechatPublicationDraftUnsupported):
		return "create_draft_unsupported"
	case errors.Is(err, ErrWechatPublicationDraftRejected):
		return "create_draft_rejected"
	case errors.Is(err, ErrWechatPublicationDraftFailed):
		return "create_draft_reconciliation_failed"
	case errors.Is(err, ErrWechatPublicationConflict):
		return "create_draft_conflict"
	case errors.Is(err, ErrWechatPublicationPending):
		return "create_draft_pending_reconciliation"
	default:
		return "create_draft_provider_failure"
	}
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

func (s *WechatPublicationService) finishConcurrentDraftRecovery(ctx context.Context, userID, taskID, publicationID string) (*model.WechatPublication, error) {
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
	if latest.Status == model.WechatPublicationStatusDrafted {
		return s.autoPublishDraft(ctx, userID, taskID, latest)
	}
	if latest.Status == model.WechatPublicationStatusPublishing {
		s.enqueuePoll(latest.ID, latest.NextCheckAt)
	} else {
		s.enqueueReconcile(latest.ProjectID, latest.NextCheckAt)
	}
	return latest, nil
}

func (s *WechatPublicationService) autoPublishDraft(ctx context.Context, userID, taskID string, publication *model.WechatPublication) (*model.WechatPublication, error) {
	if publication.Status != model.WechatPublicationStatusDrafted {
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
	defer s.syncTaskLifecycle(ctx, taskID)
	_, project, err := s.ownedTaskProject(ctx, userID, taskID)
	if err != nil {
		return nil, err
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
	if publication.Status == model.WechatPublicationStatusPublished || publication.Status == model.WechatPublicationStatusUnsupported || publication.Status == model.WechatPublicationStatusAwaitingManual {
		return publication, nil
	}
	deniedCode, denied, err := s.cachedDeniedFormalPublishCode(ctx, project.ID)
	if err != nil {
		return publication, err
	}
	if denied {
		applyWechatManualPublishFallback(publication, deniedCode)
		if err := s.repo.WechatPublications().Update(ctx, publication); err != nil {
			return publication, err
		}
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
	s.recordCapability(ctx, publication.ProjectID, model.WechatCapabilityPublishedArticleQuery, model.WechatCapabilityAvailable, 0)
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
	attempt := &model.WechatPublicationAttempt{ID: uuid.NewString(), PublicationID: publication.ID, Operation: "freepublish_submit", RequestFingerprint: publication.DraftRequestFingerprint, StartedAt: now, ResultStatus: "attempted"}
	if err := s.repo.WechatPublications().CreateAttempt(ctx, attempt); err != nil {
		return publication, fmt.Errorf("persist WeChat publish attempt: %w", err)
	}
	response, submitErr := api.SubmitFreePublish(ctx, appwechat.FreePublishSubmitRequest{MediaID: publication.DraftMediaID})
	completedAt := s.now()
	attempt.CompletedAt = &completedAt
	if submitErr != nil {
		attempt.ResultStatus = "failed"
		attempt.DurableEvidence = safeWechatPublicationError(submitErr)
		if code, ok := publicationWechatErrCode(submitErr); ok {
			attempt.WechatCode = code
		}
	} else {
		attempt.ResultStatus = "accepted"
		if response != nil {
			attempt.DurableEvidence = strings.TrimSpace(response.PublishID)
		}
	}
	_ = s.repo.WechatPublications().UpdateAttempt(context.WithoutCancel(ctx), attempt)
	publication.Status = model.WechatPublicationStatusPublishSubmitting
	if submitErr != nil {
		publication.LastError = safeWechatPublicationError(submitErr)
		publication.NextCheckAt = nextWechatManualCheck(now, wechatFormalReconciliationAnchor(publication))
		if code, definitive := definitiveWechatPublicationError(submitErr); definitive {
			publication.WechatStatusCode = code
			publication.Status, publication.NextCheckAt = model.WechatPublicationStatusUnsupported, nil
			if code == 48001 && publication.DraftMediaID != "" {
				s.recordCapability(ctx, publication.ProjectID, model.WechatCapabilityFreePublishSubmit, model.WechatCapabilityDenied, code)
				publication.Status = model.WechatPublicationStatusAwaitingManual
				publication.ManualPublishRequired = true
				publication.Source = model.WechatPublicationSourceWechatConsole
			}
			publication.SubmitAttemptedAt = nil
		} else if code, ok := publicationWechatErrCode(submitErr); ok {
			publication.WechatStatusCode = code
		} else {
			s.recordCapability(ctx, publication.ProjectID, model.WechatCapabilityFreePublishSubmit, model.WechatCapabilityTemporarilyUnavailable, 0)
		}
		won, persistErr := s.repo.WechatPublications().UpdateClaimed(context.WithoutCancel(ctx), publication, token)
		if persistErr != nil {
			return publication, fmt.Errorf("persist ambiguous WeChat publish outcome: %w", persistErr)
		}
		if !won {
			return publication, ErrWechatPublicationConflict
		}
		if publication.Status == model.WechatPublicationStatusUnsupported || publication.Status == model.WechatPublicationStatusAwaitingManual {
			return s.repo.WechatPublications().FindByID(context.WithoutCancel(ctx), publication.ID)
		}
		if _, ok := publicationWechatErrCode(submitErr); !ok {
			s.enqueueReconcile(publication.ProjectID, publication.NextCheckAt)
			return publication, ErrWechatPublicationPending
		}
		return publication, submitErr
	}
	s.recordCapability(ctx, publication.ProjectID, model.WechatCapabilityFreePublishSubmit, model.WechatCapabilityAvailable, 0)
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
		s.recordCapability(ctx, publication.ProjectID, model.WechatCapabilityPublishedArticleQuery, model.WechatCapabilityTemporarilyUnavailable, 0)
		return publication, cause
	}
	expectedStatus, expectedUpdatedAt := publication.Status, publication.UpdatedAt
	publication.Status = model.WechatPublicationStatusUnsupported
	publication.WechatStatusCode = code
	publication.LastError = safeWechatPublicationError(cause)
	publication.NextCheckAt = nil
	if code == 48001 && publication.DraftMediaID != "" {
		s.recordCapability(ctx, publication.ProjectID, model.WechatCapabilityPublishedArticleQuery, model.WechatCapabilityDenied, code)
		publication.Status = model.WechatPublicationStatusAwaitingManual
		publication.ManualPublishRequired = true
		publication.Source = model.WechatPublicationSourceWechatConsole
	}
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
	defer s.syncTaskLifecycle(ctx, taskID)
	_, _, err := s.ownedTaskProject(ctx, userID, taskID)
	if err != nil {
		return nil, err
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
	if publication.Status == model.WechatPublicationStatusAwaitingManual && publication.ManualPublishRequired && publication.WechatStatusCode == 48001 {
		return publication, nil
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
	defer s.syncTaskLifecycle(ctx, publication.TaskID)
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
			s.recordCapability(ctx, publication.ProjectID, model.WechatCapabilityFreePublishQuery, model.WechatCapabilityDenied, code)
			publication.Status, publication.WechatStatusCode, publication.NextCheckAt = model.WechatPublicationStatusUnsupported, code, nil
		} else {
			s.recordCapability(ctx, publication.ProjectID, model.WechatCapabilityFreePublishQuery, model.WechatCapabilityTemporarilyUnavailable, 0)
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
	s.recordCapability(ctx, publication.ProjectID, model.WechatCapabilityFreePublishQuery, model.WechatCapabilityAvailable, 0)
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
	defer s.syncTaskLifecycle(ctx, taskID)
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

func (s *WechatPublicationService) reconcileProject(ctx context.Context, project *model.Project) (resultErr error) {
	pending, err := s.repo.WechatPublications().FindPendingByProject(ctx, project.ID)
	if err != nil || len(pending) == 0 {
		return err
	}
	pending, err = s.fallbackDraftsForCachedDeniedFormalPublish(ctx, project.ID, pending)
	if err != nil || len(pending) == 0 {
		return err
	}
	defer func() {
		if syncErr := s.syncReconciledDraftDeliveries(context.WithoutCancel(ctx), pending); syncErr != nil {
			resultErr = errors.Join(resultErr, syncErr)
		}
		seen := make(map[string]struct{}, len(pending))
		for _, publication := range pending {
			if publication == nil {
				continue
			}
			if _, ok := seen[publication.TaskID]; ok {
				continue
			}
			seen[publication.TaskID] = struct{}{}
			s.syncTaskLifecycle(ctx, publication.TaskID)
		}
	}()
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
			publication.WechatStatusCode = 0
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
		pending, err = s.fallbackDraftsForCachedDeniedFormalPublish(ctx, project.ID, pending)
		if err != nil || len(pending) == 0 {
			return err
		}
	}
	articles, err := listAllWechatPublishes(ctx, api)
	if err != nil {
		if code, definitive := definitiveWechatPublicationError(err); definitive {
			for _, publication := range pending {
				expectedStatus, expectedUpdatedAt := publication.Status, publication.UpdatedAt
				if publication.DraftMediaID == "" && publication.DraftAddAttemptedAt != nil {
					publication.WechatStatusCode = code
					publication.LastError = "WeChat draft reconciliation failed: " + safeWechatPublicationError(err)
					if next := nextWechatManualCheck(s.now(), valueOrTime(publication.DraftCreatedAt, publication.CreatedAt)); next != nil {
						publication.Status, publication.NextCheckAt = model.WechatPublicationStatusDrafting, next
					} else {
						publication.Status, publication.NextCheckAt = model.WechatPublicationStatusPublishFailed, nil
					}
				} else {
					publication.Status, publication.WechatStatusCode, publication.NextCheckAt, publication.LastError = model.WechatPublicationStatusUnsupported, code, nil, safeWechatPublicationError(err)
				}
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
		if publication.Status == model.WechatPublicationStatusDrafted && !hasWechatSubmissionEvidence(publication) {
			automatic, automaticErr := s.autoPublishDraft(ctx, publication.UserID, publication.TaskID, publication)
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

func (s *WechatPublicationService) fallbackDraftsForCachedDeniedFormalPublish(ctx context.Context, projectID string, pending []*model.WechatPublication) ([]*model.WechatPublication, error) {
	code, denied, err := s.cachedDeniedFormalPublishCode(ctx, projectID)
	if err != nil || !denied {
		return pending, err
	}
	changed := false
	for _, publication := range pending {
		if publication.Status != model.WechatPublicationStatusDrafted || hasWechatSubmissionEvidence(publication) {
			continue
		}
		expectedStatus, expectedUpdatedAt := publication.Status, publication.UpdatedAt
		applyWechatManualPublishFallback(publication, code)
		won, updateErr := s.repo.WechatPublications().UpdateReconciliation(ctx, publication, expectedStatus, expectedUpdatedAt)
		if updateErr != nil {
			return nil, updateErr
		}
		if !won {
			return nil, ErrWechatPublicationConflict
		}
		changed = true
	}
	if !changed {
		return pending, nil
	}
	return s.repo.WechatPublications().FindPendingByProject(ctx, projectID)
}

func (s *WechatPublicationService) syncReconciledDraftDeliveries(ctx context.Context, publications []*model.WechatPublication) error {
	var resultErr error
	for _, publication := range publications {
		if publication == nil {
			continue
		}
		latest, err := s.repo.WechatPublications().FindByID(ctx, publication.ID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			resultErr = errors.Join(resultErr, err)
			continue
		}
		if err := s.syncReconciledDraftDelivery(ctx, latest); err != nil {
			resultErr = errors.Join(resultErr, err)
		}
	}
	return resultErr
}

func reconciledDraftDeliveryResult(publication *model.WechatPublication) (string, string, string, bool) {
	if publication == nil {
		return "", "", "", false
	}
	if strings.TrimSpace(publication.DraftMediaID) != "" {
		return model.TaskExecutionDraftDeliverySucceeded, "", "", true
	}
	switch publication.Status {
	case model.WechatPublicationStatusUnsupported:
		if draftRetryEligible(publication) && publication.WechatStatusCode == 48001 {
			return model.TaskExecutionDraftDeliveryFailed, "create_draft_unsupported", "retry_draft", true
		}
		if draftRetryEligible(publication) {
			return model.TaskExecutionDraftDeliveryFailed, "create_draft_rejected", "retry_draft", true
		}
		return model.TaskExecutionDraftDeliveryFailed, "create_draft_reconciliation_failed", "check_wechat", true
	case model.WechatPublicationStatusPublishFailed:
		return model.TaskExecutionDraftDeliveryFailed, "create_draft_reconciliation_failed", "check_wechat", true
	default:
		return "", "", "", false
	}
}

func (s *WechatPublicationService) syncReconciledDraftDelivery(ctx context.Context, publication *model.WechatPublication) error {
	targetStatus, code, action, terminal := reconciledDraftDeliveryResult(publication)
	if !terminal {
		return nil
	}
	evidence, err := json.Marshal(draftDeliveryEvidence{
		Source: "wechat_reconciliation", Status: targetStatus, Code: code, Attempted: true,
		Action: action, OccurredAt: s.now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	return s.repo.WithTx(ctx, func(tx repository.Repository) error {
		execution, err := tx.TaskExecutions().FindByIDForUpdate(ctx, publication.ExecutionID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if execution.TaskID != publication.TaskID {
			return ErrWechatPublicationExecutionMismatch
		}
		task, err := tx.Tasks().FindByIDForUpdate(ctx, publication.TaskID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if task.CurrentExecutionID == nil || *task.CurrentExecutionID != execution.ID {
			return nil
		}
		if execution.DraftDeliveryStatus != targetStatus {
			if execution.DraftDeliveryStatus == model.TaskExecutionDraftDeliverySucceeded ||
				execution.DraftDeliveryStatus != model.TaskExecutionDraftDeliveryAmbiguous &&
					execution.DraftDeliveryStatus != model.TaskExecutionDraftDeliveryInFlight &&
					execution.DraftDeliveryStatus != model.TaskExecutionDraftDeliveryFailed {
				return nil
			}
			won, transitionErr := tx.TaskExecutions().TransitionDraftDelivery(
				ctx, execution.ID, execution.DraftDeliveryStatus, targetStatus, evidence,
			)
			if transitionErr != nil {
				return transitionErr
			}
			if !won {
				return ErrWechatPublicationConflict
			}
			execution.DraftDeliveryStatus = targetStatus
			execution.DraftDeliveryResult = evidence
		}
		if task.Outcome == nil {
			return nil
		}
		outcome := *task.Outcome
		outcome.Publication = publicationTaskOutcome(execution)
		won, updateErr := tx.Tasks().UpdateOutcomeForExecution(ctx, task.ID, execution.ID, outcome)
		if updateErr != nil {
			return updateErr
		}
		if !won {
			return ErrWechatPublicationConflict
		}
		return nil
	})
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
			publication.WechatStatusCode = code
			publication.LastError = safeWechatPublicationError(cause)
			if publication.DraftAddAttemptedAt == nil {
				publication.Status, publication.NextCheckAt = model.WechatPublicationStatusUnsupported, nil
			} else if next := nextWechatManualCheck(now, valueOrTime(publication.DraftCreatedAt, publication.CreatedAt)); next != nil {
				publication.Status, publication.NextCheckAt = model.WechatPublicationStatusDrafting, next
			} else {
				publication.Status, publication.NextCheckAt = model.WechatPublicationStatusPublishFailed, nil
				publication.LastError = "WeChat draft reconciliation timed out after 72 hours: " + safeWechatPublicationError(cause)
			}
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
			publication.Status = model.WechatPublicationStatusPublishFailed
			publication.LastError = "微信正式发布状态连续 72 小时无法确认：" + safeWechatPublicationError(cause)
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
	defer s.syncTaskLifecycle(ctx, taskID)
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
	publication.AnalyticsStatus = "official_fetching"
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
