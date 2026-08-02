package service

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	srvconfig "github.com/anbanai/anban-creator/server/config"
)

func TestDesignerRequestUsesCapabilityKey(t *testing.T) {
	requestType := reflect.TypeOf(DesignerGenerateRequest{})
	for _, legacyField := range []string{"Provider", "Model"} {
		if _, exists := requestType.FieldByName(legacyField); exists {
			t.Fatalf("DesignerGenerateRequest still exposes legacy field %s", legacyField)
		}
	}
	for _, requiredField := range []string{"Quality", "Size", "N", "OutputFormat"} {
		field, exists := requestType.FieldByName(requiredField)
		if !exists {
			t.Fatalf("DesignerGenerateRequest missing field %s", requiredField)
		}
		if strings.Contains(field.Tag.Get("json"), "omitempty") {
			t.Fatalf("DesignerGenerateRequest.%s is still optional in JSON", requiredField)
		}
	}
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
		{name: "unsupported", features: srvconfig.DesignerProviderCapabilities{QualityLevels: []string{"auto"}}, references: 1, wantError: ErrDesignerReferenceUnsupported},
		{name: "over limit", features: srvconfig.DesignerProviderCapabilities{QualityLevels: []string{"auto"}, SupportsReference: true, MaxReferenceImages: 1}, references: 2, wantError: ErrDesignerReferenceLimitExceeded, wantLimit: 1},
		{name: "within limit", features: srvconfig.DesignerProviderCapabilities{QualityLevels: []string{"auto"}, SupportsReference: true, MaxReferenceImages: 2}, references: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := DesignerGenerateRequest{
				Size: "1:1:2K", Quality: "auto", N: 1, OutputFormat: "png",
				ReferenceFileIDs: make([]string, tt.references),
			}
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

func TestValidateDesignerGenerateRequestRequiresCompleteFixedSpecification(t *testing.T) {
	caps := DesignerProviderCapabilities{
		SizePresets:   []string{"1:1:2K"},
		QualityLevels: []string{"auto", "high"},
		OutputFormats: []string{"png", "jpeg"},
		MaxBatch:      1,
	}
	valid := DesignerGenerateRequest{Size: "1:1:2K", Quality: "auto", N: 1, OutputFormat: "png"}
	tests := []struct {
		name   string
		mutate func(*DesignerGenerateRequest)
		want   string
	}{
		{name: "missing size", mutate: func(req *DesignerGenerateRequest) { req.Size = "" }, want: "size is required"},
		{name: "missing quality", mutate: func(req *DesignerGenerateRequest) { req.Quality = "" }, want: "quality is required"},
		{name: "missing count", mutate: func(req *DesignerGenerateRequest) { req.N = 0 }, want: "n is required"},
		{name: "missing output format", mutate: func(req *DesignerGenerateRequest) { req.OutputFormat = "" }, want: "output_format is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := valid
			tt.mutate(&req)
			if err := validateDesignerGenerateRequest(req, caps); err == nil || err.Error() != tt.want {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}

	withoutQualityLevels := caps
	withoutQualityLevels.QualityLevels = nil
	valid.Quality = ""
	if err := validateDesignerGenerateRequest(valid, withoutQualityLevels); err == nil || err.Error() != "quality is required" {
		t.Fatalf("missing quality error = %v, want quality is required", err)
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

func TestDesignerRuntimeConfigResolversHandleMissingConfig(t *testing.T) {
	svc := &DesignerService{}
	if got := svc.resolveAPIKey("missing"); got != "" {
		t.Fatalf("resolveAPIKey() = %q, want empty", got)
	}
	if got := svc.resolveBaseURL("missing"); got != "" {
		t.Fatalf("resolveBaseURL() = %q, want empty", got)
	}
}

func TestValidateDesignerGenerateRequestRequiresExactFixedSizePreset(t *testing.T) {
	caps := DesignerProviderCapabilities{
		SizePresets:   []string{"1:1:2K", "3:4:2K"},
		QualityLevels: []string{"auto"},
		DefaultSize:   "1:1:2K",
		MaxBatch:      1,
		OutputFormats: []string{"png"},
	}

	if err := validateDesignerGenerateRequest(DesignerGenerateRequest{
		Prompt: "poster", Size: "3:4:2K", Quality: "auto", N: 1, OutputFormat: "png",
	}, caps); err != nil {
		t.Fatalf("exact fixed size preset rejected: %v", err)
	}
	if err := validateDesignerGenerateRequest(DesignerGenerateRequest{
		Prompt: "poster", Size: "3:4", Quality: "auto", N: 1, OutputFormat: "png",
	}, caps); err == nil {
		t.Fatal("legacy ratio-only size accepted, want exact fixed preset rejection")
	}
}
