package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/anbanai/anban-creator/agent/videoexec"
	"github.com/urfave/cli/v3"
)

func runVideoCommand(args []string, stdout io.Writer) error {
	if stdout == nil {
		stdout = io.Discard
	}
	return newVideoCommand(stdout).Run(context.Background(), append([]string{"video"}, args...))
}

func newVideoCommand(stdout io.Writer) *cli.Command {
	if stdout == nil {
		stdout = io.Discard
	}
	return &cli.Command{
		Name:           "video",
		Usage:          "run local video media tools",
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
		Action: func(context.Context, *cli.Command) error {
			return fmt.Errorf("video subcommand is required")
		},
		Commands: []*cli.Command{
			videoProbeCommand(stdout),
			videoExtractAudioCommand(stdout),
			videoSaveASRResultCommand(stdout),
			videoPackTranscriptsCommand(stdout),
			videoMatchScriptCommand(stdout),
			videoRenderCommand(stdout),
			videoVerifyCommand(stdout),
		},
	}
}

func videoProbeCommand(stdout io.Writer) *cli.Command {
	return &cli.Command{
		Name:  "probe",
		Usage: "probe source video metadata",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "source", Usage: "source video path", Required: true},
			&cli.StringFlag{Name: "out", Usage: "manifest output path", Required: true},
			&cli.StringFlag{Name: "checksum", Usage: "optional source checksum"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			source := cmd.String("source")
			out := cmd.String("out")
			manifest, err := videoexec.Probe(source, cmd.String("checksum"))
			if err != nil {
				return err
			}
			if err := writeJSONFile(out, manifest); err != nil {
				return err
			}
			return json.NewEncoder(stdout).Encode(map[string]any{"output_path": out, "display_width": manifest.DisplayWidth, "display_height": manifest.DisplayHeight})
		},
	}
}

func videoExtractAudioCommand(stdout io.Writer) *cli.Command {
	return &cli.Command{
		Name:  "extract-audio",
		Usage: "extract 16k mono audio from a source video",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "source", Usage: "source video path", Required: true},
			&cli.StringFlag{Name: "out", Usage: "audio output path", Required: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			out := cmd.String("out")
			if err := videoexec.ExtractAudio(cmd.String("source"), out); err != nil {
				return err
			}
			return json.NewEncoder(stdout).Encode(map[string]any{"output_path": out})
		},
	}
}

func videoSaveASRResultCommand(stdout io.Writer) *cli.Command {
	return &cli.Command{
		Name:  "save-asr-result",
		Usage: "download and save normalized transcript JSON",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "transcript-url", Usage: "normalized transcript download URL", Required: true},
			&cli.StringFlag{Name: "out", Usage: "transcript output path", Required: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			out := cmd.String("out")
			if err := videoexec.SaveASRResult(ctx, nil, cmd.String("transcript-url"), out); err != nil {
				return err
			}
			return json.NewEncoder(stdout).Encode(map[string]any{"output_path": out})
		},
	}
}

func videoPackTranscriptsCommand(stdout io.Writer) *cli.Command {
	return &cli.Command{
		Name:  "pack-transcripts",
		Usage: "pack normalized transcript JSON files into markdown",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "transcripts-dir", Usage: "directory containing normalized transcript JSON files", Required: true},
			&cli.StringFlag{Name: "out", Usage: "takes_packed.md output path", Required: true},
			&cli.FloatFlag{Name: "silence-threshold", Usage: "silence threshold in seconds", Value: 0.5},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			out := cmd.String("out")
			md, err := videoexec.PackTranscriptFiles(cmd.String("transcripts-dir"), cmd.Float("silence-threshold"))
			if err != nil {
				return err
			}
			if err := writeTextFile(out, md); err != nil {
				return err
			}
			return json.NewEncoder(stdout).Encode(map[string]any{"output_path": out})
		},
	}
}

func videoMatchScriptCommand(stdout io.Writer) *cli.Command {
	return &cli.Command{
		Name:  "match-script",
		Usage: "match a script against transcript word ranges",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "script", Usage: "script markdown path", Required: true},
			&cli.StringFlag{Name: "transcripts-dir", Usage: "directory containing normalized transcript JSON files", Required: true},
			&cli.StringFlag{Name: "out", Usage: "edit-candidates JSON output path", Required: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			out := cmd.String("out")
			plan, err := videoexec.MatchScriptToTranscripts(cmd.String("script"), cmd.String("transcripts-dir"))
			if err != nil {
				return err
			}
			if err := writeJSONFile(out, plan); err != nil {
				return err
			}
			return json.NewEncoder(stdout).Encode(map[string]any{"output_path": out, "matches": len(plan.Matches), "unmatched": len(plan.UnmatchedLines)})
		},
	}
}

func videoRenderCommand(stdout io.Writer) *cli.Command {
	return &cli.Command{
		Name:  "render",
		Usage: "render an EDL to video",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "edl", Usage: "edl.json path", Required: true},
			&cli.StringFlag{Name: "out", Usage: "output video path", Required: true},
			&cli.StringFlag{Name: "mode", Usage: "draft, preview, or final", Value: string(videoexec.RenderModePreview)},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			out := cmd.String("out")
			plan, err := videoexec.Render(cmd.String("edl"), out, videoexec.RenderMode(cmd.String("mode")))
			if err != nil {
				return err
			}
			return json.NewEncoder(stdout).Encode(map[string]any{"output_path": out, "render_plan": plan})
		},
	}
}

func videoVerifyCommand(stdout io.Writer) *cli.Command {
	return &cli.Command{
		Name:  "verify",
		Usage: "validate an EDL",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "edl", Usage: "edl.json path", Required: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			edl := cmd.String("edl")
			var value videoexec.EDL
			if err := readJSONFile(edl, &value); err != nil {
				return err
			}
			if err := videoexec.ValidateEDL(value); err != nil {
				return err
			}
			return json.NewEncoder(stdout).Encode(map[string]any{"valid": true, "edl": edl})
		},
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
