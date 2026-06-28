package service

import (
	"fmt"
	"strings"
)

// encodeOptions captures deterministic ffmpeg re-encode parameters shared by every clip.
// Quality flags (yuv420p, CRF, faststart) and optional loudnorm apply on every re-encode path;
// a non-empty videoFilter adds a -vf chain and forces re-encoding (suppressing fast stream-copy).
type encodeOptions struct {
	videoFilter  string // -vf chain (empty = no video filter)
	audioFilter  string // -af chain (empty = no audio filter)
	preset       string
	crf          string
	pixFmt       string
	audioCodec   string
	audioBitrate string
	faststart    bool
}

// encodePlanInput holds the orientation and loudness knobs common to both clip-plan request types.
type encodePlanInput struct {
	targetMode    string
	verticalFill  string
	sourceWidth   int
	sourceHeight  int
	targetWidth   int
	targetHeight  int
	normalizeLoud bool
}

// encodeDecision is the resolved re-encode configuration for a clip plan.
type encodeDecision struct {
	opts          encodeOptions
	needsReencode bool   // true when a video filter forces re-encoding (suppresses fast copy)
	orientation   string // human-readable transform label for transparency / decision logs
}

const (
	defaultTargetWidth  = 1080
	defaultTargetHeight = 1920
	// loudnormFilter is an EBU R128-ish loudness filter used on re-encode paths (Douyin-safe).
	loudnormFilter = "loudnorm=I=-16:TP=-1.5:LRA=11"
)

// computeEncodeOptions resolves the deterministic re-encode configuration from request knobs.
// Quality flags and loudnorm apply on every re-encode path; an orientation transform adds a
// -vf chain and forces re-encoding (which suppresses the fast stream-copy attempt).
// Source dimensions are required to convert orientation — without them the clip passes through.
func computeEncodeOptions(in encodePlanInput) encodeDecision {
	targetW := in.targetWidth
	if targetW <= 0 {
		targetW = defaultTargetWidth
	}
	targetH := in.targetHeight
	if targetH <= 0 {
		targetH = defaultTargetHeight
	}
	opts := encodeOptions{
		preset:       "veryfast",
		crf:          "20",
		pixFmt:       "yuv420p",
		audioCodec:   "aac",
		audioBitrate: "128k",
		faststart:    true,
	}
	if in.normalizeLoud {
		opts.audioFilter = loudnormFilter
	}
	dec := encodeDecision{opts: opts}
	mode := strings.ToLower(strings.TrimSpace(in.targetMode))
	fill := strings.ToLower(strings.TrimSpace(in.verticalFill))
	sourceKnown := in.sourceWidth > 0 && in.sourceHeight > 0
	if !sourceKnown || mode == "original" {
		dec.orientation = "passthrough"
		return dec
	}
	switch mode {
	case "horizontal":
		if in.sourceHeight > in.sourceWidth {
			// landscape canvas = the vertical target dims swapped (defaults 1080x1920 → 1920x1080)
			dec.opts.videoFilter = fmt.Sprintf("scale=%d:%d,setsar=1", targetH, targetW)
			dec.orientation = "vertical-to-horizontal"
		} else {
			dec.orientation = "horizontal"
		}
	default: // "", "auto", "vertical" → target vertical (short-video default)
		if in.sourceWidth >= in.sourceHeight {
			// landscape or square source → convert to vertical
			vf := verticalFillFilter(fill, targetW, targetH)
			dec.opts.videoFilter = vf
			dec.orientation = "landscape-to-vertical:" + fillLabel(fill)
		} else if in.sourceWidth != targetW || in.sourceHeight != targetH {
			// already vertical but not the target canvas → normalize size
			dec.opts.videoFilter = fmt.Sprintf("scale=%d:%d,setsar=1", targetW, targetH)
			dec.orientation = "vertical:scale"
		} else {
			dec.orientation = "vertical:passthrough"
		}
	}
	dec.needsReencode = dec.opts.videoFilter != ""
	return dec
}

// verticalFillFilter returns the -vf chain that converts a landscape source to a vertical canvas.
func verticalFillFilter(fill string, targetW, targetH int) string {
	switch fill {
	case "crop":
		return fmt.Sprintf("crop=ih*%d/%d:ih,scale=%d:%d,setsar=1", targetW, targetH, targetW, targetH)
	case "none":
		return ""
	default: // blur: blurred full-frame background + centered foreground (mainstream 直播切片 look)
		return fmt.Sprintf("split[bg][fg];[bg]scale=%d:%d,boxblur=20:5[bg];[fg]scale=%d:-2[fg];[bg][fg]overlay=(W-w)/2:(H-h)/2", targetW, targetH, targetW)
	}
}

func fillLabel(fill string) string {
	switch fill {
	case "crop":
		return "crop"
	case "none":
		return "none"
	default:
		return "blur"
	}
}

// buildEncodeArgs assembles the accurate (re-encode) ffmpeg command. Output is always the last arg
// so callers can safely reference args[len-1] without index surgery.
func buildEncodeArgs(start, duration float64, videoPath, output string, opts encodeOptions) []string {
	args := []string{"ffmpeg", "-y", "-ss", formatSeconds(start), "-i", videoPath, "-t", formatSeconds(duration)}
	if opts.videoFilter != "" {
		args = append(args, "-vf", opts.videoFilter)
	}
	if opts.audioFilter != "" {
		args = append(args, "-af", opts.audioFilter)
	}
	args = append(args, "-c:v", "libx264")
	if opts.preset != "" {
		args = append(args, "-preset", opts.preset)
	}
	if opts.crf != "" {
		args = append(args, "-crf", opts.crf)
	}
	if opts.pixFmt != "" {
		args = append(args, "-pix_fmt", opts.pixFmt)
	}
	if opts.audioCodec != "" {
		args = append(args, "-c:a", opts.audioCodec)
	}
	if opts.audioBitrate != "" {
		args = append(args, "-b:a", opts.audioBitrate)
	}
	if opts.faststart {
		args = append(args, "-movflags", "+faststart")
	}
	return append(args, output)
}
