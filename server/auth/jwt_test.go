package auth

import (
	"testing"
	"time"
)

const testSecret = "test-secret-key-that-is-long-enough"

func newTestService(t *testing.T) *JWTService {
	t.Helper()
	svc, err := NewJWTService(testSecret, "1h", "168h")
	if err != nil {
		t.Fatalf("NewJWTService: %v", err)
	}
	return svc
}

func TestNewJWTService(t *testing.T) {
	tests := []struct {
		name          string
		secret        string
		accessExpiry  string
		refreshExpiry string
		wantErr       bool
	}{
		{"valid", "secret", "1h", "168h", false},
		{"empty secret", "", "1h", "168h", true},
		{"invalid access expiry", "secret", "not-a-duration", "168h", true},
		{"invalid refresh expiry", "secret", "1h", "bad", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewJWTService(tt.secret, tt.accessExpiry, tt.refreshExpiry)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewJWTService() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGenerateAccessToken(t *testing.T) {
	svc := newTestService(t)

	token, err := svc.GenerateAccessToken("user-123")
	if err != nil {
		t.Fatalf("GenerateAccessToken: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	// Validate the generated token.
	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if claims.UserID != "user-123" {
		t.Errorf("expected UserID=user-123, got %s", claims.UserID)
	}
	if claims.Role != "user" {
		t.Errorf("expected Role=user, got %s", claims.Role)
	}
}

func TestGenerateRefreshToken(t *testing.T) {
	svc := newTestService(t)

	token, err := svc.GenerateRefreshToken("user-456")
	if err != nil {
		t.Fatalf("GenerateRefreshToken: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if claims.UserID != "user-456" {
		t.Errorf("expected UserID=user-456, got %s", claims.UserID)
	}
}

func TestValidateToken_Valid(t *testing.T) {
	svc := newTestService(t)

	token, err := svc.GenerateAccessToken("user-abc")
	if err != nil {
		t.Fatalf("GenerateAccessToken: %v", err)
	}

	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken valid token: %v", err)
	}
	if claims.UserID != "user-abc" {
		t.Errorf("expected UserID=user-abc, got %s", claims.UserID)
	}
}

func TestValidateToken_Expired(t *testing.T) {
	// Create a service with very short expiry.
	svc, err := NewJWTService(testSecret, "1ms", "168h")
	if err != nil {
		t.Fatalf("NewJWTService: %v", err)
	}

	token, err := svc.GenerateAccessToken("user-expired")
	if err != nil {
		t.Fatalf("GenerateAccessToken: %v", err)
	}

	// Wait for the token to expire.
	time.Sleep(5 * time.Millisecond)

	_, err = svc.ValidateToken(token)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

func TestValidateToken_Invalid(t *testing.T) {
	svc := newTestService(t)

	tests := []struct {
		name  string
		token string
	}{
		{"empty string", ""},
		{"random string", "not.a.valid.token"},
		{"wrong secret", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyX2lkIjoiMSJ9.xxx"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.ValidateToken(tt.token)
			if err == nil {
				t.Error("expected error for invalid token, got nil")
			}
		})
	}
}

func TestValidateToken_WrongSecret(t *testing.T) {
	svc1 := newTestService(t)
	svc2, err := NewJWTService("different-secret-key", "1h", "168h")
	if err != nil {
		t.Fatalf("NewJWTService: %v", err)
	}

	token, err := svc1.GenerateAccessToken("user-cross")
	if err != nil {
		t.Fatalf("GenerateAccessToken: %v", err)
	}

	_, err = svc2.ValidateToken(token)
	if err == nil {
		t.Fatal("expected error when validating token signed with different secret")
	}
}
