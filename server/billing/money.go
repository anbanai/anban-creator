package billing

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

const microCNYPerCNY int64 = 1_000_000

type MicroCNY int64

var ErrInvalidMoney = errors.New("invalid money amount")

type MoneyErrorReason string

const (
	MoneyErrorEmpty     MoneyErrorReason = "empty"
	MoneyErrorNegative  MoneyErrorReason = "negative"
	MoneyErrorPrecision MoneyErrorReason = "excess_precision"
	MoneyErrorSyntax    MoneyErrorReason = "invalid_syntax"
	MoneyErrorOverflow  MoneyErrorReason = "overflow"
)

type MoneyError struct {
	Input  string
	Reason MoneyErrorReason
}

func (e *MoneyError) Error() string {
	return fmt.Sprintf("invalid money amount %q: %s", e.Input, e.Reason)
}

func (e *MoneyError) Is(target error) bool {
	return target == ErrInvalidMoney
}

// ParseMicroCNY parses a non-negative decimal CNY amount using at most six
// fractional digits. It never converts through floating point.
func ParseMicroCNY(input string) (MicroCNY, error) {
	if input == "" {
		return 0, &MoneyError{Input: input, Reason: MoneyErrorEmpty}
	}
	if strings.HasPrefix(input, "-") {
		return 0, &MoneyError{Input: input, Reason: MoneyErrorNegative}
	}
	if strings.Count(input, ".") > 1 {
		return 0, &MoneyError{Input: input, Reason: MoneyErrorSyntax}
	}

	whole, fraction, hasFraction := strings.Cut(input, ".")
	if whole == "" || (hasFraction && fraction == "") {
		return 0, &MoneyError{Input: input, Reason: MoneyErrorSyntax}
	}
	if len(fraction) > 6 {
		return 0, &MoneyError{Input: input, Reason: MoneyErrorPrecision}
	}
	if !decimalDigits(whole) || !decimalDigits(fraction) {
		return 0, &MoneyError{Input: input, Reason: MoneyErrorSyntax}
	}

	wholeValue, ok := parseDigits(whole, math.MaxInt64/microCNYPerCNY)
	if !ok {
		return 0, &MoneyError{Input: input, Reason: MoneyErrorOverflow}
	}
	fractionValue, _ := parseDigits(fraction, microCNYPerCNY-1)
	for i := len(fraction); i < 6; i++ {
		fractionValue *= 10
	}
	if wholeValue > (math.MaxInt64-fractionValue)/microCNYPerCNY {
		return 0, &MoneyError{Input: input, Reason: MoneyErrorOverflow}
	}
	return MicroCNY(wholeValue*microCNYPerCNY + fractionValue), nil
}

func decimalDigits(value string) bool {
	for i := range len(value) {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

func parseDigits(value string, max int64) (int64, bool) {
	var result int64
	for i := range len(value) {
		digit := int64(value[i] - '0')
		if result > (max-digit)/10 {
			return 0, false
		}
		result = result*10 + digit
	}
	return result, true
}
