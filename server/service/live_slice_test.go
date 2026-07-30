package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/storage"
)

type fakeLiveSliceTingWu struct {
	createReq LiveAnalysisTaskRequest
	task      *TingWuTaskInfo
	completed bool
}

type fakeLiveStorage struct {
	name         string
	customDomain bool
	signErr      error
}

func newFakeLiveStorage(name string) (storage.Provider, error) {
	return &fakeLiveStorage{name: name}, nil
}

func (s *fakeLiveStorage) Name() string { return s.name }

func (s *fakeLiveStorage) Upload(_ context.Context, key string, reader io.Reader, contentType string) (*storage.UploadResult, error) {
	n, _ := io.Copy(io.Discard, reader)
	return &storage.UploadResult{URL: "https://cdn.example.com/" + key, Key: key, Size: n, MimeType: contentType}, nil
}

func (s *fakeLiveStorage) UploadFile(ctx context.Context, key string, filePath string, contentType string) (*storage.UploadResult, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return s.Upload(ctx, key, f, contentType)
}

func (s *fakeLiveStorage) UploadURL(_ context.Context, key string, _ string, _ int) (string, error) {
	return "https://signed-upload.example.com/" + key, nil
}

func (s *fakeLiveStorage) GetURL(key string) string { return "https://cdn.example.com/" + key }

func (s *fakeLiveStorage) Read(context.Context, string) ([]byte, error) {
	return nil, errors.New("not implemented")
}

func (s *fakeLiveStorage) Delete(context.Context, string) error { return nil }

func (s *fakeLiveStorage) DownloadURL(_ context.Context, key string, _ int) (string, error) {
	if s.signErr != nil {
		return "", s.signErr
	}
	return "https://signed.example.com/" + key, nil
}

func (s *fakeLiveStorage) HasCustomDomain() bool { return s.customDomain }

func (s *fakeLiveStorage) IsOwnedURL(rawURL string) bool {
	return strings.HasPrefix(rawURL, "https://cdn.example.com/") || strings.HasPrefix(rawURL, "https://signed.example.com/")
}

func (f *fakeLiveSliceTingWu) CreateTask(_ context.Context, req LiveAnalysisTaskRequest) (string, error) {
	f.createReq = req
	return "tw-task-1", nil
}

func (f *fakeLiveSliceTingWu) QueryTask(context.Context, string) (*TingWuTaskInfo, bool, error) {
	return f.task, f.completed, nil
}

func TestCreateLiveAnalysisTaskSignsOwnedAudioURL(t *testing.T) {
	tw := &fakeLiveSliceTingWu{}
	store := &fakeLiveStorage{name: "oss"}
	svc := NewLiveSliceServiceWithClients(tw, store, nil)

	_, err := svc.CreateLiveAnalysisTask(context.Background(), LiveAnalysisTaskRequest{
		AudioURL: "https://cdn.example.com/uploads/live-audio/take.wav",
	})
	if err != nil {
		t.Fatalf("CreateLiveAnalysisTask: %v", err)
	}
	if got := tw.createReq.AudioURL; got != "https://signed.example.com/uploads/live-audio/take.wav" {
		t.Fatalf("AudioURL = %q", got)
	}
}

func TestCreateLiveAnalysisTaskSignsAudioKey(t *testing.T) {
	tw := &fakeLiveSliceTingWu{}
	store := &fakeLiveStorage{name: "oss"}
	svc := NewLiveSliceServiceWithClients(tw, store, nil)

	_, err := svc.CreateLiveAnalysisTask(context.Background(), LiveAnalysisTaskRequest{
		AudioKey: "uploads/live-audio/take.mp3",
	})
	if err != nil {
		t.Fatalf("CreateLiveAnalysisTask: %v", err)
	}
	if got := tw.createReq.AudioURL; got != "https://signed.example.com/uploads/live-audio/take.mp3" {
		t.Fatalf("AudioURL = %q", got)
	}
}

func TestCreateLiveAnalysisTaskRejectsNonLiveAudioKey(t *testing.T) {
	tw := &fakeLiveSliceTingWu{}
	store := &fakeLiveStorage{name: "oss"}
	svc := NewLiveSliceServiceWithClients(tw, store, nil)

	_, err := svc.CreateLiveAnalysisTask(context.Background(), LiveAnalysisTaskRequest{
		AudioKey: "uploads/video-audio/take.wav",
	})
	if err == nil || !strings.Contains(err.Error(), "audio_key must be under uploads/live-audio/") {
		t.Fatalf("CreateLiveAnalysisTask error = %v, want live audio prefix rejection", err)
	}
}

func TestCreateLiveAnalysisTaskRejectsAudioKeyWithoutOSS(t *testing.T) {
	tw := &fakeLiveSliceTingWu{}
	store := &fakeLiveStorage{name: "local"}
	svc := NewLiveSliceServiceWithClients(tw, store, nil)

	_, err := svc.CreateLiveAnalysisTask(context.Background(), LiveAnalysisTaskRequest{
		AudioKey: "uploads/live-audio/take.mp3",
	})
	if err == nil || !strings.Contains(err.Error(), "audio_key requires OSS storage") {
		t.Fatalf("CreateLiveAnalysisTask error = %v, want OSS requirement", err)
	}
}

func TestCreateLiveAnalysisTaskKeepsExternalAudioURL(t *testing.T) {
	tw := &fakeLiveSliceTingWu{}
	store := &fakeLiveStorage{name: "oss"}
	svc := NewLiveSliceServiceWithClients(tw, store, nil)

	_, err := svc.CreateLiveAnalysisTask(context.Background(), LiveAnalysisTaskRequest{
		AudioURL: "https://media.example.org/take.wav",
	})
	if err != nil {
		t.Fatalf("CreateLiveAnalysisTask: %v", err)
	}
	if got := tw.createReq.AudioURL; got != "https://media.example.org/take.wav" {
		t.Fatalf("AudioURL = %q", got)
	}
}

func TestCreateLiveAnalysisTaskReportsOwnedAudioURLSignFailure(t *testing.T) {
	tw := &fakeLiveSliceTingWu{}
	store := &fakeLiveStorage{name: "oss", signErr: errors.New("sign failed")}
	svc := NewLiveSliceServiceWithClients(tw, store, nil)

	_, err := svc.CreateLiveAnalysisTask(context.Background(), LiveAnalysisTaskRequest{
		AudioURL: "https://cdn.example.com/uploads/live-audio/take.wav",
	})
	if err == nil || !strings.Contains(err.Error(), "resolve audio URL") || !strings.Contains(err.Error(), "sign failed") {
		t.Fatalf("CreateLiveAnalysisTask error = %v", err)
	}
}

