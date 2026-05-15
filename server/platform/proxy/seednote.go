package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/elazarl/goproxy"
	"github.com/rs/zerolog"
)

// SeednotePlugin intercepts responses from xiaohongshu.com and extracts
// user profile data embedded in the HTML. It is meant to be registered with
// a proxy.Server via RegisterPlugin.
//
// When a profile page is detected, the extracted data is delivered through
// the OnProfile callback. The caller can also use CollectProfile to
// retrieve the most recently extracted profile synchronously.
type SeednotePlugin struct {
	logger    zerolog.Logger
	callbacks []ProfileCallback

	// userIDPattern matches profile page URLs.
	userIDPattern *regexp.Regexp
	// initialStatePattern matches the __INITIAL_STATE__ JSON blob.
	initialStatePattern *regexp.Regexp
}

// NewSeednotePlugin creates a new plugin for intercepting xiaohongshu.com.
func NewSeednotePlugin(logger zerolog.Logger, callbacks ...ProfileCallback) *SeednotePlugin {
	return &SeednotePlugin{
		logger:              logger.With().Str("plugin", "seednote").Logger(),
		callbacks:           callbacks,
		userIDPattern:       regexp.MustCompile(`/user/profile/([a-f0-9]+)`),
		initialStatePattern: regexp.MustCompile(`window\.__INITIAL_STATE__\s*=\s*({.+?})\s*</script>`),
	}
}

// Domains returns the domains this plugin handles.
func (p *SeednotePlugin) Domains() []string {
	return []string{"xiaohongshu.com"}
}

// OnRequest is a no-op for this plugin; all logic is in OnResponse.
func (p *SeednotePlugin) OnRequest(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
	return req, nil
}

// OnResponse inspects responses from xiaohongshu.com. If the response is a
// user profile page (HTML containing __INITIAL_STATE__), it extracts profile
// data and fires registered callbacks.
func (p *SeednotePlugin) OnResponse(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
	if resp == nil || resp.Request == nil {
		return resp
	}

	// Only process HTML responses.
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		return resp
	}

	// Only process profile page requests.
	reqPath := resp.Request.URL.Path
	if !p.userIDPattern.MatchString(reqPath) {
		return resp
	}

	// Read the response body.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		p.logger.Warn().Err(err).Msg("failed to read response body")
		return resp
	}
	resp.Body.Close()

	// Extract profile data.
	profile := p.extractProfile(string(body))

	// Fire callbacks.
	if profile != nil {
		name, _ := profile["nickname"].(string)
		p.logger.Info().
			Str("name", name).
			Str("path", reqPath).
			Msg("profile data extracted")

		for _, cb := range p.callbacks {
			cb("xiaohongshu.com", profile)
		}
	}

	// Restore the body so downstream consumers can still read it.
	resp.Body = io.NopCloser(strings.NewReader(string(body)))
	resp.ContentLength = int64(len(body))
	return resp
}

// extractProfile parses the HTML body for __INITIAL_STATE__ JSON and returns
// a map of profile fields. Returns nil if nothing could be extracted.
func (p *SeednotePlugin) extractProfile(html string) map[string]any {
	// Try __INITIAL_STATE__ first (most reliable).
	matches := p.initialStatePattern.FindStringSubmatch(html)
	if len(matches) < 2 {
		p.logger.Debug().Msg("no __INITIAL_STATE__ found in page")
		return p.extractProfileFallback(html)
	}

	raw := matches[1]
	// Fix common JSON issues: the state may contain undefined which is not valid JSON.
	raw = strings.ReplaceAll(raw, "undefined", "null")

	var state map[string]any
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		p.logger.Warn().Err(err).Msg("failed to parse __INITIAL_STATE__ JSON")
		return p.extractProfileFallback(html)
	}

	result := make(map[string]any)

	// Navigate the state tree to find user info.
	// Typical path: state -> user -> userInfo (or similar).
	if user := navigateMap(state, "user"); user != nil {
		if nickname := navigateStr(user, "nickname"); nickname != "" {
			result["nickname"] = nickname
		}
		if desc := navigateStr(user, "desc"); desc != "" {
			result["desc"] = desc
		}
		if avatar := navigateStr(user, "image"); avatar != "" {
			result["avatar"] = avatar
		}
		if userid := navigateStr(user, "userid"); userid != "" {
			result["userid"] = userid
		}
		// Include the raw user subtree.
		result["_raw_user"] = user
	}

	if len(result) == 0 {
		return p.extractProfileFallback(html)
	}

	result["_source"] = "__INITIAL_STATE__"
	return result
}

// extractProfileFallback uses simple string matching when JSON parsing fails.
func (p *SeednotePlugin) extractProfileFallback(html string) map[string]any {
	result := make(map[string]any)
	result["_source"] = "html_fallback"

	if v := extractBetween(html, `"nickname":"`, `"`); v != "" {
		result["nickname"] = v
	}
	if v := extractBetween(html, `"desc":"`, `"`); v != "" {
		result["desc"] = v
	}
	if v := extractBetween(html, `"image":"`, `"`); v != "" {
		// Unescape URL encoding.
		v = strings.ReplaceAll(v, `\u002F`, "/")
		result["avatar"] = v
	}

	if len(result) <= 1 { // only _source
		return nil
	}
	return result
}

// --- helpers ---

// navigateMap traverses a nested map by key path segments.
func navigateMap(m map[string]any, keys ...string) map[string]any {
	current := m
	for _, k := range keys {
		v, ok := current[k]
		if !ok {
			return nil
		}
		sub, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		current = sub
	}
	return current
}

// navigateStr extracts a string value from a nested map.
func navigateStr(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return s
}

// extractBetween extracts the substring between start and end delimiters.
func extractBetween(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	i += len(start)
	j := strings.Index(s[i:], end)
	if j < 0 {
		return ""
	}
	return s[i : i+j]
}
