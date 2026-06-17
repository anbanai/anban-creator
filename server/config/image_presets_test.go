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
				{Key: "volcengine-standard", Provider: "volcengine", Model: "doubao-seedream"},
			},
			wantErr: false,
		},
		{
			name: "valid multiple presets with distinct keys",
			presets: []ImageModelPreset{
				{Key: "volcengine-standard"},
				{Key: "gemini-pro"},
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
				{Key: strings.Repeat("a", 50)},
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
				{Key: "gemini-pro"},
				{Key: "gemini-pro"},
			},
			wantErr: true,
			substr:  "duplicate key",
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
