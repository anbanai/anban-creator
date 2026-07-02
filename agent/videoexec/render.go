package videoexec

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func ValidateEDL(edl EDL) error {
	if edl.OutputWidth <= 0 || edl.OutputHeight <= 0 {
		return fmt.Errorf("edl output_width and output_height are required")
	}
	if len(edl.Sources) == 0 {
		return fmt.Errorf("edl sources are required")
	}
	if len(edl.Ranges) == 0 {
		return fmt.Errorf("edl ranges are required")
	}
	for i, r := range edl.Ranges {
		if strings.TrimSpace(r.Source) == "" {
			return fmt.Errorf("range %d source is required", i)
		}
		if _, ok := edl.Sources[r.Source]; !ok {
			return fmt.Errorf("range %d source %q is not declared", i, r.Source)
		}
		if r.End <= r.Start {
			return fmt.Errorf("range %d end must be greater than start", i)
		}
	}
	for i, overlay := range edl.Overlays {
		if strings.TrimSpace(overlay.File) == "" {
			return fmt.Errorf("overlay %d file is required", i)
		}
		if overlay.End <= overlay.Start {
			return fmt.Errorf("overlay %d end must be greater than start", i)
		}
		if overlay.Width != edl.OutputWidth || overlay.Height != edl.OutputHeight {
			return fmt.Errorf("overlay %d size %dx%d does not match output %dx%d", i, overlay.Width, overlay.Height, edl.OutputWidth, edl.OutputHeight)
		}
	}
	if strings.TrimSpace(edl.Grade) != "" && strings.TrimSpace(edl.Grade) != "none" {
		return fmt.Errorf("grade %q is not supported by Go video render yet; use grade.py before rendering or set grade to none", edl.Grade)
	}
	return nil
}

func BuildRenderPlan(edl EDL, mode RenderMode) (*RenderPlan, error) {
	if mode == "" {
		mode = RenderModePreview
	}
	if err := ValidateEDL(edl); err != nil {
		return nil, err
	}
	filter := fixedFrameFilter(edl.OutputWidth, edl.OutputHeight)
	switch mode {
	case RenderModeDraft:
		draftHeight := minInt(1280, edl.OutputHeight)
		if draftHeight <= 0 {
			draftHeight = edl.OutputHeight
		}
		draftWidth := evenInt(edl.OutputWidth * draftHeight / edl.OutputHeight)
		if draftWidth <= 0 {
			draftWidth = edl.OutputWidth
		}
		return &RenderPlan{Mode: mode, OutputWidth: draftWidth, OutputHeight: draftHeight, ScaleHeight: draftHeight, VideoFilter: fixedFrameFilter(draftWidth, draftHeight), ApplyOverlays: false, ApplyLoudnorm: false, CRF: 28, Preset: "ultrafast", SegmentCount: len(edl.Ranges)}, nil
	case RenderModePreview:
		return &RenderPlan{Mode: mode, OutputWidth: edl.OutputWidth, OutputHeight: edl.OutputHeight, ScaleHeight: edl.OutputHeight, VideoFilter: filter, ApplyOverlays: len(edl.Overlays) > 0, ApplyLoudnorm: false, CRF: 22, Preset: "medium", SegmentCount: len(edl.Ranges)}, nil
	case RenderModeFinal:
		return &RenderPlan{Mode: mode, OutputWidth: edl.OutputWidth, OutputHeight: edl.OutputHeight, ScaleHeight: edl.OutputHeight, VideoFilter: filter, ApplyOverlays: len(edl.Overlays) > 0, ApplyLoudnorm: true, CRF: 20, Preset: "fast", SegmentCount: len(edl.Ranges)}, nil
	default:
		return nil, fmt.Errorf("unsupported render mode %q", mode)
	}
}

func Render(edlPath, outputPath string, mode RenderMode) (*RenderPlan, error) {
	raw, err := os.ReadFile(edlPath)
	if err != nil {
		return nil, fmt.Errorf("read edl: %w", err)
	}
	var edl EDL
	if err := json.Unmarshal(raw, &edl); err != nil {
		return nil, fmt.Errorf("decode edl: %w", err)
	}
	plan, err := BuildRenderPlan(edl, mode)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}
	workDir := filepath.Join(filepath.Dir(outputPath), ".anban-video-render", strings.TrimSuffix(filepath.Base(outputPath), filepath.Ext(outputPath)))
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, fmt.Errorf("create render work dir: %w", err)
	}
	var segmentPaths []string
	for i, r := range edl.Ranges {
		segmentPath := filepath.Join(workDir, fmt.Sprintf("segment_%03d.mp4", i))
		source := edl.Sources[r.Source]
		if !filepath.IsAbs(source) {
			source = filepath.Join(filepath.Dir(edlPath), source)
		}
		if err := renderSegment(source, segmentPath, r, plan); err != nil {
			return plan, err
		}
		segmentPaths = append(segmentPaths, segmentPath)
	}
	basePath := filepath.Join(workDir, "base.mp4")
	if len(segmentPaths) == 1 {
		if err := os.Rename(segmentPaths[0], basePath); err != nil {
			return plan, fmt.Errorf("move rendered segment: %w", err)
		}
	} else if err := concatSegments(segmentPaths, basePath, workDir); err != nil {
		return plan, err
	}
	currentPath := basePath
	if plan.ApplyOverlays || edl.Subtitles != nil {
		compositePath := filepath.Join(workDir, "composited.mp4")
		if err := compositeTimeline(currentPath, compositePath, edl, filepath.Dir(edlPath)); err != nil {
			return plan, err
		}
		currentPath = compositePath
	}
	if plan.ApplyLoudnorm {
		if err := applyLoudnorm(currentPath, outputPath); err != nil {
			return plan, err
		}
		return plan, nil
	}
	if err := os.Rename(currentPath, outputPath); err != nil {
		return plan, fmt.Errorf("move rendered output: %w", err)
	}
	return plan, nil
}

