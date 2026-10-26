package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestStageAssistantDraftUsesInteractionCapabilityAndReturnsDurableReceipt(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AssistantInteraction{}, &model.AssistantInteractionRuntime{}, &model.AssistantInteractionDraft{}, &model.AssistantInteractionEvent{}, &model.AssistantInteractionAttempt{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	interactionSvc := service.NewAssistantInteractionService(repo, nil, nil)
	snapshot, err := interactionSvc.CreateOrResume(context.Background(), "user-1", service.AssistantInteractionInput{ModeID: service.AssistantModeCreation}, "create-1")
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"plan":{"goal":"write"}}`)
	digest := sha256.Sum256(payload)
	old := svcs
	svcs = &Services{InteractionSvc: interactionSvc}
	t.Cleanup(func() { svcs = old })
	result, err := stageAssistantDraftHandler(withMCPInteractionCapability(context.Background(), "user-1", snapshot.ID, snapshot.Generation), interactiveMCPRequest(t, map[string]any{
		"interaction_id": snapshot.ID, "generation": snapshot.Generation, "attempt_id": "attempt-1", "mode_id": service.AssistantModeCreation,
		"payload": json.RawMessage(payload), "payload_digest": hex.EncodeToString(digest[:]), "ready": true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.IsError || !strings.Contains(toolResultText(result), `"durable":true`) || strings.Contains(toolResultText(result), "session_id") {
		t.Fatalf("interaction stage result = %#v", result)
	}
}

type interactiveMCPRuntime struct{}

func (interactiveMCPRuntime) Command(context.Context, string, any) error { return nil }
func (interactiveMCPRuntime) Close() error                               { return nil }

type interactiveMCPFactory struct {
	tokens chan string
}

func newAssistantMCPProject(t *testing.T, userID, name string) (repository.Repository, *model.Project) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}, &model.Task{}, &model.AssistantCheckpoint{}, &model.AssistantSession{}, &model.AssistantSessionLease{}, &model.AnalyticsState{}, &model.FeedbackLease{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	repo := repository.New(db)
	project := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformWechat, Name: name, Status: model.ProjectStatusActive, Profile: datatypes.NewJSONType(model.NewProjectProfile())}
	if err := repo.Projects().Create(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	return repo, project
}

func (f *interactiveMCPFactory) Start(_ context.Context, _ string, _ string, token string, _ func(string, any)) (service.Runtime, error) {
	f.tokens <- token
	return interactiveMCPRuntime{}, nil
}

func interactiveMCPStageInput() service.InteractiveStageInput {
	return service.InteractiveStageInput{
		Project: service.InteractiveProject{Name: "Project Creator", Platform: model.PlatformWechat},
		Profile: model.NewProjectProfile(),
		Progress: service.InteractiveProgress{
			"identity": "collected", "style": "collected", "audience": "collected",
			"platforms": "collected", "preferences": "collected", "memory": "collected",
		},
		Ready: true,
	}
}

func interactiveMCPRequest(t *testing.T, args map[string]any) *mcp.CallToolRequest {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: raw}}
}

func TestStageProjectCreatorDraftToolSchema(t *testing.T) {
	tools := listRegisteredTools(t, registerInteractiveTools)
	for _, tool := range tools {
		if tool.Name != "stage_project_creator_draft" {
			continue
		}
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("schema = %T, want map", tool.InputSchema)
		}
		required, _ := schema["required"].([]any)
		for _, field := range []string{"project", "profile", "progress", "ready", "missing_fields", "inferred_fields"} {
			found := false
			for _, item := range required {
				if item == field {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("required fields = %#v, missing %q", required, field)
			}
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("schema properties = %T, want map", schema["properties"])
		}
		projectSchema, ok := properties["project"].(map[string]any)
		if !ok || projectSchema["additionalProperties"] != false {
			t.Fatalf("project schema = %#v, want closed object", properties["project"])
		}
		projectProperties, ok := projectSchema["properties"].(map[string]any)
		if !ok || projectProperties["platform"].(map[string]any)["enum"] == nil {
			t.Fatalf("project platform schema = %#v, want enum", projectProperties["platform"])
		}
		profileSchema, ok := properties["profile"].(map[string]any)
		if !ok || profileSchema["type"] != "object" {
			t.Fatalf("profile schema = %#v, want object", properties["profile"])
		}
		profileProperties, ok := profileSchema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("profile properties = %#v", profileSchema)
		}
		dimensions, ok := profileProperties["dimensions"].(map[string]any)
		if !ok || dimensions["type"] != "object" {
			t.Fatalf("profile dimensions schema = %#v", profileProperties["dimensions"])
		}
		progressSchema, ok := properties["progress"].(map[string]any)
		if !ok || progressSchema["additionalProperties"] != false {
			t.Fatalf("progress schema = %#v, want closed object", properties["progress"])
		}
		progressProperties, ok := progressSchema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("progress properties = %#v", progressSchema)
		}
		progressRequired, ok := progressSchema["required"].([]any)
		if !ok {
			t.Fatalf("progress required = %#v, want six dimension statuses", progressSchema["required"])
		}
		for _, field := range []string{"stage", "collected_dimensions", "current_dimension", "notes", "identity", "style", "audience", "platforms", "preferences", "memory"} {
			entry, ok := progressProperties[field].(map[string]any)
			if !ok || entry["type"] != "string" {
				t.Fatalf("progress.%s schema = %#v, want string", field, progressProperties[field])
			}
		}
		for _, field := range []string{"identity", "style", "audience", "platforms", "preferences", "memory"} {
			entry := progressProperties[field].(map[string]any)
			if entry["enum"] == nil {
				t.Fatalf("progress.%s schema = %#v, want status enum", field, entry)
			}
		}
		for _, field := range []string{"identity", "style", "audience", "platforms", "preferences", "memory"} {
			found := false
			for _, required := range progressRequired {
				if required == field {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("progress required = %#v, missing %q", progressRequired, field)
			}
		}
		return
	}
	t.Fatal("stage_project_creator_draft tool not registered")
}

func TestStageAssistantDraftToolSchema(t *testing.T) {
	tools := listRegisteredTools(t, registerInteractiveTools)
	for _, tool := range tools {
		if tool.Name != "stage_assistant_draft" {
			continue
		}
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok || schema["additionalProperties"] != false {
			t.Fatalf("assistant draft schema = %#v, want closed object", tool.InputSchema)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok || properties["payload"].(map[string]any)["type"] != "object" || properties["ready"].(map[string]any)["type"] != "boolean" {
			t.Fatalf("assistant draft properties = %#v", schema["properties"])
		}
		payloadSchema := properties["payload"].(map[string]any)
		if payloadSchema["additionalProperties"] != false {
			t.Fatalf("assistant payload schema = %#v, want closed envelope", payloadSchema)
		}
		planSchema := payloadSchema["properties"].(map[string]any)["plan"].(map[string]any)
		planProperties := planSchema["properties"].(map[string]any)
		for _, field := range []string{"goal", "content_type", "expected_result", "requirements", "constraints", "assets", "missing_inputs", "agent_decisions", "target_audience", "angle", "outline", "style_constraints", "asset_plan", "acceptance_criteria", "cost_estimate", "execution_options"} {
			if _, ok := planProperties[field]; !ok {
				t.Fatalf("assistant plan schema missing %q: %#v", field, planSchema)
			}
		}
		for _, field := range []string{"account", "platform", "column", "topic", "visual_plan", "success_criteria"} {
			if _, ok := planProperties[field]; ok {
				t.Fatalf("assistant plan schema still exposes unsupported %q", field)
			}
		}
		return
	}
	t.Fatal("stage_assistant_draft tool not registered")
}

func TestStageAssistantDraftUsesProtectedSessionMode(t *testing.T) {
	repo, project := newAssistantMCPProject(t, "user-1", "创作画像")
	factory := &interactiveMCPFactory{tokens: make(chan string, 2)}
	svc := service.NewInteractiveServiceWithRuntimeAndAgents(repo, factory, []service.InteractiveAgent{
		{ID: "creation-planner", Available: true, Intents: []string{"plan-creation"}},
		{ID: "project-creator", Available: true, Intents: []string{"create-project"}},
	})
	svc.SetEnabled(true)
	session, err := svc.CreateSession(context.Background(), "user-1", "creation-planner", "plan-creation", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := session.ID
	old := svcs
	svcs = &Services{InteractiveSvc: svc}
	t.Cleanup(func() { svcs = old })
	payload := map[string]any{"plan": map[string]any{
		"goal": "发布新品", "content_type": "公众号文章", "target_audience": "潜在客户", "angle": "真实使用问题",
		"outline": []string{"问题", "方案"}, "style_constraints": "具体", "asset_plan": "产品图",
		"acceptance_criteria": "事实准确", "cost_estimate": "低", "execution_options": []string{"生成完整初稿"},
	}}
	result, err := stageAssistantDraftHandler(withMCPInteractiveIdentity(context.Background(), "user-1", sessionID), interactiveMCPRequest(t, map[string]any{
		"name": "新品计划", "payload": payload, "ready": true,
	}))
	if err != nil || result == nil || result.IsError {
		t.Fatalf("stage assistant draft = %#v, %v", result, err)
	}
	if session.Draft.Name != "新品计划" || !session.Draft.Ready {
		t.Fatalf("staged assistant draft = %#v", session.Draft)
	}
	if err := svc.Close("user-1", session.ID); err != nil {
		t.Fatalf("close creation session before mode check: %v", err)
	}

	profileSession, err := svc.CreateSession(context.Background(), "user-1", "project-creator", "create-project", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	spoofed, err := stageAssistantDraftHandler(withMCPInteractiveIdentity(context.Background(), "user-1", profileSession.ID), interactiveMCPRequest(t, map[string]any{
		"mode_id": "creation", "agent_id": "creation-planner", "payload": payload, "ready": true,
	}))
	if err != nil || spoofed == nil || !spoofed.IsError || !strings.Contains(toolResultText(spoofed), `unknown assistant draft field "mode_id"`) {
		t.Fatalf("spoofed mode staging = %#v, %v, want server-owned profile mode rejection", spoofed, err)
	}
	if profileSession.Draft.Ready {
		t.Fatalf("spoofed request altered profile mode draft: %#v", profileSession.Draft)
	}
}

func TestStageProjectCreatorDraftReturnsValidationCause(t *testing.T) {
	svc := service.NewInteractiveServiceWithRuntime(nil, &interactiveMCPFactory{tokens: make(chan string, 1)})
	svc.SetEnabled(true)
	old := svcs
	svcs = &Services{InteractiveSvc: svc}
	t.Cleanup(func() { svcs = old })

	sess, err := svc.CreateSession(context.Background(), "user-1", "project-creator", "create-project")
	if err != nil {
		t.Fatal(err)
	}
	in := interactiveMCPStageInput()
	in.Profile.Dimensions.Identity.Content = nil
	result, err := stageProjectCreatorDraftHandler(withMCPInteractiveIdentity(context.Background(), "user-1", sess.ID), interactiveMCPRequest(t, map[string]any{
		"project": in.Project, "profile": in.Profile, "progress": in.Progress, "ready": true,
		"missing_fields": []string{}, "inferred_fields": []string{},
	}))
	if err != nil {
		t.Fatal(err)
	}
	message := toolResultText(result)
	if !result.IsError || !strings.Contains(message, "interactive project draft is invalid") || !strings.Contains(message, "profile dimension identity") || !strings.Contains(message, "content") {
		t.Fatalf("draft error = %#v, want root cause details", result)
	}
}

func TestStageProjectCreatorDraftKeepsProfileMetadataAtToolLevel(t *testing.T) {
	schema := projectCreatorProfileSchema()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("profile schema properties = %#v", schema["properties"])
	}
	if _, ok := properties["missing_fields"]; ok {
		t.Fatal("profile schema must not accept profile-level missing_fields")
	}
	if _, ok := properties["inferred_fields"]; ok {
		t.Fatal("profile schema must not accept profile-level inferred_fields")
	}
	dimensions, ok := properties["dimensions"].(map[string]any)
	if !ok {
		t.Fatalf("profile dimensions schema = %#v", properties["dimensions"])
	}
	dimensionProperties := dimensions["properties"].(map[string]any)
	identity := dimensionProperties["identity"].(map[string]any)
	identityProperties := identity["properties"].(map[string]any)
	if _, ok := identityProperties["inferred_fields"]; ok {
		t.Fatal("profile dimensions must not accept inferred_fields")
	}
	if _, ok := identityProperties["missing_fields"]; !ok {
		t.Fatal("profile dimensions must accept dimension-level missing_fields")
	}
}

func TestStageProjectCreatorDraftRejectsProfileLevelInferenceFields(t *testing.T) {
	svc := service.NewInteractiveServiceWithRuntime(nil, &interactiveMCPFactory{tokens: make(chan string, 1)})
	svc.SetEnabled(true)
	old := svcs
	svcs = &Services{InteractiveSvc: svc}
	t.Cleanup(func() { svcs = old })
	sess, err := svc.CreateSession(context.Background(), "user-1", "project-creator", "create-project")
	if err != nil {
		t.Fatal(err)
	}
	in := interactiveMCPStageInput()
	rawProfile, err := json.Marshal(in.Profile)
	if err != nil {
		t.Fatal(err)
	}
	var profile map[string]any
	if err := json.Unmarshal(rawProfile, &profile); err != nil {
		t.Fatal(err)
	}
	profile["missing_fields"] = []string{"style.tone"}
	profile["inferred_fields"] = []string{"audience.primary"}
	result, err := stageProjectCreatorDraftHandler(withMCPInteractiveIdentity(context.Background(), "user-1", sess.ID), interactiveMCPRequest(t, map[string]any{
		"project": in.Project, "profile": profile, "progress": in.Progress, "ready": false,
		"missing_fields": []string{}, "inferred_fields": []string{},
	}))
	if err != nil {
		t.Fatal(err)
	}
	message := toolResultText(result)
	if !result.IsError || !strings.Contains(message, "unknown field") || (!strings.Contains(message, `"missing_fields"`) && !strings.Contains(message, `"inferred_fields"`)) {
		t.Fatalf("profile-level metadata result = %#v, want explicit unknown field", result)
	}
}

func TestStageProjectCreatorDraftRejectsLegacyRootDimensionShape(t *testing.T) {
	svc := service.NewInteractiveServiceWithRuntime(nil, &interactiveMCPFactory{tokens: make(chan string, 1)})
	svc.SetEnabled(true)
	old := svcs
	svcs = &Services{InteractiveSvc: svc}
	t.Cleanup(func() { svcs = old })
	sess, err := svc.CreateSession(context.Background(), "user-1", "project-creator", "create-project")
	if err != nil {
		t.Fatal(err)
	}
	in := interactiveMCPStageInput()
	rawProfile, err := json.Marshal(in.Profile)
	if err != nil {
		t.Fatal(err)
	}
	var legacy map[string]any
	if err := json.Unmarshal(rawProfile, &legacy); err != nil {
		t.Fatal(err)
	}
	legacy["identity"] = map[string]any{"content": map[string]any{"summary": "x"}}
	result, err := stageProjectCreatorDraftHandler(withMCPInteractiveIdentity(context.Background(), "user-1", sess.ID), interactiveMCPRequest(t, map[string]any{
		"project": in.Project, "profile": legacy, "progress": in.Progress, "ready": false,
		"missing_fields": []string{}, "inferred_fields": []string{},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.Contains(toolResultText(result), "unknown field") {
		t.Fatalf("legacy profile result = %#v, want unknown field error", result)
	}
}

func TestStageProjectCreatorDraftUsesProtectedInteractiveIdentity(t *testing.T) {
	factory := &interactiveMCPFactory{tokens: make(chan string, 1)}
	svc := service.NewInteractiveServiceWithRuntime(nil, factory)
	svc.SetEnabled(true)
	old := svcs
	svcs = &Services{InteractiveSvc: svc}
	t.Cleanup(func() { svcs = old })

	sess, err := svc.CreateSession(context.Background(), "user-1", "project-creator", "create-project")
	if err != nil {
		t.Fatal(err)
	}
	var runtimeToken string
	select {
	case runtimeToken = <-factory.tokens:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runtime token")
	}
	in := interactiveMCPStageInput()
	args := map[string]any{
		"project":         in.Project,
		"profile":         in.Profile,
		"progress":        in.Progress,
		"ready":           in.Ready,
		"missing_fields":  []string{"线下门店地址"},
		"inferred_fields": []string{"主要受众为一二线城市白领"},
	}
	ctx := withMCPInteractiveIdentity(context.Background(), "user-1", sess.ID)
	result, err := stageProjectCreatorDraftHandler(ctx, interactiveMCPRequest(t, args))
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.IsError {
		t.Fatalf("result = %#v, want success", result)
	}
	if sess.Draft.Project.Name != in.Project.Name || !sess.Draft.Ready {
		t.Fatalf("staged draft = %#v, want ready project draft", sess.Draft)
	}
	history, _, closeEvents, err := svc.Events("user-1", sess.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer closeEvents()
	var staged map[string]any
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Type != "draft.updated" && history[i].Type != "draft.ready" {
			continue
		}
		if err := json.Unmarshal(interactiveTestJSON(t, history[i].Data), &staged); err != nil {
			t.Fatal(err)
		}
		break
	}
	if staged == nil {
		t.Fatal("staged draft event was not found in event history")
	}
	if got := staged["missing_fields"]; got == nil {
		t.Fatalf("staged event = %#v, missing missing_fields", staged)
	}
	if got := staged["inferred_fields"]; got == nil {
		t.Fatalf("staged event = %#v, missing inferred_fields", staged)
	}

	if runtimeToken == "" {
		t.Fatal("runtime token was empty")
	}
	resolvedID, resolvedUser, expiresAt, err := svc.ResolveRuntimeSession(runtimeToken)
	if err != nil || resolvedID != sess.ID || resolvedUser != "user-1" || !expiresAt.After(time.Now()) {
		t.Fatalf("resolved runtime identity = %q/%q/%v, want session/user/future expiry", resolvedID, resolvedUser, expiresAt)
	}
	if _, _, _, err := svc.ResolveRuntimeSession("wrong-token"); err == nil {
		t.Fatal("wrong runtime token was accepted")
	}
}

func TestStageProjectCreatorDraftRejectsMissingRuntimeContext(t *testing.T) {
	old := svcs
	svcs = &Services{InteractiveSvc: service.NewInteractiveService(nil)}
	t.Cleanup(func() { svcs = old })

	result, err := stageProjectCreatorDraftHandler(context.Background(), interactiveMCPRequest(t, map[string]any{
		"project":  map[string]any{"name": "Project Creator", "platform": model.PlatformWechat},
		"profile":  model.NewProjectProfile(),
		"progress": map[string]any{},
		"ready":    false,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("result = %#v, want MCP error", result)
	}
}

func TestInteractiveRuntimeCredentialCanCallReadOnlyMCPTools(t *testing.T) {
	factory := &interactiveMCPFactory{tokens: make(chan string, 1)}
	svc := service.NewInteractiveServiceWithRuntime(nil, factory)
	svc.SetEnabled(true)
	sess, err := svc.CreateSession(context.Background(), "user-1", "project-creator", "create-project")
	if err != nil {
		t.Fatal(err)
	}
	var runtimeToken string
	select {
	case runtimeToken = <-factory.tokens:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runtime token")
	}
	handler := NewMCPHandler(nil, "", nil, WithInteractiveAuthentication(svc))
	sessionID := initializeMCPExecutionSession(t, handler, runtimeToken)
	if sessionID == "" || sess.ID == "" {
		t.Fatal("MCP or interactive session ID is empty")
	}
	rec := callMCPToolForScopeTest(handler, runtimeToken, sessionID, "list_trends", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("interactive read-only tool call status = %d, body = %s; want 200", rec.Code, rec.Body.String())
	}
	for _, forbidden := range []struct {
		name string
		args string
	}{
		{name: "search_web", args: `{"query":"anban"}`},
		{name: "fetch_web_page", args: `{"url":"https://example.com"}`},
	} {
		search := callMCPToolForScopeTest(handler, runtimeToken, sessionID, forbidden.name, forbidden.args)
		if search.Code != http.StatusForbidden {
			t.Fatalf("interactive %s status = %d, body = %s; want 403", forbidden.name, search.Code, search.Body.String())
		}
	}
	batch := `[{"jsonrpc":"2.0","method":"tools/call","params":{"name":"stage_project_creator_draft","arguments":{}},"id":3},{"jsonrpc":"2.0","method":"tools/call","params":{"name":"search_web","arguments":{"query":"anban"}},"id":4}]`
	batchRec := postMCPForScopeTest(handler, runtimeToken, sessionID, batch)
	if batchRec.Code != http.StatusForbidden {
		t.Fatalf("interactive mixed batch with search status = %d, body = %s; want 403", batchRec.Code, batchRec.Body.String())
	}
}

func TestInteractiveProfileCredentialCannotSearchUserMediaAssets(t *testing.T) {
	factory := &interactiveMCPFactory{tokens: make(chan string, 1)}
	svc := service.NewInteractiveServiceWithRuntimeAndAgents(nil, factory, []service.InteractiveAgent{{ID: "project-creator", Available: true, Intents: []string{"create-project"}}})
	svc.SetEnabled(true)
	if _, err := svc.CreateSession(context.Background(), "user-1", "project-creator", "create-project"); err != nil {
		t.Fatal(err)
	}
	var runtimeToken string
	select {
	case runtimeToken = <-factory.tokens:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runtime token")
	}
	handler := NewMCPHandler(nil, "", nil, WithInteractiveAuthentication(svc))
	sessionID := initializeMCPExecutionSession(t, handler, runtimeToken)
	rec := callMCPToolForScopeTest(handler, runtimeToken, sessionID, "search_user_media_assets", `{}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("interactive profile media search status = %d, body = %s; want 403", rec.Code, rec.Body.String())
	}
}

