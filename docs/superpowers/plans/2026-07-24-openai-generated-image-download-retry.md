# OpenAI Generated Image Download Retry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Recover from transient OpenAI generated-image URL download failures without repeating image generation or changing other remote-download behavior.

**Architecture:** Keep `wechat.DownloadFile` as a one-attempt compatibility wrapper and add a context-aware sibling. Add a private retry policy and helper to `OpenAIProvider`; thread the generation context through response conversion so single and batch URL results share the helper. Retry only transport failures, 408, 429, and 5xx within three attempts and a 120-second total phase deadline.

**Tech Stack:** Go 1.24, Resty v3, OpenAI Go SDK v3, `context`, `httptest`, Zerolog.

---

## File Map

- Modify `app/wechat/service.go`: add the context-aware one-attempt download entry point while preserving the existing wrapper.
- Modify `app/wechat/errors_test.go`: cover cancellation and prove the wrapper still performs one request.
- Modify `app/image/openai.go`: define the OpenAI-only retry policy, classify retryable failures, retry URL downloads, thread context through response conversion, and enrich logs.
- Modify `app/image/openai_test.go`: cover transient recovery, truncated responses, non-retryable responses, exhaustion, cancellation, diagnostics, and single/batch integration.

No Studio, server API, database, billing, or plugin file changes are required.

### Task 1: Add A Context-Aware Single-Attempt Download

**Files:**
- Modify: `app/wechat/service.go:3-23,290-347`
- Test: `app/wechat/errors_test.go:3-10,77-136`

- [ ] **Step 1: Write failing cancellation and one-attempt compatibility tests**

Add `context`, `sync/atomic`, and `time` to the imports in `app/wechat/errors_test.go`, then append:

```go
func TestDownloadFileContextHonorsCancellation(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		path, err := DownloadFileContext(ctx, srv.URL+"/generated.png")
		if path != "" {
			_ = os.Remove(path)
		}
		result <- err
	}()

	<-started
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("DownloadFileContext did not stop after cancellation")
	}
}

func TestDownloadFileRemainsSingleAttempt(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "temporary failure", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := DownloadFile(srv.URL + "/generated.png")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
}

func TestDownloadFileContextRemovesPartialFile(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMPDIR", tempDir)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "64")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("short"))
	}))
	defer srv.Close()

	_, err := DownloadFileContext(context.Background(), srv.URL+"/generated.png")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	entries, readErr := os.ReadDir(tempDir)
	if readErr != nil {
		t.Fatalf("read temp dir: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("partial download files remain: %v", entries)
	}
}
```

- [ ] **Step 2: Run the focused test and verify it fails**

Run:

```bash
go test ./app/wechat -run 'TestDownloadFile(ContextHonorsCancellation|RemainsSingleAttempt|ContextRemovesPartialFile)$' -count=1
```

Expected: build failure with `undefined: DownloadFileContext`.

- [ ] **Step 3: Implement the context-aware entry point without adding retries**

Add `context` to `app/wechat/service.go` imports. Replace the current `DownloadFile` implementation with:

```go
// DownloadFile downloads a file with the existing one-attempt behavior.
func DownloadFile(url string) (string, error) {
	return DownloadFileContext(context.Background(), url)
}

// DownloadFileContext downloads a file to a temporary path and honors ctx.
// Retry policy belongs to the caller.
func DownloadFileContext(ctx context.Context, url string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	client := resty.New().
		SetTimeout(60*time.Second).
		SetHeader("User-Agent", "Mozilla/5.0 (compatible; AnbanCreator/1.0; +https://anbanai.com)").
		SetHeader("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8").
		SetHeader("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	defer client.Close()

	ext := ".jpg"
	if parsedURL, err := neturl.Parse(url); err == nil {
		if pathExt := filepath.Ext(parsedURL.Path); pathExt != "" {
			ext = pathExt
		}
	}
	tmpFile, err := os.CreateTemp("", "anban-creator_download_*"+ext)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("close temp file: %w", err)
	}

	start := time.Now()
	resp, err := client.R().
		SetContext(ctx).
		SetResponseSaveFileName(tmpPath).
		Get(url)
	elapsed := time.Since(start)
	if err != nil {
		_ = os.Remove(tmpPath)
		return "", &DownloadError{URL: url, Elapsed: elapsed, Original: err}
	}

	if resp.StatusCode() != http.StatusOK {
		_ = os.Remove(tmpPath)
		return "", &DownloadError{
			URL:           url,
			StatusCode:    resp.StatusCode(),
			ContentType:   resp.Header().Get("Content-Type"),
			ContentLength: resp.Header().Get("Content-Length"),
			Server:        resp.Header().Get("Server"),
			CFRay:         resp.Header().Get("Cf-Ray"),
			Location:      resp.Header().Get("Location"),
			BodyPreview:   truncateDownloadBodyPreview(resp.Bytes(), 80),
			Elapsed:       elapsed,
		}
	}

	return tmpPath, nil
}
```

