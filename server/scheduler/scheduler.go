package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
)

// Task type constants for Asynq.
const (
	TypeContentGenerate        = "content:generate"
	TypePlanTrigger            = "plan:trigger"
	TypeSeednoteDiscover       = "seednote:discover"
	TypeSeednoteCaptureMetrics = "seednote:capture_metrics"
	TypeViralAnalysis          = "viral:analyze"
)

// TaskEnqueuer abstracts the async task enqueue mechanism.
type TaskEnqueuer interface {
	Enqueue(taskType string, payload []byte) error
	EnqueueIn(taskType string, payload []byte, delay time.Duration) error
}

// AsynqClient wraps an asynq.Client for enqueuing tasks.
type AsynqClient struct {
	client  *asynq.Client
	timeout time.Duration
}

// NewAsynqClient creates a new AsynqClient with the given Redis configuration.
// timeout is applied as the asynq task Timeout for every enqueued task; pass 0
// to use the default of 60 minutes.
func NewAsynqClient(redisAddr, redisPassword string, redisDB int, timeout time.Duration) *AsynqClient {
	client := asynq.NewClient(asynq.RedisClientOpt{
		Addr:     redisAddr,
		Password: redisPassword,
		DB:       redisDB,
	})
	return &AsynqClient{client: client, timeout: timeout}
}

// effectiveTimeout returns the configured task timeout, defaulting to 60 minutes.
func (c *AsynqClient) effectiveTimeout() time.Duration {
	if c.timeout > 0 {
		return c.timeout
	}
	return 60 * time.Minute
}

// Enqueue creates an Asynq task and enqueues it.
func (c *AsynqClient) Enqueue(taskType string, payload []byte) error {
	_, err := c.client.Enqueue(
		asynq.NewTask(taskType, payload),
		asynq.MaxRetry(3),
		asynq.Timeout(c.effectiveTimeout()),
	)
	return err
}

// EnqueueIn creates an Asynq task and enqueues it with a delay.
func (c *AsynqClient) EnqueueIn(taskType string, payload []byte, delay time.Duration) error {
	_, err := c.client.Enqueue(
		asynq.NewTask(taskType, payload),
		asynq.ProcessIn(delay),
		asynq.MaxRetry(3),
		asynq.Timeout(c.effectiveTimeout()),
	)
	return err
}

// Close closes the Asynq client.
func (c *AsynqClient) Close() error {
	return c.client.Close()
}

// TaskProcessor wraps an asynq.Server and its serve mux for lifecycle management.
type TaskProcessor struct {
	server *asynq.Server
	mux    *asynq.ServeMux
}

// ContentGenerateHandler is the function signature for handling content generation tasks.
type ContentGenerateHandler func(ctx context.Context, taskID, userID string) error

// PlanTriggerHandler is the function signature for handling plan trigger tasks.
type PlanTriggerHandler func(ctx context.Context, planID string) error

// SeednoteTrackingHandler is the function signature for SeedNote tracking jobs.
type SeednoteTrackingHandler func(ctx context.Context, trackingID string) error

// ViralAnalysisHandler is the function signature for viral analysis jobs.
type ViralAnalysisHandler func(ctx context.Context, analysisID string) error

