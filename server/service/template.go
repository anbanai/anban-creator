package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"

	"gorm.io/gorm"
)

// TemplateService handles template business logic.
type TemplateService struct {
	repo          repository.Repository
	logger        *zerolog.Logger
	imageAnalyses *ImageAnalysisService
}

// TemplatePatch carries explicit field-presence for template updates. A nil
// pointer means "leave unchanged"; a non-nil pointer, including "", means "set
// to this value".
type TemplatePatch struct {
	Name             *string
	Type             *string
	ThumbnailAssetID *string
	Prompt           *string
	Visibility       *string
	Category         *string
	SortOrder        *int
	IsActive         *bool
}

// NewTemplateService creates a new TemplateService.
func NewTemplateService(repo repository.Repository, logger *zerolog.Logger) *TemplateService {
	return &TemplateService{repo: repo, logger: logger}
}

func (s *TemplateService) SetImageAnalysisService(analyses *ImageAnalysisService) {
	s.imageAnalyses = analyses
}

// Sentinel errors for template operations. Handlers should use errors.Is to
// map these to appropriate HTTP status codes (404 vs 403 vs 500).
var (
	ErrTemplateNotFound        = errors.New("template not found")
	ErrTemplateForbidden       = errors.New("template administration requires admin")
	ErrTemplateNameMissing     = errors.New("template name is required")
	ErrTemplatePromptMissing   = errors.New("template prompt is required")
	ErrTemplateTypeInvalid     = errors.New("template type must be seednote")
	ErrTemplateCategoryInvalid = errors.New("invalid seednote template category")
	ErrTemplateScopeInvalid    = errors.New("invalid template scope")
)

func (s *TemplateService) requireAdmin(ctx context.Context, userID string) error {
	isAdmin, err := s.isAdmin(ctx, userID)
	if err != nil {
		return err
	}
	if !isAdmin {
		return ErrTemplateForbidden
	}
	return nil
}

// AuthorizeAdmin performs the same database-backed authorization used by all
// template mutations. Handlers call it before storage side effects.
func (s *TemplateService) AuthorizeAdmin(ctx context.Context, userID string) error {
	return s.requireAdmin(ctx, userID)
}

func (s *TemplateService) isAdmin(ctx context.Context, userID string) (bool, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false, nil
	}
	user, err := s.repo.Users().FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("find template user: %w", err)
	}
	return user.IsAdmin, nil
}

func validateSeednoteTemplate(tmpl *model.Template) error {
	if tmpl.Type != model.TemplateTypeSeednote {
		return ErrTemplateTypeInvalid
	}
	if !model.IsSeednoteTemplateCategory(tmpl.Category) {
		return ErrTemplateCategoryInvalid
	}
	return nil
}

// ensureTagsNotNil 保证 Tags 字段在 JSON 序列化时输出 [] 而非 null。
// Go 标准 json.Marshal(nil slice) 会输出 null，前端访问 tags.length 会崩。
func ensureTagsNotNil(ts ...*model.Template) {
	for _, t := range ts {
		if t != nil && t.Tags == nil {
			t.Tags = []string{}
		}
	}
}

// deriveNameFromPrompt 从 prompt 截取第一段并限长 20 rune，作为默认 name。
// 当调用方未提供 name 时使用，前后端语义保持一致。
func deriveNameFromPrompt(style string) string {
	for _, sep := range []string{"\n", "。", "，", ",", "."} {
		if i := strings.Index(style, sep); i > 0 {
			style = style[:i]
			break
		}
	}
	style = strings.TrimSpace(style)
	// 去掉开头的维度前缀（如多行格式中的"整体氛围："）
	if idx := strings.IndexAny(style, "：:"); idx > 0 {
		// idx 是 byte index；全角"："占 3 字节，需按 rune 跳过整个匹配字符避免切坏 UTF-8
		_, sz := utf8.DecodeRuneInString(style[idx:])
		style = strings.TrimSpace(style[idx+sz:])
	}
	runes := []rune(style)
	if len(runes) > 20 {
		return string(runes[:20])
	}
	return string(runes)
}

// ResolveTemplateName returns an explicit trimmed name or derives one from the
// visual Prompt. Callers use it before any storage side effects and again at
// the persistence boundary so blank template names cannot be committed.
func ResolveTemplateName(name, prompt string) string {
	name = strings.TrimSpace(name)
	if name != "" {
		return name
	}
	return deriveNameFromPrompt(prompt)
}

