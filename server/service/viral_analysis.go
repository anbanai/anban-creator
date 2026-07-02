package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/platform"
	"github.com/anbanai/anban-creator/server/repository"
)

const ViralAnalysisTaskType = "viral:analyze"

var evidenceDrivenDimensionNames = []string{
	"topic_angle",
	"title",
	"cover",
	"body",
	"interaction",
	"tags",
	"comment_signals",
}

// EvidenceDrivenAnalysisResult is the strict schema saved in viral_analyses.analysis_result.
type EvidenceDrivenAnalysisResult struct {
	Summary               []string            `json:"summary"`
	EvidenceTable         []EvidenceItem      `json:"evidence_table"`
	Dimensions            []AnalysisDimension `json:"dimensions"`
	CloneSuggestions      CloneSuggestions    `json:"clone_suggestions"`
	Risks                 []string            `json:"risks"`
	RecommendedCloneDepth string              `json:"recommended_clone_depth"`
	OverallScore          ScoreResult         `json:"overall_score"`
	ViralTemplate         ViralTemplate       `json:"viral_template"`
	TemplateMeta          TemplateMeta        `json:"template_meta"`
}

type EvidenceItem struct {
	Claim    string `json:"claim"`
	Evidence string `json:"evidence"`
	Source   string `json:"source"`
}

type AnalysisDimension struct {
	Name            string `json:"name"`
	Observation     string `json:"observation"`
	Mechanism       string `json:"mechanism"`
	Transferability string `json:"transferability"`
	Action          string `json:"action"`
}

type CloneSuggestions struct {
	Title       []string `json:"title"`
	Body        []string `json:"body"`
	Cover       []string `json:"cover"`
	Tags        []string `json:"tags"`
	Interaction []string `json:"interaction"`
}

type ScoreResult struct {
	Score         int      `json:"score"`
	Confidence    string   `json:"confidence"`
	EvidenceCount int      `json:"evidence_count"`
	MissingData   []string `json:"missing_data"`
	WhyNotHigher  string   `json:"why_not_higher"`
}

type ViralTemplate struct {
	TitleTemplate         string   `json:"title_template"`
	CoverTemplate         string   `json:"cover_template"`
	BodyTemplate          string   `json:"body_template"`
	InteractionTemplate   string   `json:"interaction_template"`
	TagTemplate           string   `json:"tag_template"`
	AudienceInsight       string   `json:"audience_insight"`
	ViralMechanism        string   `json:"viral_mechanism"`
	RewriteConstraints    []string `json:"rewrite_constraints"`
	DoNotCopy             []string `json:"do_not_copy"`
	RecommendedCloneDepth string   `json:"recommended_clone_depth"`
	Confidence            string   `json:"confidence"`
}

type TemplateMeta struct {
	Type         string   `json:"type"`
	Name         string   `json:"name"`
	Category     string   `json:"category"`
	SourceFeedID string   `json:"source_feed_id"`
	SourceURL    string   `json:"source_url"`
	Tags         []string `json:"tags"`
	TemplateHash string   `json:"template_hash"`
	SaveEligible bool     `json:"save_eligible"`
}

// ViralNoteFetcher abstracts fetching Seednote note content.
type ViralNoteFetcher interface {
	FetchNoteContent(ctx context.Context, noteURL string) (*platform.SeednoteNoteContent, error)
}

// ViralAnalysisService handles viral content analysis business logic.
type ViralAnalysisService struct {
	repo      repository.Repository
	fetcher   ViralNoteFetcher
	llm       LLMClient
	enqueuer  TaskEnqueuer
	creditSvc *CreditService
	logger    *zerolog.Logger
}

// NewViralAnalysisService creates a new ViralAnalysisService.
func NewViralAnalysisService(repo repository.Repository, fetcher ViralNoteFetcher, llm LLMClient, enqueuer TaskEnqueuer, logger *zerolog.Logger) *ViralAnalysisService {
	return &ViralAnalysisService{
		repo:     repo,
		fetcher:  fetcher,
		llm:      llm,
		enqueuer: enqueuer,
		logger:   logger,
	}
}

// SetCreditService enables credit deduction/refund for analysis jobs.
func (s *ViralAnalysisService) SetCreditService(creditSvc *CreditService) {
	s.creditSvc = creditSvc
}

