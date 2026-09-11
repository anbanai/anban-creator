package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestExecutionTokenIssueAndValidate(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	svc, err := NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return now }
	token, err := svc.Issue(ExecutionClaims{UserID: "u1", ProjectID: "p1", TaskID: "t1", ExecutionID: "e1"}, now.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	claims, err := svc.Validate(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "e1" || claims.ExecutionID != "e1" || claims.UserID != "u1" || claims.ProjectID != "p1" || claims.TaskID != "t1" {
		t.Fatalf("claims = %#v", claims)
	}
}

func TestExecutionTokenAcceptsDefaultLocalExecutionLifecycle(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	svc, err := NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return now }
	identity := ExecutionClaims{UserID: "u1", ProjectID: "p1", TaskID: "t1", ExecutionID: "e1"}
	token, err := svc.Issue(identity, now.Add(70*time.Minute))
	if err != nil {
		t.Fatalf("issue default local lifecycle token: %v", err)
	}
	if _, err := svc.Validate(token); err != nil {
		t.Fatalf("validate default local lifecycle token: %v", err)
	}
}

func TestExecutionTokenRejectsWeakSecretAndWrongAlgorithm(t *testing.T) {
	if _, err := NewExecutionTokenService(""); err == nil {
		t.Fatal("empty secret accepted")
	}
	svc, err := NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	claims := ExecutionClaims{ExecutionID: "e1", RegisteredClaims: jwt.RegisteredClaims{
		Issuer: executionTokenIssuer, Subject: "e1", Audience: jwt.ClaimStrings{executionTokenAudience}, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}}
	token, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Validate(token); err == nil {
		t.Fatal("alg=none token accepted")
	}
}

func TestExecutionTokenRejectsExecutionSubjectMismatchAndFutureToken(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	svc, _ := NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	svc.now = func() time.Time { return now }
	for _, tc := range []ExecutionClaims{
		{UserID: "u1", ProjectID: "p1", TaskID: "t1", ExecutionID: "e1", RegisteredClaims: jwt.RegisteredClaims{Issuer: executionTokenIssuer, Subject: "other", Audience: jwt.ClaimStrings{executionTokenAudience}, ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)), NotBefore: jwt.NewNumericDate(now), IssuedAt: jwt.NewNumericDate(now), ID: "j1"}},
		{UserID: "u1", ProjectID: "p1", TaskID: "t1", ExecutionID: "e1", RegisteredClaims: jwt.RegisteredClaims{Issuer: executionTokenIssuer, Subject: "e1", Audience: jwt.ClaimStrings{executionTokenAudience}, ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)), NotBefore: jwt.NewNumericDate(now.Add(time.Minute)), IssuedAt: jwt.NewNumericDate(now), ID: "j2"}},
		{UserID: "u1", ProjectID: "p1", TaskID: "t1", ExecutionID: "e1", RegisteredClaims: jwt.RegisteredClaims{Issuer: executionTokenIssuer, Subject: "e1", Audience: jwt.ClaimStrings{executionTokenAudience}, ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour))}},
		{UserID: "u1", ProjectID: "p1", TaskID: "t1", ExecutionID: "e1", RegisteredClaims: jwt.RegisteredClaims{Issuer: "other", Subject: "e1", Audience: jwt.ClaimStrings{executionTokenAudience}, ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)), NotBefore: jwt.NewNumericDate(now), IssuedAt: jwt.NewNumericDate(now), ID: "j3"}},
		{UserID: "u1", ProjectID: "p1", TaskID: "t1", ExecutionID: "e1", RegisteredClaims: jwt.RegisteredClaims{Issuer: executionTokenIssuer, Subject: "e1", Audience: jwt.ClaimStrings{"other"}, ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)), NotBefore: jwt.NewNumericDate(now), IssuedAt: jwt.NewNumericDate(now), ID: "j4"}},
		{UserID: "u1", ProjectID: "p1", TaskID: "t1", ExecutionID: "e1", RegisteredClaims: jwt.RegisteredClaims{Issuer: executionTokenIssuer, Subject: "e1", Audience: jwt.ClaimStrings{executionTokenAudience}, ExpiresAt: jwt.NewNumericDate(now.Add(-time.Second)), NotBefore: jwt.NewNumericDate(now.Add(-time.Hour)), IssuedAt: jwt.NewNumericDate(now.Add(-time.Hour)), ID: "j5"}},
	} {
		raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, tc).SignedString(svc.secret)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Validate(raw); err == nil {
			t.Fatalf("accepted claims %#v", tc)
		}
	}
}

func TestExecutionTokenRejectsLifetimeBeyondMaximum(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	svc, _ := NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	svc.now = func() time.Time { return now }
	identity := ExecutionClaims{UserID: "u1", ProjectID: "p1", TaskID: "t1", ExecutionID: "e1"}
	if _, err := svc.Issue(identity, now.Add(MaximumExecutionTokenLifetime+time.Second)); err == nil {
		t.Fatal("issued overlong execution token")
	}
	identity.RegisteredClaims = jwt.RegisteredClaims{Issuer: executionTokenIssuer, Subject: "e1", Audience: jwt.ClaimStrings{executionTokenAudience}, IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(MaximumExecutionTokenLifetime + time.Second)), ID: "j1"}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, identity).SignedString(svc.secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Validate(raw); err == nil {
		t.Fatal("accepted overlong execution token")
	}
}
