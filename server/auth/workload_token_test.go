package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestWorkloadTokenRoundTripUsesDistinctPurpose(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	svc, err := NewWorkloadTokenService("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return now }
	want := WorkloadClaims{
		RuntimeScope:      "docker",
		RuntimeWorkload:   "exec-1",
		RuntimeInstanceID: "container-id",
		UserID:            "user-1",
		ProjectID:         "project-1",
		TaskID:            "task-1",
		ExecutionID:       "execution-1",
	}
	raw, err := svc.Issue(want, now.Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Validate(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.RuntimeScope != want.RuntimeScope || got.RuntimeWorkload != want.RuntimeWorkload || got.RuntimeInstanceID != want.RuntimeInstanceID ||
		got.UserID != want.UserID || got.ProjectID != want.ProjectID || got.TaskID != want.TaskID || got.ExecutionID != want.ExecutionID {
		t.Fatalf("claims = %#v, want identity %#v", got, want)
	}
	if got.Issuer == executionTokenIssuer || len(got.Audience) != 1 || got.Audience[0] == executionTokenAudience {
		t.Fatalf("workload token purpose = issuer %q audience %v, want distinct from execution token", got.Issuer, got.Audience)
	}
	if got.Subject != want.ExecutionID || got.ExpiresAt == nil || !got.ExpiresAt.Time.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("registered claims = %#v", got.RegisteredClaims)
	}

	executionSvc, err := NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	executionSvc.now = func() time.Time { return now }
	if _, err := executionSvc.Validate(raw); err == nil {
		t.Fatal("workload token accepted as an execution token")
	}
}

func TestWorkloadTokenRejectsIncompleteIdentityAndWrongAlgorithm(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	svc, _ := NewWorkloadTokenService("0123456789abcdef0123456789abcdef")
	svc.now = func() time.Time { return now }
	if _, err := svc.Issue(WorkloadClaims{}, now.Add(5*time.Minute)); err == nil {
		t.Fatal("incomplete workload identity accepted")
	}
	claims := WorkloadClaims{
		RuntimeScope: "docker", RuntimeWorkload: "exec-1", RuntimeInstanceID: "container-id",
		UserID: "user-1", ProjectID: "project-1", TaskID: "task-1", ExecutionID: "execution-1",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: workloadTokenIssuer, Subject: "execution-1", Audience: jwt.ClaimStrings{workloadTokenAudience},
			IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)), ID: "token-1",
		},
	}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Validate(raw); err == nil {
		t.Fatal("alg=none workload token accepted")
	}
}
