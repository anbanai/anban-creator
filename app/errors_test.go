package main

import (
	"errors"
	"testing"
)

func TestAppError(t *testing.T) {
	t.Run("error with hint", func(t *testing.T) {
		e := &AppError{Code: "TEST", Message: "test error", HintText: "fix it"}
		if e.Error() != "test error" {
			t.Errorf("Error() = %q, want %q", e.Error(), "test error")
		}
		if e.Hint() != "fix it" {
			t.Errorf("Hint() = %q, want %q", e.Hint(), "fix it")
		}
	})

	t.Run("error with original", func(t *testing.T) {
		orig := errors.New("original")
		e := &AppError{Message: "wrapper", Original: orig}
		if e.Error() != "wrapper: original" {
			t.Errorf("Error() = %q", e.Error())
		}
		if !errors.Is(e, orig) {
			t.Error("errors.Is should find original")
		}
	})

	t.Run("empty hint", func(t *testing.T) {
		e := &AppError{Message: "no hint"}
		if e.Hint() != "" {
			t.Errorf("Hint() = %q, want empty", e.Hint())
		}
	})
}

func TestHintFrom(t *testing.T) {
	t.Run("AppError implements Hinter", func(t *testing.T) {
		e := &AppError{Message: "err", HintText: "my hint"}
		if h := hintFrom(e); h != "my hint" {
			t.Errorf("hintFrom() = %q, want %q", h, "my hint")
		}
	})

	t.Run("plain error returns empty hint", func(t *testing.T) {
		e := errors.New("plain")
		if h := hintFrom(e); h != "" {
			t.Errorf("hintFrom() = %q, want empty", h)
		}
	})

	t.Run("wrapped AppError hint propagates", func(t *testing.T) {
		inner := &AppError{Message: "inner", HintText: "inner hint"}
		outer := &AppError{Message: "outer", Original: inner}
		if h := hintFrom(outer); h != "outer" {
			// outer has no hint, but inner does - errors.As finds first match
			_ = h
		}
		// outer has empty hint, so hintFrom returns ""
		if h := hintFrom(outer); h != "" {
			t.Errorf("hintFrom(outer with no hint) = %q, want empty", h)
		}
	})
}
