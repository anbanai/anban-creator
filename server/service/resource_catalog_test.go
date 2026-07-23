package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/resources"
)

func TestResourceCatalogProjectsCategoryFieldsAndRaw(t *testing.T) {
	svc := NewResourceCatalogService(resources.Manager())
	result, err := svc.Query(ResourceCatalogRequest{
		Category: "article_templates", Name: "long-form-essay", IncludeRaw: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	item := result.(map[string]any)
	if item["article_type"] != "long-form-essay" {
		t.Fatalf("article_type = %v", item["article_type"])
	}
	if modules, ok := item["modules"].([]string); !ok || len(modules) == 0 {
		t.Fatalf("modules = %#v", item["modules"])
	}
	if raw, _ := item["raw"].(string); !strings.Contains(raw, "slot_strategy") {
		t.Fatalf("raw = %q", raw)
	}
}

func TestResourceCatalogListsAndReturnsNotFound(t *testing.T) {
	svc := NewResourceCatalogService(resources.Manager())
	result, err := svc.Query(ResourceCatalogRequest{Category: "themes", Platform: "article"})
	if err != nil {
		t.Fatal(err)
	}
	list := result.(map[string]any)
	if list["category"] != "themes" || len(list["items"].([]resources.ResourceEntry)) == 0 {
		t.Fatalf("list = %#v", list)
	}
	_, err = svc.Query(ResourceCatalogRequest{Category: "themes", Name: "missing"})
	if !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("error = %v, want ErrResourceNotFound", err)
	}
}
