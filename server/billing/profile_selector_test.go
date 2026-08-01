package billing

import "testing"

func TestFindSKUByExecutionProfile(t *testing.T) {
	catalog := ProductCatalog{SKUs: []SKUConfig{
		{ID: "task.seednote.cost.v1", Operation: "task.seednote", ExecutionProfile: "effective"},
		{ID: "task.seednote.balance.v1", Operation: "task.seednote", ExecutionProfile: "balanced"},
		{ID: "task.seednote.quality.v1", Operation: "task.seednote", ExecutionProfile: "quality"},
		{ID: "image.standard", Operation: "image.generate", Route: "image_generation.capabilities.standard"},
	}}

	for _, want := range []string{"effective", "balanced", "quality"} {
		sku, ok := catalog.FindSKUByExecutionProfile("task.seednote", want)
		if !ok || sku.ExecutionProfile != want {
			t.Fatalf("profile %q resolved to %#v, ok=%v", want, sku, ok)
		}
	}
	if _, ok := catalog.FindSKUByExecutionProfile("image.generate", "balanced"); ok {
		t.Fatal("Designer must not resolve Agent execution profiles")
	}
}