// Create saves a new admin-managed template to the database. Visibility must
// be "public" or "private"; an empty value defaults to "public".
func (s *TemplateService) Create(ctx context.Context, tmpl *model.Template, userID string) (*model.Template, error) {
	return s.create(ctx, tmpl, userID, true)
}

// CreateWithActive is the HTTP admin creation path, where is_active is an
// explicit canonical field rather than an omitted-value default.
func (s *TemplateService) CreateWithActive(ctx context.Context, tmpl *model.Template, userID string, isActive bool) (*model.Template, error) {
	return s.create(ctx, tmpl, userID, isActive)
}

func (s *TemplateService) create(ctx context.Context, tmpl *model.Template, userID string, isActive bool) (*model.Template, error) {
	if err := s.requireAdmin(ctx, userID); err != nil {
		return nil, err
	}
	if err := validateSeednoteTemplate(tmpl); err != nil {
		return nil, err
	}
	tmpl.Prompt = strings.TrimSpace(tmpl.Prompt)
	tmpl.Name = ResolveTemplateName(tmpl.Name, tmpl.Prompt)
	if tmpl.Name == "" {
		return nil, ErrTemplateNameMissing
	}
	if strings.TrimSpace(tmpl.Prompt) == "" && strings.TrimSpace(tmpl.ThumbnailAssetID) == "" {
		return nil, ErrTemplatePromptMissing
	}
	if tmpl.Visibility != "public" && tmpl.Visibility != "private" {
		tmpl.Visibility = "public"
	}
	if tmpl.ID == "" {
		tmpl.ID = uuid.NewString()
	}
	prepareTemplateForCreate(tmpl, userID)
	tmpl.ActivateWhenReady = isActive
	if strings.TrimSpace(tmpl.Prompt) != "" {
		tmpl.PromptSource = model.ImageAnalysisSourceManual
		tmpl.ReadinessStatus = model.TemplateReadinessReady
		tmpl.IsActive = isActive
	} else {
		tmpl.ReadinessStatus = model.TemplateReadinessAnalyzing
		tmpl.IsActive = false
	}

	if s.imageAnalyses != nil && tmpl.Prompt == "" {
		job, err := s.imageAnalyses.CreateTemplateWithJob(ctx, tmpl)
		if err != nil {
			return nil, fmt.Errorf("create template: %w", err)
		}
		tmpl.ImageAnalysis = job.View()
		return tmpl, nil
	}
	if err := s.repo.Templates().Create(ctx, tmpl); err != nil {
		return nil, fmt.Errorf("create template: %w", err)
	}
	return tmpl, nil
}

func prepareTemplateForCreate(tmpl *model.Template, userID string) {
	if tmpl.Visibility != "public" && tmpl.Visibility != "private" {
		tmpl.Visibility = "public"
	}
	tmpl.UserID = userID
	tmpl.IsActive = true
	tmpl.Writer = ""
	tmpl.Theme = ""
	tmpl.Author = ""
	tmpl.Structure = nil
	tmpl.ExampleContent = nil
	tmpl.SetEcommerce(model.EcommerceTemplateDefaults{})
	ensureTagsNotNil(tmpl)
}

// Update modifies an existing template through the explicit patch path.
func (s *TemplateService) Update(ctx context.Context, id string, userID string, patch *model.Template) (*model.Template, error) {
	p := TemplatePatch{}
	if patch.Name != "" {
		p.Name = &patch.Name
	}
	if patch.Type != "" {
		p.Type = &patch.Type
	}
	if patch.ThumbnailAssetID != "" {
		p.ThumbnailAssetID = &patch.ThumbnailAssetID
	}
	if patch.Prompt != "" {
		p.Prompt = &patch.Prompt
	}
	if patch.Visibility != "" {
		p.Visibility = &patch.Visibility
	}
	if patch.Category != "" {
		p.Category = &patch.Category
	}
	if patch.SortOrder != 0 {
		p.SortOrder = &patch.SortOrder
	}
	return s.UpdatePatch(ctx, id, userID, p)
}