func TestInteractiveRuntimeCredentialCanStageOwnDraftOverMCP(t *testing.T) {
	factory := &interactiveMCPFactory{tokens: make(chan string, 1)}
	svc := service.NewInteractiveServiceWithRuntime(nil, factory)
	svc.SetEnabled(true)
	old := svcs
	svcs = &Services{InteractiveSvc: svc}
	t.Cleanup(func() { svcs = old })

	sess, err := svc.CreateSession(context.Background(), "user-1", "project-creator", "create-project")
	if err != nil {
		t.Fatal(err)
	}
	var runtimeToken string
	select {
	case runtimeToken = <-factory.tokens:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runtime token")
	}

	handler := NewMCPHandler(nil, "", nil, WithInteractiveAuthentication(svc))
	sessionID := initializeMCPExecutionSession(t, handler, runtimeToken)
	input := interactiveMCPStageInput()
	args, err := json.Marshal(map[string]any{
		"project":         input.Project,
		"profile":         input.Profile,
		"progress":        input.Progress,
		"ready":           input.Ready,
		"missing_fields":  []string{"门店地址"},
		"inferred_fields": []string{"受众偏好短内容"},
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := callMCPToolForScopeTest(handler, runtimeToken, sessionID, "stage_project_creator_draft", string(args))
	if rec.Code != http.StatusOK {
		t.Fatalf("interactive staging status = %d, body = %s; want 200", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `\"staged\":true`) {
		t.Fatalf("interactive staging response = %s, want staged result", rec.Body.String())
	}
	if sess.Draft.Project.Name != input.Project.Name || !sess.Draft.Ready {
		t.Fatalf("staged draft = %#v, want ready project draft", sess.Draft)
	}
}

func TestInteractiveRuntimeCredentialCanStageAssistantDraftOverMCP(t *testing.T) {
	repo, project := newAssistantMCPProject(t, "user-1", "创作画像")
	factory := &interactiveMCPFactory{tokens: make(chan string, 1)}
	svc := service.NewInteractiveServiceWithRuntimeAndAgents(repo, factory, []service.InteractiveAgent{{ID: "creation-planner", Available: true, Intents: []string{"plan-creation"}}})
	svc.SetEnabled(true)
	session, err := svc.CreateSession(context.Background(), "user-1", "creation-planner", "plan-creation", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	old := svcs
	svcs = &Services{InteractiveSvc: svc}
	t.Cleanup(func() { svcs = old })
	var runtimeToken string
	select {
	case runtimeToken = <-factory.tokens:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runtime token")
	}
	handler := NewMCPHandler(nil, "", nil, WithInteractiveAuthentication(svc))
	sessionID := initializeMCPExecutionSession(t, handler, runtimeToken)
	mediaSearch := callMCPToolForScopeTest(handler, runtimeToken, sessionID, "search_user_media_assets", `{}`)
	if mediaSearch.Code != http.StatusOK {
		t.Fatalf("interactive creation media search status = %d, body = %s; want 200", mediaSearch.Code, mediaSearch.Body.String())
	}
	for _, tool := range []string{"list_projects", "get_project", "list_drafts"} {
		rec := callMCPToolForScopeTest(handler, runtimeToken, sessionID, tool, `{"project_id":"foreign-project"}`)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s status=%d; frozen-context token must not read user-wide data", tool, rec.Code)
		}
	}
	args, err := json.Marshal(map[string]any{"payload": map[string]any{"plan": map[string]any{"goal": "目标"}}, "ready": false})
	if err != nil {
		t.Fatal(err)
	}
	rec := callMCPToolForScopeTest(handler, runtimeToken, sessionID, "stage_assistant_draft", string(args))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `\"staged\":true`) {
		t.Fatalf("interactive assistant draft status/body = %d/%s", rec.Code, rec.Body.String())
	}
	if session.Draft.Name != "创作需求" || session.Draft.Fields["goal"] != "目标" {
		t.Fatalf("staged assistant draft = %#v", session.Draft)
	}
}

func interactiveTestJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
