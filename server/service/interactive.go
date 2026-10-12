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
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anbanai/anban-creator/server/agentpack"
	projectmemory "github.com/anbanai/anban-creator/server/memory"
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
	ErrProjectMemoryConflict      = projectmemory.ErrProjectMemoryConflict
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
	MemoryAppend      string               `json:"memory_append,omitempty"`
}
type InteractiveStageInput struct {
	Project        InteractiveProject   `json:"project"`
	Profile        model.ProjectProfile `json:"profile"`
	Progress       InteractiveProgress  `json:"progress"`
	Ready          bool                 `json:"ready"`
	MissingFields  []string             `json:"missing_fields"`
	InferredFields []string             `json:"inferred_fields"`
	MemoryAppend   string               `json:"memory_append,omitempty"`
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
	runtimeStarted              bool
	runtimeExitPending          bool
	runtimeExitData             any
	runtime                     Runtime
	cancel                      context.CancelFunc
	runtimeTokenHash            string
	// confirmationTokenHash is server-only state. The raw token is included
	// only in the draft.ready event sent to the owning Studio client.
	confirmationTokenHash string
	ProjectID             string
	ProfileVersion        int64
	initialContext        string
	memoryExpected        string
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

type RuntimeFactoryWithContext interface {
	StartWithContext(context.Context, string, string, string, string, func(string, any)) (Runtime, error)
}

type InteractiveMemoryStore interface {
	ReadProject(context.Context, string) (projectmemory.ProjectView, error)
	EnsureProject(context.Context, string) error
	WriteMarkdownFile(context.Context, string, string, string) error
	AppendMarkdownFileIfUnchanged(context.Context, string, string, string, string) error
}
type InteractiveService struct {
	repo         repository.Repository
	projectSvc   *ProjectService
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
	memory       InteractiveMemoryStore
	receiptMu    sync.Mutex
}

func (s *InteractiveService) SetProjectMemory(memory InteractiveMemoryStore) {
	s.mu.Lock()
	s.memory = memory
	s.mu.Unlock()
}

func NewInteractiveService(repo repository.Repository) *InteractiveService {
	return NewInteractiveServiceWithRuntimeAndAgents(repo, nil, interactiveAgentsFromCatalog(false))
}
func NewInteractiveServiceWithRuntime(repo repository.Repository, factory RuntimeFactory) *InteractiveService {
	return NewInteractiveServiceWithRuntimeAndAgents(repo, factory, interactiveAgentsFromCatalog(factory != nil))
}

func NewInteractiveServiceWithRuntimeAndAgents(repo repository.Repository, factory RuntimeFactory, agents []InteractiveAgent) *InteractiveService {
	return &InteractiveService{
		repo: repo, projectSvc: NewProjectService(repo, nil), factory: factory, sessions: map[string]*InteractiveSession{},
		ttl: 30 * time.Minute, maxHistory: 256, agents: append([]InteractiveAgent(nil), agents...),
		maxSessions: 32, maxPerUser: 2, tombstoneTTL: 5 * time.Minute,
	}
}

// SetProjectService supplies the Server's configured project application
// service so interactive commits share the same capability validation and
// project defaults as the regular project API.
func (s *InteractiveService) SetProjectService(projectSvc *ProjectService) {
	s.mu.Lock()
	s.projectSvc = projectSvc
	s.mu.Unlock()
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

const interactiveContextMaxBytes = 256 << 10

func interactiveInitialContext(project *model.Project, profile model.ProjectProfile, view projectmemory.ProjectView) (string, string) {
	type contextFile struct {
		Path      string `json:"path"`
		Content   string `json:"content"`
		Truncated bool   `json:"truncated,omitempty"`
	}
	// The database profile is the authoritative source for rebuildable Agent
	// projections. Read the NAS only for the continuously maintained root
	// MEMORY.md, so a stale or incomplete NAS projection cannot hide current
	// profile facts from the update conversation.
	files := make([]contextFile, 0, len(model.ProfileDimensions())+2)
	files = append(files, contextFile{Path: "AGENTS.md", Content: ProfileAgentsMarkdown()})
	for _, name := range model.ProfileDimensions() {
		content, err := ProfileDimensionMarkdownAtVersion(name, profileDimension(profile, name), profile.Version)
		if err != nil {
			return "", ""
		}
		files = append(files, contextFile{Path: "profile/" + name + ".md", Content: content})
	}
	memoryExpected := ""
	for _, file := range view.Files {
		if file.Path != "MEMORY.md" {
			continue
		}
		content := file.Content
		if file.Path == "MEMORY.md" && !file.Truncated {
			memoryExpected = content
		}
		truncated := file.Truncated
		files = append(files, contextFile{Path: file.Path, Content: content, Truncated: truncated})
	}
	ctx := map[string]any{
		"kind": "anban.project_profile_reference", "version": 1,
		"untrusted_reference_data": true,
		"project":                  map[string]string{"id": project.ID, "name": project.Name, "platform": project.Platform, "instructions": project.Instructions, "keywords": project.Keywords, "visual_style": project.VisualStyle},
		"profile_version":          profile.Version, "profile": profile, "files": files,
	}
	for {
		raw, err := json.Marshal(ctx)
		if err != nil {
			return "", memoryExpected
		}
		if len(raw) <= interactiveContextMaxBytes {
			return string(raw), memoryExpected
		}
		// Keep the context valid UTF-8 while reducing the largest file until
		// the complete serialized snapshot, including profile metadata, fits
		// the bridge limit. The limit applies to the wire value, not just file
		// bodies, so a future profile expansion cannot bypass it.
		largest := -1
		largestBytes := 0
		for index, file := range files {
			if size := len([]byte(file.Content)); size > largestBytes {
				largest, largestBytes = index, size
			}
		}
		if largest < 0 || largestBytes == 0 {
			return "", memoryExpected
		}
		keep := largestBytes / 2
		if excess := len(raw) - interactiveContextMaxBytes; excess > largestBytes-keep {
			keep = largestBytes - excess
		}
		if keep < 0 {
			keep = 0
		}
		bytes := []byte(files[largest].Content)
		keep = truncateUTF8Boundary(bytes, keep)
		files[largest].Content = string(bytes[:keep])
		files[largest].Truncated = true
	}
}

func truncateUTF8Boundary(value []byte, limit int) int {
	if limit <= 0 {
		return 0
	}
	if len(value) <= limit {
		return len(value)
	}
	for limit > 0 && (value[limit]&0xc0) == 0x80 {
		limit--
	}
	return limit
}

func writeInteractiveProfileProjection(ctx context.Context, memory InteractiveMemoryStore, projectID string, profile model.ProjectProfile) error {
	if memory == nil {
		return nil
	}
	if err := memory.EnsureProject(ctx, projectID); err != nil {
		return err
	}
	for _, name := range model.ProfileDimensions() {
		content, err := ProfileDimensionMarkdownAtVersion(name, profileDimension(profile, name), profile.Version)
		if err != nil {
			return err
		}
		if err := memory.WriteMarkdownFile(ctx, projectID, "profile/"+name+".md", content); err != nil {
			return err
		}
	}
	return memory.WriteMarkdownFile(ctx, projectID, "AGENTS.md", ProfileAgentsMarkdownAtVersion(profile.Version))
}

func (s *InteractiveService) CreateSession(ctx context.Context, userID, agentID, intent string, projectIDs ...string) (*InteractiveSession, error) {
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
	projectID := ""
	if len(projectIDs) > 0 {
		projectID = strings.TrimSpace(projectIDs[0])
	}
	var initialContext, memoryExpected string
	var profileVersion int64
	if intent == "update-project-profile" {
		if projectID == "" || s.repo == nil {
			return nil, ErrInteractiveDraftInvalid
		}
		project, err := s.repo.Projects().FindByID(ctx, projectID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrInteractiveDraftInvalid
			}
			return nil, errors.Join(ErrInteractivePersistence, err)
		}
		if project.UserID != userID {
			return nil, ErrInteractiveSessionOwner
		}
		profile := projectProfileFor(project)
		profileVersion = profile.Version
		var view projectmemory.ProjectView
		s.mu.RLock()
		memory := s.memory
		s.mu.RUnlock()
		if memory != nil {
			view, err = memory.ReadProject(ctx, projectID)
			if err != nil && !errors.Is(err, projectmemory.ErrProjectMemoryMissing) {
				return nil, errors.Join(ErrInteractivePersistence, err)
			}
		}
		initialContext, memoryExpected = interactiveInitialContext(project, profile, view)
	}
	now := time.Now()
	runtimeToken, err := randomToken()
	if err != nil {
		return nil, err
	}
	runtimeCtx, cancel := context.WithCancel(context.Background())
	sess := &InteractiveSession{ID: uuid.NewString(), UserID: userID, AgentID: agentID, Intent: intent, ProjectID: projectID, ProfileVersion: profileVersion, initialContext: initialContext, memoryExpected: memoryExpected, CreatedAt: now, ExpiresAt: now.Add(s.ttl), Status: "starting", watchers: map[*interactiveWatcher]struct{}{}, pendingQuestions: map[string]InteractiveQuestion{}, runtimeTokenHash: confirmationHash(runtimeToken), cancel: cancel}
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
	emit := func(k string, d any) { s.runtimeEvent(sess.ID, k, d) }
	var rt Runtime
	var e error
	if factory, ok := s.factory.(RuntimeFactoryWithContext); ok {
		rt, e = factory.StartWithContext(ctx, sess.ID, sess.AgentID, runtimeToken, sess.initialContext, emit)
	} else {
		rt, e = s.factory.Start(ctx, sess.ID, sess.AgentID, runtimeToken, emit)
	}
	if e != nil {
		s.runtimeEvent(sess.ID, "session.error", map[string]any{
			"status":  "runtime_failed",
			"message": fmt.Sprintf("interactive runtime failed to start: %v", e),
		})
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
	if e = rt.Command(ctx, "session/start", map[string]any{"sessionId": sess.ID, "presetId": sess.AgentID, "initialContext": sess.initialContext}); e != nil {
		_ = rt.Close()
		s.runtimeEvent(sess.ID, "session.error", map[string]any{
			"status":  "runtime_failed",
			"message": fmt.Sprintf("interactive runtime failed to initialize: %v", e),
		})
		s.closeSession(sess.ID, false)
		return
	}
	sess.mu.Lock()
	sess.runtimeStarted = true
	pendingExit := sess.runtimeExitPending
	pendingExitData := sess.runtimeExitData
	sess.runtimeExitPending = false
	sess.runtimeExitData = nil
	sess.mu.Unlock()
	if pendingExit {
		s.runtimeEvent(sess.ID, "runtime.exit", pendingExitData)
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

// SessionState returns the current state, including terminal error/committed
// states, for the create response and other control-plane callers. Unlike
// SessionStatus it does not reject a closed tombstone, so a runtime that fails
// before the HTTP response is written is reported accurately.
func (s *InteractiveService) SessionState(uid, id string) (string, error) {
	s.mu.RLock()
	sess := s.sessions[id]
	s.mu.RUnlock()
	if sess == nil {
		return "", ErrInteractiveSessionNotFound
	}
	if sess.UserID != uid {
		return "", ErrInteractiveSessionOwner
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
	// A fresh connection (after == 0) may legitimately start at a later event
	// ID when the in-memory history was compacted or a failed session retained
	// only its terminal event. Only a reconnect with an explicit cursor can
	// prove that events were lost and must receive a 409 for recovery.
	if after > 0 && len(sess.events) > 0 && sess.events[0].ID > after+1 {
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
	case "session.ready":
		sess.mu.Lock()
		if !sess.Closed {
			sess.Status = "running"
		}
		sess.mu.Unlock()
	case "runtime.exit":
		sess.mu.Lock()
		if sess.Closed {
			sess.mu.Unlock()
			return
		}
		if !sess.runtimeStarted {
			sess.runtimeExitPending = true
			sess.runtimeExitData = data
			sess.mu.Unlock()
			return
		}
		sess.mu.Unlock()
		if message := interactiveRuntimeExitMessage(data); message != "" {
			sess.mu.Lock()
			if !sess.Closed {
				sess.Status = "error"
			}
			sess.mu.Unlock()
			s.emit(sess, "session.error", map[string]any{"status": "runtime_failed", "message": message})
			s.closeSession(id, false)
			return
		}
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
	if len([]rune(in.MemoryAppend)) > 64*1024 {
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
	if sess.ProjectID == "" && strings.TrimSpace(in.MemoryAppend) != "" {
		sess.mu.Unlock()
		return ErrInteractiveDraftInvalid
	}
	sess.Draft.Revision++
	sess.Draft.Project = in.Project
	sess.Draft.Profile = in.Profile
	sess.Draft.Progress = in.Progress
	sess.Draft.MissingFields = append([]string(nil), in.MissingFields...)
	sess.Draft.InferredFields = append([]string(nil), in.InferredFields...)
	sess.Draft.MemoryAppend = in.MemoryAppend
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
	MemoryAppend      string `json:"memory_append,omitempty"`
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
	// The confirmation token is generated for one staged draft. The browser
	// must not be able to alter the staged memory change by sending a second
	// copy in the commit request, so the request fingerprint is independent of
	// any legacy client-provided memory_append field.
	requestHash := confirmationHash(tokenHash)
	// Durable receipts are checked before the process-local session. This is
	// what makes an idempotent replay work after a lease expiry or restart.
	if receipt, err := s.repo.InteractiveCommitReceipts().FindByUserAndKey(ctx, userID, req.IdempotencyKey); err == nil {
		if receipt.SessionID != id || receipt.ConfirmationHash != tokenHash || (receipt.RequestHash != "" && receipt.RequestHash != requestHash) {
			return InteractiveCommitResult{}, ErrInteractiveIdempotency
		}
		var replay InteractiveCommitResult
		if err := json.Unmarshal(receipt.ResultJSON, &replay); err != nil {
			return InteractiveCommitResult{}, err
		}
		if err := s.applyReceipt(ctx, receipt, replay); err != nil {
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
	projectID := sess.ProjectID
	profileVersion := sess.ProfileVersion
	memoryExpected := sess.memoryExpected
	memoryAppend := draft.MemoryAppend
	if projectID == "" && memoryAppend != "" {
		sess.committing = false
		sess.mu.Unlock()
		return InteractiveCommitResult{}, ErrInteractiveDraftInvalid
	}
	sess.mu.Unlock()
	var result InteractiveCommitResult
	e = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		receipt, err := tx.InteractiveCommitReceipts().FindByUserAndKeyForUpdate(ctx, userID, req.IdempotencyKey)
		if err == nil {
			if receipt.SessionID != id || receipt.ConfirmationHash != tokenHash || (receipt.RequestHash != "" && receipt.RequestHash != requestHash) {
				return ErrInteractiveIdempotency
			}
			return json.Unmarshal(receipt.ResultJSON, &result)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var p *model.Project
		var profile model.ProjectProfile
		if projectID != "" {
			p, err = tx.Projects().FindByIDForUpdate(ctx, projectID)
			if err != nil {
				return err
			}
			if p.UserID != userID {
				return ErrInteractiveSessionOwner
			}
			current := projectProfileFor(p)
			if current.Version != profileVersion {
				return ErrProjectProfileVersionConflict
			}
			profile = current
			profile.Dimensions = draft.Profile.Dimensions
			profile.AnalysisLimits = draft.Profile.AnalysisLimits
			profile.FollowUpQuestions = draft.Profile.FollowUpQuestions
			// The interactive confirmation updates the profile facts only. Lifecycle
			// state is owned by the profile service and must not be promoted or
			// cleared as a side effect of this conversation.
			profile.Version = current.Version + 1
			if err := profile.Validate(); err != nil {
				return err
			}
			p.Profile = datatypes.NewJSONType(profile)
			if err := tx.Projects().Update(ctx, p); err != nil {
				return err
			}
			dims, _ := json.Marshal(profile.Dimensions)
			if err := tx.ProjectProfileRevisions().Create(ctx, &model.ProjectProfileRevision{ID: uuid.NewString(), ProjectID: p.ID, Revision: profile.Version, SourceTaskID: profile.AnalysisTaskID, SixDimensions: dims}); err != nil {
				return err
			}
			if err := persistProfileState(ctx, tx, p.ID, profile); err != nil {
				return err
			}
		} else {
			profile = draft.Profile
			// Project creation records a draft profile. Confirmation is a separate
			// user action in the profile editor and must not be implied by creation.
			profile.Status = model.ProfileStatusDraft
			profile.InitializationStatus = model.ProfileInitializationReady
			profile.Version = 1
			if err := profile.Validate(); err != nil {
				return err
			}
			p = &model.Project{
				Platform: draft.Project.Platform, Name: draft.Project.Name,
				Instructions: draft.Project.Instructions, Keywords: draft.Project.Keywords,
				VisualStyle: draft.Project.VisualStyle, Profile: datatypes.NewJSONType(profile),
			}
			// Reuse the regular project creation validation/defaults while keeping
			// the write on this transaction. Image-analysis side effects are not
			// part of an interactive draft commit.
			s.mu.RLock()
			projectSvc := s.projectSvc
			s.mu.RUnlock()
			if projectSvc == nil {
				projectSvc = NewProjectService(tx, nil)
			} else {
				configured := *projectSvc
				configured.repo = tx
				configured.imageAnalyses = nil
				projectSvc = &configured
			}
			if err := projectSvc.prepareCreate(userID, p); err != nil {
				return err
			}
			if err := tx.Projects().Create(ctx, p); err != nil {
				return err
			}
			dims, _ := json.Marshal(profile.Dimensions)
			if err := tx.ProjectProfileRevisions().Create(ctx, &model.ProjectProfileRevision{ID: uuid.NewString(), ProjectID: p.ID, Revision: 1, SixDimensions: dims}); err != nil {
				return err
			}
			if err := tx.ProjectProfileStates().Create(ctx, &model.ProjectProfileState{ProjectID: p.ID, Status: model.ProfileInitializationReady, Revision: 1}); err != nil && !missingProfileStateTable(err) {
				return err
			}
		}
		result = InteractiveCommitResult{Project: p, Profile: profile}
		raw, _ := json.Marshal(result)
		return tx.InteractiveCommitReceipts().Create(ctx, &model.InteractiveCommitReceipt{ID: uuid.NewString(), UserID: userID, IdempotencyKey: req.IdempotencyKey, SessionID: id, ConfirmationHash: tokenHash, RequestHash: requestHash, ProjectID: p.ID, MemoryAppend: memoryAppend, MemoryExpected: memoryExpected, ResultJSON: raw})
	})
	if e != nil {
		sess.mu.Lock()
		sess.committing = false
		sess.mu.Unlock()
		// Another process may have won the unique idempotency race. Resolve the
		// durable winner before reporting a transient duplicate-key failure.
		if repository.IsDuplicateKeyError(e) {
			if receipt, lookupErr := s.repo.InteractiveCommitReceipts().FindByUserAndKey(ctx, userID, req.IdempotencyKey); lookupErr == nil {
				if receipt.SessionID != id || receipt.ConfirmationHash != tokenHash || (receipt.RequestHash != "" && receipt.RequestHash != requestHash) {
					return InteractiveCommitResult{}, ErrInteractiveIdempotency
				}
				if jsonErr := json.Unmarshal(receipt.ResultJSON, &result); jsonErr == nil {
					if applyErr := s.applyReceipt(ctx, receipt, result); applyErr != nil {
						return InteractiveCommitResult{}, applyErr
					}
					s.closeSession(id, false)
					return result, nil
				}
			}
		}
		return InteractiveCommitResult{}, e
	}
	receipt, receiptErr := s.repo.InteractiveCommitReceipts().FindByUserAndKey(ctx, userID, req.IdempotencyKey)
	if receiptErr != nil {
		sess.mu.Lock()
		sess.committing = false
		sess.mu.Unlock()
		return InteractiveCommitResult{}, errors.Join(ErrInteractivePersistence, receiptErr)
	}
	if applyErr := s.applyReceipt(ctx, receipt, result); applyErr != nil {
		sess.mu.Lock()
		sess.committing = false
		sess.mu.Unlock()
		return InteractiveCommitResult{}, applyErr
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

// applyReceipt materializes the durable database result into the Server-owned
// memory NAS. It is deliberately outside the database transaction: NAS writes
// are recoverable projections and must never make a committed project roll
// back. MEMORY.md uses expected-content CAS so a later editor cannot be
// overwritten by a replay or retry.
func (s *InteractiveService) applyReceipt(ctx context.Context, receipt *model.InteractiveCommitReceipt, result InteractiveCommitResult) error {
	if receipt == nil || result.Project == nil {
		return ErrInteractivePersistence
	}
	s.mu.RLock()
	memory := s.memory
	s.mu.RUnlock()
	if memory == nil {
		return nil
	}
	// A durable receipt can be replayed after a later profile revision was
	// committed. Resolve the projection source from the database before writing
	// NAS so an old receipt cannot reintroduce an older profile. The database is
	// the authoritative source; the receipt only carries the original result
	// and the controlled MEMORY.md append intent.
	if s.repo == nil {
		return ErrInteractivePersistence
	}
	currentProject, err := s.repo.Projects().FindByID(ctx, result.Project.ID)
	if err != nil {
		return s.recordReceiptProjectionError(ctx, receipt, err)
	}
	currentProfile := projectProfileFor(currentProject)
	if currentProfile.Version > result.Profile.Version {
		result.Profile = currentProfile
	}
	s.receiptMu.Lock()
	defer s.receiptMu.Unlock()
	if receipt.MemoryAppliedAt != nil {
		return nil
	}
	if err := writeInteractiveProfileProjection(ctx, memory, result.Project.ID, result.Profile); err != nil {
		return s.recordReceiptProjectionError(ctx, receipt, err)
	}
	if strings.TrimSpace(receipt.MemoryAppend) == "" {
		now := time.Now()
		receipt.MemoryAppliedAt = &now
		receipt.MemoryError = ""
		if err := s.repo.InteractiveCommitReceipts().Update(ctx, receipt); err != nil {
			return errors.Join(ErrInteractivePersistence, err)
		}
		return nil
	}
	if err := memory.EnsureProject(ctx, result.Project.ID); err != nil {
		return s.recordReceiptProjectionError(ctx, receipt, err)
	}
	if err := memory.AppendMarkdownFileIfUnchanged(ctx, result.Project.ID, "MEMORY.md", receipt.MemoryExpected, receipt.MemoryAppend); err != nil {
		return s.recordReceiptProjectionError(ctx, receipt, err)
	}
	now := time.Now()
	receipt.MemoryAppliedAt = &now
	receipt.MemoryError = ""
	if err := s.repo.InteractiveCommitReceipts().Update(ctx, receipt); err != nil {
		return errors.Join(ErrInteractivePersistence, err)
	}
	return nil
}

// recordReceiptProjectionError persists the failure marker used by replay and
// operational repair. Never discard an error from this update: a missing
// marker would make a committed project look fully materialized when its NAS
// projection actually needs retrying.
func (s *InteractiveService) recordReceiptProjectionError(ctx context.Context, receipt *model.InteractiveCommitReceipt, projectionErr error) error {
	receipt.MemoryError = projectionErr.Error()
	updateErr := s.repo.InteractiveCommitReceipts().Update(ctx, receipt)
	if updateErr != nil {
		return errors.Join(ErrInteractivePersistence, projectionErr, updateErr)
	}
	return errors.Join(ErrInteractivePersistence, projectionErr)
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
	Command   string
	Args      []string
	Dir       string
	DSHPatch  string
	Provider  string
	API       string
	BaseURL   string
	APIKeyEnv string
	Model     string
	MCPURL    string
}

func (f ExecRuntimeFactory) Start(ctx context.Context, sessionID, _ string, runtimeToken string, emit func(string, any)) (Runtime, error) {
	return f.start(ctx, sessionID, runtimeToken, "", emit)
}

func (f ExecRuntimeFactory) StartWithContext(ctx context.Context, sessionID, _ string, runtimeToken, initialContext string, emit func(string, any)) (Runtime, error) {
	return f.start(ctx, sessionID, runtimeToken, initialContext, emit)
}

func (f ExecRuntimeFactory) start(ctx context.Context, sessionID, runtimeToken, initialContext string, emit func(string, any)) (Runtime, error) {
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
	initialContextFile := ""
	if strings.TrimSpace(initialContext) != "" {
		initialContextFile = filepath.Join(dshHome, "initial-context.json")
		if err := os.WriteFile(initialContextFile, []byte(initialContext), 0o600); err != nil {
			_ = os.RemoveAll(dshHome)
			return nil, fmt.Errorf("write interactive initial context: %w", err)
		}
	}
	args := append([]string(nil), f.Args...)
	args = append(args, "--provider", f.Provider, "--model", f.Model, "--route-api", f.API, "--base-url", f.BaseURL, "--api-key-env", f.APIKeyEnv)
	cmd := exec.CommandContext(ctx, f.Command, args...)
	if f.Dir != "" {
		cmd.Dir = f.Dir
	}
	cmd.Env = interactiveRuntimeEnvironment(dshHome, f.APIKeyEnv)
	cmd.Env = append(cmd.Env,
		"ANBAN_INTERACTIVE_SESSION_ID="+sessionID,
		"ANBAN_INTERACTIVE_RUNTIME_TOKEN="+runtimeToken,
		"ANBAN_INTERACTIVE_DSH_HOME="+dshHome,
		"ANBAN_INTERACTIVE_DSH_PATCH="+f.DSHPatch,
		"ANBAN_INTERACTIVE_INITIAL_CONTEXT_FILE="+initialContextFile,
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
	stderrTail := &interactiveTailBuffer{limit: 16 * 1024}
	redactions := []string{runtimeToken}
	if providerSecret := interactiveProviderCredential(f.APIKeyEnv); providerSecret != "" {
		redactions = append(redactions, providerSecret)
	}
	r := &execRuntime{cmd: cmd, in: stdin, done: make(chan struct{}), pending: map[uint64]chan error{}, cleanup: func() { _ = os.RemoveAll(dshHome) }, redactions: redactions}
	stdoutDone := make(chan error, 1)
	stderrDone := make(chan error, 1)
	go func() { _, copyErr := io.Copy(stderrTail, stderr); stderrDone <- copyErr }()
	go func() { stdoutDone <- r.read(stdout, emit) }()
	go func() {
		waitErr := cmd.Wait()
		stdoutErr := <-stdoutDone
		stderrErr := <-stderrDone
		terminalErr := interactiveRuntimeExitError(waitErr, stdoutErr, stderrErr, stderrTail.String(), r.redactions...)
		r.finish(terminalErr)
		emit("runtime.exit", interactiveRuntimeExitDetails(cmd, waitErr, stdoutErr, stderrErr, stderrTail.String(), r.redactions...))
	}()
	return r, nil
}

func interactiveRuntimeEnvironment(dshHome, providerAPIKeyEnv string) []string {
	keys := []string{"PATH", "TMPDIR", "TEMP", "TMP", "LANG", "LC_ALL", "TZ", "SYSTEMROOT", "WINDIR", "NODE_EXTRA_CA_CERTS"}
	env := make([]string, 0, len(keys)+5)
	for _, key := range keys {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	env = append(env, "HOME="+dshHome, "USERPROFILE="+dshHome)
	if validInteractiveEnvName(providerAPIKeyEnv) {
		value, ok := os.LookupEnv(providerAPIKeyEnv)
		// Server deployments keep the provider credential under the
		// ANBAN_* namespace. The DSH DeepSeek adapter, however, intentionally
		// reads DEEPSEEK_API_KEY. Bridge that deployment name at the process
		// boundary without exposing the Server's full environment.
		if (!ok || value == "") && providerAPIKeyEnv == "DEEPSEEK_API_KEY" {
			value, ok = os.LookupEnv("ANBAN_DEEPSEEK_API_KEY")
		}
		if ok {
			env = append(env, providerAPIKeyEnv+"="+value)
		}
	}
	return env
}

func interactiveProviderCredential(providerAPIKeyEnv string) string {
	if !validInteractiveEnvName(providerAPIKeyEnv) {
		return ""
	}
	value, ok := os.LookupEnv(providerAPIKeyEnv)
	if (!ok || value == "") && providerAPIKeyEnv == "DEEPSEEK_API_KEY" {
		value, _ = os.LookupEnv("ANBAN_DEEPSEEK_API_KEY")
	}
	return value
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
	redactions  []string
}

type interactiveTailBuffer struct {
	mu    sync.Mutex
	limit int
	data  []byte
}

func (b *interactiveTailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.limit > 0 {
		b.data = append(b.data, p...)
		if len(b.data) > b.limit {
			b.data = append([]byte(nil), b.data[len(b.data)-b.limit:]...)
		}
	}
	return len(p), nil
}
func (b *interactiveTailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(append([]byte(nil), b.data...))
}
func redactInteractiveRuntimeDiagnostic(value string, secrets ...string) string {
	for _, secret := range secrets {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[redacted]")
		}
	}
	return value
}
func interactiveRuntimeExitError(waitErr, stdoutErr, stderrErr error, stderrTail string, secrets ...string) error {
	parts := make([]string, 0, 4)
	if waitErr != nil {
		parts = append(parts, waitErr.Error())
	}
	if stdoutErr != nil {
		parts = append(parts, fmt.Sprintf("stdout: %v", stdoutErr))
	}
	if stderrErr != nil {
		parts = append(parts, fmt.Sprintf("stderr read: %v", stderrErr))
	}
	if trimmed := strings.TrimSpace(stderrTail); trimmed != "" {
		parts = append(parts, "stderr: "+trimmed)
	}
	if len(parts) == 0 {
		return errors.New("interactive runtime exited before responding")
	}
	return errors.New("interactive runtime exited before responding: " + redactInteractiveRuntimeDiagnostic(strings.Join(parts, "; "), secrets...))
}
func interactiveRuntimeExitDetails(cmd *exec.Cmd, waitErr, stdoutErr, stderrErr error, stderrTail string, secrets ...string) map[string]any {
	details := map[string]any{}
	if cmd != nil && cmd.ProcessState != nil && cmd.ProcessState.ExitCode() != 0 {
		details["exit_code"] = cmd.ProcessState.ExitCode()
	}
	if waitErr != nil {
		details["error"] = redactInteractiveRuntimeDiagnostic(waitErr.Error(), secrets...)
	}
	if stdoutErr != nil {
		details["stdout_error"] = redactInteractiveRuntimeDiagnostic(stdoutErr.Error(), secrets...)
	}
	if stderrErr != nil {
		details["stderr_error"] = redactInteractiveRuntimeDiagnostic(stderrErr.Error(), secrets...)
	}
	if trimmed := strings.TrimSpace(stderrTail); trimmed != "" {
		details["stderr"] = redactInteractiveRuntimeDiagnostic(trimmed, secrets...)
	}
	return details
}
func interactiveRuntimeExitMessage(data any) string {
	fields, ok := data.(map[string]any)
	if !ok {
		return ""
	}
	parts := make([]string, 0, 4)
	for _, key := range []string{"error", "stdout_error", "stderr_error", "stderr"} {
		if value, _ := fields[key].(string); strings.TrimSpace(value) != "" {
			switch key {
			case "stdout_error":
				value = "stdout: " + value
			case "stderr_error":
				value = "stderr read: " + value
			case "stderr":
				value = "stderr: " + value
			}
			parts = append(parts, value)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "interactive runtime exited before responding: " + strings.Join(parts, "; ")
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

func bridgeResponseError(value any, secrets ...string) error {
	if value == nil {
		return nil
	}
	if message, ok := value.(string); ok {
		if strings.TrimSpace(message) == "" {
			return errors.New("interactive bridge returned an empty error")
		}
		return errors.New(redactInteractiveRuntimeDiagnostic(message, secrets...))
	}
	b, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("interactive bridge error: %v", value)
	}
	return errors.New(redactInteractiveRuntimeDiagnostic(string(b), secrets...))
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

func (r *execRuntime) read(out io.Reader, emit func(string, any)) error {
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
			r.deliverResponse(id, bridgeResponseError(m["error"], r.redactions...))
		}
	}
	return sc.Err()
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
