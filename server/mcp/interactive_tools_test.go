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

func TestInteractionDraftStageSanitizesRepositoryErrors(t *testing.T) {
	testCases := []struct {
		name    string
		modeID  string
		handler func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error)
	}{
		{name: "assistant", modeID: service.AssistantModeCreation, handler: stageAssistantDraftHandler},
		{name: "project creator", modeID: service.AssistantModeProfile, handler: stageProjectCreatorDraftHandler},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.AutoMigrate(&model.AssistantInteraction{}, &model.AssistantInteractionRuntime{}, &model.AssistantInteractionDraft{}, &model.AssistantInteractionEvent{}, &model.AssistantInteractionAttempt{}); err != nil {
				t.Fatal(err)
			}
			interactionSvc := service.NewAssistantInteractionService(repository.New(db), nil, nil)
			snapshot, err := interactionSvc.CreateOrResume(context.Background(), "user-1", service.AssistantInteractionInput{ModeID: testCase.modeID}, "create-sanitize-"+testCase.name)
			if err != nil {
				t.Fatal(err)
			}
			old := svcs
			svcs = &Services{InteractionSvc: interactionSvc}
			t.Cleanup(func() { svcs = old })
			sqlDB, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			if err := sqlDB.Close(); err != nil {
				t.Fatal(err)
			}

			payload := json.RawMessage(`{"plan":{"goal":"write"}}`)
			digest := sha256.Sum256(payload)
			args := map[string]any{
				"interaction_id": snapshot.ID, "generation": snapshot.Generation,
				"attempt_id": "sanitize-" + testCase.name, "mode_id": testCase.modeID,
				"payload": payload, "payload_digest": hex.EncodeToString(digest[:]), "ready": true,
			}
			result, err := testCase.handler(withMCPInteractionCapability(context.Background(), "user-1", snapshot.ID, snapshot.Generation), interactiveMCPRequest(t, args))
			if err != nil {
				t.Fatal(err)
			}
			text := toolResultText(result)
			if result == nil || !result.IsError || !strings.Contains(text, service.InteractionErrorMessageNotAccepted) {
				t.Fatalf("stage error result = %#v, want sanitized interaction error", result)
			}
			if strings.Contains(strings.ToLower(text), "database") || strings.Contains(strings.ToLower(text), "closed") || strings.Contains(strings.ToLower(text), "sql:") {
				t.Fatalf("stage error leaked repository details: %q", text)
			}
		})
	}
}

func TestDraftStagingRejectsLegacySessionOnlyService(t *testing.T) {
	t.Run("assistant draft", func(t *testing.T) {
		repo, project := newAssistantMCPProject(t, "user-1", "创作画像")
		svc := service.NewInteractiveServiceWithRuntimeAndAgents(repo, &interactiveMCPFactory{tokens: make(chan string, 1)}, []service.InteractiveAgent{
			{ID: "creation-planner", Available: true, Intents: []string{"plan-creation"}},
		})
		svc.SetEnabled(true)
		session, err := svc.CreateSession(context.Background(), "user-1", "creation-planner", "plan-creation", project.ID)
		if err != nil {
			t.Fatal(err)
		}
		old := svcs
		svcs = &Services{InteractiveSvc: svc}
		t.Cleanup(func() { svcs = old })
		result, err := stageAssistantDraftHandler(withMCPInteractiveIdentity(context.Background(), "user-1", session.ID), interactiveMCPRequest(t, map[string]any{
			"name": "新品计划", "payload": map[string]any{"plan": map[string]any{"goal": "发布新品"}}, "ready": true,
		}))
		if err != nil {
			t.Fatal(err)
		}
		if result == nil || !result.IsError || strings.Contains(toolResultText(result), "session_id") {
			t.Fatalf("legacy assistant stage = %#v, want fail-closed interaction-service error", result)
		}
	})

	t.Run("project creator draft", func(t *testing.T) {
		repo, project := newAssistantMCPProject(t, "user-1", "创作画像")
		svc := service.NewInteractiveServiceWithRuntimeAndAgents(repo, &interactiveMCPFactory{tokens: make(chan string, 1)}, []service.InteractiveAgent{
			{ID: "project-creator", Available: true, Intents: []string{"create-project"}},
		})
		svc.SetEnabled(true)
		session, err := svc.CreateSession(context.Background(), "user-1", "project-creator", "create-project", project.ID)
		if err != nil {
			t.Fatal(err)
		}
		old := svcs
		svcs = &Services{InteractiveSvc: svc}
		t.Cleanup(func() { svcs = old })
		input := interactiveMCPStageInput()
		result, err := stageProjectCreatorDraftHandler(withMCPInteractiveIdentity(context.Background(), "user-1", session.ID), interactiveMCPRequest(t, map[string]any{
			"project": input.Project, "profile": input.Profile, "progress": input.Progress, "ready": true,
			"missing_fields": []string{}, "inferred_fields": []string{},
		}))
		if err != nil {
			t.Fatal(err)
		}
		if result == nil || !result.IsError || strings.Contains(toolResultText(result), "session_id") {
			t.Fatalf("legacy project-creator stage = %#v, want fail-closed interaction-service error", result)
		}
	})
}

