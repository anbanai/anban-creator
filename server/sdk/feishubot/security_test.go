package feishubot

import (
	"strconv"
	"testing"
	"time"
)

func TestVerifySignatureRejectsTimestampOutsideAbsoluteWindow(t *testing.T) {
	const secret = "security-test-secret"
	now := time.Now().Unix()
	for _, tc := range []struct {
		name      string
		timestamp int64
		want      bool
	}{
		{name: "current seconds timestamp", timestamp: now, want: true},
		{name: "stale timestamp", timestamp: now - 601, want: false},
		{name: "future timestamp", timestamp: now + 601, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := strconv.FormatInt(tc.timestamp, 10)
			sign := GenerateSignature(tc.timestamp, secret)
			if got := VerifySignature(ts, sign, secret); got != tc.want {
				t.Fatalf("VerifySignature() = %v, want %v", got, tc.want)
			}
		})
	}
}
