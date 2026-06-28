package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/storage"
)

const (
	// LiveScriptPromptName is the TingWu custom prompt name used to ask for live-script analysis.
	LiveScriptPromptName = "ANALYZE_LIVE_SCRIPT"
	defaultAudioURLTTL   = 24 * 3600
	defaultMinClipLength = 5
	defaultMaxClipLength = 180
)

// LiveSliceService coordinates storage, TingWu transcription, and LLM-based live slicing.
type LiveSliceService struct {
	tingwu TingWuClient
	llm    LLMClient
	store  storage.Provider
	logger *zerolog.Logger
}

// TingWuClient is the direct adapter boundary for Alibaba TingWu calls.
type TingWuClient interface {
	CreateTask(ctx context.Context, req LiveAnalysisTaskRequest) (string, error)
	QueryTask(ctx context.Context, taskID string) (*TingWuTaskInfo, bool, error)
}

// NewLiveSliceService creates a live-slice service with a real TingWu client when config is present.
func NewLiveSliceService(cfg config.TingWuConfig, llm LLMClient, store storage.Provider, logger *zerolog.Logger) (*LiveSliceService, error) {
	tw, err := NewAlibabaTingWuClient(cfg)
	if err != nil {
		return nil, err
	}
	return NewLiveSliceServiceWithClients(tw, llm, store, logger), nil
}

// NewLiveSliceServiceWithClients creates a live-slice service with injected clients for tests.
func NewLiveSliceServiceWithClients(tw TingWuClient, llm LLMClient, store storage.Provider, logger *zerolog.Logger) *LiveSliceService {
	return &LiveSliceService{
		tingwu: tw,
		llm:    llm,
		store:  store,
		logger: logger,
	}
}

// LiveAudioUploadResult is returned by upload_live_audio.
type LiveAudioUploadResult struct {
	URL           string `json:"url"`
	Key           string `json:"key"`
	DownloadURL   string `json:"download_url,omitempty"`
	Size          int64  `json:"size"`
	MimeType      string `json:"mime_type"`
	ExpiresSecond int    `json:"expires_seconds"`
}

// UploadLiveAudio uploads local audio to OSS and returns a URL suitable for TingWu.
func (s *LiveSliceService) UploadLiveAudio(ctx context.Context, filePath string, expiresSeconds int) (*LiveAudioUploadResult, error) {
	if s.store == nil {
		return nil, fmt.Errorf("storage provider is not available; configure OSS or pass audio_url directly")
	}
	if s.store.Name() != "oss" {
		return nil, fmt.Errorf("upload_live_audio requires OSS storage because TingWu must fetch a public URL; configure OSS or pass audio_url directly")
	}
	if strings.TrimSpace(filePath) == "" {
		return nil, fmt.Errorf("file_path is required")
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("stat audio file: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("file_path must point to an audio file, got directory")
	}
	if expiresSeconds <= 0 {
		expiresSeconds = defaultAudioURLTTL
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	contentType := audioContentType(ext)
	key := fmt.Sprintf("uploads/live-audio/%s%s", uuid.NewString(), ext)
	uploaded, err := s.store.UploadFile(ctx, key, filePath, contentType)
	if err != nil {
		return nil, fmt.Errorf("upload audio to OSS: %w", err)
	}
	if uploaded.Size == 0 {
		uploaded.Size = info.Size()
	}

	downloadURL := uploaded.URL
	if !s.store.HasCustomDomain() {
		downloadURL, err = s.store.DownloadURL(ctx, key, expiresSeconds)
		if err != nil {
			return nil, fmt.Errorf("create signed audio URL: %w", err)
		}
	}

	return &LiveAudioUploadResult{
		URL:           uploaded.URL,
		Key:           uploaded.Key,
		DownloadURL:   downloadURL,
		Size:          uploaded.Size,
		MimeType:      uploaded.MimeType,
		ExpiresSecond: expiresSeconds,
	}, nil
}

func audioContentType(ext string) string {
	switch ext {
	case ".mp3":
		return "audio/mpeg"
	case ".m4a", ".mp4":
		return "audio/mp4"
	case ".wav":
		return "audio/wav"
	case ".aac":
		return "audio/aac"
	case ".flac":
		return "audio/flac"
	case ".ogg":
		return "audio/ogg"
	default:
		if t := mime.TypeByExtension(ext); t != "" {
			return t
		}
		return "application/octet-stream"
	}
}

// LiveAnalysisTaskRequest is the normalized MCP/service input for TingWu task creation.
type LiveAnalysisTaskRequest struct {
	AudioURL                 string `json:"audio_url"`
	AutoChaptersEnabled      bool   `json:"auto_chapters_enabled"`
	SummarizationEnabled     bool   `json:"summarization_enabled"`
	MeetingAssistanceEnabled bool   `json:"meeting_assistance_enabled"`
	DiarizationEnabled       bool   `json:"diarization_enabled"`
	DiarizationSpeakerCount  int32  `json:"diarization_speaker_count,omitempty"`
	ScriptTemplateEnable     bool   `json:"script_template_enable,omitempty"`
}

// LiveAnalysisTaskResult is returned when a TingWu task is created.
type LiveAnalysisTaskResult struct {
	TaskID string `json:"task_id"`
}

// CreateLiveAnalysisTask creates a direct TingWu analysis task.
func (s *LiveSliceService) CreateLiveAnalysisTask(ctx context.Context, req LiveAnalysisTaskRequest) (*LiveAnalysisTaskResult, error) {
	if s.tingwu == nil {
		return nil, fmt.Errorf("TingWu client is not configured")
	}
	if strings.TrimSpace(req.AudioURL) == "" {
		return nil, fmt.Errorf("audio_url is required")
	}
	taskID, err := s.tingwu.CreateTask(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("create TingWu task: %w", err)
	}
	return &LiveAnalysisTaskResult{TaskID: taskID}, nil
}

// QueryLiveAnalysisTask returns the normalized analysis result when the TingWu task is complete.
func (s *LiveSliceService) QueryLiveAnalysisTask(ctx context.Context, taskID string) (*LiveAnalysisResult, error) {
	if s.tingwu == nil {
		return nil, fmt.Errorf("TingWu client is not configured")
	}
	if strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("task_id is required")
	}
	raw, completed, err := s.tingwu.QueryTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("query TingWu task: %w", err)
	}
	if raw == nil {
		return &LiveAnalysisResult{Status: "UNKNOWN"}, nil
	}
	result := NormalizeLiveAnalysisResult(raw)
	result.Completed = completed
	return result, nil
}

// LiveAnalysisResult is the stable JSON shape consumed by the skill and desktop concepts.
type LiveAnalysisResult struct {
	Status       string               `json:"status"`
	Completed    bool                 `json:"completed"`
	Title        string               `json:"title,omitempty"`
	Summary      string               `json:"summary,omitempty"`
	Chapters     []LiveChapter        `json:"chapters"`
	Sentences    []LiveSentence       `json:"sentences"`
	Subjects     []LiveSubject        `json:"subjects"`
	Segments     []LiveSegment        `json:"segments"`
	Invalid      []LiveInvalid        `json:"invalid"`
	QAS          []LiveQuestionAnswer `json:"qas"`
	Topics       []LiveTopic          `json:"topics"`
	Words        []LiveWordStat       `json:"words"`
	Silents      []LiveSilentStat     `json:"silents"`
	Keywords     []string             `json:"keywords"`
	KeySentences []string             `json:"key_sentences"`
	Templates    string               `json:"templates,omitempty"`
	AudioInfo    *LiveAudioInfo       `json:"audio_info,omitempty"`
}

// LiveSentence is a sentence-level slice candidate with seconds-based timestamps.
type LiveSentence struct {
	Index   int64   `json:"index"`
	Start   float64 `json:"start,omitempty"`
	End     float64 `json:"end,omitempty"`
	Text    string  `json:"text"`
	Invalid bool    `json:"invalid,omitempty"`
	Reason  string  `json:"reason,omitempty"`
}

type LiveChapter struct {
	ID        int64          `json:"id"`
	Headline  string         `json:"headline"`
	Summary   string         `json:"summary"`
	Start     float64        `json:"start"`
	End       float64        `json:"end"`
	Sentences []LiveSentence `json:"sentences"`
	Subjects  []LiveSubject  `json:"subjects,omitempty"`
	Segments  []LiveSegment  `json:"segments,omitempty"`
}

type LiveSubject struct {
	Title    string `json:"title"`
	Thoughts string `json:"thoughts"`
}

type LiveInvalid struct {
	Index  int64  `json:"index"`
	Reason string `json:"reason"`
}

type LiveSegment struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Thoughts    string `json:"thoughts,omitempty"`
	Start       int64  `json:"start"`
	End         int64  `json:"end"`
}

type LiveQuestionAnswer struct {
	Question LiveTimedText `json:"question"`
	Answer   LiveTimedText `json:"answer"`
}