func TestNormalizeLiveAnalysisResultBuildsSentencesChaptersAndStats(t *testing.T) {
	raw := &TingWuTaskInfo{
		Status: "COMPLETED",
		Summarization: &TingWuSummarizationResult{
			TaskID: "tw-task-1",
			Summarization: TingWuSummarization{
				ParagraphTitle:   "一场茶叶直播",
				ParagraphSummary: "主播讲解茶叶产地和冲泡方法。",
				QuestionsAnsweringSummary: []TingWuQuestionAnsweringSummary{
					{
						Question:              "茶来自哪里？",
						Answer:                "武夷山核心产区。",
						SentenceIDsOfQuestion: []int{1},
						SentenceIDsOfAnswer:   []int{2},
					},
				},
				MindMapSummary: []TingWuMindMapTopic{
					{Title: "产地", Topic: []TingWuMindMapTopic{{Title: "武夷山"}}},
				},
			},
		},
		AutoChapters: &TingWuAutoChaptersResult{
			TaskID: "tw-task-1",
			AutoChapters: []TingWuAutoChapter{
				{ID: 10, Start: 0, End: 9000, Headline: "茶叶介绍", Summary: "介绍产地与汤色"},
			},
		},
		MeetingAssistance: &TingWuMeetingAssistanceResult{
			TaskID: "tw-task-1",
			MeetingAssistance: TingWuMeetingAssistance{
				Keywords: []string{"茶叶", "武夷山"},
				KeySentences: []TingWuKeySentence{
					{ID: 1, SentenceID: 2, Start: 3600, End: 8200, Text: "武夷山核心产区。"},
				},
			},
		},
		CustomPrompt: &TingWuCustomPromptResult{
			TaskID: "tw-task-1",
			CustomPrompt: []TingWuCustomPromptItem{
				{Name: LiveScriptPromptName, Result: "开场-产品-福利", Truncated: false},
			},
		},
		Transcription: &TingWuTranscriptionResult{
			TaskID: "tw-task-1",
			Transcription: TingWuTranscription{
				AudioInfo: TingWuAudioInfo{Size: 1234, Duration: 9000, SampleRate: 16000, Language: "cn"},
				Paragraphs: []TingWuParagraph{
					{
						ParagraphID: "p1",
						SpeakerID:   "speaker-1",
						Words: []TingWuWord{
							{ID: 1, SentenceID: 1, Start: 0, End: 900, Text: "大家好"},
							{ID: 2, SentenceID: 1, Start: 900, End: 2200, Text: "今天讲茶"},
							{ID: 3, SentenceID: 2, Start: 3600, End: 5000, Text: "武夷山"},
							{ID: 4, SentenceID: 2, Start: 5000, End: 8200, Text: "核心产区。"},
						},
					},
				},
				AudioSegments: [][]float64{{0, 2200}, {3600, 8200}},
			},
		},
	}

	result := NormalizeLiveAnalysisResult(raw)

	if result.Title != "一场茶叶直播" {
		t.Fatalf("title = %q", result.Title)
	}
	if len(result.Sentences) != 2 {
		t.Fatalf("sentences len = %d", len(result.Sentences))
	}
	if result.Sentences[0].Text != "大家好今天讲茶" {
		t.Fatalf("first sentence text = %q", result.Sentences[0].Text)
	}
	if result.Sentences[1].Start != 3.6 || result.Sentences[1].End != 8.2 {
		t.Fatalf("second sentence range = %.1f..%.1f", result.Sentences[1].Start, result.Sentences[1].End)
	}
	if len(result.Chapters) != 1 || len(result.Chapters[0].Sentences) != 2 {
		t.Fatalf("chapters = %#v", result.Chapters)
	}
	if len(result.Words) == 0 || result.Words[0].Count == 0 {
		t.Fatalf("word stats missing: %#v", result.Words)
	}
	if len(result.Silents) == 0 || result.Silents[0].Seconds == 0 {
		t.Fatalf("silent stats missing: %#v", result.Silents)
	}
	if len(result.QAS) != 1 || result.QAS[0].Question.Start != 0 || result.QAS[0].Answer.Start != 3.6 {
		t.Fatalf("qas = %#v", result.QAS)
	}
	if len(result.Topics) != 1 || result.Topics[0].Topics[0].Title != "武夷山" {
		t.Fatalf("topics = %#v", result.Topics)
	}
	if result.Templates != "开场-产品-福利" {
		t.Fatalf("templates = %q", result.Templates)
	}
}

