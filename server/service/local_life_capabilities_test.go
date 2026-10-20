package service

import "testing"

func TestLocalLifeCapabilityCatalogUsesServerOwnedMixTypes(t *testing.T) {
	catalog := NewLocalLifeCapabilityService(true).Catalog()
	if !catalog.Enabled || catalog.DefaultMixType != "general-store" || catalog.DefaultVariantCount != 3 || catalog.MaxVariantCount != 10 {
		t.Fatalf("catalog defaults = %#v", catalog)
	}
	if len(catalog.Types) != 6 {
		t.Fatalf("got %d types, want 6", len(catalog.Types))
	}
	for _, item := range catalog.Types {
		if item.ID == "" || item.Name == "" || item.Description == "" || item.UseCases == "" {
			t.Fatalf("incomplete catalog item: %#v", item)
		}
	}
}

func TestLocalLifeCapabilityServiceIsDisabledByDefault(t *testing.T) {
	if got := NewLocalLifeCapabilityService().Catalog(); got.Enabled {
		t.Fatalf("default catalog enabled = true, want false")
	}
}
