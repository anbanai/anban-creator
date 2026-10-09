package agentpack

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestCatalogDigestUsesEmbeddedBytes(t *testing.T) {
	sum := sha256.Sum256(embeddedCatalogJSON)
	want := hex.EncodeToString(sum[:])
	if got := CatalogDigest(); got != want {
		t.Fatalf("CatalogDigest() = %q, want %q", got, want)
	}
	if len(CatalogDigest()) != 64 {
		t.Fatalf("CatalogDigest() length = %d, want 64", len(CatalogDigest()))
	}
}