// UpdatePatch modifies an existing template with explicit PATCH semantics.
func (s *TemplateService) UpdatePatch(ctx context.Context, id string, userID string, patch TemplatePatch) (*model.Template, error) {
	if err := s.requireAdmin(ctx, userID); err != nil {
		return nil, err
	}
	if patch.Type != nil && *patch.Type != model.TemplateTypeSeednote {
		return nil, ErrTemplateTypeInvalid
	}
	if patch.Category != nil && !model.IsSeednoteTemplateCategory(*patch.Category) {
		return nil, ErrTemplateCategoryInvalid
	}
	if patch.Name != nil && strings.TrimSpace(*patch.Name) == "" {
		return nil, ErrTemplateNameMissing
	}
	var existing *model.Template
	var queued *model.ImageAnalysisJob
	err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		current, err := tx.Templates().FindByIDForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if err := validateSeednoteTemplate(current); err != nil {
			return ErrTemplateNotFound
		}
		previousPrompt := current.Prompt
		if patch.Name != nil {
			current.Name = strings.TrimSpace(*patch.Name)
		}
		if patch.Type != nil {
			current.Type = *patch.Type
		}
		thumbnailChanged := false
		if patch.ThumbnailAssetID != nil {
			thumbnailChanged = current.ThumbnailAssetID != *patch.ThumbnailAssetID
			current.ThumbnailAssetID = *patch.ThumbnailAssetID
		}
		promptExplicit := patch.Prompt != nil
		manualPrompt := false
		if patch.Prompt != nil {
			current.Prompt = strings.TrimSpace(*patch.Prompt)
			manualPrompt = current.Prompt != ""
		}
		if patch.Visibility != nil && (*patch.Visibility == "public" || *patch.Visibility == "private") {
			current.Visibility = *patch.Visibility
		}
		if patch.Category != nil {
			current.Category = *patch.Category
		}
		if patch.SortOrder != nil {
			current.SortOrder = *patch.SortOrder
		}
		if patch.IsActive != nil {
			current.ActivateWhenReady = *patch.IsActive
			if current.ReadinessStatus == model.TemplateReadinessReady {
				current.IsActive = *patch.IsActive
			}
		}
		generatedPromptFollowsThumbnail := thumbnailChanged && !promptExplicit && current.PromptSource == model.ImageAnalysisSourceAnalysis
		startAnalysis := strings.TrimSpace(current.ThumbnailAssetID) != "" &&
			((strings.TrimSpace(current.Prompt) == "" && (promptExplicit || thumbnailChanged)) || generatedPromptFollowsThumbnail)
		if strings.TrimSpace(current.Prompt) == "" && strings.TrimSpace(current.ThumbnailAssetID) == "" {
			return ErrTemplatePromptMissing
		}
		if s.imageAnalyses != nil && manualPrompt {
			current.PromptSource = model.ImageAnalysisSourceManual
			current.ReadinessStatus = model.TemplateReadinessReady
			current.IsActive = current.ActivateWhenReady
			if err := supersedeImageAnalysisTx(ctx, tx, model.ImageAnalysisSubjectTemplate, current.ID, model.ImageAnalysisKindTemplatePrompt); err != nil {
				return err
			}
		}
		if s.imageAnalyses != nil && startAnalysis {
			current.Prompt = ""
			current.PromptSource = ""
			current.ReadinessStatus = model.TemplateReadinessAnalyzing
			current.IsActive = false
			queued, err = s.imageAnalyses.upsertJobTx(ctx, tx, current.UserID, model.ImageAnalysisSubjectTemplate, current.ID, model.ImageAnalysisKindTemplatePrompt, current.ThumbnailAssetID, previousPrompt)
			if err != nil {
				return err
			}
		}
		ensureTagsNotNil(current)
		if err := tx.Templates().Update(ctx, current); err != nil {
			return err
		}
		existing = current
		return nil
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTemplateNotFound
		}
		return nil, fmt.Errorf("update template: %w", err)
	}
	if s.imageAnalyses != nil {
		s.imageAnalyses.enqueue(ctx, queued)
		if queued != nil {
			existing.ImageAnalysis = queued.View()
		} else if err := s.imageAnalyses.PresentTemplate(ctx, existing); err != nil {
			return nil, fmt.Errorf("present template image analysis: %w", err)
		}
	}
	return existing, nil
}

// Delete removes a template after database-backed admin authorization.
func (s *TemplateService) Delete(ctx context.Context, id string, userID string) error {
	if err := s.requireAdmin(ctx, userID); err != nil {
		return err
	}
	existing, err := s.repo.Templates().FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrTemplateNotFound
		}
		return fmt.Errorf("find template: %w", err)
	}
	if err := validateSeednoteTemplate(existing); err != nil {
		return ErrTemplateNotFound
	}
	if err := s.repo.Templates().Delete(ctx, id); err != nil {
		return fmt.Errorf("delete template: %w", err)
	}
	return nil
}

