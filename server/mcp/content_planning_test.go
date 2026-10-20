package mcp

import (
	"context"
	"errors"
	serverauth "github.com/anbanai/anban-creator/server/auth"
	"net/http"
	"testing"
	"time"
)

type planningPolicyAuthorizer struct {
	executionAuthorizerStub
	denied int
}

func (a *planningPolicyAuthorizer) ValidateAgentExecutionToolAccess(_ context.Context, _, _, _, _, tool string) error {
	if tool == "generate_image" {
		a.denied++
		return errors.New("text stage forbids media")
	}
	return nil
}
func TestExecutionMiddlewareChecksPlanningCostPolicyBeforeToolDispatch(t *testing.T) {
	old := svcs
	spy := &fixedSKUImageServiceSpy{}
	svcs = &Services{TaskImageSvc: spy}
	t.Cleanup(func() { svcs = old })
	tokens, err := serverauth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	token, err := tokens.Issue(serverauth.ExecutionClaims{UserID: "u", ProjectID: "p", TaskID: "t", ExecutionID: "e"}, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	authorizer := &planningPolicyAuthorizer{executionAuthorizerStub: executionAuthorizerStub{wantUserID: "u", wantProjectID: "p", wantTaskID: "t", wantExecutionID: "e"}}
	handler := NewMCPHandler(nil, "admin", nil, WithExecutionAuthentication(tokens, authorizer))
	session := initializeMCPExecutionSession(t, handler, token)
	response := callMCPToolForScopeTest(handler, token, session, "generate_image", `{"project_id":"p","task_id":"t","prompt":"image"}`)
	if response.Code != http.StatusForbidden || spy.generateCalls != 0 || authorizer.denied != 1 {
		t.Fatalf("policy bypass: status=%d provider_calls=%d checked=%d", response.Code, spy.generateCalls, authorizer.denied)
	}
}
