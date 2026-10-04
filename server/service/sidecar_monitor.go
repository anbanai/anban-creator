package service

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

type Readiness interface {
	Ready() bool
}

type SidecarMonitorConfig struct {
	Name               string
	HealthCheck        func(context.Context) error
	InitialBackoff     time.Duration
	MaxBackoff         time.Duration
	HealthyInterval    time.Duration
	CheckTimeout       time.Duration
	FailureLogInterval time.Duration
}

type SidecarSnapshot struct {
	Name          string    `json:"name"`
	Ready         bool      `json:"ready"`
	LastError     string    `json:"last_error,omitempty"`
	LastCheckedAt time.Time `json:"last_checked_at,omitempty"`
	LastReadyAt   time.Time `json:"last_ready_at,omitempty"`
}

type SidecarMonitor struct {
	cfg    SidecarMonitorConfig
	logger *zerolog.Logger

	mu       sync.RWMutex
	snapshot SidecarSnapshot
}

func NewSidecarMonitor(cfg SidecarMonitorConfig, logger *zerolog.Logger) *SidecarMonitor {
	if cfg.InitialBackoff <= 0 {
		cfg.InitialBackoff = time.Second
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = 30 * time.Second
	}
	if cfg.HealthyInterval <= 0 {
		cfg.HealthyInterval = 30 * time.Second
	}
	if cfg.CheckTimeout <= 0 {
		cfg.CheckTimeout = 3 * time.Second
	}
	if cfg.FailureLogInterval <= 0 {
		cfg.FailureLogInterval = 5 * time.Minute
	}
	return &SidecarMonitor{
		cfg:    cfg,
		logger: logger,
		snapshot: SidecarSnapshot{
			Name: cfg.Name,
		},
	}
}

func (m *SidecarMonitor) Ready() bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshot.Ready
}

func (m *SidecarMonitor) Snapshot() SidecarSnapshot {
	if m == nil {
		return SidecarSnapshot{}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshot
}

func (m *SidecarMonitor) Run(ctx context.Context) {
	if m == nil || m.cfg.HealthCheck == nil {
		return
	}
	backoff := m.cfg.InitialBackoff
	var unhealthy bool
	var lastError string
	var lastFailureLogAt time.Time
	var suppressedFailures uint64
	for ctx.Err() == nil {
		err := m.check(ctx)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			if unhealthy && m.logger != nil {
				m.logger.Info().Str("sidecar", m.cfg.Name).Str("previous_error", lastError).Uint64("suppressed_failures", suppressedFailures).Msg("sidecar recovered")
			}
			unhealthy = false
			suppressedFailures = 0
			backoff = m.cfg.InitialBackoff
			if !sleepContext(ctx, m.cfg.HealthyInterval) {
				return
			}
			continue
		}
		now := time.Now()
		if !unhealthy || err.Error() != lastError || now.Sub(lastFailureLogAt) >= m.cfg.FailureLogInterval {
			if m.logger != nil {
				event := m.logger.Warn().Err(err).Str("sidecar", m.cfg.Name).Dur("retry_after", backoff).Uint64("suppressed_failures", suppressedFailures)
				if unhealthy && err.Error() != lastError && suppressedFailures > 0 {
					event.Str("previous_error", lastError)
				}
				event.Msg("sidecar health check failed")
			}
			lastFailureLogAt = now
			suppressedFailures = 0
		} else {
			suppressedFailures++
		}
		unhealthy = true
		lastError = err.Error()
		if !sleepContext(ctx, backoff) {
			return
		}
		backoff *= 2
		if backoff > m.cfg.MaxBackoff {
			backoff = m.cfg.MaxBackoff
		}
	}
}

func (m *SidecarMonitor) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	checkCtx, cancel := context.WithTimeout(ctx, m.cfg.CheckTimeout)
	defer cancel()

	err := m.cfg.HealthCheck(checkCtx)
	// A parent cancellation is shutdown, not an observation about the sidecar.
	// The check's own deadline still counts as a health-check failure.
	if ctx.Err() != nil {
		return ctx.Err()
	}
	now := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()
	m.snapshot.LastCheckedAt = now
	if err != nil {
		m.snapshot.Ready = false
		m.snapshot.LastError = err.Error()
		return err
	}
	m.snapshot.Ready = true
	m.snapshot.LastError = ""
	m.snapshot.LastReadyAt = now
	return nil
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
