package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"net/url"
	"testing"

	"github.com/anbanai/anban-creator/server/config"
)

func TestOSSUploadURLWithMetadataBindsSHA256Header(t *testing.T) {
	const (
		accessKeyID     = "test-access-key"
		accessKeySecret = "test-access-secret"
		bucketName      = "test-bucket"
		objectKey       = "artifacts/output/compliance-report.md"
		contentType     = "text/markdown"
		hash            = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	provider, err := NewOSSProvider(config.StorageConfig{
		Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
		AccessKeyID:     accessKeyID,
		AccessKeySecret: accessKeySecret,
		BucketName:      bucketName,
	}, nil)
	if err != nil {
		t.Fatalf("NewOSSProvider: %v", err)
	}

	signedURL, err := provider.UploadURLWithMetadata(context.Background(), objectKey, contentType, map[string]string{
		ObjectMetadataSHA256: hash,
	}, 60)
	if err != nil {
		t.Fatalf("UploadURLWithMetadata: %v", err)
	}
	parsed, err := url.Parse(signedURL)
	if err != nil {
		t.Fatalf("parse signed URL: %v", err)
	}
	expires := parsed.Query().Get("Expires")
	stringToSign := "PUT\n\n" + contentType + "\n" + expires + "\n" +
		"x-oss-meta-sha256:" + hash + "\n/" + bucketName + "/" + objectKey
	mac := hmac.New(sha1.New, []byte(accessKeySecret))
	_, _ = mac.Write([]byte(stringToSign))
	wantSignature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	if got := parsed.Query().Get("Signature"); got != wantSignature {
		t.Fatalf("signature = %q, want metadata-bound signature %q", got, wantSignature)
	}
}
