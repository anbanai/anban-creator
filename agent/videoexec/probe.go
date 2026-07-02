package videoexec

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

var hdrTransfers = map[string]bool{
	"smpte2084":    true,
	"arib-std-b67": true,
}

type ffprobeOutput struct {
	Streams []ffprobeStream `json:"streams"`
	Format  struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

type ffprobeStream struct {
	CodecType     string             `json:"codec_type"`
	CodecName     string             `json:"codec_name"`
	Width         int                `json:"width"`
	Height        int                `json:"height"`
	AvgFrameRate  string             `json:"avg_frame_rate"`
	Duration      string             `json:"duration"`
	ColorTransfer string             `json:"color_transfer"`
	Channels      int                `json:"channels"`
	SampleRate    string             `json:"sample_rate"`
	SideDataList  []map[string]any   `json:"side_data_list"`
	Tags          map[string]string  `json:"tags"`
	Disposition   map[string]float64 `json:"disposition"`
}

func Probe(sourcePath, checksum string) (*MediaManifest, error) {
	out, err := exec.Command(
		"ffprobe", "-v", "error",
		"-show_entries", "format=duration:stream=codec_type,codec_name,width,height,avg_frame_rate,duration,color_transfer,channels,sample_rate:stream_side_data=rotation",
		"-of", "json",
		sourcePath,
	).Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe %s: %w", sourcePath, err)
	}
	return ParseProbeJSON(sourcePath, out, checksum)
}

func ParseProbeJSON(sourcePath string, raw []byte, checksum string) (*MediaManifest, error) {
	var parsed ffprobeOutput
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("decode ffprobe json: %w", err)
	}
	var video *ffprobeStream
	for i := range parsed.Streams {
		s := &parsed.Streams[i]
		switch s.CodecType {
		case "video":
			if video == nil {
				video = s
			}
		case "audio":
			// collected below after manifest creation
		}
	}
	if video == nil {
		return nil, fmt.Errorf("no video stream found")
	}

	rotation := displayRotation(*video)
	displayW, displayH := video.Width, video.Height
	if absInt(rotation)%180 == 90 {
		displayW, displayH = displayH, displayW
	}
	duration := parseFloat(video.Duration)
	if duration == 0 {
		duration = parseFloat(parsed.Format.Duration)
	}
	manifest := &MediaManifest{
		SourcePath:        sourcePath,
		Checksum:          checksum,
		DurationSeconds:   duration,
		EncodedWidth:      video.Width,
		EncodedHeight:     video.Height,
		DisplayWidth:      displayW,
		DisplayHeight:     displayH,
		RotationDegrees:   rotation,
		IsPortraitDisplay: displayH > displayW,
		FPS:               parseRational(video.AvgFrameRate),
		HDR:               hdrTransfers[strings.TrimSpace(video.ColorTransfer)],
		ColorTransfer:     video.ColorTransfer,
		ProbedAt:          time.Now().UTC(),
	}
	for _, s := range parsed.Streams {
		if s.CodecType != "audio" {
			continue
		}
		manifest.AudioStreams = append(manifest.AudioStreams, AudioStream{
			CodecName:  s.CodecName,
			Channels:   s.Channels,
			SampleRate: s.SampleRate,
		})
	}
	return manifest, nil
}

func displayRotation(stream ffprobeStream) int {
	for _, sideData := range stream.SideDataList {
		for _, key := range []string{"rotation", "displaymatrix"} {
			if v, ok := sideData[key]; ok {
				if rot, ok := numberToInt(v); ok {
					return rot
				}
			}
		}
	}
	if stream.Tags != nil {
		if raw := strings.TrimSpace(stream.Tags["rotate"]); raw != "" {
			if rot, err := strconv.Atoi(raw); err == nil {
				return rot
			}
		}
	}
	return 0
}

func parseRational(raw string) float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "0/0" {
		return 0
	}
	parts := strings.Split(raw, "/")
	if len(parts) == 1 {
		return parseFloat(raw)
	}
	if len(parts) != 2 {
		return 0
	}
	num := parseFloat(parts[0])
	den := parseFloat(parts[1])
	if den == 0 {
		return 0
	}
	return num / den
}

func parseFloat(raw string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	return v
}

func numberToInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(n))
		return i, err == nil
	default:
		return 0, false
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