- [ ] **Step 4: Format and run all shared-download tests**

Run:

```bash
gofmt -w app/wechat/service.go app/wechat/errors_test.go
go test ./app/wechat -count=1
```

Expected: PASS. Existing browser-header and diagnostics tests remain green.

- [ ] **Step 5: Commit the context-aware download boundary**

```bash
git add app/wechat/service.go app/wechat/errors_test.go
git commit -m "fix(image): support cancelable remote downloads"
```

### Task 2: Implement The OpenAI-Only Retry Helper

**Files:**
- Modify: `app/image/openai.go:24-32,703-729`
- Test: `app/image/openai_test.go:3-19,668-706`

- [ ] **Step 1: Write failing retry behavior tests**

Add `sync/atomic`, `time`, and `github.com/anbanai/anban-creator/app/wechat` to `app/image/openai_test.go`, then append:

```go
func fastOpenAIImageDownloadPolicy() openAIImageDownloadRetryPolicy {
	return openAIImageDownloadRetryPolicy{
		maxAttempts: 3,
		totalTimeout: time.Second,
		backoffs:     []time.Duration{time.Millisecond, time.Millisecond},
	}
}

func testOpenAIProviderWithDownloadPolicy(log *zerolog.Logger) *OpenAIProvider {
	policy := fastOpenAIImageDownloadPolicy()
	return &OpenAIProvider{
		model:               "gpt-image-2",
		sizeRatio:           "auto",
		log:                 log,
		downloadRetryPolicy: &policy,
	}
}

func TestOpenAIGeneratedImageDownloadRetries503ThenSucceeds(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\nimage"))
	}))
	defer srv.Close()

	provider := testOpenAIProviderWithDownloadPolicy(testLogger())
	path, attempts, err := provider.downloadGeneratedImage(context.Background(), srv.URL+"/generated.png")
	if err != nil {
		t.Fatalf("downloadGeneratedImage: %v", err)
	}
	defer os.Remove(path)
	if attempts != 2 || requests.Load() != 2 {
		t.Fatalf("attempts = %d, requests = %d, want 2 and 2", attempts, requests.Load())
	}
}

func TestOpenAIGeneratedImageDownloadRetriesUnexpectedEOF(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set("Content-Length", "64")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("short"))
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\nimage"))
	}))
	defer srv.Close()

	provider := testOpenAIProviderWithDownloadPolicy(testLogger())
	path, attempts, err := provider.downloadGeneratedImage(context.Background(), srv.URL+"/generated.png")
	if err != nil {
		t.Fatalf("downloadGeneratedImage: %v", err)
	}
	defer os.Remove(path)
	if attempts != 2 || requests.Load() != 2 {
		t.Fatalf("attempts = %d, requests = %d, want 2 and 2", attempts, requests.Load())
	}
}

func TestOpenAIGeneratedImageDownloadDoesNotRetry403(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	provider := testOpenAIProviderWithDownloadPolicy(testLogger())
	_, attempts, err := provider.downloadGeneratedImage(context.Background(), srv.URL+"/generated.png")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if attempts != 1 || requests.Load() != 1 {
		t.Fatalf("attempts = %d, requests = %d, want 1 and 1", attempts, requests.Load())
	}
}

func TestOpenAIGeneratedImageDownloadStopsAfterThreeAttempts(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "temporarily unavailable", http.StatusBadGateway)
	}))
	defer srv.Close()

	provider := testOpenAIProviderWithDownloadPolicy(testLogger())
	_, attempts, err := provider.downloadGeneratedImage(context.Background(), srv.URL+"/generated.png")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if attempts != 3 || requests.Load() != 3 {
		t.Fatalf("attempts = %d, requests = %d, want 3 and 3", attempts, requests.Load())
	}
}

func TestOpenAIGeneratedImageDownloadStopsOnContextCancellation(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()

	provider := testOpenAIProviderWithDownloadPolicy(testLogger())
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, _, err := provider.downloadGeneratedImage(ctx, srv.URL+"/generated.png")
		result <- err
	}()
	<-started
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("download retry helper did not stop after cancellation")
	}
}

func TestOpenAIGeneratedImageDownloadStopsAtPolicyDeadline(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()

	policy := fastOpenAIImageDownloadPolicy()
	policy.totalTimeout = 20 * time.Millisecond
	provider := testOpenAIProviderWithDownloadPolicy(testLogger())
	provider.downloadRetryPolicy = &policy

	_, attempts, err := provider.downloadGeneratedImage(context.Background(), srv.URL+"/generated.png")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestRetryableGeneratedImageDownloadClassification(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "transport", err: &wechat.DownloadError{Original: io.ErrUnexpectedEOF}, want: true},
		{name: "408", err: &wechat.DownloadError{StatusCode: http.StatusRequestTimeout}, want: true},
		{name: "429", err: &wechat.DownloadError{StatusCode: http.StatusTooManyRequests}, want: true},
		{name: "500", err: &wechat.DownloadError{StatusCode: http.StatusInternalServerError}, want: true},
		{name: "599", err: &wechat.DownloadError{StatusCode: 599}, want: true},
		{name: "403", err: &wechat.DownloadError{StatusCode: http.StatusForbidden}, want: false},
		{name: "404", err: &wechat.DownloadError{StatusCode: http.StatusNotFound}, want: false},
		{name: "600", err: &wechat.DownloadError{StatusCode: 600}, want: false},
		{name: "untyped", err: errors.New("download failed"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableGeneratedImageDownload(tt.err); got != tt.want {
				t.Fatalf("isRetryableGeneratedImageDownload() = %t, want %t", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests and verify undefined retry symbols**

```bash
go test ./app/image -run 'Test(OpenAIGeneratedImageDownload|RetryableGeneratedImageDownload)' -count=1
```

Expected: build failures for undefined `openAIImageDownloadRetryPolicy`, `downloadRetryPolicy`, and `downloadGeneratedImage`.

- [ ] **Step 3: Define the fixed policy and retry classifier**

Add this field to `OpenAIProvider`:

```go
	downloadRetryPolicy *openAIImageDownloadRetryPolicy
