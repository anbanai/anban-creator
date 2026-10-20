package billing

import "testing"

func TestContentPlanningPricesDeriveFromSameProfileTextAnalysis(t *testing.T) {
	catalog := loadProductionBundle(t).Products
	for _, kind := range []string{"content_research", "content_validation", "content_script"} {
		for _, profile := range []string{"effective", "balanced", "quality"} {
			source, ok := catalog.FindSKUByExecutionProfile("task.viral_analysis", profile)
			if !ok {
				t.Fatal("source tariff missing")
			}
			sku, ok := catalog.FindSKUByExecutionProfile("task."+kind, profile)
			if !ok || sku.PriceCredits != source.PriceCredits || sku.PriceSource != source.ID || sku.ChargePolicy != "task_admission" {
				t.Fatalf("%s/%s tariff: %+v", kind, profile, sku)
			}
		}
	}
}
