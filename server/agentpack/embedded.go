package agentpack

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"sync"
)

//go:embed catalog.generated.json
var embeddedCatalogJSON []byte

var (
	defaultOnce    sync.Once
	defaultCatalog *Catalog
	digestOnce     sync.Once
	digest         string
)

// CatalogDigest returns the SHA-256 identity of the exact generated Catalog
// bytes embedded in this Server binary.
func CatalogDigest() string {
	digestOnce.Do(func() {
		sum := sha256.Sum256(embeddedCatalogJSON)
		digest = hex.EncodeToString(sum[:])
	})
	return digest
}

func Default() *Catalog {
	defaultOnce.Do(func() {
		var catalog Catalog
		if err := json.Unmarshal(embeddedCatalogJSON, &catalog); err != nil {
			panic("decode embedded Agent Pack Catalog: " + err.Error())
		}
		catalog.reindex()
		defaultCatalog = &catalog
	})
	return defaultCatalog
}
