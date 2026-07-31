package config

import (
	"strings"
	"testing"
)

func TestValidateImagePresets(t *testing.T) {
	tests := []struct {
		name    string
		presets []ImageModelPreset
		wantErr bool
		substr  string
	}{
		{
			name:    "nil list is valid",
			presets: nil,
			wantErr: false,
		},
		{
			name:    "empty list is valid",
			presets: []ImageModelPreset{},
			wantErr: false,
		},
		{
			name: "valid single preset",
			presets: []ImageModelPreset{
				{Key: "standard_image", DisplayName: "标准图像", Description: "适合日常内容配图", BillingSKU: "image.seedream.designer"},
			},
			wantErr: false,
		},
		{
			name: "valid multiple presets with distinct keys",
			presets: []ImageModelPreset{
				{Key: "standard_image", BillingSKU: "image.seedream.designer"},
				{Key: "professional_enhance", BillingSKU: "image.gpt-image-2.designer"},
			},
			wantErr: false,
		},
		{
			name: "empty key rejected",
			presets: []ImageModelPreset{
				{Key: ""},
			},
			wantErr: true,
			substr:  "key is required",
		},
		{
			name: "key exceeding 50 chars rejected",
			presets: []ImageModelPreset{
				{Key: strings.Repeat("a", 51)},
			},
			wantErr: true,
			substr:  "exceeds 50 characters",
		},
		{
			name: "key at exactly 50 chars accepted",
			presets: []ImageModelPreset{
				{Key: strings.Repeat("a", 50), BillingSKU: "image.seedream.designer"},
			},
			wantErr: false,
		},
		{
			name: "key with space rejected",
			presets: []ImageModelPreset{
				{Key: "has space"},
			},
			wantErr: true,
			substr:  "must not contain whitespace",
		},
		{
			name: "key with tab rejected",
			presets: []ImageModelPreset{
				{Key: "has\ttab"},
			},
			wantErr: true,
			substr:  "must not contain whitespace",
		},
		{
			name: "reserved key 'custom' rejected",
			presets: []ImageModelPreset{
				{Key: "custom"},
			},
			wantErr: true,
			substr:  "reserved",
		},
		{
			name: "duplicate keys rejected",
			presets: []ImageModelPreset{
				{Key: "standard_image", BillingSKU: "image.seedream.designer"},
				{Key: "standard_image", BillingSKU: "image.seedream.designer"},
			},
			wantErr: true,
			substr:  "duplicate key",
		},
		{
			name:    "missing billing SKU rejected",
			presets: []ImageModelPreset{{Key: "standard_image", DisplayName: "标准图像"}},
			wantErr: true,
			substr:  "billing_sku",
		},
		{
			name:    "blocked brand in display name rejected case insensitive",
			presets: []ImageModelPreset{{Key: "standard_image", DisplayName: "OpenAI 标准图像", BillingSKU: "image.seedream.designer"}},
			wantErr: true,
			substr:  "blocked",
		},
		{
			name:    "blocked brand in description rejected",
			presets: []ImageModelPreset{{Key: "standard_image", DisplayName: "标准图像", Description: "Gemini 细节增强", BillingSKU: "image.seedream.designer"}},
			wantErr: true,
			substr:  "blocked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateImagePresets(tt.presets)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.substr != "" && !strings.Contains(err.Error(), tt.substr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.substr)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
		})
	}
}

func TestValidateImagePresetsBlocksSensitiveTermsInPublicKeys(t *testing.T) {
	for _, term := range []string{"openai", "chatgpt", "gpt", "gemini", "claude", "seedream", "doubao"} {
		t.Run(term, func(t *testing.T) {
			err := ValidateImagePresets([]ImageModelPreset{{
				Key: "capability_" + strings.ToUpper(term), BillingSKU: "image.seedream.designer",
			}})
			if err == nil || !strings.Contains(err.Error(), "blocked") {
				t.Fatalf("ValidateImagePresets error = %v, want blocked public key", err)
			}
		})
	}
}
