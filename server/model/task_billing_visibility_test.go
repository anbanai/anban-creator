package model

import (
	"encoding/json"
	"strings"
	"testing"

	"gorm.io/datatypes"
)

func TestTaskJSONHidesInternalProviderCostEvidence(t *testing.T) {
	value := int64(10)
	task := Task{
		ID: "task-1", UserID: "user-1", Type: PlatformArticle,
		TerminalModelUsage: datatypes.NewJSONType([]ModelTokenUsage{{Provider: "provider", Model: "model", InputTokens: 1}}),
		CostStatus:         "reconciled", InputTokens: &value, OutputTokens: &value, CacheReadTokens: &value, CacheCreationTokens: &value,
		BillingCatalogID: "retail-v1", BillingSKUID: "task.article.v1", BillingPriceCredits: 6000,
	}
	raw, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(raw)
	for _, forbidden := range []string{`"terminal_model_usage"`, `"cost_status"`, `"input_tokens"`, `"output_tokens"`, `"cache_read_tokens"`, `"cache_creation_tokens"`, `"provider"`, `"model"`} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("task JSON leaked %q: %s", forbidden, encoded)
		}
	}
	for _, required := range []string{"billing_catalog_id", "billing_sku_id", "billing_price_credits"} {
		if !strings.Contains(encoded, required) {
			t.Fatalf("task JSON omitted fixed billing identity %q: %s", required, encoded)
		}
	}
}
