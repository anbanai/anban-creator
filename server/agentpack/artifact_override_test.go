package agentpack

import "testing"

func TestRequiredArtifactsForTaskTypeUsesCompleteArtifactOverride(t *testing.T) {
	manifest := Manifest{
		Artifacts: []ArtifactSpec{
			{Role: "content", Path: "output/content.md", MIMEType: "text/markdown", Required: true},
			{Role: "image", Path: "output/image.png", MIMEType: "image/png", Required: false},
		},
		ArtifactsByTaskType: map[string][]ArtifactSpec{
			"viral_analysis": {
				{Role: "analysis", Path: "output/source-analysis.md", MIMEType: "text/markdown", Required: true},
				{Role: "template", Path: "output/viral-template.json", MIMEType: "application/json", Required: true},
			},
		},
	}
	required, err := manifest.RequiredArtifactsForTaskType("viral_analysis")
	if err != nil {
		t.Fatal(err)
	}
	if len(required) != 2 || required[0].Path != "output/source-analysis.md" || required[1].Path != "output/viral-template.json" {
		t.Fatalf("task override required artifacts = %#v", required)
	}
	fallback, err := manifest.RequiredArtifactsForTaskType("normal")
	if err != nil {
		t.Fatal(err)
	}
	if len(fallback) != 1 || fallback[0].Path != "output/content.md" {
		t.Fatalf("fallback required artifacts = %#v", fallback)
	}
}
