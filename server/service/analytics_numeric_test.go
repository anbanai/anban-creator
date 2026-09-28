package service

import "testing"

func TestAnalyticsImportNumbersPreserveInt64(t *testing.T) {
	for _, raw := range []string{"9007199254740993", "9223372036854775807"} {
		var errors []string
		value := parseSeednoteCount("count", raw, &errors)
		want := int64(9007199254740993)
		if raw == "9223372036854775807" {
			want = 9223372036854775807
		}
		if len(errors) != 0 || value == nil || *value != want {
			t.Errorf("%s parsed as %v, errors %v", raw, value, errors)
		}
	}
}

func TestAnalyticsImportCountsAcceptIntegralXLSXDecimals(t *testing.T) {
	for _, raw := range []string{"0.0", "260.0", "1180.0", "5048.0"} {
		var errors []string
		value := parseSeednoteCount("count", raw, &errors)
		if len(errors) != 0 || value == nil {
			t.Errorf("did not accept integral XLSX count %s: value=%v errors=%v", raw, value, errors)
		}
	}
}
func TestAnalyticsImportNumbersRejectNonfiniteAndOverflow(t *testing.T) {
	for _, raw := range []string{"NaN", "+Inf", "-Inf", "9223372036854775808", "1.2", "-1", "1/1"} {
		var errors []string
		if value := parseSeednoteCount("count", raw, &errors); value != nil || len(errors) == 0 {
			t.Errorf("accepted invalid count %s", raw)
		}
	}
	for _, raw := range []string{"NaN", "+Inf", "-Inf"} {
		var errors []string
		if value := parseSeednoteNumber("duration", raw, &errors); value != nil || len(errors) == 0 {
			t.Errorf("accepted invalid duration %s", raw)
		}
		if value, errs := parseWechatRate(raw, "rate", nil); value != nil || len(errs) == 0 {
			t.Errorf("accepted invalid rate %s", raw)
		}
	}
}
