package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/config"
)

var ErrArchestraNotConfigured = errors.New("archestra A2A service is not configured")

type ArchestraAgent struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type ArchestraMessage struct {
	Role      string `json:"role,omitempty"`
	ContextID string `json:"context_id,omitempty"`
	Text      string `json:"text,omitempty"`
}

type ApprovalDecision struct {
	ID       string `json:"approvalId"`
	Approved bool   `json:"approved"`
}

type ArchestraApprovalRequest struct {
	ID        string `json:"approval_id"`
	ToolName  string `json:"tool_name,omitempty"`
	Text      string `json:"text,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
	Resolved  bool   `json:"resolved"`
}

type ArchestraTask struct {
	ID               string                     `json:"task_id"`
	ContextID        string                     `json:"context_id,omitempty"`
	State            string                     `json:"state"`
	StatusMessage    string                     `json:"status_message,omitempty"`
	Response         string                     `json:"response,omitempty"`
	ApprovalRequests []ArchestraApprovalRequest `json:"approval_requests,omitempty"`
}

type ArchestraSendResult struct {
	Message *ArchestraMessage
	Task    *ArchestraTask
}

type ArchestraClient struct {
	baseURL string
	token   string
	client  *http.Client
	timeout time.Duration
}

func NewArchestraClient(cfg config.ArchestraConfig, client *http.Client) *ArchestraClient {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	return &ArchestraClient{
		baseURL: strings.TrimRight(strings.TrimSpace(cfg.A2ABaseURL), "/"),
		token:   strings.TrimSpace(cfg.A2AToken),
		client:  client,
		timeout: timeout,
	}
}

func (c *ArchestraClient) configured() error {
	if c == nil || c.client == nil || c.baseURL == "" || c.token == "" {
		return ErrArchestraNotConfigured
	}
	return nil
}

func (c *ArchestraClient) ListAgents(ctx context.Context) ([]ArchestraAgent, error) {
	if err := c.configured(); err != nil {
		return nil, err
	}
	var payload json.RawMessage
	if err := c.do(ctx, http.MethodGet, c.baseURL+"/v2/a2a/agents", nil, &payload); err != nil {
		return nil, err
	}
	var rawAgents []rawAgent
	if err := json.Unmarshal(payload, &rawAgents); err != nil {
		var wrapped struct {
			Agents []rawAgent `json:"agents"`
		}
		if err := json.Unmarshal(payload, &wrapped); err != nil {
			return nil, fmt.Errorf("invalid Archestra agent list")
		}
		rawAgents = wrapped.Agents
	}
	agents := make([]ArchestraAgent, 0, len(rawAgents))
	for _, raw := range rawAgents {
		id := strings.TrimSpace(raw.ID)
		if id == "" {
			id = strings.TrimSpace(raw.AgentID)
		}
		if id == "" {
			id = strings.TrimSpace(raw.AgentIDSnake)
		}
		if id == "" {
			id = agentIDFromURL(raw.URL)
		}
		if id == "" {
			return nil, fmt.Errorf("invalid Archestra agent list")
		}
		agents = append(agents, ArchestraAgent{ID: id, Name: raw.Name, Description: raw.Description})
	}
	return agents, nil
}

type rawAgent struct {
	ID           string `json:"id"`
	AgentID      string `json:"agentId"`
	AgentIDSnake string `json:"agent_id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	URL          string `json:"url"`
}

func agentIDFromURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "a2a" && parts[i+1] != "agents" {
			return strings.TrimSpace(parts[i+1])
		}
	}
	return ""
}

func (c *ArchestraClient) SendMessage(ctx context.Context, agentID, contextID, taskID, text string, decisions []ApprovalDecision) (ArchestraSendResult, error) {
	params := map[string]any{
		"message": map[string]any{
			"role":      "ROLE_USER",
			"parts":     []map[string]string{},
			"contextId": strings.TrimSpace(contextID),
			"taskId":    strings.TrimSpace(taskID),
		},
	}
	message := params["message"].(map[string]any)
	if strings.TrimSpace(text) != "" {
		message["parts"] = []map[string]string{{"text": text}}
	}
	if strings.TrimSpace(contextID) == "" {
		delete(message, "contextId")
	}
	if strings.TrimSpace(taskID) == "" {
		delete(message, "taskId")
	}
	if len(decisions) > 0 {
		message["metadata"] = map[string]any{"taskOps": map[string]any{"approvalDecisions": decisions}}
	}
	var response rpcResponse
	if err := c.callAgent(ctx, agentID, "SendMessage", params, &response); err != nil {
		return ArchestraSendResult{}, err
	}
	return parseSendResult(response)
}