// Create creates a new viral analysis record and enqueues it for execution.
func (s *ViralAnalysisService) Create(ctx context.Context, userID, sourceType, sourceURL string) (*model.ViralAnalysis, error) {
	analysis := &model.ViralAnalysis{
		ID:         uuid.New().String(),
		UserID:     userID,
		SourceType: sourceType,
		SourceURL:  sourceURL,
		Status:     "pending",
	}

	if s.creditSvc != nil {
		if _, err := s.creditSvc.DeductForTask(ctx, userID, model.CreditTypeViralAnalysis, analysis.ID); err != nil {
			return nil, fmt.Errorf("deduct credits: %w", err)
		}
	}

	if err := s.repo.ViralAnalyses().Create(ctx, analysis); err != nil {
		if s.creditSvc != nil {
			if refundErr := s.creditSvc.RefundForTask(ctx, analysis.ID); refundErr != nil {
				s.logger.Error().Err(refundErr).Str("analysis_id", analysis.ID).Msg("failed to refund credits during viral analysis rollback")
			}
		}
		return nil, fmt.Errorf("create viral analysis: %w", err)
	}

	s.logger.Info().
		Str("analysis_id", analysis.ID).
		Str("user_id", userID).
		Str("source_type", sourceType).
		Msg("viral analysis created")

	if s.enqueuer != nil {
		payload, err := json.Marshal(map[string]string{"analysis_id": analysis.ID})
		if err != nil {
			return nil, fmt.Errorf("marshal payload: %w", err)
		}
		if err := s.enqueuer.Enqueue(ViralAnalysisTaskType, payload); err != nil {
			s.logger.Warn().Err(err).Str("analysis_id", analysis.ID).Msg("failed to enqueue viral analysis")
			errMsg := "failed to enqueue viral analysis: " + err.Error()
			if failErr := s.FailAnalysis(ctx, analysis.ID, errMsg); failErr != nil {
				s.logger.Error().Err(failErr).Str("analysis_id", analysis.ID).Msg("failed to mark viral analysis as failed after enqueue error")
			}
			analysis.Status = "failed"
			analysis.ErrorMessage = errMsg
		}
	} else {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					s.logger.Error().
						Str("analysis_id", analysis.ID).
						Interface("panic", r).
						Msg("panic recovered in viral analysis goroutine")
				}
			}()
			fallbackCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			if err := s.ExecuteAnalysis(fallbackCtx, analysis.ID); err != nil {
				s.logger.Error().Err(err).Str("analysis_id", analysis.ID).Msg("fallback viral analysis failed")
			}
		}()
	}

	return analysis, nil
}

// GetByID returns a viral analysis by ID, verifying ownership.
func (s *ViralAnalysisService) GetByID(ctx context.Context, id, userID string) (*model.ViralAnalysis, error) {
	analysis, err := s.repo.ViralAnalyses().FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("viral analysis not found: %s", id)
	}

	if analysis.UserID != userID {
		return nil, fmt.Errorf("viral analysis does not belong to user: %s", id)
	}

	return analysis, nil
}

// ListByUserID returns paginated viral analyses for a user.
func (s *ViralAnalysisService) ListByUserID(ctx context.Context, userID string, offset, limit int) ([]*model.ViralAnalysis, int64, error) {
	analyses, err := s.repo.ViralAnalyses().FindByUserID(ctx, userID, offset, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list viral analyses: %w", err)
	}

	total, err := s.repo.ViralAnalyses().CountByUserID(ctx, userID)
	if err != nil {
		return nil, 0, fmt.Errorf("count viral analyses: %w", err)
	}

	return analyses, total, nil
}

// ExecuteAnalysis runs the full viral analysis pipeline.
func (s *ViralAnalysisService) ExecuteAnalysis(ctx context.Context, analysisID string) error {
	analysis, err := s.repo.ViralAnalyses().FindByID(ctx, analysisID)
	if err != nil {
		return fmt.Errorf("find analysis: %w", err)
	}

	if analysis.Status != "pending" {
		s.logger.Info().Str("analysis_id", analysisID).Str("status", analysis.Status).Msg("analysis not in pending state, skipping")
		return nil
	}

	if err := s.StartAnalysis(ctx, analysisID); err != nil {
		return fmt.Errorf("start analysis: %w", err)
	}

	if s.fetcher == nil {
		return s.FailAnalysis(ctx, analysisID, "note fetcher unavailable")
	}

	content, err := s.fetcher.FetchNoteContent(ctx, analysis.SourceURL)
	if err != nil {
		return s.FailAnalysis(ctx, analysisID, fmt.Sprintf("fetch note content: %v", err))
	}
	if content == nil {
		return s.FailAnalysis(ctx, analysisID, "fetch note content: empty response")
	}

	// Store fetched source data.
	sourceData, err := json.Marshal(content)
	if err != nil {
		s.logger.Warn().Err(err).Str("analysis_id", analysisID).Msg("failed to marshal source data")
	} else {
		if err := s.repo.ViralAnalyses().UpdateSourceData(ctx, analysisID, sourceData); err != nil {
			s.logger.Warn().Err(err).Str("analysis_id", analysisID).Msg("failed to store source data")
		}
	}

	if s.llm == nil {
		return s.FailAnalysis(ctx, analysisID, "AI analysis service unavailable")
	}

	result, err := s.analyzeWithLLM(ctx, content, analysis.SourceURL)
	if err != nil {
		return s.FailAnalysis(ctx, analysisID, fmt.Sprintf("AI analysis failed: %v", err))
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return s.FailAnalysis(ctx, analysisID, fmt.Sprintf("marshal result: %v", err))
	}

	return s.CompleteAnalysis(ctx, analysisID, resultJSON)
}

