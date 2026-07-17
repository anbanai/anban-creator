package billing

import (
	"errors"
	"testing"
)

func TestParseMicroCNYIsExact(t *testing.T) {
	got, err := ParseMicroCNY("0.30")
	if err != nil || got != 300_000 {
		t.Fatalf("ParseMicroCNY(0.30) = %d, %v; want 300000, nil", got, err)
	}
}

func TestParseMicroCNYBoundaryValues(t *testing.T) {
	tests := []struct {
		input string
		want  MicroCNY
	}{
		{input: "0", want: 0},
		{input: "1", want: 1_000_000},
		{input: "1.000001", want: 1_000_001},
		{input: "9223372036854.775807", want: MicroCNY(1<<63 - 1)},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseMicroCNY(tt.input)
			if err != nil || got != tt.want {
				t.Fatalf("ParseMicroCNY(%q) = %d, %v; want %d, nil", tt.input, got, err, tt.want)
			}
		})
	}
}

func TestParseMicroCNYRejectsInvalidValuesWithReason(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		reason MoneyErrorReason
	}{
		{name: "empty", input: "", reason: MoneyErrorEmpty},
		{name: "negative", input: "-0.01", reason: MoneyErrorNegative},
		{name: "excess precision", input: "0.0000001", reason: MoneyErrorPrecision},
		{name: "invalid", input: "1e3", reason: MoneyErrorSyntax},
		{name: "whitespace", input: " 1.00", reason: MoneyErrorSyntax},
		{name: "overflow whole", input: "9223372036855", reason: MoneyErrorOverflow},
		{name: "overflow fraction", input: "9223372036854.775808", reason: MoneyErrorOverflow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseMicroCNY(tt.input)
			if !errors.Is(err, ErrInvalidMoney) {
				t.Fatalf("ParseMicroCNY(%q) error = %v, want ErrInvalidMoney", tt.input, err)
			}
			var moneyErr *MoneyError
			if !errors.As(err, &moneyErr) || moneyErr.Reason != tt.reason {
				t.Fatalf("ParseMicroCNY(%q) error = %#v, want reason %q", tt.input, err, tt.reason)
			}
		})
	}
}
