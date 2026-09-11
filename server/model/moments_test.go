package model

import (
	"reflect"
	"testing"
)

func TestMomentsPlatformConstantsAndConfig(t *testing.T) {
	if PlatformMoments != "moments" {
		t.Fatalf("PlatformMoments = %q, want moments", PlatformMoments)
	}
	if ScopeMoments != "moments" {
		t.Fatalf("ScopeMoments = %q, want moments", ScopeMoments)
	}
	if DefaultImageRatio(PlatformMoments) != "3:4" {
		t.Fatalf("moments default image ratio = %q, want 3:4", DefaultImageRatio(PlatformMoments))
	}

	cfg := GetPlatformConfig(PlatformMoments)
	if cfg == nil {
		t.Fatal("moments platform config missing")
	}
	if cfg.Label != "朋友圈" || cfg.SupportsPublishing {
		t.Fatalf("moments config = %+v, want label 朋友圈 and no publishing", cfg)
	}

	found := false
	for _, item := range GetAllPlatformConfigs() {
		if item.ID == PlatformMoments {
			found = true
		}
	}
	if !found {
		t.Fatal("GetAllPlatformConfigs missing moments")
	}
}

func TestPlatformImageRatiosMatchBusinessContracts(t *testing.T) {
	tests := []struct {
		platform string
		want     []string
	}{
		{PlatformSeednote, []string{"3:4", "1:1", "4:3"}},
		{PlatformArticle, []string{"16:9", "4:3", "1:1"}},
		{PlatformMoments, []string{"3:4", "1:1"}},
		{PlatformEcommerce, []string{"1:1", "3:4", "4:3", "16:9"}},
		{PlatformMontage, []string{"9:16", "16:9", "1:1"}},
	}
	for _, tt := range tests {
		t.Run(tt.platform, func(t *testing.T) {
			cfg := GetPlatformConfig(tt.platform)
			if cfg == nil {
				t.Fatalf("platform config %q missing", tt.platform)
			}
			if !reflect.DeepEqual(cfg.SupportedImageRatios, tt.want) {
				t.Fatalf("supported ratios = %#v, want %#v", cfg.SupportedImageRatios, tt.want)
			}
			for _, ratio := range tt.want {
				if !IsBusinessImageRatioAllowed(tt.platform, ratio) {
					t.Fatalf("ratio %q should be allowed", ratio)
				}
			}
			if !IsBusinessImageRatioAllowed(tt.platform, ImageRatioAuto) {
				t.Fatal("auto should be allowed")
			}
		})
	}
	if IsBusinessImageRatioAllowed(PlatformSeednote, "2:3") {
		t.Fatal("seednote must reject non-business ratio 2:3")
	}
	if IsBusinessImageRatioAllowed(PlatformMontage, "3:4") {
		t.Fatal("montage must reject unsupported ratio 3:4")
	}
	if got := DefaultImageRatio(PlatformMontage); got != "9:16" {
		t.Fatalf("montage default image ratio = %q, want 9:16", got)
	}
}