func (s *ViralAnalysisService) analyzeWithLLM(ctx context.Context, content *platform.SeednoteNoteContent, sourceURL ...string) (json.RawMessage, error) {
	var srcURL string
	if len(sourceURL) > 0 {
		srcURL = strings.TrimSpace(sourceURL[0])
	}
	noteData, err := json.Marshal(map[string]any{
		"source_url": srcURL,
		"note":       content,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal note content: %w", err)
	}

	systemPrompt := buildEvidenceDrivenPrompt()

	userPrompt := string(noteData)

	resp, err := s.llm.Complete(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	jsonText, err := extractJSONResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("extract JSON from LLM response: %w", err)
	}

	var result EvidenceDrivenAnalysisResult
	if err := json.Unmarshal([]byte(jsonText), &result); err != nil {
		return nil, fmt.Errorf("parse LLM response as JSON: %w", err)
	}

	deriveTemplateMeta(&result, content, srcURL)
	if err := ValidateEvidenceDrivenResult(&result); err != nil {
		return nil, fmt.Errorf("validate evidence-driven result: %w", err)
	}

	normalized, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal validated result: %w", err)
	}

	return normalized, nil
}

func buildEvidenceDrivenPrompt() string {
	return `你是证据驱动的种草笔记爆款拆解专家。你的任务不是主观打分，而是解释“为什么火、我怎么用”。

核心原则：
1. 每个结论必须绑定可观察依据：原文片段、标题词、标签、封面 URL/描述、互动数据或评论信号。
2. 不预测未来爆款，只解释已发生的高互动表现及其可迁移动作。
3. 不鼓励照搬，必须区分可复用结构和源作者专属经历。
4. 如果缺少评论、发布时间、封面细节等数据，必须降低 confidence，并写入 missing_data。
5. 只输出 JSON。不要返回 Markdown、代码围栏或额外说明。

必须分析 7 个维度，dimensions 必须是数组且 name 必须严格使用：
- topic_angle：选题角度，是否命中强需求、热点、痛点或人群身份。
- title：标题，句式、关键词、情绪词、信息缺口。
- cover：封面，主视觉、文字层级、点击钩子、风格差异化。
- body：正文，开头钩子、信息密度、段落节奏、收藏理由。
- interaction：互动，评论触发点、争议点、低门槛参与机制。
- tags：标签，大词、垂直词、长尾词组合。
- comment_signals：评论信号，用户真实需求、反对意见、追问、二创空间。

每个维度只包含四段：
- observation：观察到什么，必须绑定原文片段、图片描述或数据。
- mechanism：为什么这些元素可能带来点击、收藏、评论或关注。
- transferability：high / medium / low。
- action：给当前用户的下一步建议。

评分只放在 overall_score 对象，不给裸分：
- score：0-100。
- confidence：high / medium / low。
- evidence_count：支撑证据数量。
- missing_data：缺失数据数组，例如无评论、无发布时间、无封面详情。
- why_not_higher：为什么没给更高分。

输出 JSON 格式必须完全匹配：
{
  "summary": ["3-5 条高价值结论"],
  "evidence_table": [
    {"claim": "核心结论", "evidence": "源内容证据", "source": "title|cover|body|tags|metrics|comments"}
  ],
  "dimensions": [
    {"name": "topic_angle", "observation": "", "mechanism": "", "transferability": "high|medium|low", "action": ""},
    {"name": "title", "observation": "", "mechanism": "", "transferability": "high|medium|low", "action": ""},
    {"name": "cover", "observation": "", "mechanism": "", "transferability": "high|medium|low", "action": ""},
    {"name": "body", "observation": "", "mechanism": "", "transferability": "high|medium|low", "action": ""},
    {"name": "interaction", "observation": "", "mechanism": "", "transferability": "high|medium|low", "action": ""},
    {"name": "tags", "observation": "", "mechanism": "", "transferability": "high|medium|low", "action": ""},
    {"name": "comment_signals", "observation": "", "mechanism": "", "transferability": "high|medium|low", "action": ""}
  ],
  "clone_suggestions": {
    "title": ["1-3 条标题动作"],
    "body": ["1-3 条正文动作"],
    "cover": ["1-3 条封面动作"],
    "tags": ["1-3 条标签动作"],
    "interaction": ["1-3 条互动动作"]
  },
  "risks": ["相似度、违规词、诱导互动、源作者专属经历等风险"],
  "recommended_clone_depth": "style-only|medium|tight",
  "overall_score": {
    "score": 0,
    "confidence": "high|medium|low",
    "evidence_count": 0,
    "missing_data": [],
    "why_not_higher": ""
  },
  "viral_template": {
    "title_template": "",
    "cover_template": "",
    "body_template": "",
    "interaction_template": "",
    "tag_template": "",
    "audience_insight": "",
    "viral_mechanism": "",
    "rewrite_constraints": [],
    "do_not_copy": [],
    "recommended_clone_depth": "style-only|medium|tight",
    "confidence": "high|medium|low"
  },
  "template_meta": {
    "type": "seednote",
    "name": "",
    "category": "viral_analysis",
    "source_feed_id": "",
    "source_url": "",
    "tags": [],
    "template_hash": "",
    "save_eligible": true
  }
}

recommended_clone_depth 默认 style-only。只有结构高度可迁移且风险低时，才推荐 medium 或 tight。`
}

