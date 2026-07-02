package videoexec

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

func MatchScriptToTranscripts(scriptPath, transcriptsDir string) (*ScriptMatchPlan, error) {
	rawScript, err := os.ReadFile(scriptPath)
	if err != nil {
		return nil, fmt.Errorf("read script: %w", err)
	}
	transcripts, err := readTranscriptDir(transcriptsDir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(transcripts))
	for name := range transcripts {
		names = append(names, name)
	}
	sort.Strings(names)

	plan := &ScriptMatchPlan{}
	lines := scriptLines(string(rawScript))
	for idx, line := range lines {
		needle := normalizeText(line)
		if needle == "" {
			continue
		}
		var found *ScriptMatch
		for _, name := range names {
			words := speechWords(transcripts[name].Words)
			if match, ok := findLineInWords(idx, line, needle, name, words); ok {
				found = &match
				break
			}
		}
		if found != nil {
			plan.Matches = append(plan.Matches, *found)
		} else {
			plan.UnmatchedLines = append(plan.UnmatchedLines, UnmatchedLine{LineIndex: idx, Text: line})
		}
	}
	return plan, nil
}

func readTranscriptDir(dir string) (map[string]VideoTranscript, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read transcripts dir: %w", err)
	}
	out := map[string]VideoTranscript{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read transcript %s: %w", entry.Name(), err)
		}
		var transcript VideoTranscript
		if err := json.Unmarshal(raw, &transcript); err != nil {
			return nil, fmt.Errorf("decode transcript %s: %w", entry.Name(), err)
		}
		out[strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))] = transcript
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no transcript json files found")
	}
	return out, nil
}

func scriptLines(raw string) []string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(strings.Trim(line, "#*-0123456789. "))
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func speechWords(words []VideoTranscriptWord) []VideoTranscriptWord {
	out := make([]VideoTranscriptWord, 0, len(words))
	for _, w := range words {
		if w.Type != "" && w.Type != "word" && w.Type != "audio_event" {
			continue
		}
		if strings.TrimSpace(w.Text) == "" {
			continue
		}
		out = append(out, w)
	}
	return out
}

func findLineInWords(lineIndex int, original, needle, source string, words []VideoTranscriptWord) (ScriptMatch, bool) {
	for start := range words {
		var b strings.Builder
		for end := start; end < len(words); end++ {
			b.WriteString(normalizeText(words[end].Text))
			current := b.String()
			if current == needle {
				return ScriptMatch{
					LineIndex: lineIndex,
					Text:      original,
					Source:    source,
					Start:     words[start].Start,
					End:       words[end].End,
					WordStart: start,
					WordEnd:   end,
				}, true
			}
			if len([]rune(current)) > len([]rune(needle))+8 {
				break
			}
		}
	}
	return ScriptMatch{}, false
}

func normalizeText(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
