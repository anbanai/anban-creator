package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"golang.org/x/sync/singleflight"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"gorm.io/gorm"
)

var TrendPlatforms = []string{"weibo", "douyin", "zhihu", "bilibili", "baidu", "toutiao"}
var trendLabels = map[string]string{"weibo": "微博", "douyin": "抖音", "zhihu": "知乎", "bilibili": "B站", "baidu": "百度", "toutiao": "头条"}

type TrendItem struct {
	Title string `json:"title"`
	Hot   string `json:"hot"`
	URL   string `json:"url"`
	Rank  int    `json:"rank"`
}

type TrendPlatformResult struct {
	Platform  string      `json:"platform"`
	Label     string      `json:"label"`
	Items     []TrendItem `json:"items"`
	FetchedAt *time.Time  `json:"fetched_at"`
	ExpiresAt *time.Time  `json:"expires_at"`
	Stale     bool        `json:"stale"`
	Source    string      `json:"source"`
	LastError string      `json:"last_error"`
}

type TrendQueryResult struct {
	Items       []TrendPlatformResult `json:"items"`
	RequestedAt time.Time             `json:"requested_at"`
	TTLSeconds  int64                 `json:"ttl_seconds"`
	Provider    string                `json:"provider"`
}

type TrendFetcher interface {
	Fetch(ctx context.Context, platform string) ([]TrendItem, string, error)
}

type RedFoxTrendFetcher struct {
	client   *http.Client
	endpoint string
	apiKey   string
	cacheTTL time.Duration
	mu       sync.Mutex
	items    map[string][]TrendItem
	cachedAt time.Time
	group    singleflight.Group
}

func NewRedFoxTrendFetcher(cfg config.TrendsRedFoxConfig) *RedFoxTrendFetcher {
	return NewRedFoxTrendFetcherWithTTL(cfg, 10*time.Minute)
}

func NewRedFoxTrendFetcherWithTTL(cfg config.TrendsRedFoxConfig, cacheTTL time.Duration) *RedFoxTrendFetcher {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	if cacheTTL <= 0 {
		cacheTTL = 10 * time.Minute
	}
	return &RedFoxTrendFetcher{
		client:   &http.Client{Timeout: timeout},
		endpoint: strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/"),
		apiKey:   strings.TrimSpace(cfg.APIKey),
		cacheTTL: cacheTTL,
		items:    make(map[string][]TrendItem),
	}
}

func (f *RedFoxTrendFetcher) Fetch(ctx context.Context, platform string) ([]TrendItem, string, error) {
	if f == nil || f.client == nil || f.endpoint == "" || f.apiKey == "" {
		return nil, "", fmt.Errorf("redfox trend provider is not configured")
	}
	f.mu.Lock()
	if !f.cachedAt.IsZero() && time.Since(f.cachedAt) < f.cacheTTL {
		if cached, ok := f.items[platform]; ok {
			f.mu.Unlock()
			return append([]TrendItem(nil), cached...), "redfox", nil
		}
	}
	if !f.cachedAt.IsZero() && time.Since(f.cachedAt) >= f.cacheTTL {
		f.items = make(map[string][]TrendItem)
		f.cachedAt = time.Time{}
	}
	f.mu.Unlock()

	result, err, _ := f.group.Do("all", func() (any, error) {
		return f.fetchAll(ctx)
	})
	if err != nil {
		return nil, "", err
	}
	_ = result
	f.mu.Lock()
	items, ok := f.items[platform]
	f.mu.Unlock()
	if !ok {
		return nil, "", fmt.Errorf("redfox trend response has no supported platform %q", platform)
	}
	return append([]TrendItem(nil), items...), "redfox", nil
}