func ValidateEvidenceDrivenResult(result *EvidenceDrivenAnalysisResult) error {
	if result == nil {
		return fmt.Errorf("result is nil")
	}
	if len(result.Summary) < 3 || len(result.Summary) > 5 {
		return fmt.Errorf("summary must contain 3-5 conclusions")
	}
	for i, item := range result.Summary {
		if strings.TrimSpace(item) == "" {
			return fmt.Errorf("summary[%d] is empty", i)
		}
	}
	if len(result.EvidenceTable) == 0 {
		return fmt.Errorf("evidence_table is required")
	}
	for i, item := range result.EvidenceTable {
		if strings.TrimSpace(item.Claim) == "" || strings.TrimSpace(item.Evidence) == "" || strings.TrimSpace(item.Source) == "" {
			return fmt.Errorf("evidence_table[%d] must include claim, evidence, and source", i)
		}
	}
	if err := validateDimensions(result.Dimensions); err != nil {
		return err
	}
	if err := validateScore(result.OverallScore); err != nil {
		return err
	}
	if err := validateCloneDepth(result.RecommendedCloneDepth, "recommended_clone_depth"); err != nil {
		return err
	}
	if err := validateViralTemplate(result.ViralTemplate); err != nil {
		return err
	}
	if result.TemplateMeta.Type != "seednote" {
		return fmt.Errorf("template_meta.type must be seednote")
	}
	if result.TemplateMeta.Category != "viral_analysis" {
		return fmt.Errorf("template_meta.category must be viral_analysis")
	}
	if strings.TrimSpace(result.TemplateMeta.Name) == "" {
		return fmt.Errorf("template_meta.name is required")
	}
	if strings.TrimSpace(result.TemplateMeta.SourceFeedID) == "" {
		return fmt.Errorf("template_meta.source_feed_id is required")
	}
	if strings.TrimSpace(result.TemplateMeta.SourceURL) == "" {
		return fmt.Errorf("template_meta.source_url is required")
	}
	if strings.TrimSpace(result.TemplateMeta.TemplateHash) == "" {
		return fmt.Errorf("template_meta.template_hash is required")
	}
	return nil
}