type LiveTimedText struct {
	Text  string  `json:"text"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

type LiveTopic struct {
	Title  string      `json:"title"`
	Topics []LiveTopic `json:"topics,omitempty"`
}

type LiveWordStat struct {
	Minutes int64 `json:"minutes"`
	Count   int64 `json:"count"`
}

type LiveSilentStat struct {
	Minutes int64 `json:"minutes"`
	Seconds int64 `json:"seconds"`
}

type LiveAudioInfo struct {
	Size       int64  `json:"size"`
	Duration   int64  `json:"duration"`
	SampleRate int64  `json:"sample_rate"`
	Language   string `json:"language"`
}

type LiveSubjectCompletion struct {
	Title     string         `json:"title"`
	Subtitle  string         `json:"subtitle,omitempty"`
	Thoughts  string         `json:"thoughts"`
	Sentences []LiveSentence `json:"sentences"`
}

// LiveSubjectClipPlanRequest asks the service to turn completed subject scripts into executable clips.
type LiveSubjectClipPlanRequest struct {
	Sentences              []LiveSentence          `json:"sentences"`
	Completions            []LiveSubjectCompletion `json:"completions"`
	VideoPath              string                  `json:"video_path"`
	OutputDir              string                  `json:"output_dir"`
	Invalid                []LiveInvalid           `json:"invalid,omitempty"`
	MinDurationSeconds     float64                 `json:"min_duration_seconds,omitempty"`
	MaxDurationSeconds     float64                 `json:"max_duration_seconds,omitempty"`
	HeadPaddingSeconds     float64                 `json:"head_padding_seconds,omitempty"`
	TailPaddingSeconds     float64                 `json:"tail_padding_seconds,omitempty"`
	TargetMode             string                  `json:"target_mode,omitempty"`
	VerticalFill           string                  `json:"vertical_fill,omitempty"`
	SourceWidth            int                     `json:"source_width,omitempty"`
	SourceHeight           int                     `json:"source_height,omitempty"`
	TargetWidth            int                     `json:"target_width,omitempty"`
	TargetHeight           int                     `json:"target_height,omitempty"`
	NormalizeAudioLoudness *bool                   `json:"normalize_audio_loudness,omitempty"`
}

// LiveClipPlanRequest asks the service to turn LLM segment indexes into concrete cut commands.
type LiveClipPlanRequest struct {
	Sentences              []LiveSentence `json:"sentences"`
	Segments               []LiveSegment  `json:"segments"`
	VideoPath              string         `json:"video_path"`
	OutputDir              string         `json:"output_dir"`
	Invalid                []LiveInvalid  `json:"invalid,omitempty"`
	MinDurationSeconds     float64        `json:"min_duration_seconds,omitempty"`
	MaxDurationSeconds     float64        `json:"max_duration_seconds,omitempty"`
	HeadPaddingSeconds     float64        `json:"head_padding_seconds,omitempty"`
	TailPaddingSeconds     float64        `json:"tail_padding_seconds,omitempty"`
	TargetMode             string         `json:"target_mode,omitempty"`
	VerticalFill           string         `json:"vertical_fill,omitempty"`
	SourceWidth            int            `json:"source_width,omitempty"`
	SourceHeight           int            `json:"source_height,omitempty"`
	TargetWidth            int            `json:"target_width,omitempty"`
	TargetHeight           int            `json:"target_height,omitempty"`
	NormalizeAudioLoudness *bool          `json:"normalize_audio_loudness,omitempty"`
}

// LiveClipPlanResult is a deterministic clip plan ready for local ffmpeg execution.
type LiveClipPlanResult struct {
	Clips    []LiveClip       `json:"clips"`
	Rejected []LiveRejected   `json:"rejected"`
	Warnings []LiveClipNotice `json:"warnings"`
}

// LiveClip is a single deterministic ffmpeg cut instruction.
type LiveClip struct {
	Index             int            `json:"index"`
	Title             string         `json:"title"`
	Description       string         `json:"description,omitempty"`
	Thoughts          string         `json:"thoughts,omitempty"`
	SentenceStart     int64          `json:"sentence_start"`
	SentenceEnd       int64          `json:"sentence_end"`
	Start             float64        `json:"start"`
	End               float64        `json:"end"`
	Duration          float64        `json:"duration"`
	Output            string         `json:"output"`
	FastCutShell      string         `json:"fast_cut_shell"`
	AccurateCutShell  string         `json:"accurate_cut_shell"`
	FastCutArgs       []string       `json:"fast_cut_args"`
	AccurateCutArgs   []string       `json:"accurate_cut_args"`
	Parts             []LiveClipPart `json:"parts,omitempty"`
	ConcatListPath    string         `json:"concat_list_path,omitempty"`
	ConcatListContent string         `json:"concat_list_content,omitempty"`
	ConcatShell       string         `json:"concat_shell,omitempty"`
	ConcatArgs        []string       `json:"concat_args,omitempty"`
	Transcript        []LiveSentence `json:"transcript"`
	ScriptNotes       []LiveSentence `json:"script_notes,omitempty"`
	Orientation       string         `json:"orientation,omitempty"`
	VerticalFilter    string         `json:"vertical_filter,omitempty"`
	Method            string         `json:"method,omitempty"`
	Status            string         `json:"status,omitempty"`
}

// LiveClipPart is a source-video fragment used to assemble a final clip.
type LiveClipPart struct {
	PartIndex        int            `json:"part_index"`
	SentenceStart    int64          `json:"sentence_start"`
	SentenceEnd      int64          `json:"sentence_end"`
	Start            float64        `json:"start"`
	End              float64        `json:"end"`
	Duration         float64        `json:"duration"`
	Output           string         `json:"output"`
	AccurateCutShell string         `json:"accurate_cut_shell"`
	AccurateCutArgs  []string       `json:"accurate_cut_args"`
	Transcript       []LiveSentence `json:"transcript"`
}

type LiveRejected struct {
	Index  int    `json:"index"`
	Title  string `json:"title,omitempty"`
	Reason string `json:"reason"`
}

type LiveClipNotice struct {
	Index  int    `json:"index"`
	Title  string `json:"title,omitempty"`
	Reason string `json:"reason"`
}

// LiveClipExecutionResult is supplied by the agent after running ffmpeg.
type LiveClipExecutionResult struct {
	Index                 int                           `json:"index"`
	Status                string                        `json:"status"`
	Method                string                        `json:"method,omitempty"`
	Output                string                        `json:"output,omitempty"`
	ExitCode              int                           `json:"exit_code,omitempty"`
	Error                 string                        `json:"error,omitempty"`
	Size                  int64                         `json:"size,omitempty"`
	ActualDurationSeconds float64                       `json:"actual_duration_seconds,omitempty"`
	PartResults           []LiveClipPartExecutionResult `json:"part_results,omitempty"`
}

// LiveClipPartExecutionResult is supplied by the agent after cutting a source part.
type LiveClipPartExecutionResult struct {
	PartIndex             int     `json:"part_index"`
	Status                string  `json:"status"`
	Method                string  `json:"method,omitempty"`
	Output                string  `json:"output,omitempty"`
	ExitCode              int     `json:"exit_code,omitempty"`
	Error                 string  `json:"error,omitempty"`
	Size                  int64   `json:"size,omitempty"`
	ActualDurationSeconds float64 `json:"actual_duration_seconds,omitempty"`
}

type LiveClipManifestRequest struct {
	SourceVideo   string                    `json:"source_video"`
	TingWuTaskID  string                    `json:"tingwu_task_id"`
	AnalysisTitle string                    `json:"analysis_title,omitempty"`
	Sentences     []LiveSentence            `json:"sentences"`
	Invalid       []LiveInvalid             `json:"invalid,omitempty"`
	Warnings      []LiveClipNotice          `json:"warnings,omitempty"`
	Rejected      []LiveRejected            `json:"rejected,omitempty"`
	Clips         []LiveClip                `json:"clips"`
	ClipResults   []LiveClipExecutionResult `json:"clip_results"`
}

type LiveClipManifestResult struct {
	ClipManifest       []LiveClipManifestItem `json:"clip_manifest"`
	ClipNotesMarkdown  []LiveClipNoteMarkdown `json:"clip_notes_markdown"`
	TranscriptMarkdown string                 `json:"transcript_markdown"`
	SummaryMarkdown    string                 `json:"summary_markdown"`
}

type LiveClipManifestItem struct {
	Index                 int            `json:"index"`
	Title                 string         `json:"title"`
	Description           string         `json:"description,omitempty"`
	Thoughts              string         `json:"thoughts,omitempty"`
	SentenceStart         int64          `json:"sentence_start"`
	SentenceEnd           int64          `json:"sentence_end"`
	Start                 float64        `json:"start"`
	End                   float64        `json:"end"`
	Duration              float64        `json:"duration"`
	Output                string         `json:"output"`
	Method                string         `json:"method,omitempty"`
	Status                string         `json:"status"`
	ExitCode              int            `json:"exit_code,omitempty"`
	Error                 string         `json:"error,omitempty"`
	Size                  int64          `json:"size,omitempty"`
	ActualDurationSeconds float64        `json:"actual_duration_seconds,omitempty"`
	Parts                 []LiveClipPart `json:"parts,omitempty"`
	Transcript            []LiveSentence `json:"transcript"`
	ScriptNotes           []LiveSentence `json:"script_notes,omitempty"`
}

type LiveClipNoteMarkdown struct {
	Index        int    `json:"index"`
	Title        string `json:"title"`
	MarkdownPath string `json:"markdown_path"`
	Markdown     string `json:"markdown"`
}

// NormalizeLiveAnalysisResult converts raw TingWu artifacts into the stable live-slice JSON shape.
func NormalizeLiveAnalysisResult(raw *TingWuTaskInfo) *LiveAnalysisResult {
	result := &LiveAnalysisResult{
		Chapters:  []LiveChapter{},
		Sentences: []LiveSentence{},
		Subjects:  []LiveSubject{},
		Segments:  []LiveSegment{},
		Invalid:   []LiveInvalid{},
		QAS:       []LiveQuestionAnswer{},
		Topics:    []LiveTopic{},
		Words:     []LiveWordStat{},
		Silents:   []LiveSilentStat{},
		Keywords:  []string{},
	}
	if raw == nil {
		result.Status = "UNKNOWN"
		return result
	}
	result.Status = raw.Status

	sentenceMap := map[int]LiveSentence{}
	sentenceIDs := []int{}
	if raw.Transcription != nil {
		info := raw.Transcription.Transcription.AudioInfo
		result.AudioInfo = &LiveAudioInfo{
			Size:       info.Size,
			Duration:   info.Duration,
			SampleRate: info.SampleRate,
			Language:   info.Language,
		}
		sentenceMap = raw.Transcription.Sentences()
		for id := range sentenceMap {
			sentenceIDs = append(sentenceIDs, id)
		}
		sort.Ints(sentenceIDs)
		for _, id := range sentenceIDs {
			result.Sentences = append(result.Sentences, sentenceMap[id])
		}
	}

	if raw.Summarization != nil {
		summary := raw.Summarization.Summarization
		result.Title = summary.ParagraphTitle
		result.Summary = summary.ParagraphSummary
		result.Topics = normalizeTopics(summary.MindMapSummary)
		result.QAS = normalizeQAS(summary.QuestionsAnsweringSummary, sentenceMap)
	}

	if raw.MeetingAssistance != nil {
		ma := raw.MeetingAssistance.MeetingAssistance
		result.Keywords = append(result.Keywords, ma.Keywords...)
		for _, sentence := range ma.KeySentences {
			if strings.TrimSpace(sentence.Text) != "" {
				result.KeySentences = append(result.KeySentences, sentence.Text)
			}
		}
	}

	if raw.CustomPrompt != nil {
		for _, item := range raw.CustomPrompt.CustomPrompt {
			if item.Name == LiveScriptPromptName && !item.Truncated {
				result.Templates = item.Result
			}
		}
	}

	result.Chapters = normalizeChapters(raw, sentenceMap)
	result.Words, result.Silents = computeLiveStats(result.Sentences)
	return result
}

func normalizeTopics(raw []TingWuMindMapTopic) []LiveTopic {
	topics := make([]LiveTopic, 0, len(raw))
	for _, item := range raw {
		topics = append(topics, LiveTopic{
			Title:  item.Title,
			Topics: normalizeTopics(item.Topic),
		})
	}
	return topics
}

func normalizeQAS(raw []TingWuQuestionAnsweringSummary, sentenceMap map[int]LiveSentence) []LiveQuestionAnswer {
	qas := make([]LiveQuestionAnswer, 0, len(raw))
	for _, qa := range raw {
		qStart, qEnd, okQ := sentenceRange(sentenceMap, qa.SentenceIDsOfQuestion)
		aStart, aEnd, okA := sentenceRange(sentenceMap, qa.SentenceIDsOfAnswer)
		if !okQ || !okA {
			continue
		}
		qas = append(qas, LiveQuestionAnswer{
			Question: LiveTimedText{Text: qa.Question, Start: qStart, End: qEnd},
			Answer:   LiveTimedText{Text: qa.Answer, Start: aStart, End: aEnd},
		})
	}
	return qas
}

func sentenceRange(sentenceMap map[int]LiveSentence, ids []int) (float64, float64, bool) {
	if len(ids) == 0 {
		return 0, 0, false
	}
	start := 0.0
	end := 0.0
	ok := false
	for _, id := range ids {
		s, exists := sentenceMap[id]
		if !exists {
			continue
		}
		if !ok || s.Start < start {
			start = s.Start
		}
		if !ok || s.End > end {
			end = s.End
		}
		ok = true
	}
	return start, end, ok
}

func normalizeChapters(raw *TingWuTaskInfo, sentenceMap map[int]LiveSentence) []LiveChapter {
	if raw == nil || raw.AutoChapters == nil {
		if len(sentenceMap) == 0 {
			return []LiveChapter{}
		}
		all := make([]LiveSentence, 0, len(sentenceMap))
		for _, sentence := range sentenceMap {
			all = append(all, sentence)
		}
		sort.Slice(all, func(i, j int) bool { return all[i].Index < all[j].Index })
		return []LiveChapter{{
			ID:        1,
			Headline:  "全文",
			Sentences: all,
			Start:     firstSentenceStart(all),
			End:       lastSentenceEnd(all),
		}}
	}

	chapters := make([]LiveChapter, 0, len(raw.AutoChapters.AutoChapters))
	for _, item := range raw.AutoChapters.AutoChapters {
		start := item.Start / 1000
		end := item.End / 1000
		chapter := LiveChapter{
			ID:        item.ID,
			Headline:  item.Headline,
			Summary:   item.Summary,
			Start:     start,
			End:       end,
			Sentences: []LiveSentence{},
		}
		for _, sentence := range sentenceMap {
			if sentence.Start >= start && sentence.End <= end {
				chapter.Sentences = append(chapter.Sentences, sentence)
			}
		}
		sort.Slice(chapter.Sentences, func(i, j int) bool {
			return chapter.Sentences[i].Index < chapter.Sentences[j].Index
		})
		chapters = append(chapters, chapter)
	}
	return chapters
}

func firstSentenceStart(sentences []LiveSentence) float64 {
	if len(sentences) == 0 {
		return 0
	}
	return sentences[0].Start
}

func lastSentenceEnd(sentences []LiveSentence) float64 {
	if len(sentences) == 0 {
		return 0
	}
	return sentences[len(sentences)-1].End
}

func computeLiveStats(sentences []LiveSentence) ([]LiveWordStat, []LiveSilentStat) {
	if len(sentences) == 0 {
		return []LiveWordStat{}, []LiveSilentStat{}
	}
	sort.SliceStable(sentences, func(i, j int) bool {
		if sentences[i].Start == sentences[j].Start {
			return sentences[i].Index < sentences[j].Index
		}
		return sentences[i].Start < sentences[j].Start
	})

	wordBuckets := map[int64]int64{}
	silentBuckets := map[int64]float64{}
	prevEnd := 0.0
	maxMinute := int64(0)
	for _, sentence := range sentences {
		minute := int64(sentence.Start / 60)
		if minute > maxMinute {
			maxMinute = minute
		}
		wordBuckets[minute] += int64(utf8.RuneCountInString(sentence.Text))
		if sentence.Start > prevEnd {
			silentBuckets[minute] += sentence.Start - prevEnd
		}
		if sentence.End > prevEnd {
			prevEnd = sentence.End
		}
	}

	words := make([]LiveWordStat, 0, maxMinute+1)
	silents := make([]LiveSilentStat, 0, maxMinute+1)
	for minute := int64(0); minute <= maxMinute; minute++ {
		words = append(words, LiveWordStat{Minutes: minute + 1, Count: wordBuckets[minute]})
		silents = append(silents, LiveSilentStat{Minutes: minute + 1, Seconds: int64(silentBuckets[minute])})
	}
	return words, silents
}

// encodeOptions captures deterministic ffmpeg re-encode parameters shared by every clip.
// Quality flags (yuv420p, CRF, faststart) and optional loudnorm apply on every re-encode path;
// a non-empty videoFilter adds a -vf chain and forces re-encoding (suppressing fast stream-copy).
type encodeOptions struct {
	videoFilter  string // -vf chain (empty = no video filter)
	audioFilter  string // -af chain (empty = no audio filter)
	preset       string
	crf          string
	pixFmt       string
	audioCodec   string
	audioBitrate string
	faststart    bool
}

// encodePlanInput holds the orientation and loudness knobs common to both clip-plan request types.
type encodePlanInput struct {
	targetMode    string
	verticalFill  string
	sourceWidth   int
	sourceHeight  int
	targetWidth   int
	targetHeight  int
	normalizeLoud bool
}

// encodeDecision is the resolved re-encode configuration for a clip plan.
type encodeDecision struct {
	opts          encodeOptions
	needsReencode bool   // true when a video filter forces re-encoding (suppresses fast copy)
	orientation   string // human-readable transform label for transparency / decision logs
}

const (
	defaultTargetWidth  = 1080
	defaultTargetHeight = 1920
	// loudnormFilter is an EBU R128-ish loudness filter used on re-encode paths (Douyin-safe).
	loudnormFilter = "loudnorm=I=-16:TP=-1.5:LRA=11"
)

// computeEncodeOptions resolves the deterministic re-encode configuration from request knobs.
// Quality flags and loudnorm apply on every re-encode path; an orientation transform adds a
// -vf chain and forces re-encoding (which suppresses the fast stream-copy attempt).
// Source dimensions are required to convert orientation — without them the clip passes through.
func computeEncodeOptions(in encodePlanInput) encodeDecision {
	targetW := in.targetWidth
	if targetW <= 0 {
		targetW = defaultTargetWidth
	}
	targetH := in.targetHeight
	if targetH <= 0 {
		targetH = defaultTargetHeight
	}
	opts := encodeOptions{
		preset:       "veryfast",
		crf:          "20",
		pixFmt:       "yuv420p",
		audioCodec:   "aac",
		audioBitrate: "128k",
		faststart:    true,
	}
	if in.normalizeLoud {
		opts.audioFilter = loudnormFilter
	}
	dec := encodeDecision{opts: opts}
	mode := strings.ToLower(strings.TrimSpace(in.targetMode))
	fill := strings.ToLower(strings.TrimSpace(in.verticalFill))
	sourceKnown := in.sourceWidth > 0 && in.sourceHeight > 0
	if !sourceKnown || mode == "original" {
		dec.orientation = "passthrough"
		return dec
	}
	switch mode {
	case "horizontal":
		if in.sourceHeight > in.sourceWidth {
			// landscape canvas = the vertical target dims swapped (defaults 1080x1920 → 1920x1080)
			dec.opts.videoFilter = fmt.Sprintf("scale=%d:%d,setsar=1", targetH, targetW)
			dec.orientation = "vertical-to-horizontal"
		} else {
			dec.orientation = "horizontal"
		}
	default: // "", "auto", "vertical" → target vertical (short-video default)
		if in.sourceWidth >= in.sourceHeight {
			// landscape or square source → convert to vertical
			vf := verticalFillFilter(fill, targetW, targetH)
			dec.opts.videoFilter = vf
			dec.orientation = "landscape-to-vertical:" + fillLabel(fill)
		} else if in.sourceWidth != targetW || in.sourceHeight != targetH {
			// already vertical but not the target canvas → normalize size
			dec.opts.videoFilter = fmt.Sprintf("scale=%d:%d,setsar=1", targetW, targetH)
			dec.orientation = "vertical:scale"
		} else {
			dec.orientation = "vertical:passthrough"
		}
	}
	dec.needsReencode = dec.opts.videoFilter != ""
	return dec
}

// verticalFillFilter returns the -vf chain that converts a landscape source to a vertical canvas.
func verticalFillFilter(fill string, targetW, targetH int) string {
	switch fill {
	case "crop":
		return fmt.Sprintf("crop=ih*%d/%d:ih,scale=%d:%d,setsar=1", targetW, targetH, targetW, targetH)
	case "none":
		return ""
	default: // blur: blurred full-frame background + centered foreground (mainstream 直播切片 look)
		return fmt.Sprintf("split[bg][fg];[bg]scale=%d:%d,boxblur=20:5[bg];[fg]scale=%d:-2[fg];[bg][fg]overlay=(W-w)/2:(H-h)/2", targetW, targetH, targetW)
	}
}

func fillLabel(fill string) string {
	switch fill {
	case "crop":
		return "crop"
	case "none":
		return "none"
	default:
		return "blur"
	}
}

// buildEncodeArgs assembles the accurate (re-encode) ffmpeg command. Output is always the last arg
// so callers can safely reference args[len-1] without index surgery.
func buildEncodeArgs(start, duration float64, videoPath, output string, opts encodeOptions) []string {
	args := []string{"ffmpeg", "-y", "-ss", formatSeconds(start), "-i", videoPath, "-t", formatSeconds(duration)}
	if opts.videoFilter != "" {
		args = append(args, "-vf", opts.videoFilter)
	}
	if opts.audioFilter != "" {
		args = append(args, "-af", opts.audioFilter)
	}
	args = append(args, "-c:v", "libx264")
	if opts.preset != "" {
		args = append(args, "-preset", opts.preset)
	}
	if opts.crf != "" {
		args = append(args, "-crf", opts.crf)
	}
	if opts.pixFmt != "" {
		args = append(args, "-pix_fmt", opts.pixFmt)
	}
	if opts.audioCodec != "" {
		args = append(args, "-c:a", opts.audioCodec)
	}
	if opts.audioBitrate != "" {
		args = append(args, "-b:a", opts.audioBitrate)
	}
	if opts.faststart {
		args = append(args, "-movflags", "+faststart")
	}
	return append(args, output)
}

// BuildLiveClipPlan converts LLM segment indexes into concrete clip timings and ffmpeg commands.
func (s *LiveSliceService) BuildLiveClipPlan(req LiveClipPlanRequest) (*LiveClipPlanResult, error) {
	if strings.TrimSpace(req.VideoPath) == "" {
		return nil, fmt.Errorf("video_path is required")
	}
	if strings.TrimSpace(req.OutputDir) == "" {
		return nil, fmt.Errorf("output_dir is required")
	}
	if len(req.Sentences) == 0 {
		return nil, fmt.Errorf("sentences is required")
	}
	if len(req.Segments) == 0 {
		return nil, fmt.Errorf("segments is required")
	}
	sentences, byIndex, minDuration, maxDuration, err := prepareClipPlanningInputs(req.Sentences, req.MinDurationSeconds, req.MaxDurationSeconds, req.HeadPaddingSeconds, req.TailPaddingSeconds)
	if err != nil {
		return nil, err
	}
	invalid := liveInvalidSet(req.Invalid, byIndex)
	if err := validateSegmentIndexesForPlan(req.Segments, byIndex); err != nil {
		return nil, err
	}
	dec := computeEncodeOptions(encodePlanInput{
		targetMode:    req.TargetMode,
		verticalFill:  req.VerticalFill,
		sourceWidth:   req.SourceWidth,
		sourceHeight:  req.SourceHeight,
		targetWidth:   req.TargetWidth,
		targetHeight:  req.TargetHeight,
		normalizeLoud: req.NormalizeAudioLoudness == nil || *req.NormalizeAudioLoudness,
	})

	plan := &LiveClipPlanResult{
		Clips:    []LiveClip{},
		Rejected: []LiveRejected{},
		Warnings: []LiveClipNotice{},
	}
	usedOutputs := map[string]int{}
	for i, segment := range req.Segments {
		segmentIndex := i + 1
		title := strings.TrimSpace(segment.Title)
		if title == "" {
			title = fmt.Sprintf("clip-%02d", segmentIndex)
		}
		clipSentences, err := segmentSentenceRange(sentences, byIndex, segment)
		if err != nil {
			plan.Rejected = append(plan.Rejected, LiveRejected{Index: segmentIndex, Title: title, Reason: err.Error()})
			continue
		}
		if allInvalid(clipSentences, invalid) {
			plan.Rejected = append(plan.Rejected, LiveRejected{Index: segmentIndex, Title: title, Reason: "segment contains only invalid sentences"})
			continue
		}
		if invalidIndexes := invalidSentenceIndexes(clipSentences, invalid); len(invalidIndexes) > 0 {
			plan.Warnings = append(plan.Warnings, LiveClipNotice{
				Index:  segmentIndex,
				Title:  title,
				Reason: fmt.Sprintf("segment includes invalid sentences: %s", joinInt64(invalidIndexes)),
			})
		}
		start := clipSentences[0].Start - req.HeadPaddingSeconds
		if start < 0 {
			start = 0
		}
		end := clipSentences[len(clipSentences)-1].End + req.TailPaddingSeconds
		duration := end - start
		if duration <= 0 {
			plan.Rejected = append(plan.Rejected, LiveRejected{Index: segmentIndex, Title: title, Reason: "segment duration must be positive"})
			continue
		}
		start = roundMillis(start)
		end = roundMillis(end)
		duration = roundMillis(duration)
		if duration < minDuration {
			plan.Warnings = append(plan.Warnings, LiveClipNotice{Index: segmentIndex, Title: title, Reason: fmt.Sprintf("duration %.3fs is shorter than %.3fs", duration, minDuration)})
		}
		if duration > maxDuration {
			plan.Warnings = append(plan.Warnings, LiveClipNotice{Index: segmentIndex, Title: title, Reason: fmt.Sprintf("duration %.3fs is longer than %.3fs", duration, maxDuration)})
		}
		output := uniqueClipOutput(req.OutputDir, segmentIndex, title, usedOutputs)
		fastArgs := []string{"ffmpeg", "-y", "-ss", formatSeconds(start), "-i", req.VideoPath, "-t", formatSeconds(duration), "-c", "copy", output}
		accurateArgs := buildEncodeArgs(start, duration, req.VideoPath, output, dec.opts)
		method := "copy"
		fastShellField := shellJoin(fastArgs)
		fastArgsField := fastArgs
		if dec.needsReencode {
			// A video filter (orientation transform) forces re-encoding; fast stream-copy is invalid.
			method = "encode"
			fastShellField = ""
			fastArgsField = nil
		}
		part := LiveClipPart{
			PartIndex:        1,
			SentenceStart:    segment.Start,
			SentenceEnd:      segment.End,
			Start:            start,
			End:              end,
			Duration:         duration,
			Output:           output,
			AccurateCutShell: shellJoin(accurateArgs),
			AccurateCutArgs:  accurateArgs,
			Transcript:       clipSentences,
		}
		plan.Clips = append(plan.Clips, LiveClip{
			Index:            segmentIndex,
			Title:            title,
			Description:      segment.Description,
			Thoughts:         segment.Thoughts,
			SentenceStart:    segment.Start,
			SentenceEnd:      segment.End,
			Start:            start,
			End:              end,
			Duration:         duration,
			Output:           output,
			FastCutShell:     fastShellField,
			AccurateCutShell: shellJoin(accurateArgs),
			FastCutArgs:      fastArgsField,
			AccurateCutArgs:  accurateArgs,
			Parts:            []LiveClipPart{part},
			Transcript:       clipSentences,
			Orientation:      dec.orientation,
			VerticalFilter:   dec.opts.videoFilter,
			Method:           method,
			Status:           "planned",
		})
	}
	return plan, nil
}

// BuildLiveSubjectClipPlan converts completed subject scripts into deterministic clip and concat commands.
func (s *LiveSliceService) BuildLiveSubjectClipPlan(req LiveSubjectClipPlanRequest) (*LiveClipPlanResult, error) {
	if strings.TrimSpace(req.VideoPath) == "" {
		return nil, fmt.Errorf("video_path is required")
	}
	if strings.TrimSpace(req.OutputDir) == "" {
		return nil, fmt.Errorf("output_dir is required")
	}
	if len(req.Sentences) == 0 {
		return nil, fmt.Errorf("sentences is required")
	}
	if len(req.Completions) == 0 {
		return nil, fmt.Errorf("completions is required")
	}
	sentences, byIndex, minDuration, maxDuration, err := prepareClipPlanningInputs(req.Sentences, req.MinDurationSeconds, req.MaxDurationSeconds, req.HeadPaddingSeconds, req.TailPaddingSeconds)
	if err != nil {
		return nil, err
	}
	invalid := liveInvalidSet(req.Invalid, byIndex)
	dec := computeEncodeOptions(encodePlanInput{
		targetMode:    req.TargetMode,
		verticalFill:  req.VerticalFill,
		sourceWidth:   req.SourceWidth,
		sourceHeight:  req.SourceHeight,
		targetWidth:   req.TargetWidth,
		targetHeight:  req.TargetHeight,
		normalizeLoud: req.NormalizeAudioLoudness == nil || *req.NormalizeAudioLoudness,
	})
	plan := &LiveClipPlanResult{
		Clips:    []LiveClip{},
		Rejected: []LiveRejected{},
		Warnings: []LiveClipNotice{},
	}
	usedOutputs := map[string]int{}
	for i, completion := range req.Completions {
		clipIndex := i + 1
		title := strings.TrimSpace(completion.Title)
		if title == "" {
			title = fmt.Sprintf("subject-%02d", clipIndex)
		}
		clip, notices, err := buildSubjectClip(sentences, byIndex, invalid, req, completion, clipIndex, title, usedOutputs, dec)
		if err != nil {
			plan.Rejected = append(plan.Rejected, LiveRejected{Index: clipIndex, Title: title, Reason: err.Error()})
			continue
		}
		plan.Warnings = append(plan.Warnings, notices...)
		if clip.Duration < minDuration {
			plan.Warnings = append(plan.Warnings, LiveClipNotice{Index: clipIndex, Title: title, Reason: fmt.Sprintf("duration %.3fs is shorter than %.3fs", clip.Duration, minDuration)})
		}
		if clip.Duration > maxDuration {
			plan.Warnings = append(plan.Warnings, LiveClipNotice{Index: clipIndex, Title: title, Reason: fmt.Sprintf("duration %.3fs is longer than %.3fs", clip.Duration, maxDuration)})
		}
		plan.Clips = append(plan.Clips, clip)
	}
	return plan, nil
}

// BuildLiveClipManifest creates deterministic manifest and Markdown exports from clip results.
func (s *LiveSliceService) BuildLiveClipManifest(req LiveClipManifestRequest) (*LiveClipManifestResult, error) {
	clips := map[int]LiveClip{}
	for _, clip := range req.Clips {
		if clip.Index <= 0 {
			return nil, fmt.Errorf("clips index must be positive")
		}
		if _, exists := clips[clip.Index]; exists {
			return nil, fmt.Errorf("duplicate clips index %d", clip.Index)
		}
		clips[clip.Index] = clip
	}
	results := map[int]LiveClipExecutionResult{}
	for _, result := range req.ClipResults {
		if result.Index <= 0 {
			return nil, fmt.Errorf("clip_results index must be positive")
		}
		if _, exists := results[result.Index]; exists {
			return nil, fmt.Errorf("duplicate clip_results index %d", result.Index)
		}
		results[result.Index] = result
	}
	for _, clip := range req.Clips {
		if _, ok := results[clip.Index]; !ok {
			return nil, fmt.Errorf("missing clip_results for clip index %d", clip.Index)
		}
	}
	for index := range results {
		if _, ok := clips[index]; !ok {
			return nil, fmt.Errorf("clip_results index %d does not match any clip", index)
		}
	}

	manifest := make([]LiveClipManifestItem, 0, len(req.Clips))
	clipNotes := make([]LiveClipNoteMarkdown, 0, len(req.Clips))
	success := 0
	failed := 0
	for _, clip := range req.Clips {
		exec := results[clip.Index]
		status := exec.Status
		if status == "" {
			return nil, fmt.Errorf("clip_results index %d status is required", clip.Index)
		}
		status = strings.ToLower(status)
		if strings.TrimSpace(exec.Output) == "" {
			return nil, fmt.Errorf("clip_results index %d output is required", clip.Index)
		}
		if cleanPath(exec.Output) != cleanPath(clip.Output) {
			return nil, fmt.Errorf("clip_results index %d output must match planned output %q, got %q", clip.Index, clip.Output, exec.Output)
		}
		method := exec.Method
		if method == "" {
			method = clip.Method
		}
		method = strings.ToLower(method)
		if method != "" && !liveClipMethodValid(method) {
			return nil, fmt.Errorf("clip_results index %d method must be copy, encode, or concat, got %q", clip.Index, method)
		}
		if liveClipStatusOK(status) {
			if exec.Size <= 0 {
				return nil, fmt.Errorf("clip_results index %d size must be positive when status is %q", clip.Index, status)
			}
			if exec.ActualDurationSeconds <= 0 {
				return nil, fmt.Errorf("clip_results index %d actual_duration_seconds must be positive when status is %q", clip.Index, status)
			}
			if err := validateClipActualDuration(clip, exec.ActualDurationSeconds); err != nil {
				return nil, err
			}
			success++
		} else if liveClipStatusFailed(status) {
			if exec.ExitCode == 0 && strings.TrimSpace(exec.Error) == "" {
				return nil, fmt.Errorf("clip_results index %d failed result must include error or non-zero exit_code", clip.Index)
			}
			failed++
		} else {
			return nil, fmt.Errorf("clip_results index %d status must be ok or failed, got %q", clip.Index, status)
		}
		if err := validateClipPartExecutionResults(clip, exec); err != nil {
			return nil, err
		}
		transcript, err := rebuildClipTranscript(req.Sentences, clip)
		if err != nil {
			return nil, err
		}
		item := LiveClipManifestItem{
			Index:                 clip.Index,
			Title:                 clip.Title,
			Description:           clip.Description,
			Thoughts:              clip.Thoughts,
			SentenceStart:         clip.SentenceStart,
			SentenceEnd:           clip.SentenceEnd,
			Start:                 clip.Start,
			End:                   clip.End,
			Duration:              clip.Duration,
			Output:                exec.Output,
			Method:                method,
			Status:                status,
			ExitCode:              exec.ExitCode,
			Error:                 exec.Error,
			Size:                  exec.Size,
			ActualDurationSeconds: exec.ActualDurationSeconds,
			Parts:                 clip.Parts,
			Transcript:            transcript,
			ScriptNotes:           clip.ScriptNotes,
		}
		manifest = append(manifest, item)
		clipNotes = append(clipNotes, LiveClipNoteMarkdown{
			Index:        item.Index,
			Title:        item.Title,
			MarkdownPath: markdownSidecarPath(item.Output),
			Markdown:     buildClipNoteMarkdown(item),
		})
	}
	title := strings.TrimSpace(req.AnalysisTitle)
	if title == "" {
		title = "直播切片"
	}
	transcriptMarkdown := buildTranscriptMarkdown(title, sortedLiveSentences(req.Sentences))
	summaryMarkdown := buildLiveClipSummaryMarkdown(req, manifest, success, failed)
	return &LiveClipManifestResult{
		ClipManifest:       manifest,
		ClipNotesMarkdown:  clipNotes,
		TranscriptMarkdown: transcriptMarkdown,
		SummaryMarkdown:    summaryMarkdown,
	}, nil
}

// RecognizeLiveSubjects asks the LLM for short-video topics supported by the transcript.
func (s *LiveSliceService) RecognizeLiveSubjects(ctx context.Context, sentences []LiveSentence) ([]LiveSubject, error) {
	if s.llm == nil {
		return nil, fmt.Errorf("LLM client is not configured")
	}
	if len(sentences) == 0 {
		return nil, fmt.Errorf("sentences is required")
	}
	userPrompt := buildLiveSentencePrompt(sentences, "")
	raw, err := s.llm.Complete(ctx, liveSubjectSystemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("recognize live subjects: %w", err)
	}
	extracted, err := extractJSONObject(raw)
	if err != nil {
		return nil, fmt.Errorf("extract live subjects JSON: %w", err)
	}
	var result struct {
		Subjects []LiveSubject `json:"subjects"`
	}
	if err := json.Unmarshal([]byte(extracted), &result); err != nil {
		return nil, fmt.Errorf("unmarshal live subjects JSON: %w", err)
	}
	return result.Subjects, nil
}

// RecognizeLiveInvalidSentences marks sentences that should not be used for short-video slices.
func (s *LiveSliceService) RecognizeLiveInvalidSentences(ctx context.Context, sentences []LiveSentence) ([]LiveInvalid, error) {
	if s.llm == nil {
		return nil, fmt.Errorf("LLM client is not configured")
	}
	if len(sentences) == 0 {
		return nil, fmt.Errorf("sentences is required")
	}
	const batchSize = 30
	invalid := []LiveInvalid{}
	for start := 0; start < len(sentences); start += batchSize {
		end := start + batchSize
		if end > len(sentences) {
			end = len(sentences)
		}
		raw, err := s.llm.Complete(ctx, liveInvalidSystemPrompt, buildLiveSentencePrompt(sentences[start:end], ""))
		if err != nil {
			return nil, fmt.Errorf("recognize live invalid sentences: %w", err)
		}
		extracted, err := extractJSONObject(raw)
		if err != nil {
			return nil, fmt.Errorf("extract live invalid JSON: %w", err)
		}
		var result struct {
			Invalid []LiveInvalid `json:"invalid"`
		}
		if err := json.Unmarshal([]byte(extracted), &result); err != nil {
			return nil, fmt.Errorf("unmarshal live invalid JSON: %w", err)
		}
		invalid = append(invalid, result.Invalid...)
	}
	return normalizeLiveInvalids(invalid, sentences), nil
}

// RecognizeLiveSegments asks the LLM for a practical segmentation plan.
func (s *LiveSliceService) RecognizeLiveSegments(ctx context.Context, sentences []LiveSentence, ask string) ([]LiveSegment, error) {
	if s.llm == nil {
		return nil, fmt.Errorf("LLM client is not configured")
	}
	if len(sentences) == 0 {
		return nil, fmt.Errorf("sentences is required")
	}
	raw, err := s.llm.Complete(ctx, liveSegmentsSystemPrompt, buildLiveSentencePrompt(sentences, ask))
	if err != nil {
		return nil, fmt.Errorf("recognize live segments: %w", err)
	}
	extracted, err := extractJSONObject(raw)
	if err != nil {
		return nil, fmt.Errorf("extract live segments JSON: %w", err)
	}
	var result struct {
		Segments []LiveSegment `json:"segments"`
	}
	if err := json.Unmarshal([]byte(extracted), &result); err != nil {
		return nil, fmt.Errorf("unmarshal live segments JSON: %w", err)
	}
	if err := validateLiveSegments(result.Segments, sentences, nil); err != nil {
		return nil, fmt.Errorf("validate live segments: %w", err)
	}
	return result.Segments, nil
}

// CompleteLiveSubject creates a detailed short-video script from a selected subject or ask.
func (s *LiveSliceService) CompleteLiveSubject(ctx context.Context, sentences []LiveSentence, ask, subject, thoughts string) (*LiveSubjectCompletion, error) {
	if s.llm == nil {
		return nil, fmt.Errorf("LLM client is not configured")
	}
	if len(sentences) == 0 {
		return nil, fmt.Errorf("sentences is required")
	}
	extra := strings.TrimSpace(ask)
	if strings.TrimSpace(subject) != "" {
		extra += "\n主题: " + subject
	}
	if strings.TrimSpace(thoughts) != "" {
		extra += "\n思考: " + thoughts
	}
	raw, err := s.llm.Complete(ctx, liveCompleteSubjectSystemPrompt, buildLiveSentencePrompt(sentences, extra))
	if err != nil {
		return nil, fmt.Errorf("complete live subject: %w", err)
	}
	extracted, err := extractJSONObject(raw)
	if err != nil {
		return nil, fmt.Errorf("extract live subject completion JSON: %w", err)
	}
	var result LiveSubjectCompletion
	if err := json.Unmarshal([]byte(extracted), &result); err != nil {
		return nil, fmt.Errorf("unmarshal live subject completion JSON: %w", err)
	}
	if err := validateCompletionIndexes(&result, sentences); err != nil {
		return nil, fmt.Errorf("validate live subject completion: %w", err)
	}
	fillCompletionTiming(&result, sentences)
	return &result, nil
}

func fillCompletionTiming(result *LiveSubjectCompletion, source []LiveSentence) {
	byIndex := make(map[int64]LiveSentence, len(source))
	for _, sentence := range source {
		byIndex[sentence.Index] = sentence
	}
	for i := range result.Sentences {
		if result.Sentences[i].Index == 0 {
			continue
		}
		if src, ok := byIndex[result.Sentences[i].Index]; ok {
			if result.Sentences[i].Start == 0 {
				result.Sentences[i].Start = src.Start
			}
			if result.Sentences[i].End == 0 {
				result.Sentences[i].End = src.End
			}
			if strings.TrimSpace(result.Sentences[i].Text) == "" {
				result.Sentences[i].Text = src.Text
			}
		}
	}
}

func prepareClipPlanningInputs(sentences []LiveSentence, minDuration, maxDuration, headPadding, tailPadding float64) ([]LiveSentence, map[int64]LiveSentence, float64, float64, error) {
	if headPadding < 0 || tailPadding < 0 {
		return nil, nil, 0, 0, fmt.Errorf("padding seconds cannot be negative")
	}
	if minDuration <= 0 {
		minDuration = defaultMinClipLength
	}
	if maxDuration <= 0 {
		maxDuration = defaultMaxClipLength
	}
	if minDuration > maxDuration {
		return nil, nil, 0, 0, fmt.Errorf("min_duration_seconds must be <= max_duration_seconds")
	}
	seen := map[int64]bool{}
	for i, sentence := range sentences {
		if sentence.Index <= 0 {
			return nil, nil, 0, 0, fmt.Errorf("sentences[%d].index must be positive", i)
		}
		if seen[sentence.Index] {
			return nil, nil, 0, 0, fmt.Errorf("duplicate sentence index %d", sentence.Index)
		}
		seen[sentence.Index] = true
		if sentence.Start < 0 {
			return nil, nil, 0, 0, fmt.Errorf("sentences[%d] start must be >= 0", i)
		}
		if sentence.End <= sentence.Start {
			return nil, nil, 0, 0, fmt.Errorf("sentences[%d] end must be greater than start", i)
		}
	}
	sorted := sortedLiveSentences(sentences)
	return sorted, liveSentenceMap(sorted), minDuration, maxDuration, nil
}

func validateSegmentIndexesForPlan(segments []LiveSegment, byIndex map[int64]LiveSentence) error {
	for _, segment := range segments {
		if _, ok := byIndex[segment.Start]; !ok {
			return fmt.Errorf("start index %d does not exist in sentences", segment.Start)
		}
		if _, ok := byIndex[segment.End]; !ok {
			return fmt.Errorf("end index %d does not exist in sentences", segment.End)
		}
		if segment.Start > segment.End {
			return fmt.Errorf("start index %d must be <= end index %d", segment.Start, segment.End)
		}
	}
	return nil
}

func buildSubjectClip(sentences []LiveSentence, byIndex map[int64]LiveSentence, invalid map[int64]bool, req LiveSubjectClipPlanRequest, completion LiveSubjectCompletion, clipIndex int, title string, usedOutputs map[string]int, dec encodeDecision) (LiveClip, []LiveClipNotice, error) {
	scriptNotes := []LiveSentence{}
	sourceIndexes := []int64{}
	seen := map[int64]bool{}
	for _, item := range completion.Sentences {
		if item.Index == 0 {
			if strings.TrimSpace(item.Text) != "" || strings.TrimSpace(item.Reason) != "" {
				scriptNotes = append(scriptNotes, item)
			}
			continue
		}
		if seen[item.Index] {
			return LiveClip{}, nil, fmt.Errorf("duplicate sentence index %d", item.Index)
		}
		source, ok := byIndex[item.Index]
		if !ok {
			return LiveClip{}, nil, fmt.Errorf("sentence index %d does not exist in source sentences", item.Index)
		}
		if source.End <= source.Start {
			return LiveClip{}, nil, fmt.Errorf("sentence index %d duration must be positive", item.Index)
		}
		seen[item.Index] = true
		sourceIndexes = append(sourceIndexes, item.Index)
	}
	if len(sourceIndexes) == 0 {
		return LiveClip{}, nil, fmt.Errorf("subject clip contains no source sentences")
	}
	orderedSentences := make([]LiveSentence, 0, len(sourceIndexes))
	for _, index := range sourceIndexes {
		orderedSentences = append(orderedSentences, byIndex[index])
	}
	if allInvalid(orderedSentences, invalid) {
		return LiveClip{}, nil, fmt.Errorf("subject clip contains only invalid sentences")
	}
	notices := []LiveClipNotice{}
	if invalidIndexes := invalidSentenceIndexes(orderedSentences, invalid); len(invalidIndexes) > 0 {
		notices = append(notices, LiveClipNotice{
			Index:  clipIndex,
			Title:  title,
			Reason: fmt.Sprintf("subject clip includes invalid sentences: %s", joinInt64(invalidIndexes)),
		})
	}
	groups := groupSubjectIndexes(sourceIndexes)
	output := uniqueClipOutput(req.OutputDir, clipIndex, title, usedOutputs)
	partsDir := filepath.Join(filepath.Dir(output), ".parts")
	partOutputCounts := map[string]int{}
	parts := make([]LiveClipPart, 0, len(groups))
	clipTranscript := []LiveSentence{}
	duration := 0.0
	for i, group := range groups {
		partSentences := make([]LiveSentence, 0, len(group))
		for _, index := range group {
			partSentences = append(partSentences, byIndex[index])
		}
		start := partSentences[0].Start - req.HeadPaddingSeconds
		if start < 0 {
			start = 0
		}
		end := partSentences[len(partSentences)-1].End + req.TailPaddingSeconds
		partDuration := end - start
		if partDuration <= 0 {
			return LiveClip{}, nil, fmt.Errorf("subject clip part %d duration must be positive", i+1)
		}
		start = roundMillis(start)
		end = roundMillis(end)
		partDuration = roundMillis(partDuration)
		partOutput := uniquePartOutput(partsDir, clipIndex, i+1, title, partOutputCounts)
		accurateArgs := buildEncodeArgs(start, partDuration, req.VideoPath, partOutput, dec.opts)
		parts = append(parts, LiveClipPart{
			PartIndex:        i + 1,
			SentenceStart:    group[0],
			SentenceEnd:      group[len(group)-1],
			Start:            start,
			End:              end,
			Duration:         partDuration,
			Output:           partOutput,
			AccurateCutShell: shellJoin(accurateArgs),
			AccurateCutArgs:  accurateArgs,
			Transcript:       partSentences,
		})
		clipTranscript = append(clipTranscript, partSentences...)
		duration = roundMillis(duration + partDuration)
	}
	clip := LiveClip{
		Index:         clipIndex,
		Title:         title,
		Thoughts:      completion.Thoughts,
		SentenceStart: groups[0][0],
		SentenceEnd:   groups[len(groups)-1][len(groups[len(groups)-1])-1],
		Start:         parts[0].Start,
		End:           parts[len(parts)-1].End,
		Duration:      duration,
		Output:        output,
		Parts:         parts,
		Transcript:    clipTranscript,
		ScriptNotes:   scriptNotes,
		Status:        "planned",
	}
	if len(parts) == 1 {
		// Rebuild the encode command targeting the final output path (parts[0] originally wrote to a .parts path).
		accurateArgs := buildEncodeArgs(parts[0].Start, parts[0].Duration, req.VideoPath, output, dec.opts)
		accurateShell := shellJoin(accurateArgs)
		clip.Output = output
		clip.AccurateCutArgs = accurateArgs
		clip.AccurateCutShell = accurateShell
		clip.Parts[0].Output = output
		clip.Parts[0].AccurateCutArgs = accurateArgs
		clip.Parts[0].AccurateCutShell = accurateShell
		if dec.needsReencode {
			// Orientation transform forces re-encoding; expose no fast (stream-copy) command.
			clip.FastCutArgs = nil
			clip.FastCutShell = ""
		} else {
			// No transform: alias fast to the encode command so the agent's fast-then-accurate loop
			// runs the single re-encode once and succeeds.
			clip.FastCutArgs = accurateArgs
			clip.FastCutShell = accurateShell
		}
		clip.Method = "encode"
		clip.Orientation = dec.orientation
		clip.VerticalFilter = dec.opts.videoFilter
		return clip, notices, nil
	}
	concatListPath := strings.TrimSuffix(output, filepath.Ext(output)) + ".concat.txt"
	concatListContent := buildConcatListContent(parts)
	// Concat final stays stream-copy: each part was already re-encoded above to a consistent
	// yuv420p profile (with the same orientation filter), so -c copy remuxes the join losslessly.
	concatArgs := []string{"ffmpeg", "-y", "-f", "concat", "-safe", "0", "-i", concatListPath, "-c", "copy", output}
	clip.Method = "concat"
	clip.Orientation = dec.orientation
	clip.VerticalFilter = dec.opts.videoFilter
	clip.ConcatListPath = concatListPath
	clip.ConcatListContent = concatListContent
	clip.ConcatArgs = concatArgs
	clip.ConcatShell = shellJoin(concatArgs)
	return clip, notices, nil
}

func groupSubjectIndexes(indexes []int64) [][]int64 {
	groups := [][]int64{}
	for _, index := range indexes {
		if len(groups) == 0 {
			groups = append(groups, []int64{index})
			continue
		}
		last := groups[len(groups)-1]
		if index == last[len(last)-1]+1 {
			groups[len(groups)-1] = append(last, index)
			continue
		}
		groups = append(groups, []int64{index})
	}
	return groups
}

func uniquePartOutput(partsDir string, clipIndex, partIndex int, title string, used map[string]int) string {
	base := fmt.Sprintf("%02d-%s-part-%02d.mp4", clipIndex, safeClipSlug(title), partIndex)
	output := filepath.Join(partsDir, base)
	if used[output] == 0 {
		used[output] = 1
		return output
	}
	used[output]++
	ext := filepath.Ext(output)
	stem := strings.TrimSuffix(output, ext)
	return fmt.Sprintf("%s-%d%s", stem, used[output], ext)
}

func buildConcatListContent(parts []LiveClipPart) string {
	var b strings.Builder
	for _, part := range parts {
		fmt.Fprintf(&b, "file %s\n", ffmpegConcatFileQuote(part.Output))
	}
	return b.String()
}

func ffmpegConcatFileQuote(path string) string {
	escaped := strings.ReplaceAll(path, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `'`, `'\''`)
	return "'" + escaped + "'"
}

func validateCompletionIndexes(result *LiveSubjectCompletion, source []LiveSentence) error {
	byIndex := liveSentenceMap(source)
	for _, sentence := range result.Sentences {
		if sentence.Index == 0 {
			continue
		}
		if _, ok := byIndex[sentence.Index]; !ok {
			return fmt.Errorf("sentence index %d does not exist in source sentences", sentence.Index)
		}
	}
	return nil
}

func buildLiveSentencePrompt(sentences []LiveSentence, ask string) string {
	var b strings.Builder
	b.WriteString("直播切片:\n\"\"\"\n")
	for _, sentence := range sentences {
		if sentence.Start != 0 || sentence.End != 0 {
			fmt.Fprintf(&b, "[%d] %.3f-%.3fs %s\n", sentence.Index, sentence.Start, sentence.End, sentence.Text)
		} else {
			fmt.Fprintf(&b, "[%d] %s\n", sentence.Index, sentence.Text)
		}
	}
	b.WriteString("\"\"\"\n")
	if strings.TrimSpace(ask) != "" {
		b.WriteString("\n剪辑要求:\n\"\"\"\n")
		b.WriteString(strings.TrimSpace(ask))
		b.WriteString("\n\"\"\"\n")
	}
	b.WriteString("\n只返回严格 JSON，不要返回 Markdown 或解释。")
	return b.String()
}

func validateLiveSegments(segments []LiveSegment, sentences []LiveSentence, invalid []LiveInvalid) error {
	if len(segments) == 0 {
		return nil
	}
	sorted := sortedLiveSentences(sentences)
	byIndex := liveSentenceMap(sorted)
	invalidSet := liveInvalidSet(invalid, byIndex)
	for i, segment := range segments {
		if segment.Start <= 0 {
			return fmt.Errorf("segments[%d].start index %d must be positive", i, segment.Start)
		}
		if segment.End <= 0 {
			return fmt.Errorf("segments[%d].end index %d must be positive", i, segment.End)
		}
		if _, ok := byIndex[segment.Start]; !ok {
			return fmt.Errorf("segments[%d].start index %d does not exist in sentences", i, segment.Start)
		}
		if _, ok := byIndex[segment.End]; !ok {
			return fmt.Errorf("segments[%d].end index %d does not exist in sentences", i, segment.End)
		}
		if segment.Start > segment.End {
			return fmt.Errorf("segments[%d].start index %d must be <= end index %d", i, segment.Start, segment.End)
		}
		inRange, err := segmentSentenceRange(sorted, byIndex, segment)
		if err != nil {
			return fmt.Errorf("segments[%d]: %w", i, err)
		}
		if allInvalid(inRange, invalidSet) {
			return fmt.Errorf("segments[%d] contains only invalid sentences", i)
		}
	}
	return nil
}

func normalizeLiveInvalids(invalid []LiveInvalid, sentences []LiveSentence) []LiveInvalid {
	byIndex := liveSentenceMap(sentences)
	seen := map[int64]bool{}
	normalized := []LiveInvalid{}
	for _, item := range invalid {
		if item.Index <= 0 || seen[item.Index] {
			continue
		}
		if _, ok := byIndex[item.Index]; !ok {
			continue
		}
		seen[item.Index] = true
		normalized = append(normalized, item)
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i].Index < normalized[j].Index })
	return normalized
}

func sortedLiveSentences(sentences []LiveSentence) []LiveSentence {
	out := append([]LiveSentence(nil), sentences...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Start == out[j].Start {
			return out[i].Index < out[j].Index
		}
		return out[i].Start < out[j].Start
	})
	return out
}

func liveSentenceMap(sentences []LiveSentence) map[int64]LiveSentence {
	byIndex := make(map[int64]LiveSentence, len(sentences))
	for _, sentence := range sentences {
		if sentence.Index > 0 {
			byIndex[sentence.Index] = sentence
		}
	}
	return byIndex
}

func liveInvalidSet(invalid []LiveInvalid, byIndex map[int64]LiveSentence) map[int64]bool {
	set := map[int64]bool{}
	for _, item := range invalid {
		if _, ok := byIndex[item.Index]; ok {
			set[item.Index] = true
		}
	}
	return set
}

func segmentSentenceRange(sentences []LiveSentence, byIndex map[int64]LiveSentence, segment LiveSegment) ([]LiveSentence, error) {
	if _, ok := byIndex[segment.Start]; !ok {
		return nil, fmt.Errorf("start index %d does not exist in sentences", segment.Start)
	}
	if _, ok := byIndex[segment.End]; !ok {
		return nil, fmt.Errorf("end index %d does not exist in sentences", segment.End)
	}
	if segment.Start > segment.End {
		return nil, fmt.Errorf("start index %d must be <= end index %d", segment.Start, segment.End)
	}
	inRange := []LiveSentence{}
	for _, sentence := range sentences {
		if sentence.Index >= segment.Start && sentence.Index <= segment.End {
			inRange = append(inRange, sentence)
		}
	}
	if len(inRange) == 0 {
		return nil, fmt.Errorf("segment contains no source sentences")
	}
	return inRange, nil
}

func allInvalid(sentences []LiveSentence, invalid map[int64]bool) bool {
	if len(sentences) == 0 {
		return false
	}
	for _, sentence := range sentences {
		if !invalid[sentence.Index] {
			return false
		}
	}
	return true
}

func invalidSentenceIndexes(sentences []LiveSentence, invalid map[int64]bool) []int64 {
	indexes := []int64{}
	for _, sentence := range sentences {
		if invalid[sentence.Index] {
			indexes = append(indexes, sentence.Index)
		}
	}
	return indexes
}

func joinInt64(values []int64) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.FormatInt(value, 10))
	}
	return strings.Join(parts, ",")
}

func liveClipStatusOK(status string) bool {
	return strings.EqualFold(status, "ok")
}

func liveClipStatusFailed(status string) bool {
	return strings.EqualFold(status, "failed")
}

func liveClipMethodValid(method string) bool {
	switch strings.ToLower(method) {
	case "copy", "encode", "concat":
		return true
	default:
		return false
	}
}

func cleanPath(path string) string {
	return filepath.Clean(strings.TrimSpace(path))
}

func validateClipPartExecutionResults(clip LiveClip, exec LiveClipExecutionResult) error {
	if len(clip.Parts) <= 1 {
		if len(exec.PartResults) > 0 {
			return fmt.Errorf("clip_results index %d has part_results but clip does not require part execution results", clip.Index)
		}
		return nil
	}
	results := map[int]LiveClipPartExecutionResult{}
	for _, result := range exec.PartResults {
		if result.PartIndex <= 0 {
			return fmt.Errorf("clip_results index %d part_results part_index must be positive", clip.Index)
		}
		if _, exists := results[result.PartIndex]; exists {
			return fmt.Errorf("duplicate part_results for clip index %d part index %d", clip.Index, result.PartIndex)
		}
		results[result.PartIndex] = result
	}
	parts := map[int]LiveClipPart{}
	for _, part := range clip.Parts {
		if part.PartIndex <= 0 {
			return fmt.Errorf("clip index %d parts part_index must be positive", clip.Index)
		}
		if _, exists := parts[part.PartIndex]; exists {
			return fmt.Errorf("duplicate parts for clip index %d part index %d", clip.Index, part.PartIndex)
		}
		parts[part.PartIndex] = part
		if _, ok := results[part.PartIndex]; !ok {
			return fmt.Errorf("missing part_results for clip index %d part index %d", clip.Index, part.PartIndex)
		}
	}
	for partIndex := range results {
		if _, ok := parts[partIndex]; !ok {
			return fmt.Errorf("part_results clip index %d part index %d does not match any planned part", clip.Index, partIndex)
		}
	}
	for _, part := range clip.Parts {
		result := results[part.PartIndex]
		status := strings.ToLower(result.Status)
		if status == "" {
			return fmt.Errorf("clip_results index %d part index %d status is required", clip.Index, part.PartIndex)
		}
		if strings.TrimSpace(result.Output) == "" {
			return fmt.Errorf("clip_results index %d part index %d output is required", clip.Index, part.PartIndex)
		}
		if cleanPath(result.Output) != cleanPath(part.Output) {
			return fmt.Errorf("clip_results index %d part index %d part output must match planned output %q, got %q", clip.Index, part.PartIndex, part.Output, result.Output)
		}
		method := strings.ToLower(result.Method)
		if method != "" && !liveClipMethodValid(method) {
			return fmt.Errorf("clip_results index %d part index %d method must be copy, encode, or concat, got %q", clip.Index, part.PartIndex, result.Method)
		}
		if liveClipStatusOK(status) {
			if result.Size <= 0 {
				return fmt.Errorf("clip_results index %d part index %d size must be positive when status is %q", clip.Index, part.PartIndex, result.Status)
			}
			if result.ActualDurationSeconds <= 0 {
				return fmt.Errorf("clip_results index %d part index %d actual_duration_seconds must be positive when status is %q", clip.Index, part.PartIndex, result.Status)
			}
			if err := validatePartActualDuration(clip.Index, part, result.ActualDurationSeconds); err != nil {
				return err
			}
		} else if liveClipStatusFailed(status) {
			if result.ExitCode == 0 && strings.TrimSpace(result.Error) == "" {
				return fmt.Errorf("clip_results index %d part index %d failed result must include error or non-zero exit_code", clip.Index, part.PartIndex)
			}
		} else {
			return fmt.Errorf("clip_results index %d part index %d status must be ok or failed, got %q", clip.Index, part.PartIndex, result.Status)
		}
	}
	return nil
}

func validateClipActualDuration(clip LiveClip, actualDuration float64) error {
	plannedDuration := clip.Duration
	if plannedDuration <= 0 {
		plannedDuration = clip.End - clip.Start
	}
	if plannedDuration <= 0 {
		return fmt.Errorf("clip index %d planned duration must be positive", clip.Index)
	}
	tolerance := math.Max(1.0, plannedDuration*0.05)
	diff := math.Abs(actualDuration - plannedDuration)
	if diff > tolerance {
		return fmt.Errorf("clip_results index %d actual_duration_seconds %.3fs differs from planned duration %.3fs by %.3fs, tolerance %.3fs", clip.Index, actualDuration, plannedDuration, diff, tolerance)
	}
	return nil
}

func validatePartActualDuration(clipIndex int, part LiveClipPart, actualDuration float64) error {
	plannedDuration := part.Duration
	if plannedDuration <= 0 {
		plannedDuration = part.End - part.Start
	}
	if plannedDuration <= 0 {
		return fmt.Errorf("clip index %d part index %d planned duration must be positive", clipIndex, part.PartIndex)
	}
	tolerance := math.Max(1.0, plannedDuration*0.05)
	diff := math.Abs(actualDuration - plannedDuration)
	if diff > tolerance {
		return fmt.Errorf("clip_results index %d part index %d actual_duration_seconds %.3fs differs from planned duration %.3fs by %.3fs, tolerance %.3fs", clipIndex, part.PartIndex, actualDuration, plannedDuration, diff, tolerance)
	}
	return nil
}

func rebuildClipTranscript(sentences []LiveSentence, clip LiveClip) ([]LiveSentence, error) {
	if len(clip.Parts) > 0 {
		sorted := sortedLiveSentences(sentences)
		byIndex := liveSentenceMap(sorted)
		transcript := []LiveSentence{}
		for _, part := range clip.Parts {
			partTranscript, err := segmentSentenceRange(sorted, byIndex, LiveSegment{Start: part.SentenceStart, End: part.SentenceEnd})
			if err != nil {
				return nil, fmt.Errorf("cannot rebuild transcript for clip index %d part index %d: %w", clip.Index, part.PartIndex, err)
			}
			transcript = append(transcript, partTranscript...)
		}
		return transcript, nil
	}
	sorted := sortedLiveSentences(sentences)
	byIndex := liveSentenceMap(sorted)
	transcript, err := segmentSentenceRange(sorted, byIndex, LiveSegment{Start: clip.SentenceStart, End: clip.SentenceEnd})
	if err != nil {
		return nil, fmt.Errorf("cannot rebuild transcript for clip index %d: %w", clip.Index, err)
	}
	return transcript, nil
}

func uniqueClipOutput(outputDir string, index int, title string, used map[string]int) string {
	base := fmt.Sprintf("%02d-%s.mp4", index, safeClipSlug(title))
	output := filepath.Join(outputDir, "exports", base)
	if used[output] == 0 {
		used[output] = 1
		return output
	}
	used[output]++
	ext := filepath.Ext(output)
	stem := strings.TrimSuffix(output, ext)
	return fmt.Sprintf("%s-%d%s", stem, used[output], ext)
}

var unsafeClipSlugChars = regexp.MustCompile(`[^0-9A-Za-z\p{Han}]+`)

func safeClipSlug(title string) string {
	slug := strings.ToLower(strings.TrimSpace(title))
	slug = unsafeClipSlugChars.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "clip"
	}
	runes := []rune(slug)
	if len(runes) > 48 {
		slug = string(runes[:48])
		slug = strings.Trim(slug, "-")
	}
	return slug
}

func shellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shellQuote(arg)
	}
	return strings.Join(quoted, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if strings.IndexFunc(s, func(r rune) bool {
		return !(r == '/' || r == '.' || r == '_' || r == '-' || r == ':' || r == '=' ||
			(r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z'))
	}) == -1 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func formatSeconds(seconds float64) string {
	return strconv.FormatFloat(roundMillis(seconds), 'f', 3, 64)
}

func roundMillis(v float64) float64 {
	return math.Round(v*1000) / 1000
}

func buildTranscriptMarkdown(title string, sentences []LiveSentence) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)
	for _, sentence := range sentences {
		fmt.Fprintf(&b, "- [%d] %.2f-%.2fs %s\n", sentence.Index, sentence.Start, sentence.End, sentence.Text)
	}
	return b.String()
}

func buildClipNoteMarkdown(item LiveClipManifestItem) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %02d %s\n\n", item.Index, item.Title)
	fmt.Fprintf(&b, "- 输出：%s\n", item.Output)
	fmt.Fprintf(&b, "- 状态：%s\n", item.Status)
	if item.Method != "" {
		fmt.Fprintf(&b, "- 方法：%s\n", item.Method)
	}
	fmt.Fprintf(&b, "- 时间：%.2f-%.2fs（%.2fs）\n", item.Start, item.End, item.Duration)
	fmt.Fprintf(&b, "- 句子范围：%d-%d\n", item.SentenceStart, item.SentenceEnd)
	if item.Description != "" {
		fmt.Fprintf(&b, "- 说明：%s\n", item.Description)
	}
	if item.Thoughts != "" {
		fmt.Fprintf(&b, "- 剪辑思路：%s\n", item.Thoughts)
	}
	if item.Error != "" {
		fmt.Fprintf(&b, "- 错误：%s\n", item.Error)
	}
	if len(item.Parts) > 0 {
		b.WriteString("\n## 裁剪片段\n\n")
		for _, part := range item.Parts {
			fmt.Fprintf(&b, "- Part %02d: %.2f-%.2fs（%.2fs）`%s`\n", part.PartIndex, part.Start, part.End, part.Duration, part.Output)
		}
	}
	if len(item.ScriptNotes) > 0 {
		b.WriteString("\n## 脚本旁白\n\n")
		for _, note := range item.ScriptNotes {
			if note.Reason != "" {
				fmt.Fprintf(&b, "- %s（%s）\n", note.Text, note.Reason)
			} else {
				fmt.Fprintf(&b, "- %s\n", note.Text)
			}
		}
	}
	b.WriteString("\n## 原文字幕\n\n")
	for _, sentence := range item.Transcript {
		fmt.Fprintf(&b, "- [%d] %.2f-%.2fs %s\n", sentence.Index, sentence.Start, sentence.End, sentence.Text)
	}
	return b.String()
}

func markdownSidecarPath(output string) string {
	ext := filepath.Ext(output)
	if ext == "" {
		return output + ".md"
	}
	return strings.TrimSuffix(output, ext) + ".md"
}

func buildLiveClipSummaryMarkdown(req LiveClipManifestRequest, manifest []LiveClipManifestItem, success, failed int) string {
	var b strings.Builder
	title := strings.TrimSpace(req.AnalysisTitle)
	if title == "" {
		title = "直播切片"
	}
	fmt.Fprintf(&b, "# %s\n\n", title)
	fmt.Fprintf(&b, "- 源视频：%s\n", req.SourceVideo)
	fmt.Fprintf(&b, "- 听悟任务：%s\n", req.TingWuTaskID)
	fmt.Fprintf(&b, "- 字幕句数：%d\n", len(req.Sentences))
	fmt.Fprintf(&b, "- 无效句数：%d\n", len(req.Invalid))
	fmt.Fprintf(&b, "- 切片结果：成功 %d 条，失败 %d 条\n\n", success, failed)
	if len(req.Warnings) > 0 {
		b.WriteString("## 需人工复核片段\n\n")
		for _, warning := range req.Warnings {
			fmt.Fprintf(&b, "- [%02d] %s：%s\n", warning.Index, warning.Title, warning.Reason)
		}
		b.WriteString("\n")
	}
	if len(req.Rejected) > 0 {
		b.WriteString("## 已拒绝片段\n\n")
		for _, rejected := range req.Rejected {
			fmt.Fprintf(&b, "- [%02d] %s：%s\n", rejected.Index, rejected.Title, rejected.Reason)
		}
		b.WriteString("\n")
	}
	b.WriteString("## 切片列表\n\n")
	for _, item := range manifest {
		fmt.Fprintf(&b, "- [%02d] %s %.2f-%.2fs %.2fs %s `%s`\n", item.Index, item.Title, item.Start, item.End, item.Duration, item.Status, item.Output)
		if item.Method == "encode" {
			b.WriteString("  - 降级：使用重编码裁剪以保证时间准确\n")
		}
		if item.Method == "concat" {
			b.WriteString("  - 降级：使用多段重编码后 concat 拼接\n")
		}
		if item.Error != "" {
			fmt.Fprintf(&b, "  - 错误：%s\n", item.Error)
		}
	}
	return b.String()
}

var liveSubjectSystemPrompt = strings.TrimSpace(`
你是直播切片选题策划。目标是从直播字幕里找出适合做短视频的单一主题。

要求:
- 主题必须有多条原始切片支撑，不要凭空扩写。
- 每个主题只表达一个核心问题、卖点或反差。
- 避开只适合直播间当场互动的内容，比如欢迎、感谢、实时福利、催单倒计时。
- 输出 3 到 8 个主题，按短视频潜力排序。

返回 JSON:
{"subjects":[{"title":"主题名","thoughts":"为什么适合剪成短视频，以及可用哪些内容支撑"}]}
`)

var liveInvalidSystemPrompt = strings.TrimSpace(`
你是短视频剪辑筛选助手。用户会给直播字幕切片，你要找出“整条都不适合用于短视频”的切片。

无效内容包括:
- 欢迎、点名、感谢、直播间实时互动。
- 只有语气词、重复垫话、没有信息量的过渡。
- 只针对直播间当场生效的优惠、库存、倒计时。

判断标准:
- 只要切片里包含可复用的知识、卖点、故事、观点或演示，就不要标为无效。
- reason 要短，说明为什么这条不能用。

返回 JSON:
{"invalid":[{"index":1,"reason":"原因"}]}
`)

var liveSegmentsSystemPrompt = strings.TrimSpace(`
你是直播内容结构分析师。你要把连续直播字幕分成便于人工检查和批量剪辑的片段。

要求:
- 片段边界必须使用用户给出的切片 index。
- 每个片段围绕一个清晰主题，尽量避免把无关话题混在一起。
- title 写成剪辑师一眼能判断内容的短标题。
- description/thoughts 说明该片段价值、适合剪法或风险。
- 不要创造不存在的内容。

返回 JSON:
{"segments":[{"title":"片段标题","description":"片段说明","thoughts":"剪辑思路","start":1,"end":8}]}
`)

var liveCompleteSubjectSystemPrompt = strings.TrimSpace(`
你是短视频文案剪辑师。你要从直播字幕中选择切片，组合成一条可剪辑的短视频脚本。

要求:
- 只能使用用户给出的切片 index，原句可以截短但不能改写事实。
- 开头 3 到 5 秒要前置爆点、反差、痛点或核心结论。
- 中段要有递进逻辑，不要随机拼贴。
- 可以加入旁白句，旁白的 index 设为 0，并在 reason 说明用途。
- 过滤欢迎、感谢、直播间实时福利等只适合直播场景的内容。

返回 JSON:
{
  "title":"短视频标题",
  "subtitle":"可选副标题",
  "thoughts":"整体剪辑思路",
  "sentences":[{"index":2,"text":"使用或截取的字幕","reason":"选择理由"}]
}
`)

// ---------------------------------------------------------------------------
// Raw TingWu result types
// ---------------------------------------------------------------------------

type TingWuTaskInfo struct {
	Status            string
	MeetingAssistance *TingWuMeetingAssistanceResult
	AutoChapters      *TingWuAutoChaptersResult
	Summarization     *TingWuSummarizationResult
	Transcription     *TingWuTranscriptionResult
	CustomPrompt      *TingWuCustomPromptResult
}

type TingWuSummarizationResult struct {
	TaskID        string              `json:"TaskId"`
	Summarization TingWuSummarization `json:"Summarization"`
}

type TingWuSummarization struct {
	ParagraphSummary          string                           `json:"ParagraphSummary"`
	ParagraphTitle            string                           `json:"ParagraphTitle"`
	ConversationalSummary     []TingWuConversationalSummary    `json:"ConversationalSummary"`
	QuestionsAnsweringSummary []TingWuQuestionAnsweringSummary `json:"QuestionsAnsweringSummary"`
	MindMapSummary            []TingWuMindMapTopic             `json:"MindMapSummary"`
}

type TingWuConversationalSummary struct {
	SpeakerID   string `json:"SpeakerId"`
	SpeakerName string `json:"SpeakerName"`
	Summary     string `json:"Summary"`
}

type TingWuQuestionAnsweringSummary struct {
	Question              string `json:"Question"`
	Answer                string `json:"Answer"`
	SentenceIDsOfQuestion []int  `json:"SentenceIdsOfQuestion"`
	SentenceIDsOfAnswer   []int  `json:"SentenceIdsOfAnswer"`
}

type TingWuMindMapTopic struct {
	Title string               `json:"Title"`
	Topic []TingWuMindMapTopic `json:"Topic"`
}

type TingWuTranscriptionResult struct {
	TaskID        string              `json:"TaskId"`
	Transcription TingWuTranscription `json:"Transcription"`
}

type TingWuTranscription struct {
	AudioInfo     TingWuAudioInfo   `json:"AudioInfo"`
	Paragraphs    []TingWuParagraph `json:"Paragraphs"`
	AudioSegments [][]float64       `json:"AudioSegments"`
}

type TingWuAudioInfo struct {
	Size       int64  `json:"Size"`
	Duration   int64  `json:"Duration"`
	SampleRate int64  `json:"SampleRate"`
	Language   string `json:"Language"`
}

type TingWuParagraph struct {
	ParagraphID string       `json:"ParagraphId"`
	SpeakerID   string       `json:"SpeakerId"`
	Words       []TingWuWord `json:"Words"`
}

type TingWuWord struct {
	ID         int64   `json:"Id"`
	SentenceID int     `json:"SentenceId"`
	Start      float64 `json:"Start"`
	End        float64 `json:"End"`
	Text       string  `json:"Text"`
}

// Sentences groups TingWu word-level results into sentence-level live slices.
func (result *TingWuTranscriptionResult) Sentences() map[int]LiveSentence {
	sentences := make(map[int]LiveSentence)
	if result == nil {
		return sentences
	}
	for _, paragraph := range result.Transcription.Paragraphs {
		for _, word := range paragraph.Words {
			sentence, ok := sentences[word.SentenceID]
			if !ok {
				sentence = LiveSentence{
					Index: int64(word.SentenceID),
					Text:  word.Text,
					Start: word.Start / 1000,
					End:   word.End / 1000,
				}
			} else {
				sentence.Text += word.Text
				if word.Start/1000 < sentence.Start {
					sentence.Start = word.Start / 1000
				}
				if word.End/1000 > sentence.End {
					sentence.End = word.End / 1000
				}
			}
			sentences[word.SentenceID] = sentence
		}
	}
	return sentences
}

type TingWuCustomPromptResult struct {
	TaskID       string                   `json:"TaskId"`
	CustomPrompt []TingWuCustomPromptItem `json:"CustomPrompt"`
}

type TingWuCustomPromptItem struct {
	Name      string `json:"Name"`
	Result    string `json:"Result"`
	Truncated bool   `json:"Truncated"`
}

type TingWuAutoChaptersResult struct {
	TaskID       string              `json:"TaskId"`
	AutoChapters []TingWuAutoChapter `json:"AutoChapters"`
}

type TingWuAutoChapter struct {
	ID       int64   `json:"Id"`
	Start    float64 `json:"Start"`
	End      float64 `json:"End"`
	Headline string  `json:"Headline"`
	Summary  string  `json:"Summary"`
}

type TingWuMeetingAssistanceResult struct {
	TaskID            string                  `json:"TaskId"`
	MeetingAssistance TingWuMeetingAssistance `json:"MeetingAssistance"`
}

type TingWuMeetingAssistance struct {
	Keywords        []string              `json:"Keywords"`
	KeySentences    []TingWuKeySentence   `json:"KeySentences"`
	Classifications TingWuClassifications `json:"Classifications"`
	Actions         []TingWuAction        `json:"Actions"`
}

type TingWuKeySentence struct {
	ID         int    `json:"Id"`
	SentenceID int    `json:"SentenceId"`
	Start      int    `json:"Start"`
	End        int    `json:"End"`
	Text       string `json:"Text"`
}

type TingWuClassifications struct {
	Interview float64 `json:"Interview"`
	Lecture   float64 `json:"Lecture"`
	Meeting   float64 `json:"Meeting"`
}

type TingWuAction struct {
	ID         int    `json:"Id"`
	SentenceID int    `json:"SentenceId"`
	Start      int    `json:"Start"`
	End        int    `json:"End"`
	Text       string `json:"Text"`
}

func taskKey() string {
	return "live-slice-" + time.Now().Format("20060102150405") + "-" + uuid.NewString()
}
