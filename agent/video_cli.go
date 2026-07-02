package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/anbanai/anban-creator/agent/videoexec"
)

func maybeRunVideoCommand(args []string, stdout io.Writer, stderr io.Writer) bool {
	if len(args) == 0 || args[0] != "video" {
		return false
	}
	if err := runVideoCommand(args[1:], stdout); err != nil {
		fmt.Fprintf(stderr, "video command failed: %v\n", err)
		os.Exit(2)
	}
	return true
}

func runVideoCommand(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("video subcommand is required")
	}
	switch args[0] {
	case "probe":
		fs := flag.NewFlagSet("video probe", flag.ContinueOnError)
		source := fs.String("source", "", "source video path")
		out := fs.String("out", "", "manifest output path")
		checksum := fs.String("checksum", "", "optional source checksum")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *source == "" || *out == "" {
			return fmt.Errorf("--source and --out are required")
		}
		manifest, err := videoexec.Probe(*source, *checksum)
		if err != nil {
			return err
		}
		if err := writeJSONFile(*out, manifest); err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"output_path": *out, "display_width": manifest.DisplayWidth, "display_height": manifest.DisplayHeight})
	case "extract-audio":
		fs := flag.NewFlagSet("video extract-audio", flag.ContinueOnError)
		source := fs.String("source", "", "source video path")
		out := fs.String("out", "", "audio output path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *source == "" || *out == "" {
			return fmt.Errorf("--source and --out are required")
		}
		if err := videoexec.ExtractAudio(*source, *out); err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"output_path": *out})
	case "save-asr-result":
		fs := flag.NewFlagSet("video save-asr-result", flag.ContinueOnError)
		url := fs.String("transcript-url", "", "normalized transcript download URL")
		out := fs.String("out", "", "transcript output path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *url == "" || *out == "" {
			return fmt.Errorf("--transcript-url and --out are required")
		}
		if err := videoexec.SaveASRResult(context.Background(), nil, *url, *out); err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"output_path": *out})
	case "pack-transcripts":
		fs := flag.NewFlagSet("video pack-transcripts", flag.ContinueOnError)
		dir := fs.String("transcripts-dir", "", "directory containing normalized transcript JSON files")
		out := fs.String("out", "", "takes_packed.md output path")
		silence := fs.Float64("silence-threshold", 0.5, "silence threshold in seconds")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *dir == "" || *out == "" {
			return fmt.Errorf("--transcripts-dir and --out are required")
		}
		md, err := videoexec.PackTranscriptFiles(*dir, *silence)
		if err != nil {
			return err
		}
		if err := writeTextFile(*out, md); err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"output_path": *out})
	case "match-script":
		fs := flag.NewFlagSet("video match-script", flag.ContinueOnError)
		script := fs.String("script", "", "script markdown path")
		dir := fs.String("transcripts-dir", "", "directory containing normalized transcript JSON files")
		out := fs.String("out", "", "edit-candidates JSON output path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *script == "" || *dir == "" || *out == "" {
			return fmt.Errorf("--script, --transcripts-dir and --out are required")
		}
		plan, err := videoexec.MatchScriptToTranscripts(*script, *dir)
		if err != nil {
			return err
		}
		if err := writeJSONFile(*out, plan); err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"output_path": *out, "matches": len(plan.Matches), "unmatched": len(plan.UnmatchedLines)})
	case "render":
		fs := flag.NewFlagSet("video render", flag.ContinueOnError)
		edl := fs.String("edl", "", "edl.json path")
		out := fs.String("out", "", "output video path")
		mode := fs.String("mode", string(videoexec.RenderModePreview), "draft, preview, or final")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *edl == "" || *out == "" {
			return fmt.Errorf("--edl and --out are required")
		}
		plan, err := videoexec.Render(*edl, *out, videoexec.RenderMode(*mode))
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"output_path": *out, "render_plan": plan})
	case "verify":
		fs := flag.NewFlagSet("video verify", flag.ContinueOnError)
		edl := fs.String("edl", "", "edl.json path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *edl == "" {
			return fmt.Errorf("--edl is required")
		}
		var value videoexec.EDL
		if err := readJSONFile(*edl, &value); err != nil {
			return err
		}
		if err := videoexec.ValidateEDL(value); err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"valid": true, "edl": *edl})
	default:
		return fmt.Errorf("unknown video subcommand %q", args[0])
	}
}

func writeJSONFile(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func writeTextFile(path string, value string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(value), 0o644)
}

func readJSONFile(path string, value any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, value)
}
