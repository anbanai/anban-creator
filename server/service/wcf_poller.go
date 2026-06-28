package service

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/wcf"
)

// WCFCursorKey is the Redis key holding the highest event id the poller has
// consumed. Persisting it prevents a restart from replaying old commands (which
// would create duplicate billable tasks). Absent Redis, the cursor is in-memory
// only and replays on restart — a documented degraded-mode caveat.
const WCFCursorKey = "anbanwriter:wcf:last_event_id"

// wcfDispatcher is the subset of the command dispatcher the poller needs,
// extracted as an interface so the poller's trust boundary can be unit-tested
// with a fake dispatcher (the concrete *WCFCommandDispatcher satisfies it).
type wcfDispatcher interface {
	Handle(ctx context.Context, binding *model.WCFBinding, event wcf.Event)
}

// WCFPoller long-polls the wcfLink sidecar for inbound WeChat messages and
// dispatches recognized commands. It is the inbound half of the WeChat bot; the
// outbound half is WCFNotifier (task terminal states) + the dispatcher's replies.
type WCFPoller struct {
	client       *wcf.Client
	repo         repository.Repository
	dispatcher   wcfDispatcher
	redis        *redis.Client // optional: nil → in-memory cursor
	logger       *zerolog.Logger
	pollInterval time.Duration
	batchLimit   int

	mu     sync.Mutex
	lastID int64
}

// NewWCFPoller constructs the poller. pollInterval defaults to 2s when <= 0;
// batchLimit defaults to 50. redis may be nil.
func NewWCFPoller(
	client *wcf.Client,
	repo repository.Repository,
	dispatcher wcfDispatcher,
	redisClient *redis.Client,
	pollInterval time.Duration,
	logger *zerolog.Logger,
) *WCFPoller {
	if pollInterval <= 0 {
		pollInterval = 2 * time.Second
	}
	return &WCFPoller{
		client:       client,
		repo:         repo,
		dispatcher:   dispatcher,
		redis:        redisClient,
		logger:       logger,
		pollInterval: pollInterval,
		batchLimit:   50,
	}
}

// Run blocks until ctx is cancelled, polling the sidecar every pollInterval.
// Safe to call as `go poller.Run(ctx)`. Errors are logged and never fatal.
func (p *WCFPoller) Run(ctx context.Context) {
	if p == nil || p.client == nil {
		return
	}
	if found, id := p.loadCursor(ctx); found {
		p.lastID = id
	} else {
		// First-ever run: fast-forward past historical events so stale commands
		// in the sidecar DB don't fire as new tasks. New commands sent after
		// this point are processed normally.
		p.lastID = p.prime(ctx)
	}
	if p.logger != nil {
		p.logger.Info().Int64("after_id", p.lastID).Dur("interval", p.pollInterval).Msg("wcf poller started")
	}

	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			if p.logger != nil {
				p.logger.Info().Msg("wcf poller stopped")
			}
			return
		case <-ticker.C:
			if err := p.poll(ctx); err != nil && p.logger != nil {
				p.logger.Warn().Err(err).Msg("wcf poll failed")
			}
		}
	}
}

// prime reads one large batch at after_id=0 WITHOUT dispatching and returns the
// max id seen, establishing a clean starting cursor. Guards against an empty
// sidecar DB (returns 0).
func (p *WCFPoller) prime(ctx context.Context) int64 {
	events, err := p.client.ListEvents(ctx, 0, 500)
	if err != nil {
		if p.logger != nil {
			p.logger.Warn().Err(err).Msg("wcf prime failed; starting from id 0")
		}
		return 0
	}
	var max int64
	for _, ev := range events {
		if ev.ID > max {
			max = ev.ID
		}
	}
	return max
}

