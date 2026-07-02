package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	aliclient "github.com/alibabacloud-go/tingwu-20230930/v2/client"
	credential "github.com/aliyun/credentials-go/credentials"
	"golang.org/x/sync/errgroup"

	"github.com/anbanai/anban-creator/server/config"
)

// AlibabaTingWuClient calls Alibaba TingWu directly without an anban.ai service hop.
type AlibabaTingWuClient struct {
	client *aliclient.Client
	appKey string
}

// NewAlibabaTingWuClient returns nil when TingWu is not configured.
func NewAlibabaTingWuClient(cfg config.TingWuConfig) (*AlibabaTingWuClient, error) {
	if cfg.Empty() {
		return nil, nil
	}
	if !cfg.Complete() {
		return nil, fmt.Errorf("incomplete TingWu config: endpoint, region, app_key, access_key, and access_secret are required")
	}
	credCfg := credential.Config{
		Type:            ptr("access_key"),
		AccessKeyId:     ptr(cfg.AccessKey),
		AccessKeySecret: ptr(cfg.AccessSecret),
	}
	cred, err := credential.NewCredential(&credCfg)
	if err != nil {
		return nil, fmt.Errorf("create TingWu credential: %w", err)
	}
	options := openapi.Config{
		Type:       ptr("access_key"),
		RegionId:   ptr(cfg.Region),
		Credential: cred,
		Endpoint:   ptr(cfg.Endpoint),
	}
	client, err := aliclient.NewClient(&options)
	if err != nil {
		return nil, fmt.Errorf("create TingWu client: %w", err)
	}
	return &AlibabaTingWuClient{client: client, appKey: cfg.AppKey}, nil
}

// CreateTask creates an offline TingWu task for live-slice analysis.
func (c *AlibabaTingWuClient) CreateTask(_ context.Context, req LiveAnalysisTaskRequest) (string, error) {
	if c == nil || c.client == nil {
		return "", fmt.Errorf("TingWu client is not configured")
	}
	creator := new(aliclient.CreateTaskRequest).
		SetAppKey(c.appKey).
		SetType("offline")

	input := new(aliclient.CreateTaskRequestInput).
		SetFileUrl(req.AudioURL).
		SetSourceLanguage("cn").
		SetTaskKey(taskKey())
	creator.SetInput(input)

	params := new(aliclient.CreateTaskRequestParameters)
	if req.AutoChaptersEnabled {
		params.SetAutoChaptersEnabled(true)
	}
	if req.SummarizationEnabled {
		params.SetSummarizationEnabled(true)
		params.SetSummarization(new(aliclient.CreateTaskRequestParametersSummarization).SetTypes([]*string{
			ptr("Paragraph"),
			ptr("Conversational"),
			ptr("QuestionsAnswering"),
			ptr("MindMap"),
		}))
	}
	if req.MeetingAssistanceEnabled {
		params.SetMeetingAssistanceEnabled(true)
		params.SetMeetingAssistance(new(aliclient.CreateTaskRequestParametersMeetingAssistance).SetTypes([]*string{
			ptr("Actions"),
			ptr("KeyInformation"),
		}))
	}

	transcription := new(aliclient.CreateTaskRequestParametersTranscription)
	if req.DiarizationEnabled {
		diarization := new(aliclient.CreateTaskRequestParametersTranscriptionDiarization).
			SetSpeakerCount(req.DiarizationSpeakerCount)
		transcription.SetDiarizationEnabled(true).SetDiarization(diarization)
	}
	params.SetTranscription(transcription)

	if req.ScriptTemplateEnable {
		params.SetCustomPromptEnabled(true)
		params.SetCustomPrompt(new(aliclient.CreateTaskRequestParametersCustomPrompt).
			SetContents([]*aliclient.CreateTaskRequestParametersCustomPromptContents{
				new(aliclient.CreateTaskRequestParametersCustomPromptContents).
					SetTransType("default").
					SetModel("tingwu-turbo").
					SetName(LiveScriptPromptName).
					SetPrompt(liveScriptTemplatePrompt),
			}))
	}

	creator.SetParameters(params)
	result, err := c.client.CreateTask(creator)
	if err != nil {
		return "", fmt.Errorf("TingWu create task: %w", err)
	}
	if result == nil || result.Body == nil || result.Body.Code == nil {
		return "", fmt.Errorf("TingWu create task returned empty response")
	}
	if *result.Body.Code != "0" {
		msg := ""
		if result.Body.Message != nil {
			msg = *result.Body.Message
		}
		return "", fmt.Errorf("TingWu create task failed: %s", msg)
	}
	if result.Body.Data == nil || result.Body.Data.TaskId == nil {
		return "", fmt.Errorf("TingWu create task returned no task id")
	}
	return *result.Body.Data.TaskId, nil
}

