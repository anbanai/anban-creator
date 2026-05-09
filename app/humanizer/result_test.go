package humanizer

import (
	"strings"
	"testing"
)

func TestHumanizeIntensity_Description(t *testing.T) {
	tests := []struct {
		intensity HumanizeIntensity
		want      string
	}{
		{IntensityGentle, "温和处理，只修改最明显、最确定的问题"},
		{IntensityMedium, "平衡处理，保留合理的表达，去除明显 AI 痕迹"},
		{IntensityAggressive, "深度审查，最大化去除 AI 痕迹，大幅改写"},
		{IntensityAuthentic, "真实写作模式，以六维具体规则引导输出像真人写的中文"},
		{HumanizeIntensity("unknown"), "中等强度"},
	}
	for _, tt := range tests {
		got := tt.intensity.Description()
		if got != tt.want {
			t.Errorf("HumanizeIntensity(%q).Description() = %q, want %q", tt.intensity, got, tt.want)
		}
	}
}

func TestHumanizeIntensity_String(t *testing.T) {
	if IntensityMedium.String() != "medium" {
		t.Errorf("IntensityMedium.String() = %q, want %q", IntensityMedium.String(), "medium")
	}
}

func TestAllFocusPatterns(t *testing.T) {
	patterns := AllFocusPatterns()
	if len(patterns) != 5 {
		t.Fatalf("AllFocusPatterns() returned %d patterns, want 5", len(patterns))
	}
	expected := []FocusPattern{PatternContent, PatternLanguage, PatternStyle, PatternFiller, PatternCollaboration}
	for i, p := range expected {
		if patterns[i] != p {
			t.Errorf("patterns[%d] = %q, want %q", i, patterns[i], p)
		}
	}
}

func TestHumanizeResult_HasChanges(t *testing.T) {
	r := &HumanizeResult{}
	if r.HasChanges() {
		t.Error("empty result should not have changes")
	}
	r.Changes = append(r.Changes, Change{Type: ChangeTypeFillerPhrase})
	if !r.HasChanges() {
		t.Error("result with changes should report HasChanges()")
	}
}

func TestHumanizeResult_ChangeCount(t *testing.T) {
	r := &HumanizeResult{
		Changes: []Change{
			{Type: ChangeTypeFillerPhrase},
			{Type: ChangeTypeAIVocabulary},
		},
	}
	if count := r.ChangeCount(); count != 2 {
		t.Errorf("ChangeCount() = %d, want 2", count)
	}
}

func TestScore_Rating(t *testing.T) {
	tests := []struct {
		score *Score
		want  string
	}{
		{nil, "未评分"},
		{&Score{Total: 48}, "优秀 - 已去除 AI 痕迹"},
		{&Score{Total: 45}, "优秀 - 已去除 AI 痕迹"},
		{&Score{Total: 40}, "良好 - 仍有改进空间"},
		{&Score{Total: 35}, "良好 - 仍有改进空间"},
		{&Score{Total: 30}, "一般 - 需要进一步修订"},
		{&Score{Total: 25}, "一般 - 需要进一步修订"},
		{&Score{Total: 10}, "较差 - 建议重新处理"},
		{&Score{Total: 0}, "较差 - 建议重新处理"},
	}
	for _, tt := range tests {
		got := tt.score.Rating()
		if got != tt.want {
			t.Errorf("Score{Total:%d}.Rating() = %q, want %q", tt.score.Total, got, tt.want)
		}
	}
}

func TestParseIntensity_Authentic(t *testing.T) {
	tests := []struct {
		input string
		want  HumanizeIntensity
	}{
		{"authentic", IntensityAuthentic},
		{"natural", IntensityAuthentic},
		{"真实", IntensityAuthentic},
		{"自然", IntensityAuthentic},
		{"AUTHENTIC", IntensityAuthentic},
	}
	for _, tt := range tests {
		got := ParseIntensity(tt.input)
		if got != tt.want {
			t.Errorf("ParseIntensity(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseIntensity_EmptyString(t *testing.T) {
	got := ParseIntensity("")
	if got != IntensityMedium {
		t.Errorf("ParseIntensity('') = %q, want %q", got, IntensityMedium)
	}
}

func TestBuildPrompt_AuthenticMode(t *testing.T) {
	req := &HumanizeRequest{
		Content:   "测试内容",
		Intensity: IntensityAuthentic,
	}

	prompt := BuildPrompt(req)

	if !strings.Contains(prompt, "六、整体原则") {
		t.Error("authentic prompt should contain 6-dimension rules")
	}
	if strings.Contains(prompt, "需要检测和处理的模式") {
		t.Error("authentic prompt should NOT contain 24-pattern detection system")
	}
	if !strings.Contains(prompt, "真实写作模式") {
		t.Error("authentic prompt should contain authentic mode description")
	}
	if !strings.Contains(prompt, "测试内容") {
		t.Error("authentic prompt should contain the input content")
	}
}

func TestBuildPrompt_AuthenticSkipsFocusOn(t *testing.T) {
	req := &HumanizeRequest{
		Content:   "测试内容",
		Intensity: IntensityAuthentic,
		FocusOn:   []FocusPattern{PatternContent, PatternStyle},
	}

	prompt := BuildPrompt(req)

	if strings.Contains(prompt, "重点处理模式") {
		t.Error("authentic mode should ignore FocusOn parameter (it uses 6-dimension rules, not 24 patterns)")
	}
}
