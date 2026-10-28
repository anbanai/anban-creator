package model

import (
	"strings"
	"testing"
)

func TestTalkingHeadEditorInputValidatesIndependentAssetRoles(t *testing.T) {
	base := TalkingHeadEditorInput{
		Brief:        "保留自然节奏并完成动效包装",
		PrimaryVideo: TalkingHeadEditorAsset{Role: "reference", AssetID: "video-1", MimeType: "video/mp4"},
	}
	tests := []struct {
		name    string
		mutate  func(*TalkingHeadEditorInput)
		wantErr string
	}{
		{name: "primary must be video", mutate: func(input *TalkingHeadEditorInput) { input.PrimaryVideo.MimeType = "audio/mpeg" }, wantErr: "must be a video"},
		{name: "sfx must be audio", mutate: func(input *TalkingHeadEditorInput) {
			input.SupportingAssets = []TalkingHeadEditorAsset{{Role: "sfx", AssetID: "sfx-1", MimeType: "video/mp4"}}
		}, wantErr: "must be audio"},
		{name: "broll must be visual", mutate: func(input *TalkingHeadEditorInput) {
			input.SupportingAssets = []TalkingHeadEditorAsset{{Role: "broll", AssetID: "broll-1", MimeType: "audio/mpeg"}}
		}, wantErr: "must be video or image"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := base
			test.mutate(&input)
			if err := input.Validate(20, 1800); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Validate() error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}