// NewTaskProcessor creates a configured Asynq task processor with registered handlers.
func NewTaskProcessor(
	contentHandler ContentGenerateHandler,
	planHandler PlanTriggerHandler,
	seednoteDiscoverHandler SeednoteTrackingHandler,
	seednoteCaptureHandler SeednoteTrackingHandler,
	viralAnalysisHandler ViralAnalysisHandler,
	redisAddr, redisPassword string,
	redisDB int,
	concurrency int,
	logger *zerolog.Logger,
) *TaskProcessor {
	mux := asynq.NewServeMux()

	mux.HandleFunc(TypeContentGenerate, func(ctx context.Context, t *asynq.Task) error {
		var payload struct {
			TaskID string `json:"task_id"`
			UserID string `json:"user_id"`
		}
		if err := json.Unmarshal(t.Payload(), &payload); err != nil {
			logger.Error().Err(err).Msg("failed to unmarshal content generate payload")
			return fmt.Errorf("unmarshal payload: %w", err)
		}
		logger.Info().
			Str("task_id", payload.TaskID).
			Str("user_id", payload.UserID).
			Msg("processing content generate task")
		return contentHandler(ctx, payload.TaskID, payload.UserID)
	})

	mux.HandleFunc(TypePlanTrigger, func(ctx context.Context, t *asynq.Task) error {
		var payload struct {
			PlanID string `json:"plan_id"`
		}
		if err := json.Unmarshal(t.Payload(), &payload); err != nil {
			logger.Error().Err(err).Msg("failed to unmarshal plan trigger payload")
			return fmt.Errorf("unmarshal payload: %w", err)
		}
		logger.Info().Str("plan_id", payload.PlanID).Msg("processing plan trigger")
		return planHandler(ctx, payload.PlanID)
	})

	mux.HandleFunc(TypeSeednoteDiscover, func(ctx context.Context, t *asynq.Task) error {
		trackingID, err := parseSeednoteTrackingPayload(t.Payload())
		if err != nil {
			logger.Error().Err(err).Msg("failed to unmarshal seednote discover payload")
			return err
		}
		logger.Info().Str("tracking_id", trackingID).Msg("processing seednote discover task")
		if seednoteDiscoverHandler == nil {
			return fmt.Errorf("seednote discover handler unavailable")
		}
		return seednoteDiscoverHandler(ctx, trackingID)
	})

	mux.HandleFunc(TypeSeednoteCaptureMetrics, func(ctx context.Context, t *asynq.Task) error {
		trackingID, err := parseSeednoteTrackingPayload(t.Payload())
		if err != nil {
			logger.Error().Err(err).Msg("failed to unmarshal seednote capture payload")
			return err
		}
		logger.Info().Str("tracking_id", trackingID).Msg("processing seednote capture metrics task")
		if seednoteCaptureHandler == nil {
			return fmt.Errorf("seednote capture handler unavailable")
		}
		return seednoteCaptureHandler(ctx, trackingID)
	})

	mux.HandleFunc(TypeViralAnalysis, func(ctx context.Context, t *asynq.Task) error {
		var payload struct {
			AnalysisID string `json:"analysis_id"`
		}
		if err := json.Unmarshal(t.Payload(), &payload); err != nil {
			logger.Error().Err(err).Msg("failed to unmarshal viral analysis payload")
			return fmt.Errorf("unmarshal payload: %w", err)
		}
		logger.Info().Str("analysis_id", payload.AnalysisID).Msg("processing viral analysis task")
		if viralAnalysisHandler == nil {
			return fmt.Errorf("viral analysis handler unavailable")
		}
		return viralAnalysisHandler(ctx, payload.AnalysisID)
	})

	srv := asynq.NewServer(
		asynq.RedisClientOpt{
			Addr:     redisAddr,
			Password: redisPassword,
			DB:       redisDB,
		},
		asynq.Config{
			Concurrency: concurrency,
			Queues: map[string]int{
				"default":  6,
				"critical": 10,
			},
			RetryDelayFunc: func(n int, err error, task *asynq.Task) time.Duration {
				// Exponential backoff: 10s, 20s, 40s, ... capped at 5 minutes.
				delay := 10 * time.Second * time.Duration(1<<uint(n))
				if delay > 5*time.Minute {
					delay = 5 * time.Minute
				}
				return delay
			},
			LogLevel: asynq.InfoLevel,
			ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, t *asynq.Task, err error) {
				logger.Error().Err(err).
					Str("type", t.Type()).
					Str("payload", string(t.Payload())).
					Msg("asynq task failed")
			}),
		},
	)

	return &TaskProcessor{server: srv, mux: mux}
}

func parseSeednoteTrackingPayload(payloadBytes []byte) (string, error) {
	var payload struct {
		TrackingID string `json:"tracking_id"`
	}
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return "", fmt.Errorf("unmarshal payload: %w", err)
	}
	if payload.TrackingID == "" {
		return "", fmt.Errorf("tracking_id is required")
	}
	return payload.TrackingID, nil
}

// Start starts the Asynq worker server (non-blocking). Call Shutdown to stop.
func (tp *TaskProcessor) Start() error {
	return tp.server.Start(tp.mux)
}

// Shutdown gracefully stops the Asynq worker server.
func (tp *TaskProcessor) Shutdown() {
	tp.server.Shutdown()
}

// validPlanTypes defines the allowed content types for plans and tasks.
var validPlanTypes = []string{model.ScopeArticle, model.ScopeSeednote}

// IsValidType checks if a content type is valid.
func IsValidType(t string) bool {
	for _, v := range validPlanTypes {
		if v == t {
			return true
		}
	}
	return false
}
