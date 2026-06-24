package scheduler

import (
	"testing"
	"time"
)

func TestAsynqClient_EffectiveTimeout(t *testing.T) {
	// Configured timeout is returned as-is.
	configured := &AsynqClient{timeout: 90 * time.Minute}
	if got := configured.effectiveTimeout(); got != 90*time.Minute {
		t.Errorf("effectiveTimeout = %v, want 90m", got)
	}

	// Zero value falls back to the 60m default.
	zero := &AsynqClient{}
	if got := zero.effectiveTimeout(); got != 60*time.Minute {
		t.Errorf("effectiveTimeout default = %v, want 60m", got)
	}
}
