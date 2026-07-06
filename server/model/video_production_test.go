package model

import (
	"encoding/json"
	"testing"
)

func TestVideoTaskConfigProductionFieldsRoundTrip(t *testing.T) {
	cfg := VideoTaskConfig{
		ScenarioKey:    "live_selling",
		ProductionMode: "guided",
		RetakeBudget:   5,
		DeliveryTargets: []string{
			"vertical_9x16",
			"textless",
		},
		References: []VideoReferenceAsset{{
			Type:            "image_url",
			ReferenceRole:   "product appearance",
			MustKeep:        []string{"bottle shape", "silver cap"},
			CanChange:       []string{"background"},
			MustNotTransfer: []string{"donor person", "donor logo"},
		}},
	}

	payload, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	var decoded VideoTaskConfig
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	if decoded.ScenarioKey != "live_selling" || decoded.ProductionMode != "guided" || decoded.RetakeBudget != 5 {
		t.Fatalf("decoded production fields = %+v", decoded)
	}
	if len(decoded.DeliveryTargets) != 2 || decoded.DeliveryTargets[1] != "textless" {
		t.Fatalf("delivery targets = %+v", decoded.DeliveryTargets)
	}
	if got := decoded.References[0].MustNotTransfer; len(got) != 2 || got[1] != "donor logo" {
		t.Fatalf("must_not_transfer = %+v", got)
	}
}