// QueryTask fetches TingWu task status and downloads result artifacts when complete.
func (c *AlibabaTingWuClient) QueryTask(ctx context.Context, taskID string) (*TingWuTaskInfo, bool, error) {
	if c == nil || c.client == nil {
		return nil, false, fmt.Errorf("TingWu client is not configured")
	}
	result, err := c.client.GetTaskInfo(&taskID)
	if err != nil {
		return nil, false, fmt.Errorf("TingWu get task info: %w", err)
	}
	if result == nil || result.Body == nil || result.Body.Code == nil {
		return nil, false, fmt.Errorf("TingWu get task info returned empty response")
	}
	if *result.Body.Code != "0" {
		msg := ""
		if result.Body.Message != nil {
			msg = *result.Body.Message
		}
		return nil, false, fmt.Errorf("TingWu get task info failed: %s", msg)
	}
	info := &TingWuTaskInfo{}
	if result.Body.Data == nil || result.Body.Data.TaskStatus == nil {
		return info, false, nil
	}
	info.Status = *result.Body.Data.TaskStatus
	if info.Status != "COMPLETED" || result.Body.Data.Result == nil {
		return info, false, nil
	}

	eg, egCtx := errgroup.WithContext(ctx)
	if result.Body.Data.Result.Transcription != nil {
		info.Transcription = new(TingWuTranscriptionResult)
		url := *result.Body.Data.Result.Transcription
		eg.Go(func() error { return downloadAndDecodeTingWu(egCtx, url, info.Transcription) })
	}
	if result.Body.Data.Result.Summarization != nil {
		info.Summarization = new(TingWuSummarizationResult)
		url := *result.Body.Data.Result.Summarization
		eg.Go(func() error { return downloadAndDecodeTingWu(egCtx, url, info.Summarization) })
	}
	if result.Body.Data.Result.AutoChapters != nil {
		info.AutoChapters = new(TingWuAutoChaptersResult)
		url := *result.Body.Data.Result.AutoChapters
		eg.Go(func() error { return downloadAndDecodeTingWu(egCtx, url, info.AutoChapters) })
	}
	if result.Body.Data.Result.MeetingAssistance != nil {
		info.MeetingAssistance = new(TingWuMeetingAssistanceResult)
		url := *result.Body.Data.Result.MeetingAssistance
		eg.Go(func() error { return downloadAndDecodeTingWu(egCtx, url, info.MeetingAssistance) })
	}
	if result.Body.Data.Result.CustomPrompt != nil {
		info.CustomPrompt = new(TingWuCustomPromptResult)
		url := *result.Body.Data.Result.CustomPrompt
		eg.Go(func() error { return downloadAndDecodeTingWu(egCtx, url, info.CustomPrompt) })
	}
	if err := eg.Wait(); err != nil {
		return nil, false, err
	}
	return info, true, nil
}

func downloadAndDecodeTingWu(ctx context.Context, url string, val any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create TingWu result request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download TingWu result: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download TingWu result: unexpected status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read TingWu result: %w", err)
	}
	if err := json.Unmarshal(raw, val); err != nil {
		return fmt.Errorf("decode TingWu result: %w", err)
	}
	return nil
}

func ptr[T any](v T) *T {
	return &v
}

var liveScriptTemplatePrompt = strings.TrimSpace(`
你是直播话术分析师。请按时间线分析这场直播中可复用的话术结构，帮助用户复盘同行直播节奏。

重点识别:
- 开场铺垫、互动承接、产品介绍、卖点证明、对比说明、价值塑造、疑虑处理、福利/行动引导。
- 每个阶段的目标、关键话术、情绪节奏、适合复用的表达。
- 明确指出只适合直播场景、不建议剪入短视频的片段类型。

输出中文结构化文本，按时间段组织，保持简洁可执行。
`)
