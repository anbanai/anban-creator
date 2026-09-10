package agentpack

import "testing"

func TestDeliveryContractMatchesExactAndGlobPaths(t *testing.T) {
	contract := []DeliverySpec{
		{Role: "content", Path: "output/content.md", MIMEType: "text/markdown"},
		{Role: "image", Path: "output/image_*.png", MIMEType: "image/png"},
	}
	tests := []struct {
		name string
		path string
		role string
		want bool
	}{
		{name: "exact", path: "output/content.md", role: "content", want: true},
		{name: "glob", path: "output/image_01.png", role: "image", want: true},
		{name: "wrong extension", path: "output/image_01.jpg", want: false},
		{name: "wrong directory", path: "tmp/image_01.png", want: false},
		{name: "nested glob does not cross slash", path: "output/images/image_01.png", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mimeType := "image/png"
			if tt.path == "output/content.md" {
				mimeType = "text/markdown; charset=utf-8"
			}
			gotRole, ok := MatchDeliveryPath(contract, tt.path, mimeType)
			if ok != tt.want || (ok && gotRole != tt.role) {
				t.Fatalf("MatchDeliveryPath(%q) = %q, %v; want %q, %v", tt.path, gotRole, ok, tt.role, tt.want)
			}
		})
	}
}

func TestDeliveryContractFailsClosedWhenMultipleSpecsMatch(t *testing.T) {
	contract := []DeliverySpec{
		{Role: "image", Path: "output/*.png", MIMEType: "image/png"},
		{Role: "cover", Path: "output/cover.png", MIMEType: "image/png"},
	}
	if spec, ok := MatchDeliverySpec(contract, "output/cover.png"); ok {
		t.Fatalf("ambiguous delivery matched %#v", spec)
	}
}
