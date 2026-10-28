package service

import "testing"

func TestTalkingHeadEditorResumeStage(t *testing.T) {
	tests := []struct {
		decision string
		want     string
	}{
		{decision: "", want: "input_and_rough_cut"},
		{decision: "revise", want: "motion_packaging"},
		{decision: "approve", want: "sound_and_review"},
	}
	for _, tt := range tests {
		t.Run(tt.decision, func(t *testing.T) {
			if got := talkingHeadEditorResumeStage(tt.decision); got != tt.want {
				t.Fatalf("talkingHeadEditorResumeStage(%q) = %q, want %q", tt.decision, got, tt.want)
			}
		})
	}
}