// GetByID returns any template to admins and only active public templates to
// ordinary callers.
func (s *TemplateService) GetByID(ctx context.Context, id, userID string) (*model.Template, error) {
	tmpl, err := s.repo.Templates().FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("template not found: %s", id)
		}
		return nil, fmt.Errorf("find template by id: %w", err)
	}
	if err := validateSeednoteTemplate(tmpl); err != nil {
		return nil, ErrTemplateNotFound
	}
	isAdmin, err := s.isAdmin(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !isAdmin && (!tmpl.IsActive || tmpl.ReadinessStatus != model.TemplateReadinessReady || tmpl.Visibility != "public") {
		return nil, ErrTemplateNotFound
	}
	ensureTagsNotNil(tmpl)
	if s.imageAnalyses != nil {
		if err := s.imageAnalyses.PresentTemplate(ctx, tmpl); err != nil {
			return nil, err
		}
	}
	return tmpl, nil
}

// List returns paginated templates filtered by type, category, and tag.
// Admins may select all|public|private|inactive. Ordinary and anonymous callers
// always receive active public templates regardless of the requested scope.
func (s *TemplateService) List(ctx context.Context, templateType, category, tag, userID, scope string, offset, limit int) ([]*model.Template, int64, error) {
	if templateType != "" && templateType != model.TemplateTypeSeednote {
		return []*model.Template{}, 0, nil
	}
	templateType = model.TemplateTypeSeednote
	repositoryScope := "public"
	if strings.TrimSpace(userID) != "" {
		isAdmin, err := s.isAdmin(ctx, userID)
		if err != nil {
			return nil, 0, err
		}
		if isAdmin {
			switch scope {
			case "all", "public", "private", "inactive":
				repositoryScope = scope
			default:
				return nil, 0, ErrTemplateScopeInvalid
			}
		}
	}

	templates, err := s.repo.Templates().List(ctx, templateType, category, tag, userID, repositoryScope, offset, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list templates: %w", err)
	}

	total, err := s.repo.Templates().Count(ctx, templateType, category, tag, userID, repositoryScope)
	if err != nil {
		return nil, 0, fmt.Errorf("count templates: %w", err)
	}

	ensureTagsNotNil(templates...)
	if s.imageAnalyses != nil {
		for _, tmpl := range templates {
			if err := s.imageAnalyses.PresentTemplate(ctx, tmpl); err != nil {
				return nil, 0, err
			}
		}
	}
	return templates, total, nil
}

// ListByIDs returns templates matching the given IDs.
func (s *TemplateService) ListByIDs(ctx context.Context, ids []string) ([]*model.Template, error) {
	templates, err := s.repo.Templates().ListByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list templates by ids: %w", err)
	}
	seednoteTemplates := templates[:0]
	for _, tmpl := range templates {
		if validateSeednoteTemplate(tmpl) == nil {
			seednoteTemplates = append(seednoteTemplates, tmpl)
		}
	}
	ensureTagsNotNil(seednoteTemplates...)
	return seednoteTemplates, nil
}

// GetRecommended returns recommended templates based on a user's profile category and tags.
// Recommendations only draw from the public template pool.
func (s *TemplateService) GetRecommended(ctx context.Context, profileCategory string, profileTags []string, limit int) ([]*model.Template, error) {
	seen := make(map[string]struct{})
	var results []*model.Template

	if profileCategory != "" {
		categoryTemplates, err := s.repo.Templates().List(ctx, model.TemplateTypeSeednote, profileCategory, "", "", "public", 0, limit)
		if err != nil {
			return nil, fmt.Errorf("list templates by category %s: %w", profileCategory, err)
		}
		for _, t := range categoryTemplates {
			if _, exists := seen[t.ID]; !exists {
				seen[t.ID] = struct{}{}
				results = append(results, t)
			}
		}
	}

	if len(profileTags) > 0 {
		for _, tag := range profileTags {
			tagTemplates, err := s.repo.Templates().List(ctx, model.TemplateTypeSeednote, "", tag, "", "public", 0, limit)
			if err != nil {
				return nil, fmt.Errorf("list templates by tag %s: %w", tag, err)
			}
			for _, t := range tagTemplates {
				if _, exists := seen[t.ID]; !exists {
					seen[t.ID] = struct{}{}
					results = append(results, t)
				}
			}
		}
	}

	if profileCategory == "" && len(profileTags) == 0 {
		templates, err := s.repo.Templates().List(ctx, model.TemplateTypeSeednote, "", "", "", "public", 0, limit)
		if err != nil {
			return nil, fmt.Errorf("list public templates: %w", err)
		}
		results = templates
	}

	// Sort by SortOrder descending.
	sort.Slice(results, func(i, j int) bool {
		return results[i].SortOrder > results[j].SortOrder
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	ensureTagsNotNil(results...)
	return results, nil
}