func (c *ArchestraClient) GetTask(ctx context.Context, agentID, taskID string) (ArchestraTask, error) {
	var response rpcResponse
	if err := c.callAgent(ctx, agentID, "GetTask", map[string]any{"id": taskID, "historyLength": 0}, &response); err != nil {
		return ArchestraTask{}, err
	}
	return parseTask(response.Result.Task)
}

func (c *ArchestraClient) CancelTask(ctx context.Context, agentID, taskID string) (ArchestraTask, error) {
	var response rpcResponse
	if err := c.callAgent(ctx, agentID, "CancelTask", map[string]any{"id": taskID}, &response); err != nil {
		return ArchestraTask{}, err
	}
	if response.Result.Task == nil {
		return ArchestraTask{ID: taskID, State: "TASK_STATE_CANCELED"}, nil
	}
	return parseTask(response.Result.Task)
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	Error *struct {
		Code int `json:"code"`
	} `json:"error,omitempty"`
	Result struct {
		Message *rawMessage `json:"message,omitempty"`
		Task    *rawTask    `json:"task,omitempty"`
	} `json:"result,omitempty"`
}

type rawMessage struct {
	Role      string    `json:"role"`
	ContextID string    `json:"contextId"`
	Parts     []rawPart `json:"parts"`
}

type rawPart struct {
	Text string `json:"text,omitempty"`
}

type rawTask struct {
	ID        string `json:"id"`
	ContextID string `json:"contextId"`
	Status    struct {
		State   string      `json:"state"`
		Message *rawMessage `json:"message,omitempty"`
	} `json:"status"`
	Artifacts []struct {
		Name  string    `json:"name"`
		Parts []rawPart `json:"parts"`
	} `json:"artifacts,omitempty"`
	Metadata struct {
		ApprovalRequests []struct {
			ApprovalID     string `json:"approvalId"`
			ToolName       string `json:"toolName"`
			Resolved       bool   `json:"resolved"`
			OpenAppaReview struct {
				Text      string `json:"text"`
				ExpiresAt string `json:"expiresAt"`
			} `json:"openappaReview"`
		} `json:"approvalRequests"`
	} `json:"metadata"`
}

func (c *ArchestraClient) callAgent(ctx context.Context, agentID, method string, params any, out *rpcResponse) error {
	if err := c.configured(); err != nil {
		return err
	}
	id := strings.TrimSpace(agentID)
	if id == "" {
		return fmt.Errorf("agent id is required")
	}
	pathID := url.PathEscape(id)
	body := rpcRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params}
	return c.do(ctx, http.MethodPost, c.baseURL+"/v2/a2a/"+pathID, body, out)
}

func (c *ArchestraClient) do(ctx context.Context, method, endpoint string, body any, out any) error {
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode Archestra request")
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return fmt.Errorf("create Archestra request")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return fmt.Errorf("read Archestra response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Archestra request returned status %d", resp.StatusCode)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("invalid Archestra response")
	}
	if rpc, ok := out.(*rpcResponse); ok && rpc.Error != nil {
		return fmt.Errorf("Archestra request failed with code %d", rpc.Error.Code)
	}
	return nil
}

func parseSendResult(response rpcResponse) (ArchestraSendResult, error) {
	if response.Result.Message != nil {
		return ArchestraSendResult{Message: &ArchestraMessage{Role: response.Result.Message.Role, ContextID: response.Result.Message.ContextID, Text: joinParts(response.Result.Message.Parts)}}, nil
	}
	if response.Result.Task != nil {
		task, err := parseTask(response.Result.Task)
		return ArchestraSendResult{Task: &task}, err
	}
	return ArchestraSendResult{}, fmt.Errorf("Archestra response contained no message or task")
}

func parseTask(raw *rawTask) (ArchestraTask, error) {
	if raw == nil || strings.TrimSpace(raw.ID) == "" {
		return ArchestraTask{}, fmt.Errorf("Archestra response contained no task")
	}
	statusMessage := ""
	if raw.Status.Message != nil {
		statusMessage = joinParts(raw.Status.Message.Parts)
	}
	task := ArchestraTask{ID: raw.ID, ContextID: raw.ContextID, State: raw.Status.State, StatusMessage: statusMessage}
	for _, artifact := range raw.Artifacts {
		if artifact.Name == "agent-response" {
			task.Response = joinParts(artifact.Parts)
		}
	}
	for _, approval := range raw.Metadata.ApprovalRequests {
		task.ApprovalRequests = append(task.ApprovalRequests, ArchestraApprovalRequest{ID: approval.ApprovalID, ToolName: approval.ToolName, Text: approval.OpenAppaReview.Text, ExpiresAt: approval.OpenAppaReview.ExpiresAt, Resolved: approval.Resolved})
	}
	return task, nil
}

func joinParts(parts []rawPart) string {
	var texts []string
	for _, part := range parts {
		if strings.TrimSpace(part.Text) != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "")
}
