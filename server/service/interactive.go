package service

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anbanai/anban-creator/server/agentpack"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

var (
	ErrInteractiveSessionNotFound = errors.New("interactive session not found")
	ErrInteractiveSessionOwner    = errors.New("interactive session is not owned by user")
	ErrInteractiveAgent           = errors.New("interactive agent is unavailable")
	ErrInteractiveSessionClosed   = errors.New("interactive session is closed")
	ErrInteractiveDraftInvalid    = errors.New("interactive project draft is invalid")
	ErrInteractivePending         = errors.New("a question batch is pending")
	ErrInteractiveBusy            = errors.New("interactive session is busy")
	ErrInteractiveToken           = errors.New("confirmation token is invalid or stale")
	ErrInteractiveIdempotency     = errors.New("idempotency key was already used with different content")
	ErrInteractivePersistence     = errors.New("interactive persistence is unavailable")
	ErrInteractiveEventGap        = errors.New("interactive session event history is no longer available")
)

type InteractiveAgent struct {
	ID           string   `json:"id"`
	DisplayName  string   `json:"display_name"`
	Description  string   `json:"description"`
	Capabilities []string `json:"capabilities"`
	Intents      []string `json:"intents"`
	Available    bool     `json:"available"`
}
type InteractiveEvent struct {
	ID        uint64    `json:"id"`
	Type      string    `json:"type"`
	Data      any       `json:"data,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
type InteractiveQuestion struct {
	ID          string              `json:"id"`
	Question    string              `json:"question"`
	Header      string              `json:"header,omitempty"`
	Detail      string              `json:"detail,omitempty"`
	Options     []InteractiveOption `json:"options,omitempty"`
	MultiSelect bool                `json:"multiSelect,omitempty"`
}
type InteractiveOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}
type InteractiveAnswer struct {
	ID       string   `json:"id"`
	Selected []string `json:"selected"`
	Custom   string   `json:"custom,omitempty"`
}
type InteractiveProgress map[string]string
type InteractiveProject struct {
	Name         string `json:"name"`
	Platform     string `json:"platform"`
	Instructions string `json:"instructions,omitempty"`
	Keywords     string `json:"keywords,omitempty"`
	VisualStyle  string `json:"visual_style,omitempty"`
}
type InteractiveDraft struct {
	Revision          int64                `json:"revision"`
	Project           InteractiveProject   `json:"project"`
	Profile           model.ProjectProfile `json:"profile"`
	Progress          InteractiveProgress  `json:"progress"`
	MissingFields     []string             `json:"missing_fields"`
	InferredFields    []string             `json:"inferred_fields"`
	Ready             bool                 `json:"ready"`
	ConfirmationToken string               `json:"confirmation_token,omitempty"`
}
type InteractiveStageInput struct {
	Project        InteractiveProject   `json:"project"`
	Profile        model.ProjectProfile `json:"profile"`
	Progress       InteractiveProgress  `json:"progress"`
	Ready          bool                 `json:"ready"`
	MissingFields  []string             `json:"missing_fields"`
	InferredFields []string             `json:"inferred_fields"`
}

type InteractiveSession struct {
	ID, UserID, AgentID, Intent string
	CreatedAt, ExpiresAt        time.Time
	ClosedAt                    time.Time
	Status                      string
	Draft                       InteractiveDraft
	Closed                      bool
	mu                          sync.Mutex
	nextEvent                   uint64
	events                      []InteractiveEvent
	watchers                    map[*interactiveWatcher]struct{}
	pendingCall                 string
	pendingQuestions            map[string]InteractiveQuestion
	busy                        bool
	committing                  bool
	runtime                     Runtime
	cancel                      context.CancelFunc
	runtimeTokenHash            string
	// confirmationTokenHash is server-only state. The raw token is included
	// only in the draft.ready event sent to the owning Studio client.
	confirmationTokenHash string
}
type interactiveWatcher struct {
	ch     chan InteractiveEvent
	closed bool
}
type Runtime interface {
	Command(context.Context, string, any) error
	Close() error
}
type RuntimeFactory interface {
	Start(context.Context, string, string, string, func(string, any)) (Runtime, error)
}
type InteractiveService struct {
	repo         repository.Repository
	factory      RuntimeFactory
	mu           sync.RWMutex
	sessions     map[string]*InteractiveSession
	ttl          time.Duration
	maxHistory   int
	enabled      bool
	agents       []InteractiveAgent
	adminOnly    bool
	maxSessions  int
	maxPerUser   int
	tombstoneTTL time.Duration
}

func NewInteractiveService(repo repository.Repository) *InteractiveService {
	return NewInteractiveServiceWithRuntimeAndAgents(repo, nil, interactiveAgentsFromCatalog(false))
}
func NewInteractiveServiceWithRuntime(repo repository.Repository, factory RuntimeFactory) *InteractiveService {
	return NewInteractiveServiceWithRuntimeAndAgents(repo, factory, interactiveAgentsFromCatalog(factory != nil))
}

func NewInteractiveServiceWithRuntimeAndAgents(repo repository.Repository, factory RuntimeFactory, agents []InteractiveAgent) *InteractiveService {
	return &InteractiveService{
		repo: repo, factory: factory, sessions: map[string]*InteractiveSession{},
		ttl: 30 * time.Minute, maxHistory: 256, agents: append([]InteractiveAgent(nil), agents...),
		maxSessions: 32, maxPerUser: 2, tombstoneTTL: 5 * time.Minute,
	}
}

// InteractiveAgentsFromCatalog builds the client-visible registry from the
// Server's generated Agent Pack catalog. Packs must opt into user questions.
func interactiveAgentsFromCatalog(available bool) []InteractiveAgent {
	packs := agentpack.Default().InteractivePacks()
	agents := make([]InteractiveAgent, 0, len(packs))
	for _, pack := range packs {
		if pack.Interaction == nil || !pack.Interaction.UserQuestions || strings.TrimSpace(pack.ID) == "" {
			continue
		}
		agents = append(agents, InteractiveAgent{
			ID: pack.ID, DisplayName: pack.DisplayName, Description: pack.Description,
			Capabilities: append([]string(nil), pack.Interaction.Capabilities...),
			Intents:      append([]string(nil), pack.Interaction.Intents...), Available: available,
		})
	}
	return agents
}
func (s *InteractiveService) SetEnabled(v bool)   { s.mu.Lock(); s.enabled = v; s.mu.Unlock() }
func (s *InteractiveService) SetAdminOnly(v bool) { s.mu.Lock(); s.adminOnly = v; s.mu.Unlock() }
func (s *InteractiveService) Enabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled && s.factory != nil
}
func (s *InteractiveService) Agents() []InteractiveAgent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]InteractiveAgent, len(s.agents))
	copy(out, s.agents)
	return out
}
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, e := io.ReadFull(rand.Reader, b); e != nil {
		return "", e
	}
	return hex.EncodeToString(b), nil
}

func confirmationHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func confirmationMatches(token, hash string) bool {
	return subtle.ConstantTimeCompare([]byte(confirmationHash(token)), []byte(hash)) == 1
}

func trimInteractiveEvents(events []InteractiveEvent, maxHistory int) []InteractiveEvent {
	if maxHistory <= 0 || len(events) <= maxHistory {
		return events
	}
	return append([]InteractiveEvent(nil), events[len(events)-maxHistory:]...)
}

func cloneStage(in InteractiveStageInput) (InteractiveStageInput, error) {
	raw, err := json.Marshal(in)
	if err != nil {
		return InteractiveStageInput{}, err
	}
	var copy InteractiveStageInput
	if err := json.Unmarshal(raw, &copy); err != nil {
		return InteractiveStageInput{}, err
	}
	return copy, nil
}

func (s *InteractiveService) CreateSession(_ context.Context, userID, agentID, intent string) (*InteractiveSession, error) {
	if !s.Enabled() || strings.TrimSpace(userID) == "" {
		return nil, ErrInteractiveAgent
	}
	s.mu.Lock()
	s.pruneTombstonesLocked(time.Now())
	if !s.agentAllowedLocked(agentID, intent) {
		s.mu.Unlock()
		return nil, ErrInteractiveAgent
	}
	adminOnly := s.adminOnly
	s.mu.Unlock()
	if adminOnly {
		if s.repo == nil {
			return nil, ErrInteractiveAgent
		}
		user, err := s.repo.Users().FindByID(context.Background(), userID)
		if err != nil || !user.IsAdmin {
			return nil, ErrInteractiveAgent
		}
	}
	now := time.Now()
	runtimeToken, err := randomToken()
	if err != nil {
		return nil, err
	}
	runtimeCtx, cancel := context.WithCancel(context.Background())
	sess := &InteractiveSession{ID: uuid.NewString(), UserID: userID, AgentID: agentID, Intent: intent, CreatedAt: now, ExpiresAt: now.Add(s.ttl), Status: "starting", watchers: map[*interactiveWatcher]struct{}{}, pendingQuestions: map[string]InteractiveQuestion{}, runtimeTokenHash: confirmationHash(runtimeToken), cancel: cancel}
	s.mu.Lock()
	s.pruneTombstonesLocked(time.Now())
	activeTotal, activeUser := 0, 0
	for _, existing := range s.sessions {
		existing.mu.Lock()
		if !existing.Closed {
			activeTotal++
			if existing.UserID == userID {
				activeUser++
			}
		}
		existing.mu.Unlock()
	}
	if (s.maxSessions > 0 && activeTotal >= s.maxSessions) || (s.maxPerUser > 0 && activeUser >= s.maxPerUser) {
		s.mu.Unlock()
		cancel()
		return nil, ErrInteractiveBusy
	}
	s.sessions[sess.ID] = sess
	s.mu.Unlock()
	time.AfterFunc(s.ttl, func() { s.expire(sess.ID) })
	go s.startRuntime(runtimeCtx, sess, runtimeToken)
	return sess, nil
}
func (s *InteractiveService) startRuntime(ctx context.Context, sess *InteractiveSession, runtimeToken string) {
	rt, e := s.factory.Start(ctx, sess.ID, sess.AgentID, runtimeToken, func(k string, d any) { s.runtimeEvent(sess.ID, k, d) })
	if e != nil {
		s.runtimeEvent(sess.ID, "session.error", map[string]any{"status": "runtime_failed", "message": "interactive runtime failed to start"})
		s.closeSession(sess.ID, false)
		return
	}
	sess.mu.Lock()
	if sess.Closed {
		sess.mu.Unlock()
		_ = rt.Close()
		return
	}
	sess.runtime = rt
	sess.mu.Unlock()
	if e = rt.Command(ctx, "session/start", map[string]any{"sessionId": sess.ID, "presetId": sess.AgentID}); e != nil {
		_ = rt.Close()
		s.runtimeEvent(sess.ID, "session.error", map[string]any{"status": "runtime_failed", "message": "interactive runtime failed to initialize"})
		s.closeSession(sess.ID, false)
		return
	}
}
func (s *InteractiveService) expire(id string) {
	s.closeSession(id, true)
}

func (s *InteractiveService) agentAllowedLocked(agentID, intent string) bool {
	for _, agent := range s.agents {
		if agent.ID != strings.TrimSpace(agentID) || !agent.Available {
			continue
		}
		for _, allowedIntent := range agent.Intents {
			if allowedIntent == intent {
				return true
			}
		}
	}
	return false
}

func (s *InteractiveService) closeSession(id string, emitExpired bool) {
	s.mu.RLock()
	sess := s.sessions[id]
	s.mu.RUnlock()
	if sess == nil {
		return
	}
	sess.mu.Lock()
	if sess.Closed {
		sess.mu.Unlock()
		return
	}
	if sess.committing {
		sess.mu.Unlock()
		if emitExpired {
			time.AfterFunc(100*time.Millisecond, func() { s.expire(id) })
		}
		return
	}
	var terminal *InteractiveEvent
	if emitExpired {
		// Publish the terminal event while the session is still open. The event
		// is retained for clients that reconnect before the tombstone is collected.
		sess.nextEvent++
		ev := InteractiveEvent{ID: sess.nextEvent, Type: "session.expired", Data: map[string]string{"status": "expired"}, CreatedAt: time.Now()}
		terminal = &ev
		sess.events = append(sess.events, ev)
		sess.events = trimInteractiveEvents(sess.events, s.maxHistory)
		for w := range sess.watchers {
			select {
			case w.ch <- ev:
			default:
				w.closed = true
				close(w.ch)
				delete(sess.watchers, w)
			}
		}
		sess.Status = "expired"
	}
	sess.Closed = true
	sess.ClosedAt = time.Now()
	rt := sess.runtime
	sess.runtime = nil
	cancel := sess.cancel
	sess.cancel = nil
	// Do not retain generated content or a confirmation token after the lease
	// closes. Durable commit receipts are the replay source after this point.
	sess.Draft = InteractiveDraft{}
	sess.pendingCall = ""
	sess.pendingQuestions = map[string]InteractiveQuestion{}
	sess.runtimeTokenHash = ""
	sess.confirmationTokenHash = ""
	terminalEvents := make([]InteractiveEvent, 0, 2)
	for _, event := range sess.events {
		if event.Type == "session.error" {
			terminalEvents = append(terminalEvents, event)
		}
	}
	if terminal != nil {
		terminalEvents = append(terminalEvents, *terminal)
	}
	if len(terminalEvents) > 0 {
		sess.events = terminalEvents
	} else {
		sess.events = nil
	}
	sess.mu.Unlock()
	if rt != nil {
		_ = rt.Close()
	}
	if cancel != nil {
		cancel()
	}
	s.closeWatchers(sess)
	s.scheduleTombstoneCollection(id, sess)
}

func (s *InteractiveService) scheduleTombstoneCollection(id string, sess *InteractiveSession) {
	if s.tombstoneTTL > 0 {
		time.AfterFunc(s.tombstoneTTL, func() { s.deleteTombstone(id, sess) })
	}
}

func (s *InteractiveService) deleteTombstone(id string, target *InteractiveSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current := s.sessions[id]; current == target {
		current.mu.Lock()
		closed := current.Closed
		current.mu.Unlock()
		if closed {
			delete(s.sessions, id)
		}
	}
}

func (s *InteractiveService) pruneTombstonesLocked(now time.Time) {
	for id, sess := range s.sessions {
		sess.mu.Lock()
		remove := sess.Closed && !sess.ClosedAt.IsZero() && s.tombstoneTTL > 0 && now.Sub(sess.ClosedAt) >= s.tombstoneTTL
		sess.mu.Unlock()
		if remove {
			delete(s.sessions, id)
		}
	}
}
func (s *InteractiveService) getOwned(uid, id string) (*InteractiveSession, error) {
	s.mu.RLock()
	sess := s.sessions[id]
	s.mu.RUnlock()
	if sess == nil {
		return nil, ErrInteractiveSessionNotFound
	}
	if sess.UserID != uid {
		return nil, ErrInteractiveSessionOwner
	}
	sess.mu.Lock()
	closed := sess.Closed
	expired := time.Now().After(sess.ExpiresAt)
	sess.mu.Unlock()
	if expired && !closed {
		s.expire(id)
		return nil, ErrInteractiveSessionClosed
	}
	if closed {
		return nil, ErrInteractiveSessionClosed
	}
	return sess, nil
}
func (s *InteractiveService) Session(uid, id string) (*InteractiveSession, error) {
	return s.getOwned(uid, id)
}

// SessionStatus returns the current status without exposing the mutable
// session object to HTTP handlers or other service boundaries.
func (s *InteractiveService) SessionStatus(uid, id string) (string, error) {
	sess, err := s.getOwned(uid, id)
	if err != nil {
		return "", err
	}
	sess.mu.Lock()
	status := sess.Status
	sess.mu.Unlock()
	return status, nil
}

func (s *InteractiveService) emit(sess *InteractiveSession, typ string, data any) {
	sess.mu.Lock()
	if sess.Closed {
		sess.mu.Unlock()
		return
	}
	sess.nextEvent++
	now := time.Now()
	live := InteractiveEvent{ID: sess.nextEvent, Type: typ, Data: data, CreatedAt: now}
	sess.events = append(sess.events, live)
	sess.events = trimInteractiveEvents(sess.events, s.maxHistory)
	for w := range sess.watchers {
		select {
		case w.ch <- live:
		default:
			w.closed = true
			close(w.ch)
			delete(sess.watchers, w)
		}
	}
	sess.mu.Unlock()
}
func (s *InteractiveService) Publish(id, typ string, data any) {
	s.mu.RLock()
	sess := s.sessions[id]
	s.mu.RUnlock()
	if sess != nil {
		s.emit(sess, typ, data)
	}
}
func (s *InteractiveService) closeWatchers(sess *InteractiveSession) {
	sess.mu.Lock()
	for w := range sess.watchers {
		if !w.closed {
			w.closed = true
			close(w.ch)
		}
	}
	sess.watchers = map[*interactiveWatcher]struct{}{}
	sess.mu.Unlock()
}
func (s *InteractiveService) Events(uid, id string, after uint64) ([]InteractiveEvent, <-chan InteractiveEvent, func(), error) {
	s.mu.RLock()
	sess := s.sessions[id]
	s.mu.RUnlock()
	if sess == nil {
		return nil, nil, func() {}, ErrInteractiveSessionNotFound
	}
	if sess.UserID != uid {
		return nil, nil, func() {}, ErrInteractiveSessionOwner
	}
	sess.mu.Lock()
	if !sess.Closed && time.Now().After(sess.ExpiresAt) {
		sess.mu.Unlock()
		s.expire(id)
		sess.mu.Lock()
	}
	if sess.Closed && sess.Status != "expired" && sess.Status != "error" {
		sess.mu.Unlock()
		return nil, nil, func() {}, ErrInteractiveSessionClosed
	}
	if len(sess.events) > 0 && sess.events[0].ID > 0 && after < sess.events[0].ID-1 {
		sess.mu.Unlock()
		return nil, nil, func() {}, ErrInteractiveEventGap
	}
	history := []InteractiveEvent{}
	for _, ev := range sess.events {
		if ev.ID > after {
			history = append(history, ev)
		}
	}
	if sess.Closed {
		sess.mu.Unlock()
		return history, closedInteractiveEvents(), func() {}, nil
	}
	w := &interactiveWatcher{ch: make(chan InteractiveEvent, 16)}
	sess.watchers[w] = struct{}{}
	sess.mu.Unlock()
	return history, w.ch, func() {
		sess.mu.Lock()
		if !w.closed {
			w.closed = true
			delete(sess.watchers, w)
			close(w.ch)
		}
		sess.mu.Unlock()
	}, nil
}

func closedInteractiveEvents() <-chan InteractiveEvent {
	ch := make(chan InteractiveEvent)
	close(ch)
	return ch
}

func (s *InteractiveService) runtimeEvent(id, typ string, data any) {
	s.mu.RLock()
	sess := s.sessions[id]
	s.mu.RUnlock()
	if sess == nil {
		return
	}
	switch typ {
	case "question.opened":
		if !s.openQuestions(sess, data) {
			return
		}
	case "session.status":
		if m, ok := data.(map[string]any); ok {
			if st, _ := m["status"].(string); st != "" {
				switch st {
				case "starting", "running", "waiting_answer", "runtime_failed":
					sess.mu.Lock()
					if !sess.Closed {
						sess.Status = st
					}
					sess.mu.Unlock()
				}
			}
		}
	case "runtime.exit":
		s.expire(id)
	case "session.error":
		// A bridge error means the one-shot runtime can no longer make progress.
		// Publish the diagnostic while the watcher is still attached, then close
		// the lease so the failed process cannot remain alive until TTL expiry.
		sess.mu.Lock()
		if !sess.Closed {
			sess.Status = "error"
		}
		sess.mu.Unlock()
		s.emit(sess, typ, data)
		s.closeSession(id, false)
		return
	}
	s.emit(sess, typ, data)
}

// StageDraftForRuntime is the only runtime staging entry point. Its token is
// delivered to the one-shot runtime at launch and is never returned to Studio.
func (s *InteractiveService) StageDraftForRuntime(id, runtimeToken string, in InteractiveStageInput) error {
	resolvedID, _, _, err := s.ResolveRuntimeSession(runtimeToken)
	if err != nil {
		return err
	}
	if resolvedID != id {
		return ErrInteractiveToken
	}
	return s.StageDraftForSession(id, in)
}

// ResolveRuntimeSession validates the opaque runtime bearer token and returns
// the session identity bound to it. The token itself never leaves this method.
func (s *InteractiveService) ResolveRuntimeSession(runtimeToken string) (string, string, time.Time, error) {
	runtimeToken = strings.TrimSpace(runtimeToken)
	if runtimeToken == "" {
		return "", "", time.Time{}, ErrInteractiveToken
	}
	now := time.Now()
	s.mu.RLock()
	sessions := make([]*InteractiveSession, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	s.mu.RUnlock()
	for _, sess := range sessions {
		sess.mu.Lock()
		closed := sess.Closed
		expired := now.After(sess.ExpiresAt)
		hash := sess.runtimeTokenHash
		id, userID, expiresAt := sess.ID, sess.UserID, sess.ExpiresAt
		sess.mu.Unlock()
		if !closed && !expired && hash != "" && confirmationMatches(runtimeToken, hash) {
			return id, userID, expiresAt, nil
		}
	}
	return "", "", time.Time{}, ErrInteractiveToken
}

// StageDraftForSession stages a draft for an identity already authenticated by
// the MCP transport. Runtime bearer tokens must be resolved before this point.
func (s *InteractiveService) StageDraftForSession(id string, in InteractiveStageInput) error {
	s.mu.RLock()
	sess := s.sessions[id]
	s.mu.RUnlock()
	if sess == nil {
		return ErrInteractiveSessionNotFound
	}
	sess.mu.Lock()
	wasClosed := sess.Closed
	expired := time.Now().After(sess.ExpiresAt)
	if wasClosed || expired {
		sess.mu.Unlock()
		if expired && !wasClosed {
			s.expire(id)
		}
		return ErrInteractiveSessionClosed
	}
	sess.mu.Unlock()
	return s.stage(sess, in)
}
func (s *InteractiveService) openQuestions(sess *InteractiveSession, data any) bool {
	raw, _ := json.Marshal(data)
	var p struct {
		CallID    string                `json:"call_id"`
		Questions []InteractiveQuestion `json:"questions"`
	}
	if json.Unmarshal(raw, &p) != nil || p.CallID == "" || len(p.Questions) == 0 {
		return false
	}
	seen := map[string]struct{}{}
	for _, q := range p.Questions {
		if strings.TrimSpace(q.ID) == "" || strings.TrimSpace(q.Question) == "" {
			return false
		}
		if _, ok := seen[q.ID]; ok {
			return false
		}
		seen[q.ID] = struct{}{}
		if len(q.Options) > 32 {
			return false
		}
		optionLabels := map[string]struct{}{}
		for _, option := range q.Options {
			label := strings.TrimSpace(option.Label)
			if label == "" {
				return false
			}
			if _, duplicate := optionLabels[label]; duplicate {
				return false
			}
			optionLabels[label] = struct{}{}
		}
	}
	sess.mu.Lock()
	// The runtime emits the question event while the prompt/answer command is
	// still in flight. `busy` only blocks a second user command; it must not
	// suppress the question that belongs to the command already being handled.
	if sess.Closed || sess.pendingCall != "" {
		sess.mu.Unlock()
		return false
	}
	sess.pendingCall = p.CallID
	sess.pendingQuestions = map[string]InteractiveQuestion{}
	for _, q := range p.Questions {
		if q.ID != "" {
			sess.pendingQuestions[q.ID] = q
		}
	}
	sess.Status = "waiting_answer"
	sess.mu.Unlock()
	return true
}
func decodeStage(data any) (InteractiveStageInput, bool) {
	raw, _ := json.Marshal(data)
	var in InteractiveStageInput
	if json.Unmarshal(raw, &in) != nil {
		return in, false
	}
	return in, true
}

func (s *InteractiveService) Message(ctx context.Context, uid, id, text string) error {
	sess, e := s.getOwned(uid, id)
	if e != nil {
		return e
	}
	if strings.TrimSpace(text) == "" {
		return ErrInteractiveDraftInvalid
	}
	sess.mu.Lock()
	if sess.pendingCall != "" {
		sess.mu.Unlock()
		return ErrInteractivePending
	}
	if sess.busy {
		sess.mu.Unlock()
		return ErrInteractiveBusy
	}
	sess.busy = true
	rt := sess.runtime
	sess.mu.Unlock()
	if rt == nil {
		sess.mu.Lock()
		sess.busy = false
		sess.mu.Unlock()
		return ErrInteractiveSessionClosed
	}
	s.emit(sess, "user.message", map[string]string{"text": text})
	e = rt.Command(ctx, "session/prompt", map[string]any{
		"sessionId":     sess.ID,
		"contentBlocks": []map[string]string{{"type": "text", "text": text}},
	})
	sess.mu.Lock()
	closed := sess.Closed
	sess.busy = false
	sess.mu.Unlock()
	if e == nil && closed {
		return ErrInteractiveSessionClosed
	}
	return e
}
func (s *InteractiveService) Answer(ctx context.Context, uid, id, callID string, answers []InteractiveAnswer) error {
	sess, e := s.getOwned(uid, id)
	if e != nil {
		return e
	}
	sess.mu.Lock()
	if sess.pendingCall == "" || sess.pendingCall != callID || len(answers) != len(sess.pendingQuestions) {
		sess.mu.Unlock()
		return ErrInteractiveDraftInvalid
	}
	seen := map[string]struct{}{}
	for _, a := range answers {
		q, ok := sess.pendingQuestions[a.ID]
		if !ok || strings.TrimSpace(a.ID) == "" {
			sess.mu.Unlock()
			return ErrInteractiveDraftInvalid
		}
		if _, duplicate := seen[a.ID]; duplicate {
			sess.mu.Unlock()
			return ErrInteractiveDraftInvalid
		}
		seen[a.ID] = struct{}{}
		if !q.MultiSelect && len(a.Selected) > 1 {
			sess.mu.Unlock()
			return ErrInteractiveDraftInvalid
		}
		if len(a.Selected) > 0 && strings.TrimSpace(a.Custom) != "" && !q.MultiSelect {
			sess.mu.Unlock()
			return ErrInteractiveDraftInvalid
		}
		if len([]rune(a.Custom)) > 2000 {
			sess.mu.Unlock()
			return ErrInteractiveDraftInvalid
		}
		valid := map[string]struct{}{}
		selectedValues := map[string]struct{}{}
		for _, option := range q.Options {
			valid[option.Label] = struct{}{}
		}
		for _, choice := range a.Selected {
			if _, duplicate := selectedValues[choice]; duplicate {
				sess.mu.Unlock()
				return ErrInteractiveDraftInvalid
			}
			selectedValues[choice] = struct{}{}
			if _, valid := valid[choice]; !valid {
				sess.mu.Unlock()
				return ErrInteractiveDraftInvalid
			}
		}
	}
	if sess.busy {
		sess.mu.Unlock()
		return ErrInteractiveBusy
	}
	sess.busy = true
	rt := sess.runtime
	sess.mu.Unlock()
	if rt == nil {
		sess.mu.Lock()
		sess.busy = false
		sess.mu.Unlock()
		return ErrInteractiveSessionClosed
	}
	e = rt.Command(ctx, "session/answer", map[string]any{
		"sessionId": sess.ID,
		"callId":    callID,
		"answer":    map[string]any{"answers": answers},
	})
	sess.mu.Lock()
	closed := sess.Closed
	if e == nil && !closed {
		sess.pendingCall = ""
		sess.pendingQuestions = map[string]InteractiveQuestion{}
		sess.Status = "running"
	}
	sess.busy = false
	sess.mu.Unlock()
	if e == nil && closed {
		e = ErrInteractiveSessionClosed
	}
	if e == nil {
		s.emit(sess, "question.answered", map[string]any{"call_id": callID, "answers": answers})
	}
	return e
}

func allowedInteractivePlatform(p string) bool {
	switch p {
	case model.PlatformWechat, model.PlatformSeednote, model.PlatformMoments, model.PlatformEcommerce, model.PlatformMontage, model.PlatformWhiteboardAnimation, model.PlatformHypit:
		return true
	}
	return false
}
func validateStage(in InteractiveStageInput) error {
	if len([]rune(strings.TrimSpace(in.Project.Name))) == 0 || len([]rune(in.Project.Name)) > 100 || !allowedInteractivePlatform(in.Project.Platform) {
		return ErrInteractiveDraftInvalid
	}
	if len([]rune(in.Project.Instructions)) > 20000 || len([]rune(in.Project.Keywords)) > 10000 || len([]rune(in.Project.VisualStyle)) > 10000 {
		return ErrInteractiveDraftInvalid
	}
	if err := in.Profile.Validate(); err != nil {
		return errors.Join(ErrInteractiveDraftInvalid, err)
	}
	for _, fields := range [][]string{in.MissingFields, in.InferredFields} {
		if len(fields) > 64 {
			return ErrInteractiveDraftInvalid
		}
		for _, field := range fields {
			if len([]rune(strings.TrimSpace(field))) == 0 || len([]rune(field)) > 300 {
				return ErrInteractiveDraftInvalid
			}
		}
	}
	for _, k := range []string{"identity", "style", "audience", "platforms", "preferences", "memory"} {
		v, ok := in.Progress[k]
		if !ok || (v != "not_started" && v != "collected" && v != "pending_confirmation" && v != "confirmed") {
			return fmt.Errorf("invalid progress for %s", k)
		}
	}
	return nil
}
func (s *InteractiveService) stage(sess *InteractiveSession, in InteractiveStageInput) error {
	var err error
	in, err = cloneStage(in)
	if err != nil {
		return errors.Join(ErrInteractiveDraftInvalid, err)
	}
	if err := validateStage(in); err != nil {
		return err
	}
	sess.mu.Lock()
	// Runtime staging happens during the Agent turn that is represented by
	// sess.busy. The runtime bearer token is already checked by the MCP
	// boundary, so this path must not use the user-command busy flag.
	if sess.Closed || sess.committing {
		sess.mu.Unlock()
		if sess.Closed {
			return ErrInteractiveSessionClosed
		}
		return ErrInteractiveBusy
	}
	sess.Draft.Revision++
	sess.Draft.Project = in.Project
	sess.Draft.Profile = in.Profile
	sess.Draft.Progress = in.Progress
	sess.Draft.MissingFields = append([]string(nil), in.MissingFields...)
	sess.Draft.InferredFields = append([]string(nil), in.InferredFields...)
	sess.Draft.Ready = in.Ready
	sess.Draft.ConfirmationToken = ""
	sess.confirmationTokenHash = ""
	if in.Ready {
		token, e := randomToken()
		if e != nil {
			sess.mu.Unlock()
			return e
		}
		sess.Draft.ConfirmationToken = token
		sess.confirmationTokenHash = confirmationHash(token)
	}
	draft := sess.Draft
	sess.Draft.ConfirmationToken = ""
	sess.mu.Unlock()
	// Event history is process-local and owner-authenticated. Keeping the token
	// there lets an SSE reconnect recover a ready draft after a startup race.
	s.emit(sess, "draft.updated", draft)
	if draft.Ready {
		s.emit(sess, "draft.ready", draft)
	}
	return nil
}

type InteractiveCommitRequest struct {
	ConfirmationToken string `json:"confirmation_token"`
	IdempotencyKey    string `json:"idempotency_key"`
}
type InteractiveCommitResult struct {
	Project *model.Project       `json:"project"`
	Profile model.ProjectProfile `json:"profile"`
}

func (s *InteractiveService) Commit(ctx context.Context, userID, id string, req InteractiveCommitRequest) (InteractiveCommitResult, error) {
	if strings.TrimSpace(req.IdempotencyKey) == "" || strings.TrimSpace(req.ConfirmationToken) == "" {
		return InteractiveCommitResult{}, ErrInteractiveDraftInvalid
	}
	if s.repo == nil {
		return InteractiveCommitResult{}, ErrInteractivePersistence
	}
	tokenHash := confirmationHash(req.ConfirmationToken)
	// Durable receipts are checked before the process-local session. This is
	// what makes an idempotent replay work after a lease expiry or restart.
	if receipt, err := s.repo.InteractiveCommitReceipts().FindByUserAndKey(ctx, userID, req.IdempotencyKey); err == nil {
		if receipt.SessionID != id || receipt.ConfirmationHash != tokenHash {
			return InteractiveCommitResult{}, ErrInteractiveIdempotency
		}
		var replay InteractiveCommitResult
		if err := json.Unmarshal(receipt.ResultJSON, &replay); err != nil {
			return InteractiveCommitResult{}, err
		}
		s.closeSession(id, false)
		return replay, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return InteractiveCommitResult{}, err
	}
	sess, e := s.getOwned(userID, id)
	if e != nil {
		return InteractiveCommitResult{}, e
	}
	sess.mu.Lock()
	if sess.committing {
		sess.mu.Unlock()
		return InteractiveCommitResult{}, ErrInteractiveBusy
	}
	if sess.pendingCall != "" || sess.busy {
		sess.mu.Unlock()
		return InteractiveCommitResult{}, ErrInteractiveBusy
	}
	if sess.Closed || time.Now().After(sess.ExpiresAt) {
		expired := time.Now().After(sess.ExpiresAt)
		sess.mu.Unlock()
		if expired {
			s.expire(id)
		}
		return InteractiveCommitResult{}, ErrInteractiveSessionClosed
	}
	if !sess.Draft.Ready || !confirmationMatches(req.ConfirmationToken, sess.confirmationTokenHash) {
		sess.mu.Unlock()
		return InteractiveCommitResult{}, ErrInteractiveToken
	}
	sess.committing = true
	draft := sess.Draft
	sess.mu.Unlock()
	var result InteractiveCommitResult
	e = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		receipt, err := tx.InteractiveCommitReceipts().FindByUserAndKeyForUpdate(ctx, userID, req.IdempotencyKey)
		if err == nil {
			if receipt.SessionID != id || receipt.ConfirmationHash != tokenHash {
				return ErrInteractiveIdempotency
			}
			return json.Unmarshal(receipt.ResultJSON, &result)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		profile := draft.Profile
		// Project creation records a draft profile. Confirmation is a separate
		// user action in the profile editor and must not be implied by creation.
		profile.Status = model.ProfileStatusDraft
		profile.InitializationStatus = model.ProfileInitializationReady
		profile.Version = 1
		if err := profile.Validate(); err != nil {
			return err
		}
		p := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: draft.Project.Platform, Name: draft.Project.Name, Instructions: draft.Project.Instructions, Keywords: draft.Project.Keywords, VisualStyle: draft.Project.VisualStyle, Status: model.ProjectStatusActive, Profile: datatypes.NewJSONType(profile)}
		if err := tx.Projects().Create(ctx, p); err != nil {
			return err
		}
		dims, _ := json.Marshal(profile.Dimensions)
		if err := tx.ProjectProfileRevisions().Create(ctx, &model.ProjectProfileRevision{ID: uuid.NewString(), ProjectID: p.ID, Revision: 1, SixDimensions: dims}); err != nil {
			return err
		}
		if err := tx.ProjectProfileStates().Create(ctx, &model.ProjectProfileState{ProjectID: p.ID, Status: model.ProfileInitializationReady, Revision: 1}); err != nil {
			return err
		}
		result = InteractiveCommitResult{Project: p, Profile: profile}
		raw, _ := json.Marshal(result)
		return tx.InteractiveCommitReceipts().Create(ctx, &model.InteractiveCommitReceipt{ID: uuid.NewString(), UserID: userID, IdempotencyKey: req.IdempotencyKey, SessionID: id, ConfirmationHash: tokenHash, ProjectID: p.ID, ResultJSON: raw})
	})
	if e != nil {
		sess.mu.Lock()
		sess.committing = false
		sess.mu.Unlock()
		// Another process may have won the unique idempotency race. Resolve the
		// durable winner before reporting a transient duplicate-key failure.
		if repository.IsDuplicateKeyError(e) {
			if receipt, lookupErr := s.repo.InteractiveCommitReceipts().FindByUserAndKey(ctx, userID, req.IdempotencyKey); lookupErr == nil {
				if receipt.SessionID != id || receipt.ConfirmationHash != tokenHash {
					return InteractiveCommitResult{}, ErrInteractiveIdempotency
				}
				if jsonErr := json.Unmarshal(receipt.ResultJSON, &result); jsonErr == nil {
					s.closeSession(id, false)
					return result, nil
				}
			}
		}
		return InteractiveCommitResult{}, e
	}
	sess.mu.Lock()
	sess.committing = false
	sess.Closed = true
	sess.ClosedAt = time.Now()
	sess.Status = "committed"
	rt := sess.runtime
	sess.runtime = nil
	cancel := sess.cancel
	sess.cancel = nil
	sess.Draft = InteractiveDraft{}
	sess.runtimeTokenHash = ""
	sess.confirmationTokenHash = ""
	sess.events = nil
	sess.mu.Unlock()
	if rt != nil {
		_ = rt.Close()
	}
	if cancel != nil {
		cancel()
	}
	s.closeWatchers(sess)
	s.scheduleTombstoneCollection(id, sess)
	return result, nil
}
func (s *InteractiveService) Close(userID, id string) error {
	sess, e := s.getOwned(userID, id)
	if e != nil {
		return e
	}
	sess.mu.Lock()
	if sess.committing {
		sess.mu.Unlock()
		return ErrInteractiveBusy
	}
	sess.mu.Unlock()
	s.closeSession(sess.ID, false)
	return nil
}

// Shutdown closes every process-local session before the Server tears down its
// dependencies. Closing sessions concurrently keeps the grace period bounded
// even when several Bridge processes need their shutdown timeout.
func (s *InteractiveService) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.RLock()
	ids := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	s.mu.RUnlock()
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				s.closeSession(id, false)
				if s.sessionClosed(id) {
					return
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}(id)
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *InteractiveService) sessionClosed(id string) bool {
	s.mu.RLock()
	sess := s.sessions[id]
	s.mu.RUnlock()
	if sess == nil {
		return true
	}
	sess.mu.Lock()
	closed := sess.Closed
	sess.mu.Unlock()
	return closed
}

type ExecRuntimeFactory struct {
	Command           string
	Args              []string
	Dir               string
	DSHPatch          string
	Provider          string
	ProviderAPIKeyEnv string
	Model             string
	MCPURL            string
}

func (f ExecRuntimeFactory) Start(ctx context.Context, sessionID, _ string, runtimeToken string, emit func(string, any)) (Runtime, error) {
	if strings.TrimSpace(f.Command) == "" {
		return nil, ErrInteractiveAgent
	}
	if emit == nil {
		emit = func(string, any) {}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// DSH persists profiles and session logs under DSH_HOME. Interactive
	// sessions are intentionally ephemeral, so every runtime gets an isolated
	// home that is removed as soon as the child exits.
	dshHome, err := os.MkdirTemp("", "anban-interactive-dsh-")
	if err != nil {
		return nil, fmt.Errorf("create interactive DSH home: %w", err)
	}
	cmd := exec.CommandContext(ctx, f.Command, f.Args...)
	if f.Dir != "" {
		cmd.Dir = f.Dir
	}
	cmd.Env = interactiveRuntimeEnvironment(dshHome, f.ProviderAPIKeyEnv)
	cmd.Env = append(cmd.Env,
		"ANBAN_INTERACTIVE_SESSION_ID="+sessionID,
		"ANBAN_INTERACTIVE_RUNTIME_TOKEN="+runtimeToken,
		"ANBAN_INTERACTIVE_DSH_HOME="+dshHome,
		"ANBAN_INTERACTIVE_DSH_PATCH="+f.DSHPatch,
		"ANBAN_INTERACTIVE_PROVIDER="+f.Provider,
		"ANBAN_INTERACTIVE_MODEL="+f.Model,
		"ANBAN_INTERACTIVE_MCP_URL="+f.MCPURL,
	)
	stdin, e := cmd.StdinPipe()
	if e != nil {
		_ = os.RemoveAll(dshHome)
		return nil, e
	}
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		_ = os.RemoveAll(dshHome)
		return nil, e
	}
	stderr, e := cmd.StderrPipe()
	if e != nil {
		_ = os.RemoveAll(dshHome)
		return nil, e
	}
	if e = cmd.Start(); e != nil {
		_ = os.RemoveAll(dshHome)
		return nil, e
	}
	r := &execRuntime{cmd: cmd, in: stdin, done: make(chan struct{}), pending: map[uint64]chan error{}, cleanup: func() { _ = os.RemoveAll(dshHome) }}
	go io.Copy(io.Discard, stderr)
	go r.read(stdout, emit)
	go func() { err := cmd.Wait(); r.finish(err); emit("runtime.exit", map[string]any{}) }()
	return r, nil
}

func interactiveRuntimeEnvironment(dshHome, providerAPIKeyEnv string) []string {
	keys := []string{"PATH", "TMPDIR", "TEMP", "TMP", "LANG", "LC_ALL", "TZ", "SYSTEMROOT", "WINDIR"}
	env := make([]string, 0, len(keys)+5)
	for _, key := range keys {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	env = append(env, "HOME="+dshHome, "USERPROFILE="+dshHome)
	if validInteractiveEnvName(providerAPIKeyEnv) {
		if value, ok := os.LookupEnv(providerAPIKeyEnv); ok {
			env = append(env, providerAPIKeyEnv+"="+value)
		}
	}
	return env
}

func validInteractiveEnvName(name string) bool {
	if name == "" {
		return false
	}
	for index, char := range name {
		letter := char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || char == '_'
		digit := char >= '0' && char <= '9'
		if !letter && !(index > 0 && digit) {
			return false
		}
	}
	return true
}

type execRuntime struct {
	cmd         *exec.Cmd
	in          io.WriteCloser
	mu          sync.Mutex
	next        uint64
	done        chan struct{}
	closed      bool
	finished    bool
	terminalErr error
	pending     map[uint64]chan error
	cleanup     func()
	cleanupOnce sync.Once
}

func (r *execRuntime) Command(ctx context.Context, method string, params any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	response := make(chan error, 1)
	r.mu.Lock()
	if r.closed || r.finished {
		r.mu.Unlock()
		return ErrInteractiveSessionClosed
	}
	r.next++
	id := r.next
	r.pending[id] = response
	b, e := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if e != nil {
		delete(r.pending, id)
		r.mu.Unlock()
		return e
	}
	_, e = fmt.Fprintf(r.in, "%s\n", b)
	if e != nil {
		delete(r.pending, id)
		r.mu.Unlock()
		return e
	}
	r.mu.Unlock()
	select {
	case e := <-response:
		return e
	case <-r.done:
		r.mu.Lock()
		err := r.terminalErr
		r.mu.Unlock()
		if err != nil {
			return err
		}
		return errors.New("interactive runtime exited before responding")
	case <-ctx.Done():
		r.mu.Lock()
		delete(r.pending, id)
		r.mu.Unlock()
		return ctx.Err()
	}
}

func (r *execRuntime) finish(err error) {
	r.mu.Lock()
	if r.finished {
		r.mu.Unlock()
		return
	}
	r.finished = true
	r.terminalErr = err
	pending := r.pending
	r.pending = map[uint64]chan error{}
	r.mu.Unlock()
	if r.cleanup != nil {
		r.cleanupOnce.Do(r.cleanup)
	}
	r.mu.Lock()
	close(r.done)
	r.mu.Unlock()
	if err == nil {
		err = errors.New("interactive bridge closed before responding")
	}
	for _, response := range pending {
		select {
		case response <- err:
		default:
		}
	}
}

func bridgeResponseError(value any) error {
	if value == nil {
		return nil
	}
	if message, ok := value.(string); ok {
		if strings.TrimSpace(message) == "" {
			return errors.New("interactive bridge returned an empty error")
		}
		return errors.New(message)
	}
	b, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("interactive bridge error: %v", value)
	}
	return errors.New(string(b))
}

func (r *execRuntime) deliverResponse(id uint64, err error) {
	r.mu.Lock()
	response := r.pending[id]
	delete(r.pending, id)
	r.mu.Unlock()
	if response != nil {
		select {
		case response <- err:
		default:
		}
	}
}

func interactiveResponseID(value any) (uint64, bool) {
	switch id := value.(type) {
	case float64:
		if id >= 0 && id == float64(uint64(id)) {
			return uint64(id), true
		}
	case json.Number:
		parsed, err := id.Int64()
		if err == nil && parsed >= 0 {
			return uint64(parsed), true
		}
	case string:
		parsed, err := strconv.ParseUint(id, 10, 64)
		if err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func (r *execRuntime) read(out io.Reader, emit func(string, any)) {
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var m map[string]any
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		if typ, ok := m["event"].(string); ok {
			emit(typ, m["data"])
			continue
		}
		if id, ok := interactiveResponseID(m["id"]); ok {
			r.deliverResponse(id, bridgeResponseError(m["error"]))
		}
	}
	if err := sc.Err(); err != nil {
		r.finish(fmt.Errorf("interactive bridge output: %w", err))
	} else {
		r.finish(nil)
	}
}
func (r *execRuntime) Close() error {
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		_, _ = fmt.Fprintln(r.in, `{"id":0,"method":"shutdown","params":{}}`)
		_ = r.in.Close()
	}
	r.mu.Unlock()
	select {
	case <-r.done:
		return nil
	case <-time.After(3 * time.Second):
		err := r.cmd.Process.Kill()
		if r.cleanup != nil {
			r.cleanupOnce.Do(r.cleanup)
		}
		return err
	}
}
