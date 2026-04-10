package proxy

import (
	"net/http"

	"github.com/elazarl/goproxy"
)

// Plugin defines the interface for domain-specific HTTP interceptors.
// Each plugin handles requests/responses for a set of registered domains.
type Plugin interface {
	// Domains returns the list of top-level domains this plugin handles.
	Domains() []string

	// OnRequest is called for every HTTP request whose host matches one of
	// the plugin's domains. Return (req, nil) to forward the (possibly
	// modified) request, or (req, resp) to short-circuit with a synthetic
	// response.
	OnRequest(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response)

	// OnResponse is called for every HTTP response whose request host
	// matches one of the plugin's domains. Return the (possibly modified)
	// response, or nil to pass through unchanged.
	OnResponse(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response
}

// ProfileCallback is invoked when a plugin extracts profile data from an
// intercepted response. The data parameter is plugin-specific.
type ProfileCallback func(domain string, data map[string]any)
