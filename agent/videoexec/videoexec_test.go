package videoexec

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProbeManifestUsesDisplayRotation(t *testing.T) {
	raw := []byte(`{
		"streams":[{
			"codec_type":"video",
			"width":1920,
			"height":1080,
			"avg_frame_rate":"30000/1001",
			"duration":"12.345",
			"color_transfer":"bt709",
			"side_data_list":[{"rotation":-90}]
		},{
			"codec_type":"audio",
			"codec_name":"aac",
			"channels":2,
			"sample_rate":"48000"
		}],
		"format":{"duration":"12.345"}
	}`)

	manifest, err := ParseProbeJSON("/tmp/C0572.MP4", raw, "sha256:demo")
	if err != nil {
		t.Fatalf("ParseProbeJSON returned error: %v", err)
	}
	if manifest.EncodedWidth != 1920 || manifest.EncodedHeight != 1080 {
		t.Fatalf("encoded size = %dx%d, want 1920x1080", manifest.EncodedWidth, manifest.EncodedHeight)
	}
	if manifest.RotationDegrees != -90 {
		t.Fatalf("rotation = %d, want -90", manifest.RotationDegrees)
	}
	if manifest.DisplayWidth != 1080 || manifest.DisplayHeight != 1920 {
		t.Fatalf("display size = %dx%d, want 1080x1920", manifest.DisplayWidth, manifest.DisplayHeight)
	}
	if !manifest.IsPortraitDisplay {
		t.Fatal("expected rotated video to be portrait display")
	}
	if len(manifest.AudioStreams) != 1 || manifest.AudioStreams[0].CodecName != "aac" {
		t.Fatalf("audio streams not parsed: %#v", manifest.AudioStreams)
	}
}

