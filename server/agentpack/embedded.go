package agentpack

import (
	_ "embed"
	"encoding/json"
	"sync"
)

//go:embed catalog.generated.json
var embeddedCatalogJSON []byte

var (
	defaultOnce    sync.Once
	defaultCatalog *Catalog
)

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
