package service

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
)

type fakeAIEntryLLM struct {
	responses []string
	calls     []struct {
		system string
		user   string
	}
}

func (f *fakeAIEntryLLM) Complete(_ context.Context, systemPrompt, userPrompt string) (string, error) {
	f.calls = append(f.calls, struct {
		system string
		user   string
	}{system: systemPrompt, user: userPrompt})
	if len(f.responses) == 0 {
		return `{}`, nil
	}
	out := f.responses[0]
	f.responses = f.responses[1:]
	return out, nil
}

func (f *fakeAIEntryLLM) CompleteWithImage(context.Context, string, string, string) (string, error) {
	return "", nil
}

func TestAIEntryServiceSubmitCreatesArticleTaskWithAttachments(t *testing.T) {
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	llm := &fakeAIEntryLLM{responses: []string{
		`{"prompt":"写一篇新品发布公众号文章","notes":"优先参考第一张图"}`,
	}}
	logger := zerolog.New(io.Discard)
	entrySvc := NewAIEntryService(repo, taskSvc, llm, &logger)

	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{
		UserID:    userID,
		ProjectID: projectID,
		Channel:   "studio",
		Text:      "帮我写一篇新品发布公众号文章",
		Attachments: []model.EntryAttachment{{
			Type:        "image",
			URL:         "https://cdn.example.com/ref.png",
			FileName:    "ref.png",
			ContentType: "image/png",
			Size:        123,
		}},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Status != AIEntryStatusCreated || result.Task == nil {
		t.Fatalf("result = %#v, want created task", result)
	}
	found, err := repo.Tasks().FindByID(ctx, result.Task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.Prompt != "写一篇新品发布公众号文章" {
		t.Fatalf("task prompt = %q", found.Prompt)
	}
	if found.ReferenceImageURL != "https://cdn.example.com/ref.png" {
		t.Fatalf("reference_image_url = %q", found.ReferenceImageURL)
	}
	attachments := found.InputAttachments.Data()
	if len(attachments) != 1 || attachments[0].FileName != "ref.png" {
		t.Fatalf("input attachments = %#v", attachments)
	}
	if len(llm.calls) != 1 || !strings.Contains(llm.calls[0].user, "帮我写一篇新品发布公众号文章") {
		t.Fatalf("llm calls = %#v", llm.calls)
	}
}

func TestAIEntryServiceSubmitDropsUnsafeLLMImageFields(t *testing.T) {
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	llm := &fakeAIEntryLLM{responses: []string{
		`{"prompt":"写一篇新品发布文章","image_ratio":"2:1","image_model_key":"custom"}`,
	}}
	logger := zerolog.New(io.Discard)
	entrySvc := NewAIEntryService(repo, taskSvc, llm, &logger)

	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{
		UserID:    userID,
		ProjectID: projectID,
		Channel:   "studio",
		Text:      "写一篇新品发布文章",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Status != AIEntryStatusCreated || result.Task == nil {
		t.Fatalf("result = %#v, want created task", result)
	}
	found, err := repo.Tasks().FindByID(ctx, result.Task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.ImageRatio != "" {
		t.Fatalf("image_ratio = %q, want invalid LLM ratio dropped", found.ImageRatio)
	}
	if found.ImageModelKey != "" {
		t.Fatalf("image_model_key = %q, want LLM model key ignored", found.ImageModelKey)
	}
}

func TestAIEntryServiceSubmitNeedsConfigurationForEcommerceWithoutProductImage(t *testing.T) {
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformEcommerce)
	llm := &fakeAIEntryLLM{responses: []string{
		`{"prompt":"做一套保温杯电商主图","selling_points":"316 不锈钢，长效保温"}`,
	}}
	logger := zerolog.New(io.Discard)
	entrySvc := NewAIEntryService(repo, taskSvc, llm, &logger)

	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{
		UserID:    userID,
		ProjectID: projectID,
		Channel:   "studio",
		Text:      "做一套保温杯电商主图",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Status != AIEntryStatusNeedsConfiguration {
		t.Fatalf("status = %q, want needs_configuration", result.Status)
	}
	wantActionURL := "/tasks?create=true&type=ecommerce&project_id=" + projectID + "&intent=new"
	if !strings.Contains(result.Message, "产品图") || result.ActionURL != wantActionURL {
		t.Fatalf("result = %#v, want product-photo hint and action URL", result)
	}
	tasks, total, err := taskSvc.List(ctx, userID, 0, 10, "", "", "")
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if total != 0 || len(tasks) != 0 {
		t.Fatalf("created tasks = %d/%d, want none", len(tasks), total)
	}
}

func TestAIEntryServiceSubmitNormalizesEcommerceModules(t *testing.T) {
	t.Run("falls back to minimum legal default when parsed modules are invalid", func(t *testing.T) {
		taskSvc, repo := setupTaskServiceWithEnqueuer(t)
		ctx := context.Background()
		userID := uuid.NewString()
		projectID := createTestProject(t, repo, userID, model.PlatformEcommerce)
		llm := &fakeAIEntryLLM{responses: []string{
			`{"prompt":"做一套保温杯主图","selected_modules":{"bogus":9,"main_images":0}}`,
		}}
		logger := zerolog.New(io.Discard)
		entrySvc := NewAIEntryService(repo, taskSvc, llm, &logger)

		result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{
			UserID:    userID,
			ProjectID: projectID,
			Channel:   "studio",
			Text:      "做一套保温杯主图",
			Attachments: []model.EntryAttachment{{
				Type:        "image",
				URL:         "https://cdn.example.com/product.png",
				FileName:    "product.png",
				ContentType: "image/png",
				Size:        123,
			}},
		})
		if err != nil {
			t.Fatalf("Submit: %v", err)
		}
		if result.Status != AIEntryStatusCreated || result.Task == nil {
			t.Fatalf("result = %#v, want created task", result)
		}
		found, err := repo.Tasks().FindByID(ctx, result.Task.ID)
		if err != nil {
			t.Fatalf("find task: %v", err)
		}
		modules := found.Ecommerce.Data().SelectedModules
		if len(modules) != 1 || modules["main_images"] != 1 {
			t.Fatalf("selected modules = %#v, want minimum main_images default", modules)
		}
	})

	t.Run("caps parsed module quantities to Studio-supported limits", func(t *testing.T) {
		taskSvc, repo := setupTaskServiceWithEnqueuer(t)
		ctx := context.Background()
		userID := uuid.NewString()
		projectID := createTestProject(t, repo, userID, model.PlatformEcommerce)
		llm := &fakeAIEntryLLM{responses: []string{
			`{"prompt":"做一套保温杯商详","selected_modules":{"detail_page":99,"share_image":2}}`,
		}}
		logger := zerolog.New(io.Discard)
		entrySvc := NewAIEntryService(repo, taskSvc, llm, &logger)

		result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{
			UserID:    userID,
			ProjectID: projectID,
			Channel:   "studio",
			Text:      "做一套保温杯商详",
			Attachments: []model.EntryAttachment{{
				Type:        "image",
				URL:         "https://cdn.example.com/product.png",
				FileName:    "product.png",
				ContentType: "image/png",
				Size:        123,
			}},
		})
		if err != nil {
			t.Fatalf("Submit: %v", err)
		}
		if result.Status != AIEntryStatusCreated || result.Task == nil {
			t.Fatalf("result = %#v, want created task", result)
		}
		found, err := repo.Tasks().FindByID(ctx, result.Task.ID)
		if err != nil {
			t.Fatalf("find task: %v", err)
		}
		modules := found.Ecommerce.Data().SelectedModules
		if len(modules) != 2 || modules["detail_page"] != 20 || modules["share_image"] != 2 {
			t.Fatalf("selected modules = %#v, want capped detail_page and preserved share_image", modules)
		}
	})
}

func TestAIEntryServiceSubmitStoresVideoInputReferences(t *testing.T) {
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformVideoCreator)
	llm := &fakeAIEntryLLM{responses: []string{
		`{"prompt":"生成一条咖啡杯种草短视频","video":{"ratio":"9:16","duration":12}}`,
	}}
	logger := zerolog.New(io.Discard)
	entrySvc := NewAIEntryService(repo, taskSvc, llm, &logger)

	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{
		UserID:    userID,
		ProjectID: projectID,
		Channel:   "studio",
		Text:      "生成一条咖啡杯种草短视频，12 秒竖版",
		Attachments: []model.EntryAttachment{{
			Type:        "video",
			URL:         "https://cdn.example.com/demo.mp4",
			FileName:    "demo.mp4",
			ContentType: "video/mp4",
			Size:        1024,
		}},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Status != AIEntryStatusCreated || result.Task == nil {
		t.Fatalf("result = %#v, want created video task", result)
	}
	found, err := repo.Tasks().FindByID(ctx, result.Task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	input := found.VideoInput.Data()
	if input.Brief != "生成一条咖啡杯种草短视频" {
		t.Fatalf("video input brief = %q", input.Brief)
	}
	if input.HardConstraints.Ratio != "9:16" || input.HardConstraints.Duration != 12 {
		t.Fatalf("video hard constraints = %#v", input.HardConstraints)
	}
	if len(input.References) != 1 || input.References[0].Type != "video_url" || input.References[0].URL != "https://cdn.example.com/demo.mp4" {
		t.Fatalf("video references = %#v", input.References)
	}
}

func TestAIEntryServiceSubmitVideoEditorRequiresVideoAttachment(t *testing.T) {
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformVideoEditor)
	llm := &fakeAIEntryLLM{responses: []string{
		`{"prompt":"加字幕并剪成 30 秒短视频"}`,
	}}
	logger := zerolog.New(io.Discard)
	entrySvc := NewAIEntryService(repo, taskSvc, llm, &logger)

	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{
		UserID:    userID,
		ProjectID: projectID,
		Channel:   "studio",
		Text:      "把素材剪成 30 秒短视频",
		Attachments: []model.EntryAttachment{{
			Type:        "image",
			URL:         "https://cdn.example.com/frame.png",
			FileName:    "frame.png",
			ContentType: "image/png",
			Size:        1024,
		}},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Status != AIEntryStatusNeedsConfiguration || result.Task != nil {
		t.Fatalf("result = %#v, want needs configuration without task", result)
	}
	if !strings.Contains(result.Message, "视频素材") {
		t.Fatalf("message = %q, want source video hint", result.Message)
	}
}

func TestAIEntryServiceSubmitDropsInvalidVideoHardConstraints(t *testing.T) {
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformVideoCreator)
	llm := &fakeAIEntryLLM{responses: []string{
		`{"prompt":"生成一条咖啡杯短视频","video":{"ratio":"2:1","duration":9999}}`,
	}}
	logger := zerolog.New(io.Discard)
	entrySvc := NewAIEntryService(repo, taskSvc, llm, &logger)

	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{
		UserID:    userID,
		ProjectID: projectID,
		Channel:   "studio",
		Text:      "生成一条咖啡杯短视频",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Status != AIEntryStatusCreated || result.Task == nil {
		t.Fatalf("result = %#v, want created video task", result)
	}
	found, err := repo.Tasks().FindByID(ctx, result.Task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	input := found.VideoInput.Data()
	if input.HardConstraints.Ratio != "" || input.HardConstraints.Duration != 0 {
		t.Fatalf("video hard constraints = %#v, want invalid fields dropped", input.HardConstraints)
	}
}

func TestAIEntryServiceSubmitRetriesJSONRepairOnce(t *testing.T) {
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	llm := &fakeAIEntryLLM{responses: []string{
		`不是 JSON`,
		`{"prompt":"写一篇可露营咖啡壶种草笔记"}`,
	}}
	logger := zerolog.New(io.Discard)
	entrySvc := NewAIEntryService(repo, taskSvc, llm, &logger)

	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{
		UserID:    userID,
		ProjectID: projectID,
		Channel:   "studio",
		Text:      "写一篇可露营咖啡壶种草笔记",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Status != AIEntryStatusCreated {
		t.Fatalf("status = %q, want created", result.Status)
	}
	if len(llm.calls) != 2 {
		t.Fatalf("llm call count = %d, want repair retry", len(llm.calls))
	}
	if !strings.Contains(llm.calls[1].user, "只返回合法 JSON") {
		t.Fatalf("repair prompt = %q", llm.calls[1].user)
	}
}

func TestAIEntryServiceSubmitRequiresLLM(t *testing.T) {
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	logger := zerolog.New(io.Discard)
	entrySvc := NewAIEntryService(repo, taskSvc, nil, &logger)

	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{
		UserID:    userID,
		ProjectID: projectID,
		Channel:   "studio",
		Text:      "写一篇文章",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Status != AIEntryStatusNeedsConfiguration || !strings.Contains(result.Message, "模型") || result.ActionURL == "" {
		t.Fatalf("result = %#v, want model configuration action", result)
	}
}
