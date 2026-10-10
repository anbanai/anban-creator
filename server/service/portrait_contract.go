package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode/utf16"
)

var ErrPortraitInput = errors.New("对话格式或长度不符合要求，请缩短后重试。")
var ErrPortraitOutput = errors.New("画像未能对应到你的原话，原有文字与画像已保留，请重试。")
var portraitID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
var portraitFacets = []string{"identity", "audience", "style", "platforms", "preferences", "experience"}

type PortraitMessage struct {
	ID   string `json:"id"`
	Role string `json:"role"`
	Text string `json:"text"`
}
type PortraitRequest struct {
	Messages []PortraitMessage `json:"messages"`
}
type PortraitEvidence struct {
	MessageID string `json:"messageId"`
	Quote     string `json:"quote"`
}
type PortraitFact struct {
	Text      string             `json:"text"`
	Certainty string             `json:"certainty"`
	Evidence  []PortraitEvidence `json:"evidence"`
}
type PortraitCandidate struct {
	Reply        string                   `json:"reply"`
	Name         *string                  `json:"name"`
	Summary      *string                  `json:"summary"`
	Facets       map[string]*PortraitFact `json:"facets"`
	CreationIdea *PortraitFact            `json:"creationIdea"`
}

// Match JavaScript/Zod string budgets, including supplementary Unicode characters.
func portraitLength(s string) int { return len(utf16.Encode([]rune(s))) }
func portraitString(s string, max int) bool {
	return strings.TrimSpace(s) != "" && portraitLength(s) <= max
}
func strictPortraitJSON(data []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return ErrPortraitInput
	}
	return nil
}
func ParsePortraitRequest(data []byte) (PortraitRequest, error) {
	var r PortraitRequest
	if len(data) > 180000 || strictPortraitJSON(data, &r) != nil || len(r.Messages) < 1 || len(r.Messages) > 59 {
		return r, ErrPortraitInput
	}
	seen := map[string]bool{}
	total := 0
	for i, m := range r.Messages {
		if !portraitID.MatchString(m.ID) || seen[m.ID] || (m.Role != "user" && m.Role != "assistant") || !portraitString(m.Text, 6000) {
			return r, ErrPortraitInput
		}
		seen[m.ID] = true
		total += portraitLength(m.Text)
		r.Messages[i].Text = strings.TrimSpace(m.Text)
	}
	// Reserve 1,800 characters for the reply within the 40,000-character draft.
	if total > 38200 || r.Messages[len(r.Messages)-1].Role != "user" {
		return r, ErrPortraitInput
	}
	return r, nil
}
func ValidatePortraitCandidate(data []byte, messages []PortraitMessage) (*PortraitCandidate, error) {
	var c PortraitCandidate
	var keys map[string]json.RawMessage
	if len(data) > 160000 || strictPortraitJSON(data, &c) != nil || json.Unmarshal(data, &keys) != nil || len(keys) != 5 {
		return nil, ErrPortraitOutput
	}
	for _, k := range []string{"reply", "name", "summary", "facets", "creationIdea"} {
		if _, ok := keys[k]; !ok {
			return nil, ErrPortraitOutput
		}
	}
	if !portraitString(c.Reply, 1800) || (c.Name != nil && !portraitString(*c.Name, 80)) || (c.Summary != nil && !portraitString(*c.Summary, 300)) || len(c.Facets) != 6 {
		return nil, ErrPortraitOutput
	}
	users := map[string]string{}
	for _, m := range messages {
		if m.Role == "user" {
			users[m.ID] = m.Text
		}
	}
	facts := []*PortraitFact{c.CreationIdea}
	for _, k := range portraitFacets {
		f, ok := c.Facets[k]
		if !ok {
			return nil, ErrPortraitOutput
		}
		facts = append(facts, f)
	}
	for _, f := range facts {
		if f == nil {
			continue
		}
		if !portraitString(f.Text, 500) || (f.Certainty != "stated" && f.Certainty != "inferred") || len(f.Evidence) < 1 || len(f.Evidence) > 60 {
			return nil, ErrPortraitOutput
		}
		for _, e := range f.Evidence {
			if !portraitString(e.Quote, 300) || !strings.Contains(users[e.MessageID], e.Quote) {
				return nil, ErrPortraitOutput
			}
		}
	}
	if c.Facets["identity"] == nil && (c.Name != nil || c.Summary != nil) {
		return nil, ErrPortraitOutput
	}
	if c.Name != nil {
		found := false
		for _, text := range users {
			if strings.Contains(text, *c.Name) {
				found = true
			}
		}
		if !found {
			return nil, ErrPortraitOutput
		}
	}
	return &c, nil
}