func (f *RedFoxTrendFetcher) fetchAll(ctx context.Context) (any, error) {
	body := []byte(`{"source":"anban"}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-KEY", f.apiKey)
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("redfox trend request failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read redfox trend response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("redfox trend request returned HTTP %d", resp.StatusCode)
	}
	itemsByPlatform, err := parseRedFoxTrendPayload(data)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	for key, values := range itemsByPlatform {
		f.items[key] = values
	}
	f.cachedAt = time.Now().UTC()
	f.mu.Unlock()
	return nil, nil
}

func parseRedFoxTrendPayload(data []byte) (map[string][]TrendItem, error) {
	var response struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			HotSpotList []struct {
				Platform string `json:"platName"`
				Title    string `json:"title"`
				URL      string `json:"url"`
				Hot      any    `json:"maxHotScore"`
			} `json:"hotSpotList"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode redfox trend response: %w", err)
	}
	if response.Code != 2000 {
		message := strings.TrimSpace(response.Msg)
		if message == "" {
			message = fmt.Sprintf("code %d", response.Code)
		}
		return nil, fmt.Errorf("redfox trend API error: %s", message)
	}
	result := make(map[string][]TrendItem)
	for _, group := range response.Data {
		for _, spot := range group.HotSpotList {
			platform := normalizeRedFoxPlatform(spot.Platform)
			if platform == "" || strings.TrimSpace(spot.Title) == "" {
				continue
			}
			items := result[platform]
			title := strings.ReplaceAll(strings.TrimSpace(spot.Title), " ", "")
			url := strings.ReplaceAll(strings.TrimSpace(spot.URL), " ", "%20")
			hot := ""
			if spot.Hot != nil {
				hot = fmt.Sprint(spot.Hot)
			}
			items = append(items, TrendItem{Title: title, Hot: hot, URL: url, Rank: len(items) + 1})
			result[platform] = items
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("redfox trend response contains no supported items")
	}
	return result, nil
}

func normalizeRedFoxPlatform(raw string) string {
	switch strings.TrimSpace(raw) {
	case "微博":
		return "weibo"
	case "抖音":
		return "douyin"
	case "知乎":
		return "zhihu"
	case "B站", "哔哩哔哩":
		return "bilibili"
	case "百度":
		return "baidu"
	case "头条", "今日头条":
		return "toutiao"
	default:
		return ""
	}
}

type HTTPTrendFetcher struct {
	client  *http.Client
	timeout time.Duration
}

func NewHTTPTrendFetcher(timeout time.Duration) *HTTPTrendFetcher {
	return &HTTPTrendFetcher{client: &http.Client{Timeout: timeout}, timeout: timeout}
}

var trendSources = map[string][]struct{ url, source string }{
	"weibo":    {{"https://60s.viki.moe/v2/weibo", "60s"}, {"https://v2.xxapi.cn/api/weibohot", "xxapi"}},
	"douyin":   {{"https://60s.viki.moe/v2/douyin", "60s"}, {"https://v2.xxapi.cn/api/douyinhot", "xxapi"}},
	"zhihu":    {{"https://60s.viki.moe/v2/zhihu", "60s"}},
	"bilibili": {{"https://60s.viki.moe/v2/bili", "60s"}, {"https://v2.xxapi.cn/api/bilibilihot", "xxapi"}},
	"baidu":    {{"https://60s.viki.moe/v2/baidu/hot", "60s"}, {"https://v2.xxapi.cn/api/baiduhot", "xxapi"}},
	"toutiao":  {{"https://60s.viki.moe/v2/toutiao", "60s"}},
}

func (f *HTTPTrendFetcher) Fetch(ctx context.Context, platform string) ([]TrendItem, string, error) {
	sources, ok := trendSources[platform]
	if !ok {
		return nil, "", fmt.Errorf("unsupported platform %q", platform)
	}
	var last error
	for _, source := range sources {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.url, nil)
		if err != nil {
			last = err
			continue
		}
		req.Header.Set("User-Agent", "AnbanCreator/1.0")
		resp, err := f.client.Do(req)
		if err != nil {
			last = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if readErr != nil {
			last = readErr
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			last = fmt.Errorf("upstream status %d", resp.StatusCode)
			continue
		}
		items, err := parseTrendPayload(body)
		if err != nil || len(items) == 0 {
			if err == nil {
				err = fmt.Errorf("empty trend response")
			}
			last = err
			continue
		}
		return items, source.source, nil
	}
	if last == nil {
		last = fmt.Errorf("no trend source configured")
	}
	return nil, "", last
}

func parseTrendPayload(body []byte) ([]TrendItem, error) {
	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, err
	}
	var data any = root
	if obj, ok := root.(map[string]any); ok {
		data = obj["data"]
		if data == nil {
			data = obj["list"]
		}
	}
	if nested, ok := data.(map[string]any); ok {
		if v, exists := nested["data"]; exists && v != nil {
			data = v
		} else if v, exists := nested["list"]; exists {
			data = v
		} else {
			data = nil
		}
	}
	raw, ok := data.([]any)
	if !ok {
		return nil, fmt.Errorf("trend data is not an array")
	}
	out := make([]TrendItem, 0, len(raw))
	for i, value := range raw {
		item, ok := value.(map[string]any)
		if !ok {
			continue
		}
		title := firstString(item, "title", "word", "name", "keyword")
		if strings.TrimSpace(title) == "" {
			continue
		}
		out = append(out, TrendItem{Title: title, Hot: firstValueString(item, "hot", "hot_value", "num"), URL: firstString(item, "url", "link", "mobil_url"), Rank: i + 1})
	}
	return out, nil
}
func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
func firstValueString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			if s, ok := v.(string); ok {
				return s
			}
			return fmt.Sprint(v)
		}
	}
	return ""
}