```

Add these definitions near the provider type:

```go
type openAIImageDownloadRetryPolicy struct {
	maxAttempts int
	totalTimeout time.Duration
	backoffs     []time.Duration
}

var defaultOpenAIImageDownloadRetryPolicy = openAIImageDownloadRetryPolicy{
	maxAttempts: 3,
	totalTimeout: 120 * time.Second,
	backoffs:     []time.Duration{time.Second, 2 * time.Second},
}

func isRetryableGeneratedImageDownload(err error) bool {
	var downloadErr *wechat.DownloadError
	if !errors.As(err, &downloadErr) {
		return false
	}
	if downloadErr.StatusCode == 0 {
		return downloadErr.Original != nil
	}
	return downloadErr.StatusCode == http.StatusRequestTimeout ||
		downloadErr.StatusCode == http.StatusTooManyRequests ||
		(downloadErr.StatusCode >= http.StatusInternalServerError && downloadErr.StatusCode <= 599)
}
```

- [ ] **Step 4: Implement the bounded retry loop and retry warning**

Add these methods before `logImageDownloadFailure`:

```go
func (p *OpenAIProvider) effectiveImageDownloadRetryPolicy() openAIImageDownloadRetryPolicy {
	if p != nil && p.downloadRetryPolicy != nil {
		return *p.downloadRetryPolicy
	}
	return defaultOpenAIImageDownloadRetryPolicy
}

func (p *OpenAIProvider) downloadGeneratedImage(ctx context.Context, rawURL string) (string, int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	policy := p.effectiveImageDownloadRetryPolicy()
	if policy.maxAttempts < 1 {
		policy.maxAttempts = 1
	}
	if policy.totalTimeout <= 0 {
		policy.totalTimeout = defaultOpenAIImageDownloadRetryPolicy.totalTimeout
	}

	downloadCtx, cancel := context.WithTimeout(ctx, policy.totalTimeout)
	defer cancel()

	for attempt := 1; attempt <= policy.maxAttempts; attempt++ {
		path, err := wechat.DownloadFileContext(downloadCtx, rawURL)
		if err == nil {
			return path, attempt, nil
		}
		if downloadCtx.Err() != nil || !isRetryableGeneratedImageDownload(err) || attempt == policy.maxAttempts {
			return "", attempt, err
		}

		delay := time.Duration(0)
		if attempt-1 < len(policy.backoffs) {
			delay = policy.backoffs[attempt-1]
		}
		p.logImageDownloadRetry(rawURL, err, attempt, policy.maxAttempts, delay)

		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-downloadCtx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return "", attempt, downloadCtx.Err()
		}
	}

	return "", policy.maxAttempts, context.DeadlineExceeded
}