func TestStageAssistantDraftFillsInteractionCapabilityMetadata(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AssistantInteraction{}, &model.AssistantInteractionRuntime{}, &model.AssistantInteractionDraft{}, &model.AssistantInteractionEvent{}, &model.AssistantInteractionAttempt{}); err != nil {
		t.Fatal(err)
	}
	svc := service.NewAssistantInteractionService(repository.New(db), nil, nil)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", service.AssistantInteractionInput{ModeID: service.AssistantModeCreation}, "create-1")
	if err != nil {
		t.Fatal(err)
	}
	old := svcs
	svcs = &Services{InteractionSvc: svc}
	t.Cleanup(func() { svcs = old })
	request := interactiveMCPRequest(t, map[string]any{
		"payload": map[string]any{"plan": map[string]any{"goal": "write"}},
		"ready":   true,
	})
	ctx := withMCPInteractionCapabilityMode(context.Background(), "user-1", snapshot.ID, snapshot.Generation, service.AssistantModeCreation)
	first, err := stageAssistantDraftHandler(ctx, request)
	if err != nil || first == nil || first.IsError {
		t.Fatalf("implicit metadata stage = %#v, %v", first, err)
	}
	var firstResult service.InteractionStageResult
	if err := json.Unmarshal([]byte(toolResultText(first)), &firstResult); err != nil {
		t.Fatal(err)
	}
	replay, err := stageAssistantDraftHandler(ctx, request)
	if err != nil || replay == nil || replay.IsError {
		t.Fatalf("implicit metadata replay = %#v, %v", replay, err)
	}
	var replayResult service.InteractionStageResult
	if err := json.Unmarshal([]byte(toolResultText(replay)), &replayResult); err != nil {
		t.Fatal(err)
	}
	if firstResult.DraftID == "" || replayResult.DraftID != firstResult.DraftID || replayResult.Revision != firstResult.Revision {
		t.Fatalf("implicit metadata replay = %#v then %#v, want same durable receipt", firstResult, replayResult)
	}
	if err := db.Model(&model.AssistantInteraction{}).Where("id = ?", snapshot.ID).Update("generation", snapshot.Generation+1).Error; err != nil {
		t.Fatal(err)
	}
	nextGenerationCtx := withMCPInteractionCapabilityMode(context.Background(), "user-1", snapshot.ID, snapshot.Generation+1, service.AssistantModeCreation)
	nextGeneration, err := stageAssistantDraftHandler(nextGenerationCtx, request)
	if err != nil || nextGeneration == nil || nextGeneration.IsError {
		t.Fatalf("implicit metadata stage after runtime replacement = %#v, %v", nextGeneration, err)
	}
	var nextGenerationResult service.InteractionStageResult
	if err := json.Unmarshal([]byte(toolResultText(nextGeneration)), &nextGenerationResult); err != nil {
		t.Fatal(err)
	}
	if nextGenerationResult.DraftID == firstResult.DraftID || nextGenerationResult.Revision <= firstResult.Revision {
		t.Fatalf("new generation stage = %#v after %#v, want a newer durable receipt", nextGenerationResult, firstResult)
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
		for _, phrase := range []string{"topic", "expression_form", "profile_context", "temporary_overrides", "using []"} {
			if !strings.Contains(tool.Description, phrase) {
				t.Errorf("assistant draft description %q missing %q", tool.Description, phrase)
			}
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
		for _, field := range []string{"goal", "content_type", "topic", "angle", "expression_form", "expected_result", "requirements", "constraints", "assets", "missing_inputs", "agent_decisions", "target_audience", "profile_context", "temporary_overrides", "outline", "style_constraints", "asset_plan", "acceptance_criteria", "cost_estimate", "execution_options"} {
			if _, ok := planProperties[field]; !ok {
				t.Fatalf("assistant plan schema missing %q: %#v", field, planSchema)
			}
		}
		for _, field := range []string{"profile_context", "temporary_overrides"} {
			fieldSchema, ok := planProperties[field].(map[string]any)
			if !ok || fieldSchema["type"] != "array" {
				t.Fatalf("assistant plan schema %s = %#v, want string array", field, planProperties[field])
			}
			items, ok := fieldSchema["items"].(map[string]any)
			if !ok || items["type"] != "string" {
				t.Fatalf("assistant plan schema %s items = %#v, want string", field, fieldSchema["items"])
			}
		}
		for _, field := range []string{"account", "platform", "column", "visual_plan", "success_criteria"} {
			if _, ok := planProperties[field]; ok {
				t.Fatalf("assistant plan schema still exposes unsupported %q", field)
			}
		}
		return
	}
	t.Fatal("stage_assistant_draft tool not registered")
}

