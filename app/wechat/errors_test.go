package wechat

import (
	"errors"
	"testing"
)

func TestParseWechatError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantNil   bool
		wantCode  int
		wantRetry bool
	}{
		{"nil error", nil, true, 0, false},
		{"network error", errors.New("connection refused"), true, 0, false},
		{"40164 IP whitelist", errors.New("errcode=40164, IP not in whitelist"), false, 40164, false},
		{"40001 invalid credential", errors.New("errcode=40001, invalid credential"), false, 40001, false},
		{"42001 token expired", errors.New("errcode=42001, access_token expired"), false, 42001, true},
		{"45009 rate limit", errors.New("errcode=45009, api freq out of limit"), false, 45009, true},
		{"-1 system busy", errors.New("errcode=-1, system error"), false, -1, true},
		{"unknown code", errors.New("errcode=99999, unknown"), false, 99999, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseWechatError(tt.err)
			if tt.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil WechatAPIError")
			}
			if got.ErrCode != tt.wantCode {
				t.Errorf("ErrCode = %d, want %d", got.ErrCode, tt.wantCode)
			}
			if got.Retryable != tt.wantRetry {
				t.Errorf("Retryable = %v, want %v", got.Retryable, tt.wantRetry)
			}
		})
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"network error", errors.New("connection refused"), true},
		{"40164 not retryable", errors.New("errcode=40164, IP not in whitelist"), false},
		{"42001 retryable", errors.New("errcode=42001, token expired"), true},
		{"WechatAPIError direct", &WechatAPIError{ErrCode: 40164, Retryable: false}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRetryable(tt.err); got != tt.want {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.want)
			}
		})
	}
}