func (p *WCFPoller) poll(ctx context.Context) error {
	events, err := p.client.ListEvents(ctx, p.lastID, p.batchLimit)
	if err != nil {
		return err
	}
	for _, ev := range events {
		// Advance the cursor BEFORE dispatching. This is at-most-once delivery:
		// a crash between the cursor write and the dispatch may drop THIS one
		// event (the user resends the command), but it can never re-dispatch an
		// event after a restart and create a duplicate billable task. For a
		// system that charges credits per task, losing a command is recoverable;
		// double-charging is not.
		p.advanceCursor(ctx, ev.ID)
		p.processEvent(ctx, ev)
	}
	return nil
}

// processEvent resolves the event's account to a binding, captures the reply
// peer from the inbound source if unknown, and dispatches the command. Non-text
// and unbound-account events are skipped. Panics are recovered so one bad event
// can never kill the poller loop.
func (p *WCFPoller) processEvent(ctx context.Context, ev wcf.Event) {
	defer func() {
		if r := recover(); r != nil && p.logger != nil {
			p.logger.Error().Interface("panic", r).Int64("event_id", ev.ID).Msg("wcf event processing panicked")
		}
	}()

	if !ev.IsInboundText() {
		return
	}
	binding, err := p.repo.WCFBindings().FindByWCFAccountID(ctx, ev.AccountID)
	if err != nil || binding == nil || binding.Status != model.WCFBindingStatusActive {
		return // account not bound to any user or binding inactive
	}

	// Trust boundary. A bound WeChat account receives messages from ALL its
	// contacts and groups, not just the 文件传输助手 self-chat the bot is meant
	// for. Without this guard, any contact texting "写文章 ..." would create a
	// billable task charged to the account owner — and the first inbound from a
	// wrong party would lock the reply channel (hijacking their notifications).
	// So: once a peer is captured, ONLY that peer is honored; the peer is
	// captured from the FIRST recognized command, so stray non-command messages
	// (or a group's chatter) can never seize the channel.
	if binding.PeerID != "" {
		if ev.FromUserID != binding.PeerID {
			return // not the trusted peer — ignore
		}
	} else {
		// Peer unset: lock it to the sender of the first recognized command.
		if parseCommand(ev.BodyText).kind == cmdUnknown {
			return
		}
		if ev.FromUserID == "" {
			return
		}
		if err := p.repo.WCFBindings().UpdatePeerID(ctx, binding.UserID, ev.FromUserID); err == nil {
			binding.PeerID = ev.FromUserID
		} else {
			// Persisting the peer lock failed — do NOT dispatch. Otherwise a
			// billable task is created with no persisted PeerID, so the reply
			// (SendToBinding) silently no-ops: the user is charged for a task
			// they never see confirmed, and the reply channel is never locked.
			// The poll cursor already advanced (at-most-once), so this command
			// is dropped rather than retried.
			if p.logger != nil {
				p.logger.Warn().Err(err).Str("user_id", binding.UserID).Msg("wcf peer capture failed; skipping dispatch")
			}
			return
		}
	}

	if p.dispatcher != nil {
		p.dispatcher.Handle(ctx, binding, ev)
	}
}

// advanceCursor moves the high-water mark forward and persists it. Older or
// duplicate ids are ignored (after_id pagination already filters them; this is
// defensive against out-of-order delivery).
func (p *WCFPoller) advanceCursor(ctx context.Context, id int64) {
	p.mu.Lock()
	if id <= p.lastID {
		p.mu.Unlock()
		return
	}
	p.lastID = id
	p.mu.Unlock()
	p.saveCursor(ctx, id)
}

func (p *WCFPoller) loadCursor(ctx context.Context) (bool, int64) {
	if p.redis == nil {
		return false, 0
	}
	val, err := p.redis.Get(ctx, WCFCursorKey).Int64()
	if err != nil {
		// redis.Nil or any error → treat as absent (prime from history).
		return false, 0
	}
	return true, val
}

func (p *WCFPoller) saveCursor(ctx context.Context, id int64) {
	if p.redis == nil {
		return
	}
	if err := p.redis.Set(ctx, WCFCursorKey, id, 0).Err(); err != nil && p.logger != nil {
		p.logger.Warn().Err(err).Msg("wcf cursor persist failed")
	}
}
