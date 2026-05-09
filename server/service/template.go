package service

import (
	"context"
	"errors"
	"fmt"
	"sort"

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

// Create saves a new template to the database.
func (s *TemplateService) Create(ctx context.Context, tmpl *model.Template) (*model.Template, error) {
	if err := s.repo.Templates().Create(ctx, tmpl); err != nil {
		return nil, fmt.Errorf("create template: %w", err)
	}
	return tmpl, nil
}

// GetByID returns a template by its ID.
func (s *TemplateService) GetByID(ctx context.Context, id string) (*model.Template, error) {
	tmpl, err := s.repo.Templates().FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("template not found: %s", id)
		}
		return nil, fmt.Errorf("find template by id: %w", err)
	}
	return tmpl, nil
}

// List returns paginated templates filtered by type, category, and tag.
func (s *TemplateService) List(ctx context.Context, templateType, category, tag string, offset, limit int) ([]*model.Template, int64, error) {
	templates, err := s.repo.Templates().List(ctx, templateType, category, tag, offset, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list templates: %w", err)
	}

	total, err := s.repo.Templates().Count(ctx, templateType, category, tag)
	if err != nil {
		return nil, 0, fmt.Errorf("count templates: %w", err)
	}

	return templates, total, nil
}

// ListByIDs returns templates matching the given IDs.
func (s *TemplateService) ListByIDs(ctx context.Context, ids []string) ([]*model.Template, error) {
	templates, err := s.repo.Templates().ListByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list templates by ids: %w", err)
	}
	return templates, nil
}

// GetRecommended returns recommended templates based on a user's profile category and tags.
func (s *TemplateService) GetRecommended(ctx context.Context, profileCategory string, profileTags []string, limit int) ([]*model.Template, error) {
	seen := make(map[string]struct{})
	var results []*model.Template

	if profileCategory != "" {
		categoryTemplates, err := s.repo.Templates().List(ctx, "", profileCategory, "", 0, limit)
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
			tagTemplates, err := s.repo.Templates().List(ctx, "", "", tag, 0, limit)
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

	return results, nil
}
