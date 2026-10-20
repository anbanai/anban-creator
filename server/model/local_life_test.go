package model

import "testing"

func TestLocalLifeMixTypesDescribeUseCases(t *testing.T) {
	types := LocalLifeMixTypes()
	if len(types) != 6 {
		t.Fatalf("got %d mix types, want 6", len(types))
	}
	for _, item := range types {
		if item.ID == "" || item.Name == "" || item.Description == "" || item.UseCases == "" || len(item.RecommendedAssets) == 0 {
			t.Fatalf("incomplete mix type: %#v", item)
		}
	}
	if got := LocalLifeMixTypeByID("restaurant"); got == nil || got.Name != "餐饮门店" {
		t.Fatalf("restaurant type = %#v", got)
	}
}

func TestLocalLifeInputValidateBoundsAndDefaults(t *testing.T) {
	input := LocalLifeInput{Brief: "火锅店推广", MixType: "restaurant", SourceAssets: []LocalLifeAsset{{Type: "image", TaskFileID: "asset-1"}}}
	if err := input.Validate(); err != nil {
		t.Fatalf("default input: %v", err)
	}
	if input.VariantCount != 3 || input.AspectRatio != "9:16" || input.TargetDurationSeconds != 30 {
		t.Fatalf("defaults = %#v", input)
	}
	input.VariantCount = 11
	if err := input.Validate(); err == nil {
		t.Fatal("expected variant count limit")
	}
}

func TestLocalLifeInputRejectsUnsupportedAssetAndType(t *testing.T) {
	input := LocalLifeInput{Brief: "门店", MixType: "unknown", SourceAssets: []LocalLifeAsset{{Type: "audio", FileName: "a.mp3"}}}
	if err := input.Validate(); err == nil {
		t.Fatal("expected invalid type and asset error")
	}
}