func validateDimensions(dimensions []AnalysisDimension) error {
	seen := make(map[string]AnalysisDimension, len(dimensions))
	for i, dim := range dimensions {
		name := strings.TrimSpace(dim.Name)
		if name == "" {
			return fmt.Errorf("dimensions[%d].name is required", i)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("duplicate dimension %q", name)
		}
		seen[name] = dim
		if strings.TrimSpace(dim.Observation) == "" {
			return fmt.Errorf("dimension %q observation is required", name)
		}
		if !hasEvidenceSignal(dim.Observation) {
			return fmt.Errorf("dimension %q observation must reference observable evidence", name)
		}
		if strings.TrimSpace(dim.Mechanism) == "" {
			return fmt.Errorf("dimension %q mechanism is required", name)
		}
		if err := validateConfidenceLike(dim.Transferability, "dimension "+name+" transferability"); err != nil {
			return err
		}
		if strings.TrimSpace(dim.Action) == "" {
			return fmt.Errorf("dimension %q action is required", name)
		}
	}
	for _, name := range evidenceDrivenDimensionNames {
		if _, ok := seen[name]; !ok {
			return fmt.Errorf("missing required dimension %q", name)
		}
	}
	if len(dimensions) != len(evidenceDrivenDimensionNames) {
		return fmt.Errorf("dimensions must contain exactly %d items", len(evidenceDrivenDimensionNames))
	}
	return nil
}

func hasEvidenceSignal(observation string) bool {
	signals := []string{"原文", "片段", "标题", "封面", "互动", "评论", "标签", "数据", "http", "count", "点赞", "收藏", "转发", "分享", "正文"}
	for _, signal := range signals {
		if strings.Contains(observation, signal) {
			return true
		}
	}
	return false
}

func validateScore(score ScoreResult) error {
	if score.Score < 0 || score.Score > 100 {
		return fmt.Errorf("overall_score.score must be between 0 and 100")
	}
	if err := validateConfidenceLike(score.Confidence, "overall_score.confidence"); err != nil {
		return err
	}
	if score.EvidenceCount <= 0 {
		return fmt.Errorf("overall_score.evidence_count must be positive")
	}
	if strings.TrimSpace(score.WhyNotHigher) == "" {
		return fmt.Errorf("overall_score.why_not_higher is required")
	}
	return nil
}

func validateViralTemplate(template ViralTemplate) error {
	required := map[string]string{
		"title_template":       template.TitleTemplate,
		"cover_template":       template.CoverTemplate,
		"body_template":        template.BodyTemplate,
		"interaction_template": template.InteractionTemplate,
		"tag_template":         template.TagTemplate,
		"audience_insight":     template.AudienceInsight,
		"viral_mechanism":      template.ViralMechanism,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("viral_template.%s is required", field)
		}
	}
	if len(template.RewriteConstraints) == 0 {
		return fmt.Errorf("viral_template.rewrite_constraints is required")
	}
	if len(template.DoNotCopy) == 0 {
		return fmt.Errorf("viral_template.do_not_copy is required")
	}
	if err := validateCloneDepth(template.RecommendedCloneDepth, "viral_template.recommended_clone_depth"); err != nil {
		return err
	}
	if err := validateConfidenceLike(template.Confidence, "viral_template.confidence"); err != nil {
		return err
	}
	return nil
}

func validateCloneDepth(value, field string) error {
	switch value {
	case "style-only", "medium", "tight":
		return nil
	default:
		return fmt.Errorf("%s must be style-only, medium, or tight", field)
	}
}

func validateConfidenceLike(value, field string) error {
	switch value {
	case "high", "medium", "low":
		return nil
	default:
		return fmt.Errorf("%s must be high, medium, or low", field)
	}
}

func deriveTemplateMeta(result *EvidenceDrivenAnalysisResult, content *platform.SeednoteNoteContent, sourceURL string) {
	result.TemplateMeta.Type = "seednote"
	result.TemplateMeta.Category = "viral_analysis"
	result.TemplateMeta.SourceFeedID = sourceFeedID(content.NoteID, sourceURL)
	result.TemplateMeta.SourceURL = strings.TrimSpace(sourceURL)
	result.TemplateMeta.TemplateHash = viralTemplateHash(result.ViralTemplate)
	result.TemplateMeta.SaveEligible = true
	if strings.TrimSpace(result.TemplateMeta.Name) == "" {
		result.TemplateMeta.Name = templateNameFromTitle(content.Title)
	}
	if len(result.TemplateMeta.Tags) == 0 {
		result.TemplateMeta.Tags = splitSeednoteTags(content.Tags)
	}
}

func splitSeednoteTags(tags string) []string {
	parts := strings.FieldsFunc(tags, func(r rune) bool {
		return r == ',' || r == '，' || r == '#' || r == ' ' || r == '\n' || r == '\t'
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func templateNameFromTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "爆文拆解模板"
	}
	runes := []rune(title)
	if len(runes) > 24 {
		title = string(runes[:24])
	}
	return title + "模板"
}

func viralTemplateHash(template ViralTemplate) string {
	data, err := json.Marshal(template)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:])
}

