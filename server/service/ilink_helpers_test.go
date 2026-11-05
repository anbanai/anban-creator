package service

import (
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestTaskTypeLabelUsesEcommerceDesignName(t *testing.T) {
	if got := taskTypeLabel(model.PlatformEcommerce); got != "电商设计" {
		t.Fatalf("taskTypeLabel(%q) = %q, want %q", model.PlatformEcommerce, got, "电商设计")
	}
}
