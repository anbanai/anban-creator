package service

func seednoteCompletionArtifactNamesForTest(hasContentImage, hasTailImage bool) []string {
	names := []string{
		"content.md",
		"request-analysis.json",
		"request-analysis.md",
		"reference-analysis.json",
		"reference-analysis.md",
		"image-plan.md",
		"image-prompts.md",
		"image-review.md",
		"reference-usage-summary.json",
		"cover.png",
	}
	if hasContentImage {
		names = append(names, "image_01.png")
	}
	if hasTailImage {
		names = append(names, "tail.png")
	}
	return names
}
