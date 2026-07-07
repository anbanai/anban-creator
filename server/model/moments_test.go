package model

import "testing"

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
