package billing

import (
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/agentpack"
)

// Every task-admission operation declared by a managed Pack must have a
// published price for all supported execution profiles; otherwise server-owned
// task creation fails at runtime with ErrBillingProfileSKUNotFound. Packs may
// also declare internal operations (for example feedback_analysis) that are
// created directly without a billing quote and are therefore not task SKUs.
func TestEveryTaskAdmissionOperationHasProfilePriceSKU(t *testing.T) {
	bundle := loadProductionBundle(t)
	available := make(map[string]struct{})
	for _, sku := range bundle.Products.SKUs {
		if sku.ChargePolicy == "task_admission" {
			available[sku.Operation+"\x00"+sku.ExecutionProfile] = struct{}{}
		}
	}
	for _, pack := range agentpack.Default().Packs {
		for taskKind, operation := range pack.BillingOperations {
			if !strings.HasPrefix(operation, "task.") {
				continue
			}
			for _, profile := range []string{"effective", "balanced", "quality"} {
				if _, ok := available[operation+"\x00"+profile]; !ok {
					t.Errorf("pack %s task kind %s: missing %s SKU for operation %s", pack.ID, taskKind, profile, operation)
				}
			}
		}
	}
}
