package service

import (
	"errors"
	"fmt"
	"testing"
)

func TestTopicRadarRecoveryTreatsConcurrentDispatchAsNoop(t *testing.T) {
	if err := topicRadarRecoveryError(fmt.Errorf("dispatch: %w", ErrDispatchInProgress)); err != nil {
		t.Fatalf("concurrent dispatch should be ignored: %v", err)
	}

	underlying := errors.New("database unavailable")
	if err := topicRadarRecoveryError(underlying); !errors.Is(err, underlying) {
		t.Fatalf("unexpected error filtering: %v", err)
	}
}
