package notification

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

type registryTestSender struct{ typ NotifyType }

func (s registryTestSender) Send(context.Context, json.RawMessage, string, *NotifyContent) error {
	return nil
}
func (s registryTestSender) Type() NotifyType { return s.typ }

type nilRegistryTestSender struct{}

func (*nilRegistryTestSender) Send(context.Context, json.RawMessage, string, *NotifyContent) error {
	return nil
}
func (*nilRegistryTestSender) Type() NotifyType { return NotifyTypeLark }

func TestSenderRegistryIgnoresTypedNilSender(t *testing.T) {
	registry := NewSenderRegistry()
	var sender *nilRegistryTestSender
	registry.Register(sender)
	if _, err := registry.Get(NotifyTypeLark); err == nil {
		t.Fatal("typed nil sender must not be registered")
	}
}

func TestSenderRegistrySupportsConcurrentRegisterAndGet(t *testing.T) {
	registry := NewSenderRegistry()
	const workers = 8
	const iterations = 1000
	var wg sync.WaitGroup
	wg.Add(workers * 2)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				registry.Register(registryTestSender{typ: NotifyType(i%workers + 1)})
			}
		}(i)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_, _ = registry.Get(NotifyType(i%workers + 1))
			}
		}(i)
	}
	wg.Wait()
}
