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