type TrendService struct {
	repo         repository.Repository
	redis        *redis.Client
	ttl          time.Duration
	fetcher      TrendFetcher
	provider     string
	logger       *zerolog.Logger
	group        singleflight.Group
	localLocksMu sync.Mutex
	localLocks   map[string]*sync.Mutex
}

func NewTrendService(repo repository.Repository, rdb *redis.Client, cfg config.TrendsConfig, logger *zerolog.Logger) *TrendService {
	if cfg.TTL <= 0 {
		cfg.TTL = 10 * time.Minute
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 8 * time.Second
	}
	var fetcher TrendFetcher = NewHTTPTrendFetcher(cfg.Timeout)
	if strings.EqualFold(strings.TrimSpace(cfg.Provider), "redfox") {
		fetcher = NewRedFoxTrendFetcherWithTTL(cfg.RedFox, cfg.TTL)
	}
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		provider = "default"
	}
	return &TrendService{repo: repo, redis: rdb, ttl: cfg.TTL, fetcher: fetcher, provider: provider, logger: logger, localLocks: make(map[string]*sync.Mutex)}
}
func NewTrendServiceWithFetcher(repo repository.Repository, rdb *redis.Client, ttl time.Duration, fetcher TrendFetcher, logger *zerolog.Logger) *TrendService {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &TrendService{repo: repo, redis: rdb, ttl: ttl, fetcher: fetcher, provider: "default", logger: logger, localLocks: make(map[string]*sync.Mutex)}
}

func (s *TrendService) List(ctx context.Context, platforms []string, limit int, force bool) (*TrendQueryResult, error) {
	selected, err := normalizeTrendPlatforms(platforms)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 12
	}
	if limit > 100 {
		limit = 100
	}
	provider := s.provider
	if provider == "" {
		provider = "default"
	}
	result := &TrendQueryResult{RequestedAt: time.Now().UTC(), TTLSeconds: int64(s.ttl / time.Second), Provider: provider, Items: make([]TrendPlatformResult, 0, len(selected))}
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, platform := range selected {
		platform := platform
		wg.Add(1)
		go func() {
			defer wg.Done()
			item := s.getPlatform(ctx, platform, limit, force)
			mu.Lock()
			result.Items = append(result.Items, item)
			mu.Unlock()
		}()
	}
	wg.Wait()
	sortTrendResults(result.Items, selected)
	return result, nil
}

