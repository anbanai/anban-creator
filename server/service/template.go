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

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"

	"gorm.io/gorm"
)

// TemplateService handles template business logic.
type TemplateService struct {
	repo   repository.Repository
	logger *zerolog.Logger
}

// NewTemplateService creates a new TemplateService.
func NewTemplateService(repo repository.Repository, logger *zerolog.Logger) *TemplateService {
	return &TemplateService{repo: repo, logger: logger}
}

// Sentinel errors for template operations. Handlers should use errors.Is to
// map these to appropriate HTTP status codes (404 vs 403 vs 500).
var (
	ErrTemplateNotFound    = errors.New("template not found")
	ErrTemplateForbidden   = errors.New("forbidden: not the owner")
	ErrTemplateNameMissing = errors.New("name or style_prompt is required")
)

// ensureTagsNotNil 保证 Tags 字段在 JSON 序列化时输出 [] 而非 null。
// Go 标准 json.Marshal(nil slice) 会输出 null，前端访问 tags.length 会崩。
func ensureTagsNotNil(ts ...*model.Template) {
	for _, t := range ts {
		if t != nil && t.Tags == nil {
			t.Tags = []string{}
		}
	}
}

// deriveNameFromStyle 从 style_prompt 截取第一段并限长 20 rune，作为默认 name。
// 当调用方未提供 name 时使用，前后端语义保持一致。
func deriveNameFromStyle(style string) string {
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

// Create saves a new template to the database. When userID is non-empty the
// template is owned by that user; when empty it is a system template (e.g.
// seeded via MCP) that no end user can modify.
// visibility must be "public" or "private"; an empty value defaults to "public".
func (s *TemplateService) Create(ctx context.Context, tmpl *model.Template, userID string) (*model.Template, error) {
	if tmpl.Name == "" {
		tmpl.Name = deriveNameFromStyle(tmpl.StylePrompt)
	}
	if tmpl.Name == "" {
		return nil, ErrTemplateNameMissing
	}
	if tmpl.Visibility != "public" && tmpl.Visibility != "private" {
		tmpl.Visibility = "public"
	}
	if tmpl.ID == "" {
		tmpl.ID = uuid.NewString()
	}
	tmpl.UserID = userID
	tmpl.IsActive = true
	ensureTagsNotNil(tmpl)

	if err := s.repo.Templates().Create(ctx, tmpl); err != nil {
		return nil, fmt.Errorf("create template: %w", err)
	}
	return tmpl, nil
}

// Update modifies an existing template. Only the owner can update. Returns
// ErrTemplateNotFound if the id does not match a row, or ErrTemplateForbidden
// if userID is not the owner.
func (s *TemplateService) Update(ctx context.Context, id string, userID string, patch *model.Template) (*model.Template, error) {
	existing, err := s.repo.Templates().FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTemplateNotFound
		}
		return nil, fmt.Errorf("find template: %w", err)
	}
	if existing.UserID != userID {
		return nil, ErrTemplateForbidden
	}

	// Apply patch fields.
	if patch.Name != "" {
		existing.Name = patch.Name
	}
	if patch.Type != "" {
		existing.Type = patch.Type
	}
	if patch.ThumbnailURL != "" {
		existing.ThumbnailURL = patch.ThumbnailURL
	}
	existing.StylePrompt = patch.StylePrompt
	if patch.Visibility == "public" || patch.Visibility == "private" {
		existing.Visibility = patch.Visibility
	}
	// Content scaffold fields use "non-empty = set, empty = leave unchanged"
	// (matching Name/Type/ThumbnailURL). This means PATCH cannot CLEAR them — the
	// Studio edit form always round-trips the current values, so clearing in the
	// UI sends "" which is treated as "unchanged". Acceptable for v1.
	if patch.WritingStyle != "" {
		existing.WritingStyle = patch.WritingStyle
	}
	// Author persona (公众号 写作风格). Same "non-empty = set" PATCH rule as the
	// scaffold fields above; the Studio edit form round-trips current values.
	if patch.AuthorName != "" {
		existing.AuthorName = patch.AuthorName
	}
	if patch.AuthorAvatarURL != "" {
		existing.AuthorAvatarURL = patch.AuthorAvatarURL
	}
	if patch.AuthorStyleIntro != "" {
		existing.AuthorStyleIntro = patch.AuthorStyleIntro
	}
	if patch.Category != "" {
		existing.Category = patch.Category
	}
	if len(patch.Tags) > 0 {
		existing.Tags = patch.Tags
	}
	if len(patch.Structure) > 0 {
		existing.Structure = patch.Structure
	}
	if len(patch.ExampleContent) > 0 {
		existing.ExampleContent = patch.ExampleContent
	}

	ensureTagsNotNil(existing)
	if err := s.repo.Templates().Update(ctx, existing); err != nil {
		return nil, fmt.Errorf("update template: %w", err)
	}
	return existing, nil
}

// Delete removes a template. Only the owner can delete. Returns
// ErrTemplateNotFound or ErrTemplateForbidden with the same semantics as Update.
func (s *TemplateService) Delete(ctx context.Context, id string, userID string) error {
	existing, err := s.repo.Templates().FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrTemplateNotFound
		}
		return fmt.Errorf("find template: %w", err)
	}
	if existing.UserID != userID {
		return ErrTemplateForbidden
	}
	if err := s.repo.Templates().Delete(ctx, id); err != nil {
		return fmt.Errorf("delete template: %w", err)
	}
	return nil
}

// GetByID returns a template by its ID. Private templates are only visible to
// their owner; callers should enforce visibility at the handler layer.
func (s *TemplateService) GetByID(ctx context.Context, id string) (*model.Template, error) {
	tmpl, err := s.repo.Templates().FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("template not found: %s", id)
		}
		return nil, fmt.Errorf("find template by id: %w", err)
	}
	ensureTagsNotNil(tmpl)
	return tmpl, nil
}

// List returns paginated templates filtered by type, category, tag, and an
// ownership/visibility scope (all|mine|public). When userID is empty, only
// public templates are returned regardless of scope.
func (s *TemplateService) List(ctx context.Context, templateType, category, tag, userID, scope string, offset, limit int) ([]*model.Template, int64, error) {
	templates, err := s.repo.Templates().List(ctx, templateType, category, tag, userID, scope, offset, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list templates: %w", err)
	}

	total, err := s.repo.Templates().Count(ctx, templateType, category, tag, userID, scope)
	if err != nil {
		return nil, 0, fmt.Errorf("count templates: %w", err)
	}

	ensureTagsNotNil(templates...)
	return templates, total, nil
}

// ListByIDs returns templates matching the given IDs.
func (s *TemplateService) ListByIDs(ctx context.Context, ids []string) ([]*model.Template, error) {
	templates, err := s.repo.Templates().ListByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list templates by ids: %w", err)
	}
	ensureTagsNotNil(templates...)
	return templates, nil
}

// GetRecommended returns recommended templates based on a user's profile category and tags.
// Recommendations only draw from the public template pool.
func (s *TemplateService) GetRecommended(ctx context.Context, profileCategory string, profileTags []string, limit int) ([]*model.Template, error) {
	seen := make(map[string]struct{})
	var results []*model.Template

	if profileCategory != "" {
		categoryTemplates, err := s.repo.Templates().List(ctx, "", profileCategory, "", "", "public", 0, limit)
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
			tagTemplates, err := s.repo.Templates().List(ctx, "", "", tag, "", "public", 0, limit)
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
		templates, err := s.repo.Templates().ListActive(ctx, "")
		if err != nil {
			return nil, fmt.Errorf("list active templates: %w", err)
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