func (p *OpenAIProvider) logImageDownloadRetry(rawURL string, err error, attempt, maxAttempts int, delay time.Duration) {
	if p == nil || p.log == nil || err == nil {
		return
	}
	event := p.log.Warn().
		Str("error", sanitizeImageDownloadError(rawURL, err)).
		Str("url_host", downloadURLHost(rawURL)).
		Int("attempt", attempt).
		Int("max_attempts", maxAttempts).
		Dur("retry_delay", delay)
	var downloadErr *wechat.DownloadError
	if errors.As(err, &downloadErr) {
		event = event.
			Int("status_code", downloadErr.StatusCode).
			Dur("download_elapsed", downloadErr.Elapsed)
	}
	event.Msg("openai: retrying generated image URL download")
}

func sanitizeImageDownloadError(rawURL string, err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if rawURL != "" {
		message = strings.ReplaceAll(message, rawURL, "<redacted-url>")
	}
	return message
}
```

- [ ] **Step 5: Format and run the retry-helper tests**

```bash
gofmt -w app/image/openai.go app/image/openai_test.go
go test ./app/image -run 'Test(OpenAIGeneratedImageDownload|RetryableGeneratedImageDownload)' -count=1
```

Expected: PASS in under two seconds. The 403 test reports one request and the exhaustion test exactly three.

- [ ] **Step 6: Commit the provider-local retry helper**

```bash
git add app/image/openai.go app/image/openai_test.go
git commit -m "fix(image): retry OpenAI result downloads"
```

### Task 3: Route Single And Batch URL Results Through The Helper

**Files:**
- Modify: `app/image/openai.go:247-295,392-414,590-721`
- Test: `app/image/openai_test.go:668-706`

- [ ] **Step 1: Write failing diagnostics and batch integration tests**

Replace `TestOpenAIImageURLDownloadFailureLogsDiagnostics` with:

```go
func TestOpenAIImageURLDownloadFailureLogsDiagnostics(t *testing.T) {
	downloadSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Server", "cloudflare")
		w.Header().Set("Cf-Ray", "diag-ray")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("blocked"))
	}))
	defer downloadSrv.Close()

	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	provider := testOpenAIProviderWithDownloadPolicy(&logger)

	_, err := provider.imageDataToResult(context.Background(), openai.Image{
		URL:           downloadSrv.URL + "/generated.png",
		RevisedPrompt: "internal provider prompt",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var generateErr *GenerateError
	if !errors.As(err, &generateErr) {
		t.Fatalf("error type = %T, want *GenerateError", err)
	}
	if generateErr.Code != "url_download_error" || generateErr.HintMsg != "" {
		t.Fatalf("GenerateError = %#v, want url_download_error without hint", generateErr)
	}

	logged := buf.String()
	for _, want := range []string{
		`"attempts":1`,
		`"status_code":403`,
		`"content_type":"text/plain"`,
		`"cf_ray":"diag-ray"`,
		`"message":"openai: failed to download generated image URL"`,
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log %s missing %s", logged, want)
		}
	}
	if strings.Contains(logged, "internal provider prompt") {
		t.Fatalf("log leaked revised prompt: %s", logged)
	}
	if strings.Contains(logged, downloadSrv.URL) || strings.Contains(err.Error(), downloadSrv.URL) {
		t.Fatalf("download URL leaked in error or log: error=%v log=%s", err, logged)
	}
}
```

Append:

```go
func TestOpenAIImageResponseRetriesURLDownloadsForBatch(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "temporary failure", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\nimage"))
	}))
	defer srv.Close()

	provider := testOpenAIProviderWithDownloadPolicy(testLogger())
	result, err := provider.imageResponseToResult(context.Background(), &openai.ImagesResponse{
		Data: []openai.Image{
			{URL: srv.URL + "/first.png"},
			{B64JSON: base64.StdEncoding.EncodeToString([]byte("second"))},
		},
	})
	if err != nil {
		t.Fatalf("imageResponseToResult: %v", err)
	}
	for _, image := range result.Images {
		defer os.Remove(image.URL)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests = %d, want 2", requests.Load())
	}
}