func normalizeTrendPlatforms(platforms []string) ([]string, error) {
	if len(platforms) == 0 {
		return append([]string(nil), TrendPlatforms...), nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(platforms))
	for _, p := range platforms {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		if _, ok := trendLabels[p]; !ok {
			return nil, fmt.Errorf("unsupported trend platform %q", p)
		}
		seen[p] = true
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("at least one trend platform is required")
	}
	return out, nil
}
func sortTrendResults(items []TrendPlatformResult, order []string) {
	rank := map[string]int{}
	for i, p := range order {
		rank[p] = i
	}
	for i := range items {
		for j := i + 1; j < len(items); j++ {
			if rank[items[j].Platform] < rank[items[i].Platform] {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}

func (s *TrendService) getPlatform(ctx context.Context, platform string, limit int, force bool) TrendPlatformResult {
	result := TrendPlatformResult{Platform: platform, Label: trendLabels[platform], Items: make([]TrendItem, 0), Stale: true}
	snapshot, err := s.repo.TrendSnapshots().FindByPlatform(ctx, platform)
	now := time.Now().UTC()
	if err == nil {
		result = snapshotResult(snapshot, s.ttl, limit, now)
		providerMatches := s.snapshotMatchesProvider(snapshot)
		if !providerMatches {
			result.Stale = true
		}
		if !force && providerMatches && now.Before(snapshot.FetchedAt.Add(s.ttl)) {
			return result
		}
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		result.LastError = err.Error()
	}
	observedAttempt := time.Time{}
	if snapshot != nil {
		observedAttempt = snapshot.LastAttemptedAt
	}
	_, refreshErr, _ := s.group.Do(platform, func() (any, error) { return nil, s.refresh(ctx, platform, force, observedAttempt) })
	snapshot, err = s.repo.TrendSnapshots().FindByPlatform(ctx, platform)
	if err == nil {
		result = snapshotResult(snapshot, s.ttl, limit, time.Now().UTC())
		if !s.snapshotMatchesProvider(snapshot) || !time.Now().UTC().Before(snapshot.FetchedAt.Add(s.ttl)) {
			result.Stale = true
		}
	} else {
		result.Stale = true
		if refreshErr != nil {
			result.LastError = refreshErr.Error()
		} else if result.LastError == "" && !errors.Is(err, gorm.ErrRecordNotFound) {
			result.LastError = err.Error()
		}
	}
	if refreshErr != nil && result.LastError == "" {
		result.LastError = refreshErr.Error()
	}
	return result
}

func (s *TrendService) snapshotMatchesProvider(snapshot *model.TrendSnapshot) bool {
	if snapshot == nil {
		return false
	}
	provider := strings.ToLower(strings.TrimSpace(s.provider))
	if provider == "redfox" {
		return strings.EqualFold(strings.TrimSpace(snapshot.Source), "redfox")
	}
	return !strings.EqualFold(strings.TrimSpace(snapshot.Source), "redfox")
}

func snapshotResult(snapshot *model.TrendSnapshot, ttl time.Duration, limit int, now time.Time) TrendPlatformResult {
	items := make([]TrendItem, 0)
	_ = json.Unmarshal(snapshot.Items, &items)
	if items == nil {
		items = make([]TrendItem, 0)
	}
	if len(items) > limit {
		items = items[:limit]
	}
	expires := snapshot.FetchedAt.Add(ttl)
	return TrendPlatformResult{Platform: snapshot.Platform, Label: trendLabels[snapshot.Platform], Items: items, FetchedAt: &snapshot.FetchedAt, ExpiresAt: &expires, Stale: !now.Before(expires), Source: snapshot.Source, LastError: snapshot.LastError}
}

func (s *TrendService) refresh(ctx context.Context, platform string, force bool, observedAttempt time.Time) error {
	unlock, acquired := s.acquireLock(ctx, platform)
	if !acquired {
		return nil
	}
	defer unlock()
	current, err := s.repo.TrendSnapshots().FindByPlatform(ctx, platform)
	attemptedAt := time.Now().UTC()
	now := attemptedAt
	if err == nil {
		if force && current.LastAttemptedAt.After(observedAttempt) {
			// Another caller completed the forced refresh while this request
			// waited for the platform lock. Reuse that result.
			return nil
		}
		if !force && s.snapshotMatchesProvider(current) && now.Before(current.FetchedAt.Add(s.ttl)) {
			return nil
		}
	}
	items, source, fetchErr := s.fetcher.Fetch(ctx, platform)
	if fetchErr != nil {
		if err == nil {
			// There is no row to update yet, but preserve the upstream error for
			// the caller so a first-read failure is observable as stale.
			if markErr := s.repo.TrendSnapshots().MarkAttempt(ctx, platform, attemptedAt, fetchErr.Error()); markErr != nil {
				return fmt.Errorf("%v (mark attempt: %w)", fetchErr, markErr)
			}
			return fetchErr
		}
		// Keep the last successful snapshot available, while recording this
		// failed attempt for freshness diagnostics and the next read.
		if markErr := s.repo.TrendSnapshots().MarkAttempt(ctx, platform, attemptedAt, fetchErr.Error()); markErr != nil {
			return fmt.Errorf("%v (mark attempt: %w)", fetchErr, markErr)
		}
		return fetchErr
	}
	encoded, _ := json.Marshal(items)
	fetchedAt := time.Now().UTC()
	return s.repo.TrendSnapshots().Upsert(ctx, &model.TrendSnapshot{Platform: platform, Items: encoded, FetchedAt: fetchedAt, LastAttemptedAt: attemptedAt, Source: source})
}
func (s *TrendService) acquireLock(ctx context.Context, platform string) (func(), bool) {
	if s.redis == nil {
		lock := s.localLock(platform)
		lock.Lock()
		return lock.Unlock, true
	}
	key := "anban:trend:refresh:" + platform
	token := fmt.Sprintf("%d", time.Now().UnixNano())
	ok, err := s.redis.SetNX(ctx, key, token, 30*time.Second).Result()
	if err != nil {
		if s.logger != nil {
			s.logger.Warn().Err(err).Str("platform", platform).Msg("redis unavailable; using local trend refresh lock")
		}
		lock := s.localLock(platform)
		lock.Lock()
		return lock.Unlock, true
	}
	if !ok {
		// Another instance is refreshing. Wait briefly for its snapshot, then
		// let the caller re-read it. This keeps concurrent reads from duplicating
		// upstream calls while avoiding a long request stall.
		deadline := time.NewTimer(1500 * time.Millisecond)
		poll := time.NewTicker(100 * time.Millisecond)
		defer deadline.Stop()
		defer poll.Stop()
		for {
			select {
			case <-ctx.Done():
				return func() {}, false
			case <-deadline.C:
				return func() {}, false
			case <-poll.C:
				exists, e := s.redis.Exists(ctx, key).Result()
				if e == nil && exists == 0 {
					return func() {}, false
				}
			}
		}
	}
	return func() {
		const release = `if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("del", KEYS[1]) else return 0 end`
		_ = s.redis.Eval(context.Background(), release, []string{key}, token).Err()
	}, true
}

func (s *TrendService) localLock(platform string) *sync.Mutex {
	s.localLocksMu.Lock()
	defer s.localLocksMu.Unlock()
	if s.localLocks == nil {
		s.localLocks = make(map[string]*sync.Mutex)
	}
	lock := s.localLocks[platform]
	if lock == nil {
		lock = &sync.Mutex{}
		s.localLocks[platform] = lock
	}
	return lock
}
