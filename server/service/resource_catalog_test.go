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
	result, err := svc.Query(ResourceCatalogRequest{Category: "themes", Platform: "wechat"})
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

func TestResourceCatalogDefaultsLegacyMetadataAndSelectionMode(t *testing.T) {
	svc := NewResourceCatalogService(resources.Manager())
	result, err := svc.Query(ResourceCatalogRequest{Category: "layouts", Platform: "wechat"})
	if err != nil {
		t.Fatal(err)
	}
	items := result.(map[string]any)["items"].([]resources.ResourceEntry)
	if len(items) == 0 {
		t.Fatal("expected legacy layout resources")
	}
	for _, item := range items {
		if item.Lifecycle != resources.LifecycleLegacy {
			t.Errorf("resource %s lifecycle = %q, want legacy", item.Name, item.Lifecycle)
		}
		if item.Name == "quote" && item.ModuleContractVersion != "" {
			t.Fatalf("legacy resource list exposed compatibility contract summary: %+v", item)
		}
	}

	compatibilitySvc := NewResourceCatalogService(resources.Manager(), resources.LifecycleModeCompatibility)
	compatibilityResult, err := compatibilitySvc.Query(ResourceCatalogRequest{
		Category: "layouts", Platform: "wechat", LifecycleMode: "compatibility",
	})
	if err != nil {
		t.Fatal(err)
	}
	compatibilityItems := compatibilityResult.(map[string]any)["items"].([]resources.ResourceEntry)
	if len(compatibilityItems) < len(items) {
		t.Fatalf("compatibility mode returned %d layouts, legacy mode returned %d", len(compatibilityItems), len(items))
	}
	for _, item := range compatibilityItems {
		if item.Name == "quote" {
			if item.Lifecycle != resources.LifecycleLegacy || item.ModuleContractVersion != "anban.layout.v1" || item.ModuleContractLifecycle != resources.LifecycleCompatibility {
				t.Fatalf("compatibility quote selection metadata = %+v", item)
			}
			return
		}
	}
	t.Fatal("compatibility mode did not list the legacy quote resource")
}

func TestResourceCatalogServerModeBoundsCallerSelection(t *testing.T) {
	legacy := NewResourceCatalogService(resources.Manager(), resources.LifecycleModeLegacy)
	result, err := legacy.Query(ResourceCatalogRequest{
		Category: "layouts", Platform: "wechat", LifecycleMode: "native",
	})
	if err != nil {
		t.Fatal(err)
	}
	legacyItems := result.(map[string]any)["items"].([]resources.ResourceEntry)
	for _, item := range legacyItems {
		if item.Lifecycle != resources.LifecycleLegacy {
			t.Fatalf("legacy server exposed %s lifecycle %q", item.Name, item.Lifecycle)
		}
	}
	legacyQuote, err := legacy.Query(ResourceCatalogRequest{Category: "layouts", Name: "quote"})
	if err != nil {
		t.Fatalf("legacy server get quote error = %v", err)
	}
	legacyQuoteItem := legacyQuote.(map[string]any)
	if legacyQuoteItem["lifecycle"] != string(resources.LifecycleLegacy) || legacyQuoteItem["fields"] == nil {
		t.Fatalf("legacy server must retain the old quote resource contract: %#v", legacyQuoteItem)
	}
	if _, ok := legacyQuoteItem["module_contract"]; ok {
		t.Fatalf("legacy server exposed compatibility module contract: %#v", legacyQuoteItem["module_contract"])
	}

	compatibility := NewResourceCatalogService(resources.Manager(), resources.LifecycleModeCompatibility)
	result, err = compatibility.Query(ResourceCatalogRequest{
		Category: "layouts", Platform: "wechat", LifecycleMode: "native",
	})
	if err != nil {
		t.Fatal(err)
	}
	compatibilityItems := result.(map[string]any)["items"].([]resources.ResourceEntry)
	for _, item := range compatibilityItems {
		if item.Lifecycle == resources.LifecycleRecommended {
			t.Fatalf("compatibility server exposed recommended resource %s", item.Name)
		}
	}
}

func TestResourceCatalogReturnsLayoutContractMetadata(t *testing.T) {
	svc := NewResourceCatalogService(resources.Manager(), resources.LifecycleModeCompatibility)
	result, err := svc.Query(ResourceCatalogRequest{Category: "layouts", Name: "quote"})
	if err != nil {
		t.Fatal(err)
	}
	item := result.(map[string]any)
	for _, field := range []string{"schema_version", "lifecycle", "when_not_to_use", "pairs_well_with", "canonical_example", "provenance"} {
		if _, ok := item[field]; !ok {
			t.Errorf("layout metadata response missing %q", field)
		}
	}
	if item["lifecycle"] != string(resources.LifecycleLegacy) {
		t.Fatalf("resource lifecycle = %v, want legacy", item["lifecycle"])
	}
	if item["fields"] == nil {
		t.Fatal("legacy fields schema must remain available")
	}
	contract, ok := item["module_contract"].(*resources.ModuleContract)
	if !ok || contract.Version != "anban.layout.v1" || contract.Lifecycle != resources.LifecycleCompatibility {
		t.Fatalf("module contract = %#v, want compatibility anban.layout.v1", item["module_contract"])
	}

	legacyResult, err := NewResourceCatalogService(resources.Manager(), resources.LifecycleModeCompatibility).Query(ResourceCatalogRequest{
		Category: "layouts", Name: "quote", LifecycleMode: "legacy",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := legacyResult.(map[string]any)["module_contract"]; ok {
		t.Fatal("explicit legacy selection exposed compatibility module contract")
	}
}

func TestResourceCatalogExposesTemplateModuleBoundariesAndDiscoverableCompatibilityModules(t *testing.T) {
	svc := NewResourceCatalogService(resources.Manager(), resources.LifecycleModeCompatibility)
	templatesResult, err := svc.Query(ResourceCatalogRequest{Category: "article_templates", Platform: "wechat"})
	if err != nil {
		t.Fatal(err)
	}
	templates := templatesResult.(map[string]any)["items"].([]resources.ResourceEntry)
	for _, template := range templates {
		detailResult, err := svc.Query(ResourceCatalogRequest{Category: "article_templates", Name: template.Name})
		if err != nil {
			t.Fatalf("get template %s: %v", template.Name, err)
		}
		detail := detailResult.(map[string]any)
		if _, ok := detail["forbidden_modules"]; !ok {
			t.Errorf("template %s response is missing forbidden_modules", template.Name)
		}
	}

	const compatibilityContract = "anban.layout.v1"
	for _, module := range []string{"quote", "callout", "steps", "checklist", "summary", "comparison-table"} {
		found := false
		for _, template := range templates {
			if containsString(template.TemplateModules, module) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("compatibility module %q (%s) is not recommended by any article template", module, compatibilityContract)
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