func renderSegment(source, outputPath string, r EDLRange, plan *RenderPlan) error {
	cmd := exec.Command(
		"ffmpeg", "-y",
		"-ss", fmt.Sprintf("%.3f", r.Start),
		"-i", source,
		"-t", fmt.Sprintf("%.3f", r.End-r.Start),
		"-vf", plan.VideoFilter,
		"-c:v", "libx264", "-preset", plan.Preset, "-crf", fmt.Sprintf("%d", plan.CRF),
		"-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "192k", "-ar", "48000",
		"-movflags", "+faststart",
		outputPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg render segment: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func fixedFrameFilter(width, height int) string {
	return fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1", width, height, width, height)
}

func evenInt(value int) int {
	if value%2 != 0 {
		value--
	}
	return value
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func compositeTimeline(basePath, outputPath string, edl EDL, edlDir string) error {
	subtitlePath, err := resolveSubtitlePath(edl.Subtitles, edlDir)
	if err != nil {
		return err
	}
	if len(edl.Overlays) == 0 && subtitlePath == "" {
		if err := os.Rename(basePath, outputPath); err != nil {
			return fmt.Errorf("move base composite: %w", err)
		}
		return nil
	}
	args := []string{"-y", "-i", basePath}
	for _, overlay := range edl.Overlays {
		args = append(args, "-i", resolveMediaPath(overlay.File, edlDir))
	}
	var filters []string
	current := "[0:v]"
	for i, overlay := range edl.Overlays {
		inputIndex := i + 1
		overlayLabel := fmt.Sprintf("[ov%d]", inputIndex)
		next := fmt.Sprintf("[v%d]", inputIndex)
		filters = append(filters, fmt.Sprintf("[%d:v]setpts=PTS-STARTPTS+%.3f/TB%s", inputIndex, overlay.Start, overlayLabel))
		filters = append(filters, fmt.Sprintf("%s%soverlay=%d:%d:enable='between(t,%.3f,%.3f)'%s", current, overlayLabel, overlay.X, overlay.Y, overlay.Start, overlay.End, next))
		current = next
	}
	outLabel := current
	if subtitlePath != "" {
		outLabel = "[outv]"
		filters = append(filters, fmt.Sprintf("%ssubtitles='%s'%s", current, escapeFFmpegFilterPath(subtitlePath), outLabel))
	}
	args = append(args,
		"-filter_complex", strings.Join(filters, ";"),
		"-map", outLabel,
		"-map", "0:a?",
		"-c:v", "libx264", "-preset", "fast", "-crf", "18",
		"-pix_fmt", "yuv420p",
		"-c:a", "copy",
		"-movflags", "+faststart",
		outputPath,
	)
	cmd := exec.Command("ffmpeg", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg composite: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func applyLoudnorm(inputPath, outputPath string) error {
	cmd := exec.Command(
		"ffmpeg", "-y",
		"-i", inputPath,
		"-c:v", "copy",
		"-af", "loudnorm=I=-16:TP=-1.5:LRA=11",
		"-c:a", "aac", "-b:a", "192k", "-ar", "48000",
		"-movflags", "+faststart",
		outputPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg loudnorm: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func resolveMediaPath(pathValue, baseDir string) string {
	if filepath.IsAbs(pathValue) {
		return pathValue
	}
	return filepath.Join(baseDir, pathValue)
}

func resolveSubtitlePath(value any, baseDir string) (string, error) {
	switch v := value.(type) {
	case nil:
		return "", nil
	case string:
		if strings.TrimSpace(v) == "" {
			return "", nil
		}
		return resolveMediaPath(v, baseDir), nil
	case map[string]any:
		if enabled, ok := v["enabled"].(bool); ok && !enabled {
			return "", nil
		}
		for _, key := range []string{"file", "path"} {
			if raw, ok := v[key].(string); ok && strings.TrimSpace(raw) != "" {
				return resolveMediaPath(raw, baseDir), nil
			}
		}
		return "", fmt.Errorf("subtitles enabled but no file/path was provided")
	default:
		return "", fmt.Errorf("unsupported subtitles value %T", value)
	}
}

func escapeFFmpegFilterPath(pathValue string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `'`, `\'`, `:`, `\:`)
	return replacer.Replace(pathValue)
}

func concatSegments(segmentPaths []string, outputPath, workDir string) error {
	listPath := filepath.Join(workDir, "concat.txt")
	if err := os.WriteFile(listPath, []byte(concatListContent(segmentPaths)), 0o644); err != nil {
		return fmt.Errorf("write concat list: %w", err)
	}
	cmd := exec.Command(
		"ffmpeg", "-y",
		"-f", "concat", "-safe", "0",
		"-i", listPath,
		"-c", "copy",
		"-movflags", "+faststart",
		outputPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg concat: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func concatListContent(segmentPaths []string) string {
	var b strings.Builder
	for _, segmentPath := range segmentPaths {
		escaped := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(filepath.Clean(segmentPath))
		fmt.Fprintf(&b, "file '%s'\n", escaped)
	}
	return b.String()
}
