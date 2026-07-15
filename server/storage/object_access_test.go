package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

type unboundedOnlyProvider struct{}

func (*unboundedOnlyProvider) Name() string { return "unbounded" }
func (*unboundedOnlyProvider) Upload(context.Context, string, io.Reader, string) (*UploadResult, error) {
	return nil, errors.New("not implemented")
}
func (*unboundedOnlyProvider) UploadFile(context.Context, string, string, string) (*UploadResult, error) {
	return nil, errors.New("not implemented")
}
func (*unboundedOnlyProvider) UploadURL(context.Context, string, string, int) (string, error) {
	return "", errors.New("not implemented")
}
func (*unboundedOnlyProvider) GetURL(string) string                         { return "" }
func (*unboundedOnlyProvider) Read(context.Context, string) ([]byte, error) { return nil, nil }
func (*unboundedOnlyProvider) Delete(context.Context, string) error         { return nil }
func (*unboundedOnlyProvider) DownloadURL(context.Context, string, int) (string, error) {
	return "", nil
}
func (*unboundedOnlyProvider) HasCustomDomain() bool  { return false }
func (*unboundedOnlyProvider) IsOwnedURL(string) bool { return false }

func TestReadObjectFailsClosedWithoutBoundedCapability(t *testing.T) {
	_, err := ReadObject(context.Background(), &unboundedOnlyProvider{}, "object", 10)
	if !errors.Is(err, ErrBoundedReadUnsupported) {
		t.Fatalf("ReadObject error = %v, want ErrBoundedReadUnsupported", err)
	}
}

func TestLocalProviderStatsAndBoundsObjectReads(t *testing.T) {
	provider, err := NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalProvider: %v", err)
	}
	const key = "uploads/input.png"
	if _, err := provider.Upload(context.Background(), key, strings.NewReader("12345"), "image/png"); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	info, err := provider.StatObject(context.Background(), key)
	if err != nil {
		t.Fatalf("StatObject: %v", err)
	}
	if info.Size != 5 || info.ContentType != "image/png" {
		t.Fatalf("ObjectInfo = %#v", info)
	}
	if _, err := provider.ReadObject(context.Background(), key, 4); !errors.Is(err, ErrObjectExceedsMaxSize) {
		t.Fatalf("ReadObject error = %v, want ErrObjectExceedsMaxSize", err)
	}
	data, err := provider.ReadObject(context.Background(), key, 5)
	if err != nil || !bytes.Equal(data, []byte("12345")) {
		t.Fatalf("ReadObject = %q, %v", data, err)
	}
}

func TestLocalProviderStatObjectClassifiesNotFound(t *testing.T) {
	provider, err := NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalProvider: %v", err)
	}
	_, err = provider.StatObject(context.Background(), "uploads/missing.png")
	if !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("StatObject error = %v, want ErrObjectNotFound", err)
	}
}

func TestOSSProviderBoundsObjectReadsAtMaxPlusOne(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "6")
		_, _ = w.Write([]byte("123456"))
	}))
	t.Cleanup(server.Close)
	client, err := oss.New(server.URL, "access-key", "secret", oss.UseCname(true))
	if err != nil {
		t.Fatalf("oss.New: %v", err)
	}
	bucket, err := client.Bucket("bucket")
	if err != nil {
		t.Fatalf("Bucket: %v", err)
	}
	provider := &OSSProvider{bucket: bucket}
	if _, err := provider.ReadObject(context.Background(), "object", 5); !errors.Is(err, ErrObjectExceedsMaxSize) {
		t.Fatalf("ReadObject error = %v, want ErrObjectExceedsMaxSize", err)
	}
}

func TestOSSProviderStatObjectClassifiesNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<Error><Code>NoSuchKey</Code><Message>missing</Message><RequestId>request-secret</RequestId></Error>`))
	}))
	t.Cleanup(server.Close)
	client, err := oss.New(server.URL, "access-key", "secret", oss.UseCname(true))
	if err != nil {
		t.Fatalf("oss.New: %v", err)
	}
	bucket, err := client.Bucket("bucket")
	if err != nil {
		t.Fatalf("Bucket: %v", err)
	}
	provider := &OSSProvider{bucket: bucket}

	_, err = provider.StatObject(context.Background(), "missing.png")
	if !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("StatObject error = %v, want ErrObjectNotFound", err)
	}
}

func TestLocalProviderPromotesVerifiedObjectImmutably(t *testing.T) {
	provider, err := NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalProvider: %v", err)
	}
	ctx := context.Background()
	const (
		source = "uploads/pending/user-1/upload-1/input.png"
		final  = "uploads/finalized/user-1/upload-1/input.png"
	)
	if _, err := provider.Upload(ctx, source, strings.NewReader("verified"), "image/png"); err != nil {
		t.Fatalf("upload source: %v", err)
	}
	info, err := provider.StatObject(ctx, source)
	if err != nil || info.ETag == "" {
		t.Fatalf("StatObject = %#v, %v; want content ETag", info, err)
	}
	if err := provider.PromoteObject(ctx, source, final, info.ETag); err != nil {
		t.Fatalf("PromoteObject: %v", err)
	}
	if _, err := provider.Upload(ctx, source, strings.NewReader("replaced"), "image/png"); err != nil {
		t.Fatalf("replace source: %v", err)
	}
	finalData, err := provider.Read(ctx, final)
	if err != nil || string(finalData) != "verified" {
		t.Fatalf("final data = %q, %v; want immutable verified bytes", finalData, err)
	}
	if err := provider.PromoteObject(ctx, source, final+".retry", info.ETag); !errors.Is(err, ErrPromotionPreconditionFailed) {
		t.Fatalf("stale promotion error = %v, want ErrPromotionPreconditionFailed", err)
	}
	currentInfo, err := provider.StatObject(ctx, source)
	if err != nil {
		t.Fatalf("stat replaced source: %v", err)
	}
	if err := provider.PromoteObject(ctx, source, final, currentInfo.ETag); !errors.Is(err, ErrObjectAlreadyExists) {
		t.Fatalf("existing final error = %v, want ErrObjectAlreadyExists", err)
	}
}

func TestOSSProviderPromoteObjectUsesConditionalImmutableCopy(t *testing.T) {
	var gotHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<CopyObjectResult><ETag>verified-etag</ETag><LastModified>2026-07-15T00:00:00.000Z</LastModified></CopyObjectResult>`))
	}))
	t.Cleanup(server.Close)
	provider := newTestOSSProvider(t, server.URL)

	err := provider.PromoteObject(context.Background(), "uploads/pending/user-1/upload-1/input.png", "uploads/finalized/user-1/upload-1/input.png", "verified-etag")
	if err != nil {
		t.Fatalf("PromoteObject: %v", err)
	}
	if got := gotHeaders.Get("X-Oss-Copy-Source-If-Match"); got != "verified-etag" {
		t.Fatalf("copy source If-Match = %q", got)
	}
	if got := gotHeaders.Get("X-Oss-Forbid-Overwrite"); got != "true" {
		t.Fatalf("forbid overwrite = %q", got)
	}
	if got := gotHeaders.Get("X-Oss-Copy-Source"); got != "/bucket/uploads%2Fpending%2Fuser-1%2Fupload-1%2Finput.png" {
		t.Fatalf("copy source = %q", got)
	}
}

func TestOSSProviderPromoteObjectClassifiesStructuredFailures(t *testing.T) {
	tests := []struct {
		name   string
		status int
		code   string
		want   error
	}{
		{name: "source changed", status: http.StatusPreconditionFailed, code: "PreconditionFailed", want: ErrPromotionPreconditionFailed},
		{name: "destination exists", status: http.StatusConflict, code: "FileAlreadyExists", want: ErrObjectAlreadyExists},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`<Error><Code>` + tt.code + `</Code><Message>structured failure</Message><RequestId>request-id</RequestId></Error>`))
			}))
			t.Cleanup(server.Close)
			provider := newTestOSSProvider(t, server.URL)
			err := provider.PromoteObject(context.Background(), "source", "final", "etag")
			if !errors.Is(err, tt.want) {
				t.Fatalf("PromoteObject error = %v, want %v", err, tt.want)
			}
		})
	}
}

func newTestOSSProvider(t *testing.T, endpoint string) *OSSProvider {
	t.Helper()
	client, err := oss.New(endpoint, "access-key", "secret", oss.UseCname(true))
	if err != nil {
		t.Fatalf("oss.New: %v", err)
	}
	bucket, err := client.Bucket("bucket")
	if err != nil {
		t.Fatalf("Bucket: %v", err)
	}
	return &OSSProvider{bucket: bucket}
}
