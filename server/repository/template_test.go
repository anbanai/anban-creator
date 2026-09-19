package repository

import (
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestTemplateBusinessQueriesExcludeNonReadyTemplates(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db).Templates()
	ctx := t.Context()

	rows := []*model.Template{
		{
			ID: "ready-template", Type: model.TemplateTypeSeednote,
			Name: "ready", Category: model.SeednoteTemplateCategoryProduct,
			Visibility: "public", IsActive: true, ReadinessStatus: model.TemplateReadinessReady,
		},
		{
			ID: "analyzing-template", Type: model.TemplateTypeSeednote,
			Name: "analyzing", Category: model.SeednoteTemplateCategoryProduct,
			Visibility: "public", IsActive: true, ReadinessStatus: model.TemplateReadinessAnalyzing,
		},
		{
			ID: "failed-template", Type: model.TemplateTypeSeednote,
			Name: "failed", Category: model.SeednoteTemplateCategoryProduct,
			Visibility: "public", IsActive: true, ReadinessStatus: model.TemplateReadinessFailed,
		},
	}
	for _, row := range rows {
		if err := repo.Create(ctx, row); err != nil {
			t.Fatalf("create template %s: %v", row.ID, err)
		}
	}

	listed, err := repo.List(ctx, model.TemplateTypeSeednote, "", "", "", "public", 0, 20)
	if err != nil {
		t.Fatalf("list public templates: %v", err)
	}
	assertTemplateIDs(t, listed, []string{"ready-template"})

	byID, err := repo.ListByIDs(ctx, []string{"ready-template", "analyzing-template", "failed-template"})
	if err != nil {
		t.Fatalf("list templates by IDs: %v", err)
	}
	assertTemplateIDs(t, byID, []string{"ready-template"})

	active, err := repo.ListActive(ctx, model.TemplateTypeSeednote)
	if err != nil {
		t.Fatalf("list active templates: %v", err)
	}
	assertTemplateIDs(t, active, []string{"ready-template"})
}

func assertTemplateIDs(t *testing.T, templates []*model.Template, want []string) {
	t.Helper()
	if len(templates) != len(want) {
		t.Fatalf("template count = %d, want %d", len(templates), len(want))
	}
	for i, template := range templates {
		if template.ID != want[i] {
			t.Fatalf("template[%d] = %q, want %q", i, template.ID, want[i])
		}
	}
}
