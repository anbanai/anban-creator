package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	serverauth "github.com/anbanai/anban-creator/server/auth"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/rs/zerolog"
)

type requestIdentityMetadataSpy struct {
	fixedSKUContentMetadataSpy
	targets []string
}

func (s *requestIdentityMetadataSpy) FindAuthorized(_ context.Context, userID, taskID, executionID string) (*model.ContentMetadataReport, error) {
	s.targets = append(s.targets, executionID)
	return &model.ContentMetadataReport{ID: "report-1", TaskID: taskID, ExecutionID: executionID}, nil
}

type requestIdentityRepository struct {
	repository.Repository
	keys repository.APIKeyRepository
}

func (r requestIdentityRepository) APIKeys() repository.APIKeyRepository { return r.keys }

type requestIdentityAPIKeys struct {
	repository.APIKeyRepository
	hash string
}

func (r requestIdentityAPIKeys) FindByHash(_ context.Context, hash string) (*model.APIKey, error) {
	if hash != r.hash {
		return nil, errors.New("unknown API key")
	}
	return &model.APIKey{ID: "key-1", UserID: "user-1"}, nil
}

func (requestIdentityAPIKeys) UpdateLastUsed(context.Context, string) error { return nil }

func TestMCPHandlerUsesRequestIdentityAcrossSessionCredentialChanges(t *testing.T) {
	for _, test := range []struct {
		name       string
		initial    string
		caller     string
		tool       string
		args       string
		wantError  bool
		wantTarget string
	}{
		{name: "new execution defaults to itself in old execution session", initial: "old", caller: "current", tool: "get_completion_metadata_status", args: `{"task_id":"task-1"}`, wantTarget: "execution-current"},
		{name: "new execution may explicitly name itself in old session", initial: "old", caller: "current", tool: "get_completion_metadata_status", args: `{"task_id":"task-1","execution_id":"execution-current"}`, wantTarget: "execution-current"},
		{name: "API key must specify target after execution session", initial: "old", caller: "api", tool: "get_completion_metadata_status", args: `{"task_id":"task-1"}`, wantError: true},
		{name: "API key may select explicit history after execution session", initial: "old", caller: "api", tool: "get_completion_metadata_status", args: `{"task_id":"task-1","execution_id":"execution-history"}`, wantTarget: "execution-history"},
		{name: "API key cannot inherit execution authority to submit", initial: "old", caller: "api", tool: "submit_completion_metadata", args: `{"task_id":"task-1","metadata":"{}"}`, wantError: true},
		{name: "execution defaults to itself after API key session", initial: "api", caller: "current", tool: "get_completion_metadata_status", args: `{"task_id":"task-1"}`, wantTarget: "execution-current"},
		{name: "execution can submit after API key session", initial: "api", caller: "current", tool: "submit_completion_metadata", args: `{"task_id":"task-1","metadata":"{}"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			tokens, err := serverauth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
			if err != nil {
				t.Fatal(err)
			}
			credentials := map[string]string{"api": "anb_request_identity_test_key"}
			for _, name := range []string{"old", "current"} {
				credentials[name], err = tokens.Issue(serverauth.ExecutionClaims{UserID: "user-1", ProjectID: "project-1", TaskID: "task-1", ExecutionID: "execution-" + name}, time.Now().Add(10*time.Minute))
				if err != nil {
					t.Fatal(err)
				}
			}
			sum := sha256.Sum256([]byte(credentials["api"]))
			logger := zerolog.Nop()
			keys := service.NewAPIKeyService(requestIdentityRepository{keys: requestIdentityAPIKeys{hash: hex.EncodeToString(sum[:])}}, &logger)
			authorizer := &executionAuthorizerStub{wantUserID: "user-1", wantProjectID: "project-1", wantTaskID: "task-1", wantExecutionID: "execution-current"}
			metadata := &requestIdentityMetadataSpy{}
			original := svcs
			SetServices(&Services{ContentMetadataSvc: metadata})
			t.Cleanup(func() { svcs = original })
			handler := NewMCPHandler(keys, "", nil, WithExecutionAuthentication(tokens, authorizer))
			sessionID := initializeMCPExecutionSession(t, handler, credentials[test.initial])
			rec := callMCPToolForScopeTest(handler, credentials[test.caller], sessionID, test.tool, test.args)
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			isError := strings.Contains(rec.Body.String(), `"isError":true`)
			if isError != test.wantError {
				t.Fatalf("isError=%t want %t; body=%s", isError, test.wantError, rec.Body.String())
			}
			if test.wantError {
				if len(metadata.targets) != 0 || metadata.submitCalls != 0 {
					t.Fatalf("rejected credential still reached metadata service: targets=%v submits=%d", metadata.targets, metadata.submitCalls)
				}
				return
			}
			if test.wantTarget != "" && (len(metadata.targets) != 1 || metadata.targets[0] != test.wantTarget) {
				t.Fatalf("service targets=%v want [%s]", metadata.targets, test.wantTarget)
			}
			if test.tool == "submit_completion_metadata" && metadata.submitCalls != 1 {
				t.Fatalf("submitCalls=%d want 1", metadata.submitCalls)
			}
		})
	}
}
