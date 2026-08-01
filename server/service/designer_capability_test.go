package service

import (
	"encoding/json"
	"errors"
	"testing"

	srvconfig "github.com/anbanai/anban-creator/server/config"
)

func TestDesignerRequestUsesCapabilityKey(t *testing.T) {
	var req DesignerGenerateRequest
	if err := json.Unmarshal([]byte(`{"capability_key":"standard","provider_id":"legacy"}`), &req); err != nil {
		t.Fatal(err)
	}
	if req.CapabilityKey != "standard" {
		t.Fatalf("capability_key = %q, want standard", req.CapabilityKey)
	}
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) == "" || json.Valid(payload) == false {
		t.Fatalf("invalid request JSON: %s", payload)
	}
	var public map[string]any
	if err := json.Unmarshal(payload, &public); err != nil {
		t.Fatal(err)
	}
	if _, leaked := public["provider_id"]; leaked {
		t.Fatalf("provider_id leaked in public request JSON: %s", payload)
	}
}

func TestValidateDesignerReferencesUsesSelectedCapabilityWithoutFallback(t *testing.T) {
	tests := []struct {
		name       string
		features   srvconfig.DesignerProviderCapabilities
		references int
		wantError  error
		wantLimit  int
	}{
		{name: "unsupported", features: srvconfig.DesignerProviderCapabilities{}, references: 1, wantError: ErrDesignerReferenceUnsupported},
		{name: "over limit", features: srvconfig.DesignerProviderCapabilities{SupportsReference: true, MaxReferenceImages: 1}, references: 2, wantError: ErrDesignerReferenceLimitExceeded, wantLimit: 1},
		{name: "within limit", features: srvconfig.DesignerProviderCapabilities{SupportsReference: true, MaxReferenceImages: 2}, references: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := DesignerGenerateRequest{ReferenceFileIDs: make([]string, tt.references)}
			err := validateDesignerGenerateRequest(req, tt.features)
			if tt.wantError == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("error = %v, want %v", err, tt.wantError)
			}
			if tt.wantLimit > 0 {
				var limitErr *DesignerReferenceLimitError
				if !errors.As(err, &limitErr) || limitErr.MaxReferenceImages != tt.wantLimit || limitErr.Requested != tt.references {
					t.Fatalf("structured limit error = %#v", limitErr)
				}
			}
		})
	}
}

func TestDesignerRouteResolvesExactCapabilityKeyOnly(t *testing.T) {
	svc := &DesignerService{fullCfg: &srvconfig.Config{ModelRoutes: srvconfig.ModelRoutesConfig{ImageGeneration: srvconfig.ImageGenerationRoutesConfig{
		DefaultCapability: "standard",
		Capabilities:      map[string]srvconfig.ImageGenerationRouteConfig{"standard": {Enabled: true}},
	}}}}
	if _, ok := svc.designerRoute("standard"); !ok {
		t.Fatal("standard capability was not resolved")
	}
	if _, ok := svc.designerRoute("legacy-alias"); ok {
		t.Fatal("unknown capability unexpectedly fell back")
	}
}

func TestDesignerDefaultResolversHandleMissingConfig(t *testing.T) {
	svc := &DesignerService{}
	if got := svc.resolveAPIKey("missing"); got != "" {
		t.Fatalf("resolveAPIKey() = %q, want empty", got)
	}
	if got := svc.resolveBaseURL("missing"); got != "" {
		t.Fatalf("resolveBaseURL() = %q, want empty", got)
	}
	if got := svc.resolveModel("missing"); got != "" {
		t.Fatalf("resolveModel() = %q, want empty", got)
	}
	if got := svc.resolveProvider(); got != "openai" {
		t.Fatalf("resolveProvider() = %q, want openai fallback", got)
	}
}
