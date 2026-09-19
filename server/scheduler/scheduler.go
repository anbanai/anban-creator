package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
)

// Task type constants for Asynq.
const (
	TypeContentGenerate            = "content:generate"
	TypePlanTrigger                = "plan:trigger"
	TypeSeednoteCaptureMetrics     = "seednote:capture_metrics"
	TypeWechatCaptureMetrics       = "wechat:capture_metrics"
	TypeChannelsCaptureMetrics     = "channels:capture_metrics"
	TypeWechatPublicationPoll      = "wechat:publication_poll"
	TypeWechatPublicationReconcile = "wechat:publication_reconcile"
	TypeImageAnalyze               = "image:analyze"
)

// TaskEnqueuer abstracts the async task enqueue mechanism.
type TaskEnqueuer interface {
	Enqueue(taskType string, payload []byte) error
	EnqueueIn(taskType string, payload []byte, delay time.Duration) error
}

type asynqEnqueueClient interface {
	Enqueue(*asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error)
	Close() error
}

// AsynqClient wraps an asynq.Client for enqueuing tasks.
type AsynqClient struct {
	client  asynqEnqueueClient
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

// EnqueueUnique treats an existing task ID as a successful replay.
func (c *AsynqClient) EnqueueUnique(taskType string, payload []byte, uniqueKey string) (bool, error) {
	_, err := c.client.Enqueue(
		asynq.NewTask(taskType, payload),
		asynq.TaskID(uniqueKey),
		asynq.MaxRetry(3),
		asynq.Timeout(c.effectiveTimeout()),
	)
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		return false, nil
	}
	return err == nil, err
}

func (c *AsynqClient) EnqueueImageAnalysis(jobID string, generation int64) error {
	payload, err := json.Marshal(struct {
		JobID      string `json:"job_id"`
		Generation int64  `json:"generation"`
	}{JobID: jobID, Generation: generation})
	if err != nil {
		return err
	}
	_, err = c.client.Enqueue(
		asynq.NewTask(TypeImageAnalyze, payload),
		asynq.TaskID(fmt.Sprintf("%s:%d", jobID, generation)),
		asynq.Queue("analysis"),
		asynq.MaxRetry(3),
		asynq.Timeout(c.effectiveTimeout()),
	)
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
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

type ImageAnalysisHandler func(ctx context.Context, jobID string, generation int64) error

func (tp *TaskProcessor) RegisterImageAnalysisHandler(handler ImageAnalysisHandler, logger *zerolog.Logger) {
	if tp == nil || handler == nil {
		return
	}
	tp.mux.HandleFunc(TypeImageAnalyze, func(ctx context.Context, task *asynq.Task) error {
		var payload struct {
			JobID      string `json:"job_id"`
			Generation int64  `json:"generation"`
		}
		if err := json.Unmarshal(task.Payload(), &payload); err != nil || payload.JobID == "" || payload.Generation < 1 {
			return fmt.Errorf("invalid image analysis payload")
		}
		return handler(ctx, payload.JobID, payload.Generation)
	})
}

// ContentGenerateHandler is the function signature for handling content generation tasks.
type ContentGenerateHandler func(ctx context.Context, taskID, userID string) error

// PlanTriggerHandler is the function signature for handling plan trigger tasks.
type PlanTriggerHandler func(ctx context.Context, planID string) error

// SeednoteTrackingHandler is the function signature for SeedNote tracking jobs.
type SeednoteTrackingHandler func(ctx context.Context, trackingID string) error
type WechatTrackingHandler func(ctx context.Context, trackingID string) error
type ChannelsTrackingHandler func(ctx context.Context, trackingID string) error
type WechatPublicationPollHandler func(ctx context.Context, publicationID string) error
type WechatPublicationReconcileHandler func(ctx context.Context, projectID string) error

type WechatPublicationHandlers struct {
	Poll      WechatPublicationPollHandler
	Reconcile WechatPublicationReconcileHandler
}

// NewTaskProcessor creates a configured Asynq task processor with registered handlers.
func NewTaskProcessor(
	contentHandler ContentGenerateHandler,
	planHandler PlanTriggerHandler,
	seednoteCaptureHandler SeednoteTrackingHandler,
	wechatCaptureHandler WechatTrackingHandler,
	channelsCaptureHandler ChannelsTrackingHandler,
	redisAddr, redisPassword string,
	redisDB int,
	concurrency int,
	logger *zerolog.Logger,
	publicationHandlers ...WechatPublicationHandlers,
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

	mux.HandleFunc(TypeWechatCaptureMetrics, func(ctx context.Context, t *asynq.Task) error {
		trackingID, err := parseSeednoteTrackingPayload(t.Payload())
		if err != nil {
			logger.Error().Err(err).Msg("failed to unmarshal WeChat capture payload")
			return err
		}
		logger.Info().Str("tracking_id", trackingID).Msg("processing WeChat capture metrics task")
		if wechatCaptureHandler == nil {
			return fmt.Errorf("WeChat capture handler unavailable")
		}
		return wechatCaptureHandler(ctx, trackingID)
	})

	mux.HandleFunc(TypeChannelsCaptureMetrics, func(ctx context.Context, t *asynq.Task) error {
		trackingID, err := parseSeednoteTrackingPayload(t.Payload())
		if err != nil {
			logger.Error().Err(err).Msg("failed to unmarshal WeChat Channels capture payload")
			return err
		}
		logger.Info().Str("tracking_id", trackingID).Msg("processing WeChat Channels capture metrics task")
		if channelsCaptureHandler == nil {
			return fmt.Errorf("WeChat Channels capture handler unavailable")
		}
		return channelsCaptureHandler(ctx, trackingID)
	})
	if len(publicationHandlers) > 0 {
		handlers := publicationHandlers[0]
		mux.HandleFunc(TypeWechatPublicationPoll, func(ctx context.Context, t *asynq.Task) error {
			var payload struct {
				PublicationID string `json:"publication_id"`
			}
			if err := json.Unmarshal(t.Payload(), &payload); err != nil || payload.PublicationID == "" {
				return fmt.Errorf("invalid WeChat publication poll payload")
			}
			if handlers.Poll == nil {
				return fmt.Errorf("WeChat publication poll handler unavailable")
			}
			return handlers.Poll(ctx, payload.PublicationID)
		})
		mux.HandleFunc(TypeWechatPublicationReconcile, func(ctx context.Context, t *asynq.Task) error {
			var payload struct {
				ProjectID string `json:"project_id"`
			}
			if err := json.Unmarshal(t.Payload(), &payload); err != nil || payload.ProjectID == "" {
				return fmt.Errorf("invalid WeChat publication reconcile payload")
			}
			if handlers.Reconcile == nil {
				return fmt.Errorf("WeChat publication reconcile handler unavailable")
			}
			return handlers.Reconcile(ctx, payload.ProjectID)
		})
	}

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
				"analysis": 4,
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
