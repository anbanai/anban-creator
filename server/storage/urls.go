package storage

import (
	"net/url"
	"strings"
)

// StorageKeyFromURL extracts the backend object key from a URL that points at
// a server-owned storage backend. Handles both relative "/api/v1/files/<key>"
// (LocalProvider) and absolute "https://<host>/<key>" (OSSProvider, with or
// without custom domain). Returns ok=false if the URL has no extractable path.
//
// This function performs NO ownership validation. Callers handling untrusted
// URLs MUST gate with Provider.IsOwnedURL first to prevent SSRF — otherwise
// an attacker-supplied URL like "https://evil.com/key" would yield a key that
// the caller might then feed to store.Read.
func StorageKeyFromURL(rawURL string) (key string, ok bool) {
	if k, found := strings.CutPrefix(rawURL, "/api/v1/files/"); found {
		// Local path. Strip any query string so the OSS HTTPS branch and this
		// branch return symmetric keys (an attacker may append ?x=1 to bypass
		// naive prefix checks downstream).
		if i := strings.IndexByte(k, '?'); i >= 0 {
			k = k[:i]
		}
		if k == "" {
			return "", false
		}
		return k, true
	}
	if u, err := url.Parse(rawURL); err == nil && u.Path != "" {
		trimmed := strings.TrimPrefix(u.Path, "/")
		if trimmed == "" {
			return "", false
		}
		return trimmed, true
	}
	return "", false
}
