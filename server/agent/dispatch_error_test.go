package agent

import (
	"errors"
	"testing"
)

func TestPermanentDispatchErrorContract(t *testing.T) {
	cause := errors.New("execution identity mismatch")
	err := NewPermanentDispatchError(cause)
	if !IsPermanentDispatchError(err) {
		t.Fatal("typed permanent dispatch error was not recognized")
	}
	if !errors.Is(err, cause) {
		t.Fatal("permanent dispatch error did not preserve its cause")
	}
	if IsPermanentDispatchError(errors.New("connection reset")) {
		t.Fatal("untyped transport error was classified as permanent")
	}
}
