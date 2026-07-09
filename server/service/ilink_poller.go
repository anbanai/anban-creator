package service

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/wcf"
)

const IlinkCursorKey = "anban:ilink:last_event_id"

type ilinkEventClient interface {
	ListEvents(ctx context.Context, afterID int64, limit int) ([]wcf.Event, error)
}

type IlinkPoller struct {
	client       ilinkEventClient
	gateway      *IlinkGateway
	redis        *redis.Client
	readiness    Readiness
	logger       *zerolog.Logger
	pollInterval time.Duration
	batchLimit   int
	mu           sync.Mutex
	lastID       int64
}

func NewIlinkPoller(client ilinkEventClient, gateway *IlinkGateway, redisClient *redis.Client, pollInterval time.Duration, logger *zerolog.Logger) *IlinkPoller {
	if pollInterval <= 0 {
		pollInterval = 2 * time.Second
	}
	return &IlinkPoller{client: client, gateway: gateway, redis: redisClient, pollInterval: pollInterval, batchLimit: 50, logger: logger}
}

func (p *IlinkPoller) SetReadiness(readiness Readiness) {
	if p == nil {
		return
	}
	p.readiness = readiness
}

func (p *IlinkPoller) Run(ctx context.Context) {
	if p == nil || p.client == nil || p.gateway == nil {
		return
	}
	if found, id := p.loadCursor(ctx); found {
		p.lastID = id
	} else {
		p.lastID = p.prime(ctx)
	}
	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.poll(ctx); err != nil && p.logger != nil {
				p.logger.Warn().Err(err).Msg("ilink poll failed")
			}
		}
	}
}

func (p *IlinkPoller) prime(ctx context.Context) int64 {
	if !p.ready() {
		return 0
	}
	var max int64
	afterID := int64(0)
	for {
		events, err := p.client.ListEvents(ctx, afterID, p.batchLimit)
		if err != nil {
			return max
		}
		for _, ev := range events {
			if ev.ID > max {
				max = ev.ID
			}
		}
		if len(events) < p.batchLimit {
			break
		}
		afterID = max
	}
	return max
}

func (p *IlinkPoller) poll(ctx context.Context) error {
	if !p.ready() {
		return nil
	}
	events, err := p.client.ListEvents(ctx, p.lastID, p.batchLimit)
	if err != nil {
		return err
	}
	for _, ev := range events {
		p.advanceCursor(ctx, ev.ID)
		p.gateway.ProcessEvent(ctx, ev)
	}
	return nil
}

func (p *IlinkPoller) advanceCursor(ctx context.Context, id int64) {
	p.mu.Lock()
	if id <= p.lastID {
		p.mu.Unlock()
		return
	}
	p.lastID = id
	p.mu.Unlock()
	if p.redis != nil {
		_ = p.redis.Set(ctx, IlinkCursorKey, id, 0).Err()
	}
}

func (p *IlinkPoller) loadCursor(ctx context.Context) (bool, int64) {
	if p.redis == nil {
		return false, 0
	}
	id, err := p.redis.Get(ctx, IlinkCursorKey).Int64()
	return err == nil, id
}

func (p *IlinkPoller) ready() bool {
	return p != nil && (p.readiness == nil || p.readiness.Ready())
}
