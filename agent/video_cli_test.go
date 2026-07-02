package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunVideoCommandPackTranscripts(t *testing.T) {
	dir := t.TempDir()
	transcriptsDir := filepath.Join(dir, "transcripts")
	if err := os.MkdirAll(transcriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(transcriptsDir, "take-a.json"), []byte(`{"words":[{"type":"word","text":"第一","start":0,"end":0.2},{"type":"spacing","start":0.2,"end":0.8},{"type":"word","text":"第二","start":0.8,"end":1.0}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "takes_packed.md")
	var stdout bytes.Buffer

	err := runVideoCommand([]string{"pack-transcripts", "--transcripts-dir", transcriptsDir, "--out", out}, &stdout)
	if err != nil {
		t.Fatalf("runVideoCommand returned error: %v", err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "## take-a") || !strings.Contains(string(raw), "第一") {
		t.Fatalf("packed markdown not written: %s", raw)
	}
	if !strings.Contains(stdout.String(), `"output_path"`) {
		t.Fatalf("stdout should include output_path JSON, got %s", stdout.String())
	}
}

func TestRunVideoCommandPackTranscriptsCreatesOutputDirectory(t *testing.T) {
	dir := t.TempDir()
	transcriptsDir := filepath.Join(dir, "transcripts")
	if err := os.MkdirAll(transcriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(transcriptsDir, "take-a.json"), []byte(`{"words":[{"type":"word","text":"第一","start":0,"end":0.2}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "nested", "edit", "takes_packed.md")

	err := runVideoCommand([]string{"pack-transcripts", "--transcripts-dir", transcriptsDir, "--out", out}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("runVideoCommand returned error: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("expected output file to be created: %v", err)
	}
}

func TestRunVideoCommandValidateEDLRejectsOverlayMismatch(t *testing.T) {
	dir := t.TempDir()
	edlPath := filepath.Join(dir, "edl.json")
	raw := map[string]any{
		"output_width": 1080, "output_height": 1920,
		"sources":  map[string]string{"take-a": "/tmp/take-a.mp4"},
		"ranges":   []map[string]any{{"source": "take-a", "start": 0, "end": 1}},
		"overlays": []map[string]any{{"file": "ov.webm", "start": 0, "end": 1, "width": 1920, "height": 1080}},
	}
	encoded, _ := json.Marshal(raw)
	if err := os.WriteFile(edlPath, encoded, 0o644); err != nil {
		t.Fatal(err)
	}

	err := runVideoCommand([]string{"verify", "--edl", edlPath}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected verify to reject overlay mismatch")
	}
	if !strings.Contains(err.Error(), "overlay") {
		t.Fatalf("unexpected error: %v", err)
	}
}
