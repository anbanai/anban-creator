package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/config"
)

func TestArchestraClientUsesConfiguredURLTokenAndJSONRPC(t *testing.T) {
	var gotPath, gotAuth, gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth, gotMethod = r.URL.Path, r.Header.Get("Authorization"), r.Method
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"method":"SendMessage"`) || !strings.Contains(string(body), `"ROLE_USER"`) {
			t.Errorf("request body = %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{"message":{"role":"ROLE_AGENT","contextId":"ctx-1","parts":[{"text":"hello"}]}}}`)
	}))
	defer server.Close()

	client := NewArchestraClient(config.ArchestraConfig{A2ABaseURL: server.URL, A2AToken: "secret", Timeout: time.Second}, server.Client())
	result, err := client.SendMessage(context.Background(), "agent-1", "", "", "hi", nil)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v2/a2a/agent-1" || gotAuth != "Bearer secret" || gotMethod != http.MethodPost || result.Message == nil || result.Message.Text != "hello" {
		t.Fatalf("path=%q auth=%q method=%q result=%#v", gotPath, gotAuth, gotMethod, result)
	}
}

func TestArchestraClientListsAgentsAndParsesTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v2/a2a/agents" {
			fmt.Fprint(w, `[{"id":"agent-1","name":"Writer","description":"desc"}]`)
			return
		}
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{"task":{"id":"task-1","contextId":"ctx-1","status":{"state":"TASK_STATE_INPUT_REQUIRED","message":{"parts":[{"text":"need approval"}]}},"metadata":{"approvalRequests":[{"approvalId":"appr-1","toolName":"tool","resolved":false,"openappaReview":{"text":"Approve?"}}]}}}}`)
	}))
	defer server.Close()
	client := NewArchestraClient(config.ArchestraConfig{A2ABaseURL: server.URL, A2AToken: "secret", Timeout: time.Second}, server.Client())
	agents, err := client.ListAgents(context.Background())
	if err != nil || len(agents) != 1 || agents[0].ID != "agent-1" {
		t.Fatalf("agents=%#v err=%v", agents, err)
	}
	task, err := client.GetTask(context.Background(), "agent-1", "task-1")
	if err != nil || task.ID != "task-1" || task.State != "TASK_STATE_INPUT_REQUIRED" || len(task.ApprovalRequests) != 1 || task.ApprovalRequests[0].ID != "appr-1" {
		t.Fatalf("task=%#v err=%v", task, err)
	}
}

func TestArchestraClientNormalizesAgentCardIdentifiers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"agents":[{"agentId":"agent-by-camel","name":"Camel"},{"agent_id":"agent-by-snake","name":"Snake"},{"url":"https://archestra.example/v2/a2a/agent-by-url/.well-known/agent-card.json","name":"URL"}]}`)
	}))
	defer server.Close()

	client := NewArchestraClient(config.ArchestraConfig{A2ABaseURL: server.URL, A2AToken: "secret", Timeout: time.Second}, server.Client())
	agents, err := client.ListAgents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 3 || agents[0].ID != "agent-by-camel" || agents[1].ID != "agent-by-snake" || agents[2].ID != "agent-by-url" {
		t.Fatalf("agents=%#v", agents)
	}
}

func TestArchestraClientApprovalAndCancelRequests(t *testing.T) {
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{"task":{"id":"task-1","status":{"state":"TASK_STATE_WORKING"}}}}`)
	}))
	defer server.Close()
	client := NewArchestraClient(config.ArchestraConfig{A2ABaseURL: server.URL, A2AToken: "secret", Timeout: time.Second}, server.Client())
	if _, err := client.SendMessage(context.Background(), "agent-1", "ctx-1", "task-1", "", []ApprovalDecision{{ID: "appr-1", Approved: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CancelTask(context.Background(), "agent-1", "task-1"); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 || !strings.Contains(bodies[0], `"approvalDecisions":[{"approvalId":"appr-1","approved":true}]`) || !strings.Contains(bodies[1], `"method":"CancelTask"`) || !strings.Contains(bodies[1], `"id":"task-1"`) {
		t.Fatalf("bodies=%v", bodies)
	}
}

func TestArchestraClientRedactsUpstreamErrorsAndHandlesTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, `secret prompt and token body`)
	}))
	defer server.Close()
	client := NewArchestraClient(config.ArchestraConfig{A2ABaseURL: server.URL, A2AToken: "top-secret", Timeout: time.Second}, server.Client())
	_, err := client.ListAgents(context.Background())
	if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "top-secret") || !strings.Contains(err.Error(), "status 502") {
		t.Fatalf("error = %v", err)
	}

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(100 * time.Millisecond) }))
	defer slow.Close()
	client = NewArchestraClient(config.ArchestraConfig{A2ABaseURL: slow.URL, A2AToken: "secret", Timeout: 5 * time.Millisecond}, slow.Client())
	_, err = client.ListAgents(context.Background())
	if err == nil || (!errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "deadline")) {
		t.Fatalf("timeout error = %v", err)
	}
}
