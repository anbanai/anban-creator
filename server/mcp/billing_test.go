package mcp

import (
	"testing"

	"github.com/anbanai/anban-creator/server/service"
)

func TestResolveImageBillingModelUsesResolvedDescriptor(t *testing.T) {
	resolved := &service.ResolvedImageModel{
		Provider: "openai",
		Model:    "gpt-image-2",
		Source:   "preset:openai-gpt-image",
	}
	provider, modelID, source, err := resolveImageBillingModel(resolved)
	if err != nil {
		t.Fatalf("resolveImageBillingModel returned error: %v", err)
	}
	if provider != resolved.Provider || modelID != resolved.Model || source != resolved.Source {
		t.Fatalf("billing descriptor = (%q, %q, %q), want (%q, %q, %q)", provider, modelID, source, resolved.Provider, resolved.Model, resolved.Source)
	}
}

func TestResolveImageBillingModelRejectsIncompleteDescriptor(t *testing.T) {
	for _, resolved := range []*service.ResolvedImageModel{
		nil,
		{Model: "gpt-image-2"},
		{Provider: "openai"},
	} {
		if _, _, _, err := resolveImageBillingModel(resolved); err == nil {
			t.Fatalf("resolveImageBillingModel(%#v) expected error", resolved)
		}
	}
}
