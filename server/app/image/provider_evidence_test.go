package image

import "testing"

func TestGenerateResultCarriesSafeProviderEvidence(t *testing.T) {
	result := GenerateResult{
		ProviderRequestID: "internal-request-1",
		OutputWidth:       2360,
		OutputHeight:      1000,
		Usage: &ImageGenerationUsage{
			TextInputTokens: 1, TextCachedInputTokens: 2, ImageInputTokens: 3,
			ImageCachedInputTokens: 4, ImageOutputTokens: 5,
		},
	}
	if result.ProviderRequestID == "" || result.OutputWidth*result.OutputHeight != 2_360_000 {
		t.Fatalf("provider evidence = %#v", result)
	}
}
