package service

import (
	"testing"
)

func TestExtractImageSrcs(t *testing.T) {
	cases := []struct {
		name string
		html string
		want []string
	}{
		{"empty", ``, nil},
		{"no img", `<p>text</p>`, nil},
		{"single double-quote", `<img src="http://a/1.png">`, []string{"http://a/1.png"}},
		{"single single-quote", `<img src='http://a/2.png'/>`, []string{"http://a/2.png"}},
		{"case insensitive", `<IMG SRC="http://a/3.png">`, []string{"http://a/3.png"}},
		{"src not first attr", `<img class="x" style="width:100%" src="http://a/4.png">`, []string{"http://a/4.png"}},
		{"two distinct", `<img src="http://a/1.png"><img src="http://a/2.png">`, []string{"http://a/1.png", "http://a/2.png"}},
		{"two same keeps dupes", `<img src="http://a/1.png"><img src="http://a/1.png">`, []string{"http://a/1.png", "http://a/1.png"}},
		// Edge cases the regex must handle for machine-generated WeChat HTML.
		// These lock the contract so a future "simplification" of \bsrc cannot
		// silently regress the gate.
		{"srcset without src yields none", `<img srcset="http://a/1.png 2x">`, nil},
		{"src and srcset picks src", `<img src="http://a/1.png" srcset="http://a/2.png 2x">`, []string{"http://a/1.png"}},
		{"data url captured", `<img src="data:image/png;base64,iVBORw0KGgo=">`, []string{"data:image/png;base64,iVBORw0KGgo="}},
		{"multiline tags", "<img\n  src=\"http://a/1.png\">\n<img src=\"http://a/2.png\">", []string{"http://a/1.png", "http://a/2.png"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractImageSrcs(tc.html)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d srcs %v, want %d %v", len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("src[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestValidateContentImageDiversity(t *testing.T) {
	cases := []struct {
		name    string
		html    string
		wantErr bool
	}{
		{"no image", `<p>text only</p>`, false},
		{"single image", `<img src="http://a/1.png">`, false},
		{"two same url rejected", `<img src="http://a/1.png"><img src="http://a/1.png">`, true},
		{"three same url rejected", `<img src="http://a/1.png"><img src="http://a/1.png"><img src="http://a/1.png">`, true},
		{"two distinct allowed", `<img src="http://a/1.png"><img src="http://a/2.png">`, false},
		{"three two-distinct allowed", `<img src="http://a/1.png"><img src="http://a/2.png"><img src="http://a/1.png">`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateContentImageDiversity(tc.html)
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}