func TestOpenAIImageURLDownloadExhaustionLogsAttemptCount(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "temporary failure", http.StatusBadGateway)
	}))
	defer srv.Close()

	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	provider := testOpenAIProviderWithDownloadPolicy(&logger)
	_, err := provider.imageDataToResult(context.Background(), openai.Image{URL: srv.URL + "/generated.png"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if requests.Load() != 3 {
		t.Fatalf("requests = %d, want 3", requests.Load())
	}
	logged := buf.String()
	if !strings.Contains(logged, `"attempts":3`) {
		t.Fatalf("log %s missing final attempt count", logged)
	}
	if got := strings.Count(logged, `"message":"openai: retrying generated image URL download"`); got != 2 {
		t.Fatalf("retry log count = %d, want 2; log=%s", got, logged)
	}
}
```

- [ ] **Step 2: Run the integration tests and verify signature failures**

```bash
go test ./app/image -run 'TestOpenAI(ImageURLDownloadFailureLogsDiagnostics|ImageResponseRetriesURLDownloadsForBatch|ImageURLDownloadExhaustionLogsAttemptCount)$' -count=1
```

Expected: build failures because `imageDataToResult` and `imageResponseToResult` do not accept a context yet.

- [ ] **Step 3: Thread context through all non-streaming response conversion**

Change the return in `generateStandard` and `generateEdit` to:

```go
return p.imageResponseToResult(ctx, resp)
```

Replace `imageResponseToResult` with:

```go
func (p *OpenAIProvider) imageResponseToResult(ctx context.Context, resp *openai.ImagesResponse) (*GenerateResult, error) {
	size := p.sizeRatio
	if resp.Size != "" {
		size = string(resp.Size)
	}
	usage := usageFromImagesResponse(resp.Usage)
	result := &GenerateResult{Model: p.model, Size: size, Usage: usage}

	if len(resp.Data) == 1 {
		single, err := p.imageDataToResult(ctx, resp.Data[0])
		if err != nil {
			return nil, err
		}
		single.Size = size
		single.Usage = usage
		return single, nil
	}

	images := make([]GeneratedImage, 0, len(resp.Data))
	for i, img := range resp.Data {
		filePath, err := p.saveImageData(ctx, img)
		if err != nil {
			return nil, err
		}
		images = append(images, GeneratedImage{URL: filePath, Index: i})
	}
	result.Images = images
	if len(images) > 0 {
		result.URL = images[0].URL
	}
	result.ResponseType = "b64_json"
	return result, nil
}
```

Replace `saveImageData` with:

```go
func (p *OpenAIProvider) saveImageData(ctx context.Context, img openai.Image) (string, error) {
	if img.B64JSON != "" {
		return p.saveBase64Image(img.B64JSON)
	}
	if img.URL != "" {
		filePath, attempts, err := p.downloadGeneratedImage(ctx, img.URL)
		if err != nil {
			p.logImageDownloadFailure(img.URL, err, attempts)
			return "", &GenerateError{
				Provider: p.Name(),
				Code:     "url_download_error",
				Message:  "下载生成图片失败",
				Original: err,
			}
		}
		return filePath, nil
	}
	return "", &GenerateError{
		Provider: p.Name(),
		Code:     "no_image",
		Message:  "响应中没有图片数据",
	}
}
```

Replace `imageDataToResult` with:

```go
func (p *OpenAIProvider) imageDataToResult(ctx context.Context, img openai.Image) (*GenerateResult, error) {
	result := &GenerateResult{
		Model:         p.model,
		Size:          p.sizeRatio,
		RevisedPrompt: img.RevisedPrompt,
	}

	if img.B64JSON != "" {
		result.ResponseType = "b64_json"
		result.ResponsePreview = previewBase64(img.B64JSON)
		filePath, err := p.saveBase64Image(img.B64JSON)
		if err != nil {
			return nil, err
		}
		result.URL = filePath
		return result, nil
	}

	if img.URL != "" {
		result.ResponseType = "url"
		result.ResponsePreview = img.URL
		filePath, attempts, err := p.downloadGeneratedImage(ctx, img.URL)
		if err != nil {
			p.logImageDownloadFailure(img.URL, err, attempts)
			return nil, &GenerateError{
				Provider: p.Name(),
				Code:     "url_download_error",
				Message:  "OpenAI 图片接口返回了 URL，但下载失败",
				Original: err,
			}
		}
		result.URL = filePath
		return result, nil
	}

	result.ResponseType = "empty"
	return nil, &GenerateError{
		Provider: p.Name(),
		Code:     "no_image",
		Message:  "响应中没有图片 URL 或 base64 数据",
		HintMsg:  "请确认模型支持 OpenAI Images API 图片输出",
	}
}
```

The pre-existing direct `imageDataToResult` call is replaced by the test in Step 1. Do not change streaming paths; they consume base64 events and never download remote URLs.

- [ ] **Step 4: Preserve diagnostics and add final attempt metadata**

Replace `logImageDownloadFailure` with:

```go
func (p *OpenAIProvider) logImageDownloadFailure(rawURL string, err error, attempts int) {
	if p == nil || p.log == nil || err == nil {
		return
	}
	event := p.log.Error().
		Str("error", sanitizeImageDownloadError(rawURL, err)).
		Str("url_host", downloadURLHost(rawURL)).
		Int("attempts", attempts)
	var downloadErr *wechat.DownloadError
	if errors.As(err, &downloadErr) {
		event = event.
			Int("status_code", downloadErr.StatusCode).
			Str("content_type", downloadErr.ContentType).
			Str("content_length", downloadErr.ContentLength).
			Str("server", downloadErr.Server).
			Str("cf_ray", downloadErr.CFRay).
			Str("location", downloadErr.Location).
			Str("body_preview", downloadErr.BodyPreview).
			Dur("download_elapsed", downloadErr.Elapsed)
	}
	event.Msg("openai: failed to download generated image URL")
}
```

- [ ] **Step 5: Format and run all image-package tests**

```bash
gofmt -w app/image/openai.go app/image/openai_test.go
go test ./app/image -count=1
```

Expected: PASS. Existing base64, billing-usage, and provider error-classification tests remain green.

- [ ] **Step 6: Run the Designer error-contract tests**

```bash
go test ./server/service -run 'TestDesignerGenerationErrorMessage' -count=1
```

Expected: PASS. The user-visible error remains sanitized and contains no provider URL or revised prompt.

- [ ] **Step 7: Commit the integrated response path**

```bash
git add app/image/openai.go app/image/openai_test.go
git commit -m "fix(image): use retries for OpenAI URL results"
```

### Task 4: Full Verification And Scope Audit

**Files:**
- Verify: `app/wechat/service.go`
- Verify: `app/wechat/errors_test.go`
- Verify: `app/image/openai.go`
- Verify: `app/image/openai_test.go`

- [ ] **Step 1: Prove only the intended result-download path gained retries**

```bash
rg -n 'DownloadFileContext|downloadGeneratedImage|SetRetryCount|Images\.(Generate|Edit)' app/image app/wechat
```

Expected: `DownloadFileContext` is used by the OpenAI helper; existing generic callers still use `DownloadFile`; no `SetRetryCount` is added globally; no retry loop wraps an OpenAI SDK generation call.

- [ ] **Step 2: Run targeted tests without cache**

```bash
go test ./app/wechat ./app/image ./server/service -count=1
```

Expected: PASS.

- [ ] **Step 3: Run the complete Go suite without cache**

```bash
go test ./... -count=1
```

Expected: PASS. If a parallel full-suite failure appears transient, rerun `go test -p 1 ./... -count=1` before classifying it.

- [ ] **Step 4: Build both binaries to temporary paths**

```bash
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
```

Expected: both commands exit 0 and create no binary in the repository root.

- [ ] **Step 5: Review final scope and worktree hygiene**

```bash
git diff --check
git status --short
git log -4 --oneline
```

Expected: no whitespace errors. Preserve the unrelated untracked `docs/superpowers/plans/2026-07-22-managed-mcp-request-timeout.md`; do not stage or commit it.

- [ ] **Step 6: Record verification evidence in the handoff**

Report the exact targeted test, full test, and build commands with exit results. State that Studio and plugin validation were omitted because neither surface changed. Do not claim the production CDN incident itself was reproduced; the controlled truncated-response test is the reproduction for retry behavior.
