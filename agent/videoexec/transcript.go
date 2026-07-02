package videoexec

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func PackTranscriptFiles(transcriptsDir string, silenceThreshold float64) (string, error) {
	entries, err := os.ReadDir(transcriptsDir)
	if err != nil {
		return "", fmt.Errorf("read transcripts dir: %w", err)
	}
	transcripts := map[string]VideoTranscript{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(transcriptsDir, entry.Name()))
		if err != nil {
			return "", fmt.Errorf("read transcript %s: %w", entry.Name(), err)
		}
		var transcript VideoTranscript
		if err := json.Unmarshal(raw, &transcript); err != nil {
			return "", fmt.Errorf("decode transcript %s: %w", entry.Name(), err)
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		transcripts[name] = transcript
	}
	return PackTranscripts(transcripts, silenceThreshold)
}

func PackTranscripts(transcripts map[string]VideoTranscript, silenceThreshold float64) (string, error) {
	if silenceThreshold <= 0 {
		silenceThreshold = 0.5
	}
	if len(transcripts) == 0 {
		return "", fmt.Errorf("transcripts is required")
	}
	names := make([]string, 0, len(transcripts))
	for name := range transcripts {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("# Packed transcripts\n\n")
	fmt.Fprintf(&b, "Phrase-level, grouped on silences >= %.1fs or speaker change.\n", silenceThreshold)
	b.WriteString("Use `[start-end]` ranges to address cuts in the EDL.\n\n")
	for _, name := range names {
		phrases := groupTranscriptPhrases(transcripts[name].Words, silenceThreshold)
		duration := 0.0
		if len(phrases) > 0 {
			duration = phrases[len(phrases)-1].End - phrases[0].Start
		}
		fmt.Fprintf(&b, "## %s  (duration: %s, %d phrases)\n", name, formatDuration(duration), len(phrases))
		if len(phrases) == 0 {
			b.WriteString("  _no speech detected_\n\n")
			continue
		}
		for _, p := range phrases {
			fmt.Fprintf(&b, "  [%s-%s]%s %s\n", formatTime(p.Start), formatTime(p.End), speakerTag(p.SpeakerID), p.Text)
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

func groupTranscriptPhrases(words []VideoTranscriptWord, silenceThreshold float64) []VideoTranscriptPhrase {
	var phrases []VideoTranscriptPhrase
	var current []VideoTranscriptWord
	var currentStart float64
	var currentSpeaker string
	var prevEnd *float64

	flush := func() {
		if len(current) == 0 {
			return
		}
		parts := make([]string, 0, len(current))
		for _, w := range current {
			if text := strings.TrimSpace(w.Text); text != "" {
				parts = append(parts, text)
			}
		}
		if len(parts) > 0 {
			phrases = append(phrases, VideoTranscriptPhrase{
				Start:     currentStart,
				End:       current[len(current)-1].End,
				Text:      strings.Join(parts, ""),
				SpeakerID: currentSpeaker,
			})
		}
		current = nil
		currentSpeaker = ""
	}

	for _, w := range words {
		if w.Type == "spacing" {
			if w.End-w.Start >= silenceThreshold {
				flush()
			}
			continue
		}
		if w.Type != "" && w.Type != "word" && w.Type != "audio_event" {
			continue
		}
		if strings.TrimSpace(w.Text) == "" {
			continue
		}
		if currentSpeaker != "" && w.SpeakerID != "" && w.SpeakerID != currentSpeaker {
			flush()
		}
		if prevEnd != nil && w.Start-*prevEnd >= silenceThreshold {
			flush()
		}
		if len(current) == 0 {
			currentStart = w.Start
			currentSpeaker = w.SpeakerID
		}
		current = append(current, w)
		end := w.End
		prevEnd = &end
	}
	flush()
	return phrases
}

type HTTPGetter interface {
	Get(ctx context.Context, url string) ([]byte, error)
}

type NetHTTPGetter struct {
	Client *http.Client
}

func (g NetHTTPGetter) Get(ctx context.Context, url string) ([]byte, error) {
	client := g.Client
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download transcript failed: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

type StaticHTTPClient struct {
	Body []byte
	Err  error
}

func (c StaticHTTPClient) Get(context.Context, string) ([]byte, error) {
	return c.Body, c.Err
}

func SaveASRResult(ctx context.Context, getter HTTPGetter, transcriptURL, outPath string) error {
	if strings.TrimSpace(transcriptURL) == "" {
		return fmt.Errorf("transcript url is required")
	}
	if getter == nil {
		getter = NetHTTPGetter{}
	}
	raw, err := getter.Get(ctx, transcriptURL)
	if err != nil {
		return err
	}
	var transcript VideoTranscript
	if err := json.Unmarshal(raw, &transcript); err != nil {
		return fmt.Errorf("downloaded transcript is not normalized video transcript JSON: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("create transcript dir: %w", err)
	}
	return os.WriteFile(outPath, raw, 0o644)
}

func speakerTag(speakerID string) string {
	if speakerID == "" {
		return ""
	}
	return " S" + strings.TrimPrefix(speakerID, "speaker_")
}

func formatTime(seconds float64) string {
	return fmt.Sprintf("%06.2f", seconds)
}

func formatDuration(seconds float64) string {
	if seconds < 60 {
		return fmt.Sprintf("%.1fs", seconds)
	}
	minutes := int(seconds / 60)
	remaining := seconds - float64(minutes*60)
	return fmt.Sprintf("%dm %04.1fs", minutes, remaining)
}
