package wework

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/anbanai/anban-creator/server/sdk/wework/server"
	"resty.dev/v3"
)

type Config struct {
	CropID  string `yaml:"cropid"`
	Secret  string `yaml:"secret"`
	Token   string `yaml:"token"`
	Aeskey  string `yaml:"aeskey"`
	AgentID string `yaml:"agent_id"`
}

type weworkCache struct {
	tokenLocker      sync.Mutex
	ticketLocker     sync.Mutex
	accessToken      *AccessToken
	jsapiTicket      *JsapiTicket
	agentJsapiTicket *AgentJsapiTicket
}

// Wework 表示与企业微信交互的SDK。
type Wework struct {
	cfg    *Config
	client *resty.Client // HTTP客户端
	ctx    context.Context
	locker chan struct{}

	// 用户级别操作锁: 防止同时并发调用了获取客户详情接口导致的操作冲突
	userLocksMu *sync.Mutex
	userLocks   map[string]*sync.Mutex

	cache *weworkCache
}

// Neww *Wework 创建一个新的w *Wework实例。
func NewWework(cfg *Config) *Wework {
	return newWework(cfg, resty.New().SetTimeout(10*time.Second))
}

// NewWeworkWithHTTPClient creates a client with an injected HTTP transport.
// A copy is used so applying a finite default timeout does not mutate the
// caller's http.Client; custom transport and TLS settings remain in effect.
func NewWeworkWithHTTPClient(cfg *Config, httpClient *http.Client) *Wework {
	if httpClient == nil {
		return NewWework(cfg)
	}
	client := *httpClient
	if client.Timeout <= 0 {
		client.Timeout = 10 * time.Second
	}
	return newWework(cfg, resty.NewWithClient(&client))
}

func newWework(cfg *Config, client *resty.Client) *Wework {
	return &Wework{
		cfg:         cfg,
		client:      client,
		locker:      make(chan struct{}, 1),
		userLocksMu: new(sync.Mutex),
		userLocks:   make(map[string]*sync.Mutex),
		cache:       new(weworkCache),
	}
}

// WithContext returns an SDK copy whose subsequent requests use ctx. The HTTP
// transport and synchronized caches are shared with the original instance.
func (cli *Wework) WithContext(ctx context.Context) *Wework {
	if ctx == nil {
		ctx = context.Background()
	}
	client := *cli.client.Client()
	copy := *cli
	copy.ctx = ctx
	copy.client = resty.NewWithClient(&client).SetContext(ctx)
	return &copy
}

func (cli *Wework) requestContext() context.Context {
	if cli.ctx != nil {
		return cli.ctx
	}
	return context.Background()
}

func (cli *Wework) userLockFor(userID string) *sync.Mutex {
	cli.userLocksMu.Lock()
	defer cli.userLocksMu.Unlock()
	if cli.userLocks == nil {
		cli.userLocks = make(map[string]*sync.Mutex)
	}
	mutex := cli.userLocks[userID]
	if mutex == nil {
		mutex = new(sync.Mutex)
		cli.userLocks[userID] = mutex
	}
	return mutex
}

func (cli *Wework) locking(fn func() error) error {
	ctx := cli.requestContext()
	select {
	case cli.locker <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-cli.locker }()

	err := fn()
	if err != nil {
		return sanitizeRequestError(err)
	}
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		// The remote operation already succeeded; cancellation only skips throttling.
		return nil
	}
}

// 微信通知监听服务
func (cli *Wework) NewServer(handler func(map[string]any) map[string]any) (*server.Server, error) {
	return server.NewServer(cli.cfg.CropID, cli.cfg.Token, cli.cfg.Aeskey, handler)
}

type AccessToken struct {
	AccessToken string    `json:"access_token"`
	Expiration  time.Time `json:"expires_in"`
}

// 获取稳定版接口调用凭据
func (cli *Wework) AccessToken() (*AccessToken, error) {
	cli.cache.tokenLocker.Lock()
	defer cli.cache.tokenLocker.Unlock()

	// 检查访问令牌是否已经可用且未过期
	if cli.cache.accessToken == nil || cli.cache.accessToken.Expiration.IsZero() || time.Now().After(cli.cache.accessToken.Expiration) {

		res, err := cli.GetAccessToken()
		if err != nil {
			return nil, err
		}

		if err := res.GetResponseError(); err != nil {
			return nil, err
		}

		// 更新访问令牌和过期时间
		cli.cache.accessToken = &AccessToken{
			AccessToken: res.AccessToken,
			Expiration:  time.Now().Add(time.Duration(res.ExpiresIn) * time.Second),
		}
	}

	return cli.cache.accessToken, nil
}

type JsapiTicket struct {
	Ticket     string    `json:"ticket"`
	Expiration time.Time `json:"expires_in"`
}

func (cli *Wework) JsapiTicket() (*JsapiTicket, error) {
	cli.cache.ticketLocker.Lock()
	defer cli.cache.ticketLocker.Unlock()

	// 检查访问令牌是否已经可用且未过期
	if cli.cache.jsapiTicket == nil || cli.cache.jsapiTicket.Expiration.IsZero() || time.Now().After(cli.cache.jsapiTicket.Expiration) {

		res, err := cli.GetJsapiTicket()
		if err != nil {
			return nil, err
		}

		if err := res.GetResponseError(); err != nil {
			return nil, err
		}

		// 更新访问令牌和过期时间
		cli.cache.jsapiTicket = &JsapiTicket{
			Ticket:     res.Ticket,
			Expiration: time.Now().Add(time.Duration(res.ExpiresIn) * time.Second),
		}
	}

	return cli.cache.jsapiTicket, nil
}

type AgentJsapiTicket struct {
	Ticket     string    `json:"ticket"`
	Expiration time.Time `json:"expires_in"`
}

func (cli *Wework) AgentJsapiTicket() (*AgentJsapiTicket, error) {
	cli.cache.ticketLocker.Lock()
	defer cli.cache.ticketLocker.Unlock()

	// 检查访问令牌是否已经可用且未过期
	if cli.cache.agentJsapiTicket == nil || cli.cache.agentJsapiTicket.Expiration.IsZero() || time.Now().After(cli.cache.agentJsapiTicket.Expiration) {

		res, err := cli.GetAgentJsapiTicket()
		if err != nil {
			return nil, err
		}

		if err := res.GetResponseError(); err != nil {
			return nil, err
		}

		// 更新访问令牌和过期时间
		cli.cache.agentJsapiTicket = &AgentJsapiTicket{
			Ticket:     res.Ticket,
			Expiration: time.Now().Add(time.Duration(res.ExpiresIn) * time.Second),
		}
	}

	return cli.cache.agentJsapiTicket, nil
}

// 获取应用名称
func (cli *Wework) Config() *Config {
	return cli.cfg
}