func TestStageAssistantDraftUsesProtectedInteractionMode(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AssistantInteraction{}, &model.AssistantInteractionRuntime{}, &model.AssistantInteractionDraft{}, &model.AssistantInteractionEvent{}, &model.AssistantInteractionAttempt{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	svc := service.NewAssistantInteractionService(repo, nil, nil)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", service.AssistantInteractionInput{ModeID: service.AssistantModeCreation}, "protected-mode")
	if err != nil {
		t.Fatal(err)
	}
	old := svcs
	svcs = &Services{InteractionSvc: svc}
	t.Cleanup(func() { svcs = old })
	payload := map[string]any{"plan": map[string]any{
		"goal": "发布新品", "content_type": "公众号文章", "topic": "新品的真实使用场景", "target_audience": "潜在客户", "angle": "真实使用问题", "expression_form": "案例型", "profile_context": []string{}, "temporary_overrides": []string{},
		"outline": []string{"问题", "方案"}, "style_constraints": "具体", "asset_plan": "产品图",
		"acceptance_criteria": "事实准确", "cost_estimate": "低", "execution_options": []string{"生成完整初稿"},
	}}
	ctx := withMCPInteractionCapabilityMode(context.Background(), "user-1", snapshot.ID, snapshot.Generation, service.AssistantModeCreation)
	spoofed, err := stageAssistantDraftHandler(ctx, interactiveMCPRequest(t, map[string]any{
		"mode_id": service.AssistantModeProfile, "payload": payload, "ready": true,
	}))
	if err != nil || spoofed == nil || !spoofed.IsError || !strings.Contains(toolResultText(spoofed), service.InteractionErrorGenerationChanged) {
		t.Fatalf("spoofed mode staging = %#v, %v, want capability mode rejection", spoofed, err)
	}
	if drafts, err := repo.AssistantInteractions().ListDrafts(context.Background(), "user-1", snapshot.ID); err != nil || len(drafts) != 0 {
		t.Fatalf("spoofed mode drafts = %#v, %v", drafts, err)
	}
	result, err := stageAssistantDraftHandler(ctx, interactiveMCPRequest(t, map[string]any{"payload": payload, "ready": true}))
	if err != nil || result == nil || result.IsError || !strings.Contains(toolResultText(result), `"durable":true`) {
		t.Fatalf("interaction-scoped stage = %#v, %v, want durable receipt", result, err)
	}
	drafts, err := repo.AssistantInteractions().ListDrafts(context.Background(), "user-1", snapshot.ID)
	if err != nil || len(drafts) != 1 || drafts[0].ModeID != service.AssistantModeCreation || !drafts[0].Ready {
		t.Fatalf("interaction-scoped drafts = %#v, %v", drafts, err)
	}
}

func TestStageProjectCreatorDraftPersistsInteractionDraft(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AssistantInteraction{}, &model.AssistantInteractionRuntime{}, &model.AssistantInteractionDraft{}, &model.AssistantInteractionEvent{}, &model.AssistantInteractionAttempt{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	svc := service.NewAssistantInteractionService(repo, nil, nil)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", service.AssistantInteractionInput{ModeID: service.AssistantModeProfile}, "profile-stage")
	if err != nil {
		t.Fatal(err)
	}
	old := svcs
	svcs = &Services{InteractionSvc: svc}
	t.Cleanup(func() { svcs = old })

	in := interactiveMCPStageInput()
	in.Profile.Dimensions.Identity.Content = nil
	ctx := withMCPInteractionCapabilityMode(context.Background(), "user-1", snapshot.ID, snapshot.Generation, service.AssistantModeProfile)
	result, err := stageProjectCreatorDraftHandler(ctx, interactiveMCPRequest(t, map[string]any{
		"project": in.Project, "profile": in.Profile, "progress": in.Progress, "ready": false,
		"missing_fields": []string{"identity.content"}, "inferred_fields": []string{},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.IsError || !strings.Contains(toolResultText(result), `"durable":true`) {
		t.Fatalf("project creator interaction stage = %#v, want durable receipt", result)
	}
	drafts, err := repo.AssistantInteractions().ListDrafts(context.Background(), "user-1", snapshot.ID)
	if err != nil || len(drafts) != 1 || drafts[0].ModeID != service.AssistantModeProfile || drafts[0].Ready {
		t.Fatalf("profile interaction drafts = %#v, %v", drafts, err)
	}
	var persisted map[string]any
	if err := json.Unmarshal(drafts[0].Payload, &persisted); err != nil {
		t.Fatal(err)
	}
	if _, ok := persisted["missing_fields"]; !ok {
		t.Fatalf("durable profile draft omitted tool metadata: %#v", persisted)
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

func TestAssistantInteractionRuntimeCapabilityCanStageDraftOverMCP(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AssistantInteraction{}, &model.AssistantInteractionRuntime{}, &model.AssistantInteractionDraft{}, &model.AssistantInteractionEvent{}, &model.AssistantInteractionAttempt{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	factory := &interactiveMCPFactory{tokens: make(chan string, 1)}
	svc := service.NewAssistantInteractionService(repo, factory, nil)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", service.AssistantInteractionInput{ModeID: service.AssistantModeCreation}, "mcp-stage")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EnsureRuntime(context.Background(), "user-1", snapshot.ID, snapshot.Generation); err != nil {
		t.Fatal(err)
	}
	old := svcs
	svcs = &Services{InteractionSvc: svc}
	t.Cleanup(func() { svcs = old })
	var runtimeToken string
	select {
	case runtimeToken = <-factory.tokens:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runtime token")
	}
	handler := NewMCPHandler(nil, "", nil, WithInteractionAuthentication(svc))
	sessionID := initializeMCPExecutionSession(t, handler, runtimeToken)
	args, err := json.Marshal(map[string]any{"payload": map[string]any{"plan": map[string]any{"goal": "目标"}}, "ready": false})
	if err != nil {
		t.Fatal(err)
	}
	rec := callMCPToolForScopeTest(handler, runtimeToken, sessionID, "stage_assistant_draft", string(args))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `\"durable\":true`) {
		t.Fatalf("interaction assistant draft status/body = %d/%s", rec.Code, rec.Body.String())
	}
	drafts, err := repo.AssistantInteractions().ListDrafts(context.Background(), "user-1", snapshot.ID)
	if err != nil || len(drafts) != 1 || drafts[0].Ready || !strings.Contains(string(drafts[0].Payload), `"goal":"目标"`) {
		t.Fatalf("durable interaction drafts = %#v, %v", drafts, err)
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
