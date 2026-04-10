package proxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/elazarl/goproxy"
	"github.com/rs/zerolog"
	"golang.org/x/net/publicsuffix"
)

// Server is a MITM-capable HTTP/HTTPS proxy that dispatches intercepted
// traffic to registered Plugins based on domain matching.
//
// It is designed to be used as a library: create a Server via New, register
// plugins, then call Start. Use Stop to shut down cleanly.
type Server struct {
	proxy   *goproxy.ProxyHttpServer
	logger  zerolog.Logger
	rules   *RuleSet
	plugins map[string]Plugin // top-level domain -> plugin

	httpServer *http.Server
	addr       string // listen address, e.g. ":8899"

	certPEM []byte
	keyPEM  []byte

	upstreamProxy string // optional upstream proxy URL

	mu sync.RWMutex
}

// Option configures a Server during construction.
type Option func(*Server)

// WithLogger sets the zerolog logger. Defaults to zerolog.Nop().
func WithLogger(l zerolog.Logger) Option {
	return func(s *Server) { s.logger = l }
}

// WithAddr sets the listen address (default ":8899").
func WithAddr(addr string) Option {
	return func(s *Server) { s.addr = addr }
}

// WithUpstreamProxy sets an optional upstream HTTP proxy URL.
func WithUpstreamProxy(proxyURL string) Option {
	return func(s *Server) { s.upstreamProxy = proxyURL }
}

// WithTLS configures the CA certificate used for MITM. Without this, the
// server cannot intercept HTTPS traffic. Both pem and key must be non-empty.
func WithTLS(certPEM, keyPEM []byte) Option {
	return func(s *Server) {
		s.certPEM = certPEM
		s.keyPEM = keyPEM
	}
}

// WithRules sets the initial MITM rule set from text. See RuleSet.Load for
// the format. If empty, no hosts are intercepted.
func WithRules(text string) Option {
	return func(s *Server) {
		if err := s.rules.Load(text); err != nil {
			s.logger.Warn().Err(err).Msg("proxy: failed to load initial rules")
		}
	}
}

// New creates a new proxy Server with the given options.
func New(opts ...Option) (*Server, error) {
	s := &Server{
		plugins: make(map[string]Plugin),
		rules:   &RuleSet{},
		addr:    ":8899",
		logger:  zerolog.Nop(),
	}
	for _, o := range opts {
		o(s)
	}

	if err := s.setupCA(); err != nil {
		return nil, err
	}

	s.proxy = goproxy.NewProxyHttpServer()
	s.setupTransport()
	s.setupHandlers()

	return s, nil
}

// RegisterPlugin registers a plugin. The plugin's Domains() are used to
// route intercepted traffic.
func (s *Server) RegisterPlugin(p Plugin) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range p.Domains() {
		s.plugins[d] = p
		s.logger.Debug().Str("domain", d).Msg("proxy: plugin registered")
	}
}

// UpdateRules replaces the active MITM rule set.
func (s *Server) UpdateRules(text string) error {
	return s.rules.Load(text)
}

// Addr returns the address the server is (or will be) listening on.
func (s *Server) Addr() string {
	return s.addr
}

// Start starts the proxy server in the background. It returns immediately.
// Use Stop to shut down.
func (s *Server) Start() error {
	s.httpServer = &http.Server{
		Addr:    s.addr,
		Handler: s.proxy,
	}

	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}

	s.logger.Info().Str("addr", s.addr).Msg("proxy: server started")
	go func() {
		if err := s.httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.logger.Error().Err(err).Msg("proxy: server error")
		}
	}()
	return nil
}

// Stop gracefully shuts down the proxy server.
func (s *Server) Stop(ctx context.Context) error {
	if s.httpServer == nil {
		return nil
	}
	s.logger.Info().Msg("proxy: server stopping")
	return s.httpServer.Shutdown(ctx)
}

// --- internal ---

func (s *Server) setupCA() error {
	if len(s.certPEM) == 0 || len(s.keyPEM) == 0 {
		// No TLS configured - MITM won't work, but HTTP proxy still functions.
		s.logger.Warn().Msg("proxy: no CA certificate configured, HTTPS MITM disabled")
		return nil
	}

	ca, err := tls.X509KeyPair(s.certPEM, s.keyPEM)
	if err != nil {
		return err
	}
	if ca.Leaf, err = x509.ParseCertificate(ca.Certificate[0]); err != nil {
		return err
	}

	goproxy.GoproxyCa = ca
	goproxy.OkConnect = &goproxy.ConnectAction{
		Action:    goproxy.ConnectAccept,
		TLSConfig: goproxy.TLSConfigFromCA(&ca),
	}
	goproxy.MitmConnect = &goproxy.ConnectAction{
		Action:    goproxy.ConnectMitm,
		TLSConfig: goproxy.TLSConfigFromCA(&ca),
	}
	goproxy.HTTPMitmConnect = &goproxy.ConnectAction{
		Action:    goproxy.ConnectMitm,
		TLSConfig: goproxy.TLSConfigFromCA(&ca),
	}
	goproxy.RejectConnect = &goproxy.ConnectAction{
		Action:    goproxy.ConnectReject,
		TLSConfig: goproxy.TLSConfigFromCA(&ca),
	}
	return nil
}

func (s *Server) setupTransport() {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: 60 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   60 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}

	s.proxy.ConnectDial = nil
	s.proxy.ConnectDialWithReq = nil

	if s.upstreamProxy != "" {
		proxyURL, err := url.Parse(s.upstreamProxy)
		if err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
			s.proxy.ConnectDial = s.proxy.NewConnectDialToProxy(s.upstreamProxy)
		}
	}
	s.proxy.Tr = transport
}

func (s *Server) setupHandlers() {
	// MITM decision based on rule set.
	s.proxy.OnRequest().HandleConnectFunc(func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
		if s.rules.ShouldMITM(host) {
			return goproxy.MitmConnect, host
		}
		return goproxy.OkConnect, host
	})

	s.proxy.OnRequest().DoFunc(s.onRequest)
	s.proxy.OnResponse().DoFunc(s.onResponse)
}

func (s *Server) matchPlugin(host string) Plugin {
	domain := topLevelDomain(host)
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.plugins[domain]
}

func (s *Server) onRequest(r *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
	if p := s.matchPlugin(r.Host); p != nil {
		newReq, newResp := p.OnRequest(r, ctx)
		if newResp != nil {
			return newReq, newResp
		}
		if newReq != nil {
			return newReq, nil
		}
	}
	return r, nil
}

func (s *Server) onResponse(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
	if resp == nil || resp.Request == nil {
		return resp
	}
	if p := s.matchPlugin(resp.Request.Host); p != nil {
		if newResp := p.OnResponse(resp, ctx); newResp != nil {
			return newResp
		}
	}
	return resp
}

// topLevelDomain extracts the eTLD+1 from a host string (which may contain a
// port). Falls back to the raw host on error.
func topLevelDomain(host string) string {
	h := host
	if hp, _, err := net.SplitHostPort(h); err == nil {
		h = hp
	}
	domain, err := publicsuffix.EffectiveTLDPlusOne(h)
	if err != nil {
		return h
	}
	return domain
}