func sourceFeedID(noteID, sourceURL string) string {
	if strings.TrimSpace(noteID) != "" {
		return strings.TrimSpace(noteID)
	}
	normalized := normalizeSourceURL(sourceURL)
	if normalized == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("%x", sum[:])
}

func normalizeSourceURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	if parsed.Scheme != "" {
		parsed.Scheme = strings.ToLower(parsed.Scheme)
	}
	if parsed.Host != "" {
		parsed.Host = strings.ToLower(parsed.Host)
	}
	parsed.Fragment = ""
	return parsed.String()
}

// extractJSONResponse returns the first complete top-level JSON object in an LLM response.
func extractJSONResponse(s string) (string, error) {
	s = trimCodeFences(s)
	if s == "" {
		return "", fmt.Errorf("empty response")
	}
	if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
		return s, nil
	}

	start := -1
	depth := 0
	inString := false
	escaped := false
	for i, r := range s {
		if start == -1 {
			if r == '{' {
				start = i
				depth = 1
			}
			continue
		}
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch r {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		switch r {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return strings.TrimSpace(s[start : i+1]), nil
			}
		}
	}
	return "", fmt.Errorf("no complete JSON object found")
}

// trimCodeFences removes markdown ```json ... ``` wrappers from LLM output.
func trimCodeFences(s string) string {
	if len(s) >= 7 && s[:7] == "```json" {
		s = s[7:]
	}
	if len(s) >= 3 && s[:3] == "```" {
		s = s[3:]
	}
	if len(s) >= 3 && s[len(s)-3:] == "```" {
		s = s[:len(s)-3]
	}
	return strings.TrimSpace(s)
}

// StartAnalysis sets the viral analysis status to "analyzing".
func (s *ViralAnalysisService) StartAnalysis(ctx context.Context, id string) error {
	if err := s.repo.ViralAnalyses().UpdateStatus(ctx, id, "analyzing"); err != nil {
		return fmt.Errorf("start analysis: %w", err)
	}

	s.logger.Info().Str("analysis_id", id).Msg("viral analysis started")
	return nil
}

// CompleteAnalysis stores the analysis result and sets status to "completed".
func (s *ViralAnalysisService) CompleteAnalysis(ctx context.Context, id string, result json.RawMessage) error {
	if err := s.repo.ViralAnalyses().UpdateResult(ctx, id, result); err != nil {
		return fmt.Errorf("update analysis result: %w", err)
	}
	if err := s.repo.ViralAnalyses().UpdateStatus(ctx, id, "completed"); err != nil {
		return fmt.Errorf("complete analysis: %w", err)
	}

	s.logger.Info().Str("analysis_id", id).Msg("viral analysis completed")
	return nil
}

// FailAnalysis sets the viral analysis status to "failed" with an error message.
func (s *ViralAnalysisService) FailAnalysis(ctx context.Context, id, errMsg string) error {
	if err := s.repo.ViralAnalyses().UpdateStatusAndError(ctx, id, "failed", errMsg); err != nil {
		return fmt.Errorf("fail analysis: %w", err)
	}

	if s.creditSvc != nil {
		if refundErr := s.creditSvc.RefundForTask(ctx, id); refundErr != nil {
			s.logger.Error().Err(refundErr).Str("analysis_id", id).Msg("failed to refund credits for failed viral analysis")
		}
	}

	s.logger.Warn().Str("analysis_id", id).Str("error", errMsg).Msg("viral analysis failed")
	return nil
}

// CleanupOldCompleted deletes viral analyses completed more than 90 days ago.
func (s *ViralAnalysisService) CleanupOldCompleted(ctx context.Context) error {
	analyses, err := s.repo.ViralAnalyses().FindCompletedOlderThan(ctx, time.Now().AddDate(0, 0, -90))
	if err != nil {
		return fmt.Errorf("find old viral analyses: %w", err)
	}
	for _, a := range analyses {
		s.logger.Info().Str("analysis_id", a.ID).Msg("cleaning up old viral analysis")
		if err := s.repo.ViralAnalyses().Delete(ctx, a.ID); err != nil {
			s.logger.Warn().Err(err).Str("analysis_id", a.ID).Msg("failed to delete old viral analysis")
		}
	}
	return nil
}
