package videoexec

import "time"

type MediaManifest struct {
	SourcePath        string        `json:"source_path"`
	Checksum          string        `json:"checksum,omitempty"`
	DurationSeconds   float64       `json:"duration_seconds,omitempty"`
	EncodedWidth      int           `json:"encoded_width"`
	EncodedHeight     int           `json:"encoded_height"`
	DisplayWidth      int           `json:"display_width"`
	DisplayHeight     int           `json:"display_height"`
	RotationDegrees   int           `json:"rotation_degrees"`
	IsPortraitDisplay bool          `json:"is_portrait_display"`
	FPS               float64       `json:"fps,omitempty"`
	HDR               bool          `json:"hdr"`
	ColorTransfer     string        `json:"color_transfer,omitempty"`
	AudioStreams      []AudioStream `json:"audio_streams,omitempty"`
	ProbedAt          time.Time     `json:"probed_at"`
}

type AudioStream struct {
	CodecName  string `json:"codec_name,omitempty"`
	Channels   int    `json:"channels,omitempty"`
	SampleRate string `json:"sample_rate,omitempty"`
}

type VideoTranscript struct {
	Words    []VideoTranscriptWord   `json:"words"`
	Phrases  []VideoTranscriptPhrase `json:"phrases,omitempty"`
	Metadata map[string]any          `json:"metadata,omitempty"`
}

type VideoTranscriptWord struct {
	Type      string  `json:"type"`
	Text      string  `json:"text,omitempty"`
	Start     float64 `json:"start,omitempty"`
	End       float64 `json:"end,omitempty"`
	SpeakerID string  `json:"speaker_id,omitempty"`
}

type VideoTranscriptPhrase struct {
	Start     float64 `json:"start"`
	End       float64 `json:"end"`
	Text      string  `json:"text"`
	SpeakerID string  `json:"speaker_id,omitempty"`
}

type EDL struct {
	Version      string            `json:"version,omitempty"`
	Sources      map[string]string `json:"sources"`
	OutputWidth  int               `json:"output_width"`
	OutputHeight int               `json:"output_height"`
	Ranges       []EDLRange        `json:"ranges"`
	Subtitles    any               `json:"subtitles,omitempty"`
	Overlays     []EDLOverlay      `json:"overlays,omitempty"`
	Grade        string            `json:"grade,omitempty"`
}

type EDLRange struct {
	Source string  `json:"source"`
	Start  float64 `json:"start"`
	End    float64 `json:"end"`
	Beat   string  `json:"beat,omitempty"`
	Note   string  `json:"note,omitempty"`
}

type EDLOverlay struct {
	File   string  `json:"file"`
	Start  float64 `json:"start"`
	End    float64 `json:"end"`
	X      int     `json:"x,omitempty"`
	Y      int     `json:"y,omitempty"`
	Width  int     `json:"width"`
	Height int     `json:"height"`
}

type RenderMode string

const (
	RenderModeDraft   RenderMode = "draft"
	RenderModePreview RenderMode = "preview"
	RenderModeFinal   RenderMode = "final"
)

type RenderPlan struct {
	Mode          RenderMode `json:"mode"`
	OutputWidth   int        `json:"output_width"`
	OutputHeight  int        `json:"output_height"`
	ScaleHeight   int        `json:"scale_height"`
	VideoFilter   string     `json:"video_filter"`
	ApplyOverlays bool       `json:"apply_overlays"`
	ApplyLoudnorm bool       `json:"apply_loudnorm"`
	CRF           int        `json:"crf"`
	Preset        string     `json:"preset"`
	SegmentCount  int        `json:"segment_count"`
}

type ScriptMatchPlan struct {
	Matches        []ScriptMatch   `json:"matches"`
	UnmatchedLines []UnmatchedLine `json:"unmatched_lines,omitempty"`
}

type ScriptMatch struct {
	LineIndex int     `json:"line_index"`
	Text      string  `json:"text"`
	Source    string  `json:"source"`
	Start     float64 `json:"start"`
	End       float64 `json:"end"`
	WordStart int     `json:"word_start"`
	WordEnd   int     `json:"word_end"`
}

type UnmatchedLine struct {
	LineIndex int    `json:"line_index"`
	Text      string `json:"text"`
}