func TestPackTranscriptFilesMatchesInlinePacker(t *testing.T) {
	dir := t.TempDir()
	transcript := VideoTranscript{
		Words: []VideoTranscriptWord{
			{Type: "word", Text: "第一", Start: 0, End: 0.2, SpeakerID: "speaker_0"},
			{Type: "spacing", Start: 0.2, End: 0.8},
			{Type: "word", Text: "第二", Start: 0.8, End: 1.0, SpeakerID: "speaker_0"},
		},
	}
	writeJSON(t, filepath.Join(dir, "take-a.json"), transcript)

	got, err := PackTranscriptFiles(dir, 0.5)
	if err != nil {
		t.Fatalf("PackTranscriptFiles returned error: %v", err)
	}
	want, err := PackTranscripts(map[string]VideoTranscript{"take-a": transcript}, 0.5)
	if err != nil {
		t.Fatalf("PackTranscripts returned error: %v", err)
	}
	if got != want {
		t.Fatalf("packed markdown mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestMatchScriptMapsLinesAndFlagsUnmatchedText(t *testing.T) {
	dir := t.TempDir()
	transcriptsDir := filepath.Join(dir, "transcripts")
	if err := os.MkdirAll(transcriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(transcriptsDir, "take-a.json"), VideoTranscript{
		Words: []VideoTranscriptWord{
			{Type: "word", Text: "大家", Start: 0.0, End: 0.2},
			{Type: "word", Text: "好", Start: 0.2, End: 0.4},
			{Type: "word", Text: "今天", Start: 0.8, End: 1.0},
			{Type: "word", Text: "讲", Start: 1.0, End: 1.2},
			{Type: "word", Text: "视频", Start: 1.2, End: 1.5},
		},
	})
	scriptPath := filepath.Join(dir, "script.md")
	if err := os.WriteFile(scriptPath, []byte("今天讲视频\n完全不存在"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan, err := MatchScriptToTranscripts(scriptPath, transcriptsDir)
	if err != nil {
		t.Fatalf("MatchScriptToTranscripts returned error: %v", err)
	}
	if len(plan.Matches) != 1 {
		t.Fatalf("matches = %#v, want one match", plan.Matches)
	}
	match := plan.Matches[0]
	if match.Source != "take-a" || match.Start != 0.8 || match.End != 1.5 {
		t.Fatalf("unexpected match: %#v", match)
	}
	if len(plan.UnmatchedLines) != 1 || plan.UnmatchedLines[0].Text != "完全不存在" {
		t.Fatalf("unmatched lines = %#v, want 完全不存在", plan.UnmatchedLines)
	}
}

func TestValidateEDLRejectsOverlaySizeMismatch(t *testing.T) {
	edl := EDL{
		OutputWidth:  1080,
		OutputHeight: 1920,
		Sources:      map[string]string{"take-a": "/tmp/take-a.mp4"},
		Ranges:       []EDLRange{{Source: "take-a", Start: 0, End: 1}},
		Overlays: []EDLOverlay{{
			File:   "animations/slot_01/render.webm",
			Start:  0,
			End:    1,
			Width:  1920,
			Height: 1080,
		}},
	}

	err := ValidateEDL(edl)
	if err == nil {
		t.Fatal("expected overlay size mismatch error")
	}
	if !strings.Contains(err.Error(), "overlay") || !strings.Contains(err.Error(), "1080x1920") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateEDLRejectsUnsupportedGrade(t *testing.T) {
	edl := EDL{
		OutputWidth:  1080,
		OutputHeight: 1920,
		Sources:      map[string]string{"take-a": "/tmp/take-a.mp4"},
		Ranges:       []EDLRange{{Source: "take-a", Start: 0, End: 1}},
		Grade:        "neutral_punch",
	}

	err := ValidateEDL(edl)
	if err == nil {
		t.Fatal("expected unsupported grade error")
	}
	if !strings.Contains(err.Error(), "grade") || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDraftRenderPlanSkipsOverlaysAndLoudnorm(t *testing.T) {
	plan, err := BuildRenderPlan(EDL{
		OutputWidth:  1080,
		OutputHeight: 1920,
		Sources:      map[string]string{"take-a": "/tmp/take-a.mp4"},
		Ranges:       []EDLRange{{Source: "take-a", Start: 0, End: 1}},
		Overlays: []EDLOverlay{{
			File: "animations/slot_01/render.webm", Start: 0, End: 1, Width: 1080, Height: 1920,
		}},
	}, RenderModeDraft)
	if err != nil {
		t.Fatalf("BuildRenderPlan returned error: %v", err)
	}
	if plan.ApplyOverlays {
		t.Fatal("draft mode should skip overlays")
	}
	if plan.ApplyLoudnorm {
		t.Fatal("draft mode should skip loudnorm")
	}
	if plan.ScaleHeight != 1280 {
		t.Fatalf("draft scale height = %d, want 1280", plan.ScaleHeight)
	}
	if plan.SegmentCount != 1 {
		t.Fatalf("segment count = %d, want 1", plan.SegmentCount)
	}
}

func TestRenderPlanSupportsMultipleRanges(t *testing.T) {
	plan, err := BuildRenderPlan(EDL{
		OutputWidth:  1080,
		OutputHeight: 1920,
		Sources:      map[string]string{"take-a": "/tmp/take-a.mp4"},
		Ranges: []EDLRange{
			{Source: "take-a", Start: 0, End: 1},
			{Source: "take-a", Start: 2, End: 3},
		},
	}, RenderModePreview)
	if err != nil {
		t.Fatalf("BuildRenderPlan returned error: %v", err)
	}
	if plan.SegmentCount != 2 {
		t.Fatalf("segment count = %d, want 2", plan.SegmentCount)
	}
}

func TestRenderPlanUsesFixedOutputDimensions(t *testing.T) {
	plan, err := BuildRenderPlan(EDL{
		OutputWidth:  1080,
		OutputHeight: 1920,
		Sources:      map[string]string{"take-a": "/tmp/take-a.mp4"},
		Ranges:       []EDLRange{{Source: "take-a", Start: 0, End: 1}},
	}, RenderModePreview)
	if err != nil {
		t.Fatalf("BuildRenderPlan returned error: %v", err)
	}
	if plan.OutputWidth != 1080 || plan.OutputHeight != 1920 {
		t.Fatalf("plan output = %dx%d, want 1080x1920", plan.OutputWidth, plan.OutputHeight)
	}
	if !strings.Contains(plan.VideoFilter, "scale=1080:1920") {
		t.Fatalf("video filter should scale into fixed output, got %q", plan.VideoFilter)
	}
	if !strings.Contains(plan.VideoFilter, "pad=1080:1920") {
		t.Fatalf("video filter should pad to fixed output, got %q", plan.VideoFilter)
	}
}

func TestFinalRenderPlanAppliesOverlaysAndLoudnorm(t *testing.T) {
	plan, err := BuildRenderPlan(EDL{
		OutputWidth:  1080,
		OutputHeight: 1920,
		Sources:      map[string]string{"take-a": "/tmp/take-a.mp4"},
		Ranges:       []EDLRange{{Source: "take-a", Start: 0, End: 1}},
		Overlays: []EDLOverlay{{
			File: "animations/slot_01/render.webm", Start: 0, End: 1, Width: 1080, Height: 1920,
		}},
	}, RenderModeFinal)
	if err != nil {
		t.Fatalf("BuildRenderPlan returned error: %v", err)
	}
	if !plan.ApplyOverlays {
		t.Fatal("final mode should apply overlays")
	}
	if !plan.ApplyLoudnorm {
		t.Fatal("final mode should apply loudnorm")
	}
}

func TestConcatListEscapesSingleQuotes(t *testing.T) {
	got := concatListContent([]string{"/tmp/edit's/segment_000.mp4"})
	want := "file '/tmp/edit\\'s/segment_000.mp4'\n"
	if got != want {
		t.Fatalf("concat list = %q, want %q", got, want)
	}
}

func TestSaveASRResultDownloadsTranscript(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "transcript.json")
	client := StaticHTTPClient{Body: []byte(`{"words":[{"type":"word","text":"你好","start":0,"end":0.3}]}`)}

	if err := SaveASRResult(context.Background(), client, "https://example.com/transcript.json", out); err != nil {
		t.Fatalf("SaveASRResult returned error: %v", err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "你好") {
		t.Fatalf("transcript not saved: %s", raw)
	}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}