func TestLiveSliceServiceCreateAndQueryTask(t *testing.T) {
	fakeTW := &fakeLiveSliceTingWu{
		completed: true,
		task: &TingWuTaskInfo{
			Status:        "COMPLETED",
			Transcription: &TingWuTranscriptionResult{},
		},
	}
	svc := NewLiveSliceServiceWithClients(fakeTW, nil, nil)

	created, err := svc.CreateLiveAnalysisTask(context.Background(), LiveAnalysisTaskRequest{
		AudioURL:                 "https://example.com/audio.mp3",
		AutoChaptersEnabled:      true,
		SummarizationEnabled:     true,
		MeetingAssistanceEnabled: true,
		ScriptTemplateEnable:     true,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if created.TaskID != "tw-task-1" {
		t.Fatalf("task id = %q", created.TaskID)
	}
	if !fakeTW.createReq.ScriptTemplateEnable {
		t.Fatal("script template flag was not passed to TingWu client")
	}

	queried, err := svc.QueryLiveAnalysisTask(context.Background(), "tw-task-1")
	if err != nil {
		t.Fatalf("query task: %v", err)
	}
	if queried.Status != "COMPLETED" {
		t.Fatalf("status = %q", queried.Status)
	}
}

func TestBuildLiveClipPlanMapsSegmentsAndQuotesCommands(t *testing.T) {
	svc := NewLiveSliceServiceWithClients(nil, nil, nil)

	plan, err := svc.BuildLiveClipPlan(LiveClipPlanRequest{
		VideoPath: "/tmp/直播 video's.mp4",
		OutputDir: "output/live slice/task",
		Sentences: []LiveSentence{
			{Index: 1, Start: 10, End: 12.5, Text: "欢迎大家"},
			{Index: 2, Start: 12.5, End: 35.25, Text: "这个产品的核心卖点"},
			{Index: 3, Start: 35.25, End: 45.5, Text: "真实使用场景"},
		},
		Invalid: []LiveInvalid{{Index: 1, Reason: "欢迎语"}},
		Segments: []LiveSegment{
			{Title: "产品/卖点: A", Description: "说明", Thoughts: "先讲卖点", Start: 2, End: 3},
		},
		HeadPaddingSeconds: 0.5,
		TailPaddingSeconds: 1.25,
	})
	if err != nil {
		t.Fatalf("build clip plan: %v", err)
	}
	if len(plan.Clips) != 1 {
		t.Fatalf("clips = %#v", plan.Clips)
	}
	clip := plan.Clips[0]
	if clip.SentenceStart != 2 || clip.SentenceEnd != 3 {
		t.Fatalf("sentence range = %d..%d", clip.SentenceStart, clip.SentenceEnd)
	}
	if clip.Start != 12 || clip.End != 46.75 || clip.Duration != 34.75 {
		t.Fatalf("timing = %.3f..%.3f duration %.3f", clip.Start, clip.End, clip.Duration)
	}
	if !strings.Contains(clip.Output, "output/live slice/task/exports/01-产品-卖点-a.mp4") {
		t.Fatalf("output = %q", clip.Output)
	}
	if !strings.Contains(clip.FastCutShell, "'/tmp/直播 video'\"'\"'s.mp4'") {
		t.Fatalf("fast cut shell not safely quoted: %s", clip.FastCutShell)
	}
	if len(clip.FastCutArgs) == 0 || clip.FastCutArgs[0] != "ffmpeg" {
		t.Fatalf("fast cut args = %#v", clip.FastCutArgs)
	}
	if len(clip.Transcript) != 2 || clip.Transcript[0].Index != 2 {
		t.Fatalf("transcript = %#v", clip.Transcript)
	}
}

func TestBuildLiveClipPlanWarnsForShortAndLongDurations(t *testing.T) {
	svc := NewLiveSliceServiceWithClients(nil, nil, nil)

	plan, err := svc.BuildLiveClipPlan(LiveClipPlanRequest{
		VideoPath: "/tmp/live.mp4",
		OutputDir: "output/live-slice/task",
		Sentences: []LiveSentence{
			{Index: 1, Start: 0, End: 2, Text: "太短"},
			{Index: 2, Start: 10, End: 250, Text: "太长"},
		},
		Segments: []LiveSegment{
			{Title: "短片段", Start: 1, End: 1},
			{Title: "长片段", Start: 2, End: 2},
		},
		MinDurationSeconds: 5,
		MaxDurationSeconds: 180,
	})
	if err != nil {
		t.Fatalf("build clip plan: %v", err)
	}
	if len(plan.Clips) != 2 {
		t.Fatalf("clips = %#v", plan.Clips)
	}
	if len(plan.Warnings) != 2 {
		t.Fatalf("warnings = %#v", plan.Warnings)
	}
}

func TestBuildLiveClipPlanWarnsForPartialInvalidAndRejectsAllInvalid(t *testing.T) {
	svc := NewLiveSliceServiceWithClients(nil, nil, nil)

	plan, err := svc.BuildLiveClipPlan(LiveClipPlanRequest{
		VideoPath: "/tmp/live.mp4",
		OutputDir: "output/live-slice/task",
		Sentences: []LiveSentence{
			{Index: 1, Start: 0, End: 6, Text: "有效开头"},
			{Index: 2, Start: 6, End: 10, Text: "感谢直播间用户"},
			{Index: 3, Start: 10, End: 18, Text: "核心内容"},
		},
		Invalid: []LiveInvalid{{Index: 2, Reason: "直播间互动"}},
		Segments: []LiveSegment{
			{Title: "夹杂无效句", Start: 1, End: 3},
			{Title: "全无效", Start: 2, End: 2},
		},
	})
	if err != nil {
		t.Fatalf("build clip plan: %v", err)
	}
	if len(plan.Clips) != 1 {
		t.Fatalf("clips = %#v", plan.Clips)
	}
	if len(plan.Warnings) != 1 || !strings.Contains(plan.Warnings[0].Reason, "invalid sentences: 2") {
		t.Fatalf("warnings = %#v", plan.Warnings)
	}
	if len(plan.Rejected) != 1 || !strings.Contains(plan.Rejected[0].Reason, "only invalid") {
		t.Fatalf("rejected = %#v", plan.Rejected)
	}
}

func TestBuildLiveClipPlanSinglePartCanBuildManifestWithoutPartResults(t *testing.T) {
	svc := NewLiveSliceServiceWithClients(nil, nil, nil)

	plan, err := svc.BuildLiveClipPlan(LiveClipPlanRequest{
		VideoPath: "/tmp/live.mp4",
		OutputDir: "output/live-slice/task",
		Sentences: []LiveSentence{
			{Index: 1, Start: 0, End: 6, Text: "第一句"},
			{Index: 2, Start: 6, End: 12, Text: "第二句"},
		},
		Segments: []LiveSegment{{Title: "连续片段", Start: 1, End: 2}},
	})
	if err != nil {
		t.Fatalf("build clip plan: %v", err)
	}
	if len(plan.Clips) != 1 || len(plan.Clips[0].Parts) != 1 {
		t.Fatalf("plan should expose one source part: %#v", plan.Clips)
	}

	result, err := svc.BuildLiveClipManifest(LiveClipManifestRequest{
		SourceVideo:  "/tmp/live.mp4",
		TingWuTaskID: "tw-task-1",
		Sentences: []LiveSentence{
			{Index: 1, Start: 0, End: 6, Text: "第一句"},
			{Index: 2, Start: 6, End: 12, Text: "第二句"},
		},
		Clips: plan.Clips,
		ClipResults: []LiveClipExecutionResult{
			{Index: 1, Status: "ok", Method: "copy", Output: plan.Clips[0].Output, Size: 1024, ActualDurationSeconds: 12},
		},
	})
	if err != nil {
		t.Fatalf("single-part clip manifest should not require redundant part_results: %v", err)
	}
	if len(result.ClipManifest) != 1 || len(result.ClipManifest[0].Transcript) != 2 {
		t.Fatalf("manifest = %#v", result.ClipManifest)
	}
}

func TestBuildLiveClipPlanRejectsInvalidPlanningInputs(t *testing.T) {
	svc := NewLiveSliceServiceWithClients(nil, nil, nil)
	base := LiveClipPlanRequest{
		VideoPath: "/tmp/live.mp4",
		OutputDir: "output/live-slice/task",
		Sentences: []LiveSentence{
			{Index: 1, Start: 0, End: 6, Text: "第一句"},
			{Index: 2, Start: 6, End: 12, Text: "第二句"},
		},
		Segments: []LiveSegment{{Title: "片段", Start: 1, End: 2}},
	}

	tests := []struct {
		name   string
		mutate func(*LiveClipPlanRequest)
		want   string
	}{
		{
			name: "min greater than max",
			mutate: func(req *LiveClipPlanRequest) {
				req.MinDurationSeconds = 30
				req.MaxDurationSeconds = 10
			},
			want: "min_duration_seconds must be <= max_duration_seconds",
		},
		{
			name: "negative head padding",
			mutate: func(req *LiveClipPlanRequest) {
				req.HeadPaddingSeconds = -0.1
			},
			want: "padding seconds cannot be negative",
		},
		{
			name: "duplicate sentence index",
			mutate: func(req *LiveClipPlanRequest) {
				req.Sentences = append(req.Sentences, LiveSentence{Index: 2, Start: 12, End: 18, Text: "重复"})
			},
			want: "duplicate sentence index 2",
		},
		{
			name: "missing sentence timing",
			mutate: func(req *LiveClipPlanRequest) {
				req.Sentences[1].End = req.Sentences[1].Start
			},
			want: "sentences[1] end must be greater than start",
		},
		{
			name: "reverse segment",
			mutate: func(req *LiveClipPlanRequest) {
				req.Segments = []LiveSegment{{Title: "反向", Start: 2, End: 1}}
			},
			want: "start index 2 must be <= end index 1",
		},
		{
			name: "unknown segment index",
			mutate: func(req *LiveClipPlanRequest) {
				req.Segments = []LiveSegment{{Title: "越界", Start: 1, End: 9}}
			},
			want: "end index 9 does not exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := base
			req.Sentences = append([]LiveSentence(nil), base.Sentences...)
			req.Segments = append([]LiveSegment(nil), base.Segments...)
			tt.mutate(&req)

			_, err := svc.BuildLiveClipPlan(req)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestBuildLiveSubjectClipPlanBuildsSinglePartForContinuousIndexes(t *testing.T) {
	svc := NewLiveSliceServiceWithClients(nil, nil, nil)

	plan, err := svc.BuildLiveSubjectClipPlan(LiveSubjectClipPlanRequest{
		VideoPath: "/tmp/live.mp4",
		OutputDir: "output/live-slice/task",
		Sentences: []LiveSentence{
			{Index: 1, Start: 0, End: 6, Text: "先说痛点"},
			{Index: 2, Start: 6, End: 14, Text: "再讲产品卖点"},
			{Index: 3, Start: 14, End: 22, Text: "最后给案例"},
		},
		Completions: []LiveSubjectCompletion{
			{
				Title:    "产品卖点",
				Thoughts: "痛点后接卖点",
				Sentences: []LiveSentence{
					{Index: 0, Text: "先补一句旁白", Reason: "开场承接"},
					{Index: 1, Reason: "开头"},
					{Index: 2, Reason: "核心卖点"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("build subject clip plan: %v", err)
	}
	if len(plan.Clips) != 1 {
		t.Fatalf("clips = %#v", plan.Clips)
	}
	clip := plan.Clips[0]
	if len(clip.Parts) != 1 {
		t.Fatalf("parts = %#v", clip.Parts)
	}
	if clip.Parts[0].SentenceStart != 1 || clip.Parts[0].SentenceEnd != 2 {
		t.Fatalf("part sentence range = %d..%d", clip.Parts[0].SentenceStart, clip.Parts[0].SentenceEnd)
	}
	if clip.ConcatShell != "" || clip.ConcatListContent != "" {
		t.Fatalf("single-part subject clip should not require concat: %#v", clip)
	}
	if len(clip.ScriptNotes) != 1 || clip.ScriptNotes[0].Text != "先补一句旁白" {
		t.Fatalf("script notes = %#v", clip.ScriptNotes)
	}
	if len(clip.Transcript) != 2 || clip.Transcript[0].Index != 1 || clip.Transcript[1].Index != 2 {
		t.Fatalf("transcript = %#v", clip.Transcript)
	}
}

func TestBuildLiveSubjectClipPlanBuildsMultiPartConcatForReorderedIndexes(t *testing.T) {
	svc := NewLiveSliceServiceWithClients(nil, nil, nil)

	plan, err := svc.BuildLiveSubjectClipPlan(LiveSubjectClipPlanRequest{
		VideoPath: "/tmp/live video.mp4",
		OutputDir: "output/live slice/task",
		Sentences: []LiveSentence{
			{Index: 1, Start: 0, End: 6, Text: "第一句"},
			{Index: 2, Start: 6, End: 12, Text: "第二句"},
			{Index: 5, Start: 40, End: 48, Text: "第五句"},
		},
		Completions: []LiveSubjectCompletion{
			{
				Title: "重排脚本",
				Sentences: []LiveSentence{
					{Index: 5, Reason: "爆点前置"},
					{Index: 1, Reason: "回到背景"},
					{Index: 2, Reason: "补充解释"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("build subject clip plan: %v", err)
	}
	if len(plan.Clips) != 1 {
		t.Fatalf("clips = %#v", plan.Clips)
	}
	clip := plan.Clips[0]
	if len(clip.Parts) != 2 {
		t.Fatalf("parts = %#v", clip.Parts)
	}
	if clip.Parts[0].SentenceStart != 5 || clip.Parts[1].SentenceStart != 1 || clip.Parts[1].SentenceEnd != 2 {
		t.Fatalf("unexpected part ranges: %#v", clip.Parts)
	}
	if clip.ConcatListPath == "" || clip.ConcatListContent == "" || clip.ConcatShell == "" || len(clip.ConcatArgs) == 0 {
		t.Fatalf("missing concat fields: %#v", clip)
	}
	if !strings.Contains(clip.ConcatListContent, clip.Parts[0].Output) || !strings.Contains(clip.ConcatListContent, clip.Parts[1].Output) {
		t.Fatalf("concat list should reference part outputs: %s", clip.ConcatListContent)
	}
	if !strings.Contains(clip.Parts[0].AccurateCutShell, "'/tmp/live video.mp4'") {
		t.Fatalf("part command should quote video path: %s", clip.Parts[0].AccurateCutShell)
	}
	if clip.Method != "concat" {
		t.Fatalf("method = %q", clip.Method)
	}
	if len(clip.Transcript) != 3 || clip.Transcript[0].Index != 5 || clip.Transcript[1].Index != 1 {
		t.Fatalf("transcript should preserve script order: %#v", clip.Transcript)
	}
}

func TestBuildLiveSubjectClipPlanConcatListEscapesFFmpegPathsAndUsesPartDir(t *testing.T) {
	svc := NewLiveSliceServiceWithClients(nil, nil, nil)

	plan, err := svc.BuildLiveSubjectClipPlan(LiveSubjectClipPlanRequest{
		VideoPath: "/tmp/live.mp4",
		OutputDir: "output/live's slice/task",
		Sentences: []LiveSentence{
			{Index: 1, Start: 0, End: 6, Text: "第一句"},
			{Index: 3, Start: 20, End: 30, Text: "第三句"},
		},
		Completions: []LiveSubjectCompletion{
			{Title: "Bob's 方案", Sentences: []LiveSentence{{Index: 3}, {Index: 1}}},
		},
	})
	if err != nil {
		t.Fatalf("build subject clip plan: %v", err)
	}
	if len(plan.Clips) != 1 {
		t.Fatalf("clips = %#v", plan.Clips)
	}
	clip := plan.Clips[0]
	if len(clip.Parts) != 2 {
		t.Fatalf("parts = %#v", clip.Parts)
	}
	wantFirst := "file " + ffmpegConcatFileQuoteForTest(clip.Parts[0].Output)
	wantSecond := "file " + ffmpegConcatFileQuoteForTest(clip.Parts[1].Output)
	for _, want := range []string{wantFirst, wantSecond} {
		if !strings.Contains(clip.ConcatListContent, want) {
			t.Fatalf("concat list missing ffmpeg-escaped path %q:\n%s", want, clip.ConcatListContent)
		}
	}
	if strings.Contains(clip.ConcatListContent, "'\"'\"'") {
		t.Fatalf("concat list should not use shell quote escaping: %s", clip.ConcatListContent)
	}
	if filepath.Dir(clip.Parts[0].Output) != filepath.Join(filepath.Dir(clip.Output), ".parts") {
		t.Fatalf("part output should live in exports/.parts: %q", clip.Parts[0].Output)
	}
}

func ffmpegConcatFileQuoteForTest(path string) string {
	escaped := strings.ReplaceAll(path, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `'`, `'\''`)
	return "'" + escaped + "'"
}

func TestBuildLiveSubjectClipPlanRejectsInvalidCompletions(t *testing.T) {
	svc := NewLiveSliceServiceWithClients(nil, nil, nil)
	base := LiveSubjectClipPlanRequest{
		VideoPath: "/tmp/live.mp4",
		OutputDir: "output/live-slice/task",
		Sentences: []LiveSentence{
			{Index: 1, Start: 0, End: 6, Text: "第一句"},
			{Index: 2, Start: 6, End: 12, Text: "第二句"},
		},
		Completions: []LiveSubjectCompletion{{Title: "脚本", Sentences: []LiveSentence{{Index: 1}}}},
	}

	tests := []struct {
		name   string
		mutate func(*LiveSubjectClipPlanRequest)
		want   string
	}{
		{
			name: "duplicate source index",
			mutate: func(req *LiveSubjectClipPlanRequest) {
				req.Completions[0].Sentences = []LiveSentence{{Index: 1}, {Index: 1}}
			},
			want: "duplicate sentence index 1",
		},
		{
			name: "unknown source index",
			mutate: func(req *LiveSubjectClipPlanRequest) {
				req.Completions[0].Sentences = []LiveSentence{{Index: 9}}
			},
			want: "sentence index 9 does not exist",
		},
		{
			name: "all invalid",
			mutate: func(req *LiveSubjectClipPlanRequest) {
				req.Invalid = []LiveInvalid{{Index: 1, Reason: "无效"}}
				req.Completions[0].Sentences = []LiveSentence{{Index: 1}}
			},
			want: "subject clip contains only invalid sentences",
		},
		{
			name: "no cuttable sentences",
			mutate: func(req *LiveSubjectClipPlanRequest) {
				req.Completions[0].Sentences = []LiveSentence{{Index: 0, Text: "旁白"}}
			},
			want: "subject clip contains no source sentences",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := base
			req.Sentences = append([]LiveSentence(nil), base.Sentences...)
			req.Completions = append([]LiveSubjectCompletion(nil), base.Completions...)
			req.Completions[0].Sentences = append([]LiveSentence(nil), base.Completions[0].Sentences...)
			tt.mutate(&req)

			plan, err := svc.BuildLiveSubjectClipPlan(req)
			if err != nil {
				t.Fatalf("unexpected request-level error: %v", err)
			}
			if len(plan.Rejected) != 1 || !strings.Contains(plan.Rejected[0].Reason, tt.want) {
				t.Fatalf("rejected = %#v, want %q", plan.Rejected, tt.want)
			}
		})
	}
}

func TestBuildLiveClipManifestSummarizesResults(t *testing.T) {
	svc := NewLiveSliceServiceWithClients(nil, nil, nil)
	clip := LiveClip{
		Index:         1,
		Title:         "产品卖点",
		SentenceStart: 2,
		SentenceEnd:   3,
		Start:         12,
		End:           46.75,
		Duration:      34.75,
		Output:        "output/live-slice/task/exports/01-product.mp4",
		Method:        "copy",
		Status:        "planned",
	}

	result, err := svc.BuildLiveClipManifest(LiveClipManifestRequest{
		SourceVideo:   "/tmp/live.mp4",
		TingWuTaskID:  "tw-task-1",
		AnalysisTitle: "直播复盘",
		Sentences: []LiveSentence{
			{Index: 2, Start: 12.5, End: 35.25, Text: "这个产品的核心卖点"},
			{Index: 3, Start: 35.25, End: 45.5, Text: "真实使用场景"},
		},
		Invalid: []LiveInvalid{{Index: 1, Reason: "欢迎语"}},
		Clips:   []LiveClip{clip},
		ClipResults: []LiveClipExecutionResult{
			{Index: 1, Status: "ok", Method: "copy", Output: clip.Output, ExitCode: 0, Size: 1024, ActualDurationSeconds: 35.1},
		},
	})
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}
	if len(result.ClipManifest) != 1 || result.ClipManifest[0].Status != "ok" {
		t.Fatalf("manifest = %#v", result.ClipManifest)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("manifest must be JSON serializable: %v", err)
	}
	encodedText := string(encoded)
	for _, want := range []string{"clip_notes_markdown", `"transcript"`, "这个产品的核心卖点"} {
		if !strings.Contains(encodedText, want) {
			t.Fatalf("manifest payload missing %q: %s", want, encodedText)
		}
	}
	if len(result.ClipNotesMarkdown) != 1 || result.ClipNotesMarkdown[0].MarkdownPath != "output/live-slice/task/exports/01-product.md" {
		t.Fatalf("clip notes markdown = %#v", result.ClipNotesMarkdown)
	}
	if len(result.ClipManifest[0].Transcript) != 2 || result.ClipManifest[0].Transcript[0].Index != 2 {
		t.Fatalf("manifest transcript should be rebuilt from source sentences: %#v", result.ClipManifest[0].Transcript)
	}
	if !strings.Contains(result.ClipNotesMarkdown[0].Markdown, "## 原文字幕") {
		t.Fatalf("clip note markdown missing transcript: %s", result.ClipNotesMarkdown[0].Markdown)
	}
	if !strings.Contains(result.TranscriptMarkdown, "- [2] 12.50-35.25s 这个产品的核心卖点") {
		t.Fatalf("transcript markdown = %s", result.TranscriptMarkdown)
	}
	for _, want := range []string{"源视频：/tmp/live.mp4", "听悟任务：tw-task-1", "成功 1 条，失败 0 条"} {
		if !strings.Contains(result.SummaryMarkdown, want) {
			t.Fatalf("summary missing %q: %s", want, result.SummaryMarkdown)
		}
	}
}

func TestBuildLiveClipManifestValidatesOutputPartsAndSummarizesWarnings(t *testing.T) {
	svc := NewLiveSliceServiceWithClients(nil, nil, nil)
	clip := LiveClip{
		Index:          1,
		Title:          "重排脚本",
		SentenceStart:  5,
		SentenceEnd:    2,
		Start:          40,
		End:            12,
		Duration:       20,
		Output:         "output/live-slice/task/exports/01-reorder.mp4",
		Method:         "concat",
		ConcatListPath: "output/live-slice/task/exports/.parts/01-reorder.txt",
		Parts: []LiveClipPart{
			{PartIndex: 1, SentenceStart: 5, SentenceEnd: 5, Start: 40, End: 48, Duration: 8, Output: "output/live-slice/task/exports/.parts/01-reorder-part-01.mp4"},
			{PartIndex: 2, SentenceStart: 1, SentenceEnd: 2, Start: 0, End: 12, Duration: 12, Output: "output/live-slice/task/exports/.parts/01-reorder-part-02.mp4"},
		},
	}

	result, err := svc.BuildLiveClipManifest(LiveClipManifestRequest{
		SourceVideo:  "/tmp/live.mp4",
		TingWuTaskID: "tw-task-1",
		Sentences: []LiveSentence{
			{Index: 1, Start: 0, End: 6, Text: "第一句"},
			{Index: 2, Start: 6, End: 12, Text: "第二句"},
			{Index: 5, Start: 40, End: 48, Text: "第五句"},
		},
		Invalid:  []LiveInvalid{{Index: 2, Reason: "风险句"}},
		Warnings: []LiveClipNotice{{Index: 1, Title: "重排脚本", Reason: "segment includes invalid sentences: 2"}},
		Rejected: []LiveRejected{{Index: 2, Title: "无效片段", Reason: "segment contains only invalid sentences"}},
		Clips:    []LiveClip{clip},
		ClipResults: []LiveClipExecutionResult{
			{
				Index:                 1,
				Status:                "ok",
				Method:                "concat",
				Output:                clip.Output,
				ExitCode:              0,
				Size:                  4096,
				ActualDurationSeconds: 20.2,
				PartResults: []LiveClipPartExecutionResult{
					{PartIndex: 1, Status: "ok", Method: "encode", Output: clip.Parts[0].Output, ExitCode: 0, Size: 1024, ActualDurationSeconds: 8.1},
					{PartIndex: 2, Status: "ok", Method: "encode", Output: clip.Parts[1].Output, ExitCode: 0, Size: 2048, ActualDurationSeconds: 12.1},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}
	if len(result.ClipManifest) != 1 || len(result.ClipManifest[0].Parts) != 2 {
		t.Fatalf("manifest = %#v", result.ClipManifest)
	}
	if len(result.ClipManifest[0].Transcript) != 3 || result.ClipManifest[0].Transcript[0].Index != 5 {
		t.Fatalf("manifest transcript should preserve part order: %#v", result.ClipManifest[0].Transcript)
	}
	for _, want := range []string{"需人工复核片段", "segment includes invalid sentences: 2", "无效片段", "成功 1 条，失败 0 条"} {
		if !strings.Contains(result.SummaryMarkdown, want) {
			t.Fatalf("summary missing %q: %s", want, result.SummaryMarkdown)
		}
	}
}

func TestBuildLiveClipManifestRejectsOutputAndPartResultMismatch(t *testing.T) {
	svc := NewLiveSliceServiceWithClients(nil, nil, nil)
	clip := LiveClip{
		Index:    1,
		Title:    "片段",
		Start:    0,
		End:      22,
		Duration: 22,
		Output:   "output/live-slice/task/exports/01.mp4",
		Method:   "concat",
		Parts: []LiveClipPart{
			{PartIndex: 1, SentenceStart: 1, SentenceEnd: 1, Start: 0, End: 10, Duration: 10, Output: "output/live-slice/task/exports/.parts/01-part-01.mp4"},
			{PartIndex: 2, SentenceStart: 2, SentenceEnd: 2, Start: 10, End: 22, Duration: 12, Output: "output/live-slice/task/exports/.parts/01-part-02.mp4"},
		},
	}
	base := LiveClipManifestRequest{
		SourceVideo:  "/tmp/live.mp4",
		TingWuTaskID: "tw-task-1",
		Sentences: []LiveSentence{
			{Index: 1, Start: 0, End: 10, Text: "第一句"},
			{Index: 2, Start: 10, End: 22, Text: "第二句"},
		},
		Clips: []LiveClip{clip},
		ClipResults: []LiveClipExecutionResult{
			{
				Index:                 1,
				Status:                "ok",
				Method:                "concat",
				Output:                clip.Output,
				Size:                  100,
				ActualDurationSeconds: 22,
				PartResults: []LiveClipPartExecutionResult{
					{PartIndex: 1, Status: "ok", Method: "encode", Output: clip.Parts[0].Output, Size: 100, ActualDurationSeconds: 10},
					{PartIndex: 2, Status: "ok", Method: "encode", Output: clip.Parts[1].Output, Size: 100, ActualDurationSeconds: 12},
				},
			},
		},
	}

	tests := []struct {
		name   string
		mutate func(*LiveClipManifestRequest)
		want   string
	}{
		{
			name: "final output mismatch",
			mutate: func(req *LiveClipManifestRequest) {
				req.ClipResults[0].Output = "output/live-slice/task/exports/wrong.mp4"
			},
			want: "output must match planned output",
		},
		{
			name: "part output mismatch",
			mutate: func(req *LiveClipManifestRequest) {
				req.ClipResults[0].PartResults[0].Output = "output/live-slice/task/exports/.parts/wrong.mp4"
			},
			want: "part output must match planned output",
		},
		{
			name: "missing part result",
			mutate: func(req *LiveClipManifestRequest) {
				req.ClipResults[0].PartResults = req.ClipResults[0].PartResults[:1]
			},
			want: "missing part_results for clip index 1 part index 2",
		},
		{
			name: "failed without error",
			mutate: func(req *LiveClipManifestRequest) {
				req.ClipResults[0] = LiveClipExecutionResult{Index: 1, Status: "failed", Method: "concat", Output: clip.Output}
			},
			want: "failed result must include error or non-zero exit_code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := base
			req.Clips = append([]LiveClip(nil), base.Clips...)
			req.ClipResults = append([]LiveClipExecutionResult(nil), base.ClipResults...)
			req.ClipResults[0].PartResults = append([]LiveClipPartExecutionResult(nil), base.ClipResults[0].PartResults...)
			tt.mutate(&req)

			_, err := svc.BuildLiveClipManifest(req)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestBuildLiveClipManifestRequiresMatchingClipResults(t *testing.T) {
	svc := NewLiveSliceServiceWithClients(nil, nil, nil)
	clip := LiveClip{Index: 1, Title: "片段", Start: 0, End: 10, Duration: 10, Output: "out.mp4"}

	tests := []struct {
		name    string
		results []LiveClipExecutionResult
		want    string
	}{
		{
			name:    "missing result",
			results: []LiveClipExecutionResult{{Index: 2, Status: "failed", Error: "extra"}},
			want:    "missing clip_results for clip index 1",
		},
		{
			name: "duplicate result",
			results: []LiveClipExecutionResult{
				{Index: 1, Status: "failed", Error: "first"},
				{Index: 1, Status: "failed", Error: "second"},
			},
			want: "duplicate clip_results index 1",
		},
		{
			name: "extra result",
			results: []LiveClipExecutionResult{
				{Index: 1, Status: "failed", Error: "planned failure"},
				{Index: 2, Status: "failed", Error: "extra"},
			},
			want: "clip_results index 2 does not match any clip",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.BuildLiveClipManifest(LiveClipManifestRequest{
				SourceVideo:  "/tmp/live.mp4",
				TingWuTaskID: "tw-task-1",
				Sentences:    []LiveSentence{{Index: 1, Start: 0, End: 10, Text: "第一句"}},
				Clips:        []LiveClip{clip},
				ClipResults:  tt.results,
			})
			if err == nil {
				t.Fatal("expected strict clip_results validation error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestBuildLiveClipManifestRejectsSuccessfulEmptyOutputs(t *testing.T) {
	svc := NewLiveSliceServiceWithClients(nil, nil, nil)
	clip := LiveClip{Index: 1, Title: "片段", Start: 0, End: 10, Duration: 10, Output: "out.mp4"}

	tests := []struct {
		name   string
		result LiveClipExecutionResult
		want   string
	}{
		{
			name:   "missing output",
			result: LiveClipExecutionResult{Index: 1, Status: "ok", Size: 100, ActualDurationSeconds: 10},
			want:   "output is required",
		},
		{
			name:   "zero size",
			result: LiveClipExecutionResult{Index: 1, Status: "ok", Output: "out.mp4", Size: 0, ActualDurationSeconds: 10},
			want:   "size must be positive",
		},
		{
			name:   "missing actual duration",
			result: LiveClipExecutionResult{Index: 1, Status: "ok", Output: "out.mp4", Size: 100},
			want:   "actual_duration_seconds must be positive",
		},
		{
			name:   "duration mismatch",
			result: LiveClipExecutionResult{Index: 1, Status: "ok", Output: "out.mp4", Size: 100, ActualDurationSeconds: 5},
			want:   "actual_duration_seconds 5.000s differs from planned duration 10.000s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.BuildLiveClipManifest(LiveClipManifestRequest{
				SourceVideo:  "/tmp/live.mp4",
				TingWuTaskID: "tw-task-1",
				Sentences:    []LiveSentence{{Index: 1, Start: 0, End: 10, Text: "第一句"}},
				Clips:        []LiveClip{clip},
				ClipResults:  []LiveClipExecutionResult{tt.result},
			})
			if err == nil {
				t.Fatal("expected successful clip validation error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestBuildLiveClipManifestRejectsUnmappedTranscript(t *testing.T) {
	svc := NewLiveSliceServiceWithClients(nil, nil, nil)

	_, err := svc.BuildLiveClipManifest(LiveClipManifestRequest{
		SourceVideo:  "/tmp/live.mp4",
		TingWuTaskID: "tw-task-1",
		Sentences: []LiveSentence{
			{Index: 1, Start: 0, End: 10, Text: "第一句"},
		},
		Clips: []LiveClip{
			{Index: 1, Title: "片段", SentenceStart: 2, SentenceEnd: 3, Start: 0, End: 10, Duration: 10, Output: "out.mp4"},
		},
		ClipResults: []LiveClipExecutionResult{
			{Index: 1, Status: "ok", Output: "out.mp4", Size: 100, ActualDurationSeconds: 10},
		},
	})
	if err == nil {
		t.Fatal("expected transcript reconstruction error")
	}
	if !strings.Contains(err.Error(), "cannot rebuild transcript for clip index 1") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTaskKeyIncludesUUIDAndIsUnique(t *testing.T) {
	first := taskKey()
	second := taskKey()

	if first == second {
		t.Fatalf("task keys should be unique, got %q twice", first)
	}
	pattern := regexp.MustCompile(`^live-slice-\d{14}-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	for _, key := range []string{first, second} {
		if !pattern.MatchString(key) {
			t.Fatalf("task key %q does not include timestamp and uuid", key)
		}
	}
}

func TestUploadLiveAudioRejectsLocalStorage(t *testing.T) {
	tmp := t.TempDir()
	audio := filepath.Join(tmp, "audio.mp3")
	if err := os.WriteFile(audio, []byte("fake audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := newFakeLiveStorage("local")
	if err != nil {
		t.Fatal(err)
	}
	svc := NewLiveSliceServiceWithClients(nil, store, nil)

	_, err = svc.UploadLiveAudio(context.Background(), audio, 3600)
	if err == nil {
		t.Fatal("expected local storage rejection")
	}
	text := err.Error()
	if !strings.Contains(text, "OSS storage is required") {
		t.Fatalf("error should describe the OSS capability requirement, got: %v", err)
	}
	for _, forbidden := range []string{"prepare_file_upload", "create_live_analysis_task", "PUT", "audio_key", "then call", "configure OSS"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("error contains workflow directive %q: %v", forbidden, err)
		}
	}
}

// argsHave reports whether the space-joined ffmpeg args contain substr. Filter chains (-vf / -af)
// are passed as a single argv element, so whole-token equality would miss them; substring is robust.
func argsHave(args []string, substr string) bool {
	return strings.Contains(strings.Join(args, " "), substr)
}

func TestBuildLiveClipPlanAccurateArgsIncludeQualityFlags(t *testing.T) {
	svc := NewLiveSliceServiceWithClients(nil, nil, nil)

	plan, err := svc.BuildLiveClipPlan(LiveClipPlanRequest{
		VideoPath: "/tmp/live.mp4",
		OutputDir: "output/live-slice/task",
		Sentences: []LiveSentence{{Index: 1, Start: 10, End: 40, Text: "核心卖点"}},
		Segments:  []LiveSegment{{Title: "卖点", Start: 1, End: 1}},
	})
	if err != nil {
		t.Fatalf("build clip plan: %v", err)
	}
	clip := plan.Clips[0]
	args := clip.AccurateCutArgs
	if len(args) == 0 || args[len(args)-1] != clip.Output {
		t.Fatalf("output must be the last accurate arg: %#v", args)
	}
	for _, want := range []string{"-pix_fmt", "yuv420p", "-crf", "20", "-preset", "veryfast", "-c:a", "aac", "-b:a", "128k", "-movflags", "+faststart", "-af", loudnormFilter} {
		if !argsHave(args, want) {
			t.Fatalf("accurate args missing %q: %#v", want, args)
		}
	}
	// No source dimensions → passthrough: fast stream-copy must still be offered.
	if clip.Method != "copy" || len(clip.FastCutArgs) == 0 {
		t.Fatalf("expected copy/fast available for passthrough, method=%q fast=%#v", clip.Method, clip.FastCutArgs)
	}
	if clip.VerticalFilter != "" {
		t.Fatalf("passthrough should have no vertical filter, got %q", clip.VerticalFilter)
	}
}

func TestBuildLiveClipPlanLoudnormDisabled(t *testing.T) {
	svc := NewLiveSliceServiceWithClients(nil, nil, nil)
	off := false
	plan, err := svc.BuildLiveClipPlan(LiveClipPlanRequest{
		VideoPath:              "/tmp/live.mp4",
		OutputDir:              "output/live-slice/task",
		Sentences:              []LiveSentence{{Index: 1, Start: 10, End: 40, Text: "核心卖点"}},
		Segments:               []LiveSegment{{Title: "卖点", Start: 1, End: 1}},
		NormalizeAudioLoudness: &off,
	})
	if err != nil {
		t.Fatalf("build clip plan: %v", err)
	}
	args := plan.Clips[0].AccurateCutArgs
	if argsHave(args, "-af") || argsHave(args, loudnormFilter) {
		t.Fatalf("loudnorm should be absent when disabled: %#v", args)
	}
}

func TestBuildLiveClipPlanVerticalizesLandscapeSource(t *testing.T) {
	svc := NewLiveSliceServiceWithClients(nil, nil, nil)

	plan, err := svc.BuildLiveClipPlan(LiveClipPlanRequest{
		VideoPath:    "/tmp/live.mp4",
		OutputDir:    "output/live-slice/task",
		Sentences:    []LiveSentence{{Index: 1, Start: 10, End: 40, Text: "核心卖点"}},
		Segments:     []LiveSegment{{Title: "卖点", Start: 1, End: 1}},
		TargetMode:   "vertical",
		VerticalFill: "blur",
		SourceWidth:  1920,
		SourceHeight: 1080,
	})
	if err != nil {
		t.Fatalf("build clip plan: %v", err)
	}
	clip := plan.Clips[0]
	if clip.Method != "encode" {
		t.Fatalf("method = %q, want encode (filter forces re-encode)", clip.Method)
	}
	if len(clip.FastCutArgs) != 0 || clip.FastCutShell != "" {
		t.Fatalf("fast cut must be suppressed when a video filter is active: %#v / %q", clip.FastCutArgs, clip.FastCutShell)
	}
	// setsar=1 is mandatory on the blur path: scale changes pixel dims and without
	// resetting SAR ffmpeg derives one to preserve the input DAR, tagging the
	// 1080x1920 frame SAR=256:81 / DAR=16:9 → renders horizontal on Douyin/WeChat.
	for _, want := range []string{"split[bg][fg]", "boxblur=20:5", "overlay=(W-w)/2:(H-h)/2,setsar=1", "setsar=1", "-pix_fmt", "yuv420p", "-movflags", "+faststart"} {
		if !argsHave(clip.AccurateCutArgs, want) {
			t.Fatalf("accurate args missing %q: %#v", want, clip.AccurateCutArgs)
		}
	}
	if !strings.Contains(clip.Orientation, "landscape-to-vertical:blur") {
		t.Fatalf("orientation = %q", clip.Orientation)
	}
	if clip.VerticalFilter == "" {
		t.Fatalf("vertical filter should be echoed for transparency")
	}
}

func TestBuildLiveClipPlanVerticalCropFill(t *testing.T) {
	svc := NewLiveSliceServiceWithClients(nil, nil, nil)

	plan, err := svc.BuildLiveClipPlan(LiveClipPlanRequest{
		VideoPath:    "/tmp/live.mp4",
		OutputDir:    "output/live-slice/task",
		Sentences:    []LiveSentence{{Index: 1, Start: 10, End: 40, Text: "核心卖点"}},
		Segments:     []LiveSegment{{Title: "卖点", Start: 1, End: 1}},
		TargetMode:   "vertical",
		VerticalFill: "crop",
		SourceWidth:  1920,
		SourceHeight: 1080,
	})
	if err != nil {
		t.Fatalf("build clip plan: %v", err)
	}
	clip := plan.Clips[0]
	if clip.Method != "encode" {
		t.Fatalf("method = %q, want encode", clip.Method)
	}
	if !argsHave(clip.AccurateCutArgs, "crop=ih*1080/1920:ih") || !argsHave(clip.AccurateCutArgs, "scale=1080:1920") {
		t.Fatalf("crop fill vf missing: %#v", clip.AccurateCutArgs)
	}
	if argsHave(clip.AccurateCutArgs, "boxblur") {
		t.Fatalf("crop fill must not use boxblur: %#v", clip.AccurateCutArgs)
	}
}

func TestBuildLiveClipPlanSkipsConversionForAlreadyVerticalAndUnknown(t *testing.T) {
	svc := NewLiveSliceServiceWithClients(nil, nil, nil)
	base := LiveClipPlanRequest{
		VideoPath:  "/tmp/live.mp4",
		OutputDir:  "output/live-slice/task",
		Sentences:  []LiveSentence{{Index: 1, Start: 10, End: 40, Text: "核心卖点"}},
		Segments:   []LiveSegment{{Title: "卖点", Start: 1, End: 1}},
		TargetMode: "vertical",
	}

	// Already-vertical at target size → no transform, fast copy available.
	vertical := base
	vertical.SourceWidth = 1080
	vertical.SourceHeight = 1920
	vp, err := svc.BuildLiveClipPlan(vertical)
	if err != nil {
		t.Fatalf("build clip plan: %v", err)
	}
	if c := vp.Clips[0]; c.Method != "copy" || c.VerticalFilter != "" {
		t.Fatalf("already-vertical target should pass through: method=%q vf=%q", c.Method, c.VerticalFilter)
	}

	// No source dims supplied → cannot safely transform, pass through.
	unknown, err := svc.BuildLiveClipPlan(base)
	if err != nil {
		t.Fatalf("build clip plan: %v", err)
	}
	if u := unknown.Clips[0]; u.Method != "copy" || u.VerticalFilter != "" {
		t.Fatalf("unknown source should pass through: method=%q vf=%q", u.Method, u.VerticalFilter)
	}
}
