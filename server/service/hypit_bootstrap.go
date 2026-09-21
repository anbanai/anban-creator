package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
	"gorm.io/datatypes"
	"io"
	"path"
	"reflect"
	"strings"
	"time"
)

type hypitRuntimeSnapshot struct {
	Profile         map[string]any     `json:"profile"`
	Limits          config.HypitLimits `json:"limits"`
	Image           string             `json:"image"`
	SourceTaskID    string             `json:"source_task_id,omitempty"`
	SourceExecution *hypitPackSnapshot `json:"source_execution,omitempty"`
	SourceArchiveID string             `json:"source_archive_id,omitempty"`
}

func readHypitSnapshot(t *model.Task) hypitRuntimeSnapshot {
	var v hypitRuntimeSnapshot
	_ = json.Unmarshal(t.HypitRuntimeSnapshot, &v)
	return v
}
func (s *TaskService) freezeHypitTask(ctx context.Context, t *model.Task) error {
	if !model.IsHypitPlatform(t.Type) {
		return nil
	}
	if s.hypitCapabilities == nil {
		return ErrHypitInput
	}
	v := hypitRuntimeSnapshot{Profile: s.hypitCapabilities.config.NativeProfile(), Limits: s.hypitCapabilities.config.Limits}
	contract := &model.TaskExecution{}
	if err := applyAgentPackIdentity(contract, t.Type); err != nil {
		return err
	}
	v.SourceExecution = freezeHypitPack(contract)
	if s.runtimeDispatcher != nil {
		v.Image = s.runtimeDispatcher.ResolveRuntime(t.Type).Image
	}
	if t.InputSourceTaskID != "" {
		src, err := s.repo.Tasks().FindByID(ctx, t.InputSourceTaskID)
		if err != nil || src == nil || src.UserID != t.UserID || src.Type != model.PlatformHypit {
			return fmt.Errorf("%w: invalid source task", ErrHypitInput)
		}
		prior := readHypitSnapshot(src)
		if prior.Image != "" {
			v.Image = prior.Image
			v.Profile = prior.Profile
			v.Limits = prior.Limits
			v.SourceExecution = prior.SourceExecution
		}
		files, err := s.repo.TaskFiles().FindByTaskID(ctx, src.ID)
		if err != nil {
			return err
		}
		for _, f := range files {
			if f.State == model.TaskFileStateDelivered && f.FilePath == "output/project.zip" && f.Role == "project_archive" && f.FileSize > 0 && f.FileSize <= v.Limits.MaxProjectBytes && lowercaseSHA256.MatchString(f.ContentHash) {
				v.SourceTaskID = src.ID
				v.SourceArchiveID = f.ID
				if f.ExecutionID != "" {
					ex, e := s.repo.TaskExecutions().FindByID(ctx, f.ExecutionID)
					if e != nil {
						return e
					}
					if ex != nil && hasCompleteAgentPackIdentity(ex) {
						v.SourceExecution = freezeHypitPack(ex)
					}
				}
				break
			}
		}
		if v.SourceArchiveID == "" && t.HypitInput.Data().Reference == nil {
			return fmt.Errorf("%w: source task has no verified project archive", ErrHypitInput)
		}
	}
	b, err := json.Marshal(v)
	t.HypitRuntimeSnapshot = datatypes.JSON(b)
	return err
}
func resolveHypitUploadAsset(ctx context.Context, repo repository.Repository, store storage.Provider, user, raw string) (*model.Asset, error) {
	owned := strings.HasPrefix(raw, "/api/v1/files/") || strings.HasPrefix(raw, "/files/") || (store != nil && store.IsOwnedURL(raw))
	if !owned {
		return nil, nil
	}
	if store == nil || repo == nil {
		return nil, fmt.Errorf("%w: owned media requires verified storage identity", ErrHypitInput)
	}
	key, ok := uploadSessionKeyFromURL(raw)
	if !ok {
		return nil, fmt.Errorf("%w: internal media requires finalized upload or task_file_id", ErrHypitInput)
	}
	id := uploadSessionIDFromKey(key)
	if id == "" {
		return nil, ErrHypitInput
	}
	session, err := repo.UploadSessions().FindByID(ctx, id)
	if err != nil || session == nil || session.UserID != user || session.Purpose != DirectUploadPurposeHypitAsset || session.Status != model.UploadSessionFinalized {
		return nil, fmt.Errorf("%w: media upload is not finalized or owned", ErrHypitInput)
	}
	asset, err := repo.Assets().FindByID(ctx, session.AssetID)
	if err != nil || asset == nil || asset.StorageKey != key || asset.UserID != user {
		return nil, ErrHypitInput
	}
	return asset, nil
}
func (s *AgentBootstrapService) buildHypitFiles(ctx context.Context, t *model.Task, deadline time.Time, workloadDeadlines ...time.Time) ([]BootstrapFile, error) {
	if !model.IsHypitPlatform(t.Type) {
		return nil, nil
	}
	v := readHypitSnapshot(t)
	if v.Profile == nil {
		return nil, fmt.Errorf("%w: hypit snapshot missing", ErrAgentBootstrapConflict)
	}
	current := s.cfg.Hypit
	current.RuntimeProfile = v.Profile
	if len(current.MissingConfiguration()) > 0 {
		return nil, fmt.Errorf("%w: frozen hypit credentials unavailable", ErrAgentBootstrapUnavailable)
	}
	in := t.HypitInput.Data()
	in.SourceAssets = append([]model.HypitAsset(nil), in.SourceAssets...)
	if in.Reference != nil {
		a := *in.Reference
		in.Reference = &a
	}
	if v.SourceArchiveID != "" {
		src, err := s.repo.Tasks().FindByID(ctx, v.SourceTaskID)
		if err != nil || src == nil || src.UserID != t.UserID {
			return nil, ErrHypitInput
		}
		old := src.HypitInput.Data()
		if reflect.DeepEqual(in.Reference, old.Reference) {
			in.Reference = nil
		}
		next := []model.HypitAsset{}
		for _, a := range in.SourceAssets {
			found := false
			for _, previous := range old.SourceAssets {
				if reflect.DeepEqual(a, previous) {
					found = true
					break
				}
			}
			if !found {
				next = append(next, a)
			}
		}
		in.SourceAssets = next
	}
	assets := []*model.HypitAsset{}
	if in.Reference != nil {
		assets = append(assets, in.Reference)
	}
	for i := range in.SourceAssets {
		assets = append(assets, &in.SourceAssets[i])
	}
	files := []BootstrapFile{}
	var total int64
	for i, a := range assets {
		var key, hash string
		size := a.FileSize
		name := a.FileName
		mime := a.MimeType
		download := a.URL
		if a.TaskFileID != "" {
			if s.cfg.Store == nil {
				return nil, ErrAgentBootstrapUnavailable
			}
			trust, err := hypitSourceTrust(ctx, s.repo, t.UserID, t.InputSourceTaskID)
			if err != nil {
				return nil, err
			}
			if err := validateHypitTaskFiles(ctx, s.repo, t.UserID, t.ProjectID, s.cfg.Store.Name(), &model.HypitInput{SourceAssets: []model.HypitAsset{*a}}, v.Limits, trust...); err != nil {
				return nil, err
			}
			f, err := s.repo.TaskFiles().FindByID(ctx, a.TaskFileID)
			if err != nil {
				return nil, err
			}
			key, hash, size, name, mime = f.OSSKey, f.ContentHash, f.FileSize, f.FileName, f.MimeType
		} else {
			asset, err := resolveHypitUploadAsset(ctx, s.repo, s.cfg.Store, t.UserID, a.URL)
			if err != nil {
				return nil, err
			}
			if asset != nil {
				key, size, name, mime = asset.StorageKey, asset.Size, asset.FileName, asset.ContentType
			}
		}
		if key == "" {
			continue
		}
		if hash == "" {
			streamer, ok := s.cfg.Store.(storage.ObjectStreamProvider)
			if !ok {
				return nil, fmt.Errorf("%w: streaming media store required", ErrAgentBootstrapUnavailable)
			}
			stream, err := streamer.OpenObject(ctx, key)
			if err != nil {
				return nil, err
			}
			h := sha256.New()
			n, readErr := io.Copy(h, io.LimitReader(stream, v.Limits.MaxAssetBytes+1))
			closeErr := stream.Close()
			if readErr != nil || closeErr != nil || n != size || n > v.Limits.MaxAssetBytes {
				return nil, fmt.Errorf("%w: media size or stream mismatch", ErrAgentBootstrapConflict)
			}
			hash = hex.EncodeToString(h.Sum(nil))
		}
		if key != "" {
			var err error
			download, err = s.signedBootstrapObjectKey(ctx, key, deadline)
			if err != nil {
				return nil, err
			}
		}
		if size < 0 || size > v.Limits.MaxAssetBytes {
			return nil, ErrHypitInput
		}
		if size == 0 {
			total += v.Limits.MaxAssetBytes
		} else {
			total += size
		}
		if total > v.Limits.MaxInputBytes {
			return nil, fmt.Errorf("%w: input byte budget exceeded", ErrHypitInput)
		}
		if name == "" {
			name = path.Base(strings.Split(a.URL, "?")[0])
			if name == "." || name == "/" || name == "" {
				name = "media"
			}
		}
		name = serveragent.InputAttachmentFilename(i+1, model.EntryAttachment{FileName: "hypit-" + hash[:12] + "-" + name, ContentType: mime, Key: key})
		rel := path.Join("project/assets", name)
		files = append(files, BootstrapFile{Path: rel, DownloadURL: download, ContentSHA256: hash, ExpectedSize: size, MaxBytes: v.Limits.MaxAssetBytes, Mode: 0644})
		a.URL = rel
		a.TaskFileID = ""
		a.FileSize = size
		a.FileName = name
		a.MimeType = mime
	}
	payload := map[string]any{"brief": in.Brief, "reference": in.Reference, "source_assets": in.SourceAssets, "preferences": in.Preferences, "limits": v.Limits}
	runtime := map[string]any{"image": v.Image}
	if v.SourceExecution != nil {
		runtime["agent_pack_id"] = v.SourceExecution.ID
		runtime["agent_pack_version"] = v.SourceExecution.Version
		runtime["agent_pack_digest"] = v.SourceExecution.Digest
	}
	payload["runtime"] = runtime
	if len(workloadDeadlines) > 0 && !workloadDeadlines[0].IsZero() {
		payload["execution_deadline"] = workloadDeadlines[0].UTC().Format(time.RFC3339Nano)
	}
	if v.SourceArchiveID != "" {
		f, err := s.repo.TaskFiles().FindByID(ctx, v.SourceArchiveID)
		if err != nil || f == nil || f.TaskID != v.SourceTaskID || f.Role != "project_archive" || f.FilePath != "output/project.zip" || f.State != model.TaskFileStateDelivered || f.FileSize <= 0 || f.FileSize > v.Limits.MaxProjectBytes || !lowercaseSHA256.MatchString(f.ContentHash) || s.cfg.Store == nil || f.StorageProvider != s.cfg.Store.Name() {
			return nil, fmt.Errorf("%w: source project archive unavailable", ErrAgentBootstrapConflict)
		}
		signed, err := s.signedBootstrapObjectKey(ctx, f.OSSKey, deadline)
		if err != nil {
			return nil, err
		}
		files = append(files, BootstrapFile{Path: ".anban-creator/project.zip", DownloadURL: signed, ContentSHA256: f.ContentHash, ExpectedSize: f.FileSize, MaxBytes: v.Limits.MaxProjectBytes, Mode: 0644})
		payload["project_archive_path"] = ".anban-creator/project.zip"
	}
	for _, f := range []struct {
		path string
		data any
	}{{"input.json", payload}, {"runtime-profile.json", v.Profile}} {
		b, err := json.MarshalIndent(f.data, "", "  ")
		if err != nil {
			return nil, err
		}
		files = append(files, BootstrapFile{Path: f.path, Text: string(b), Mode: 0600})
	}
	return files, nil
}

func validateHypitUploadInputs(ctx context.Context, repo repository.Repository, store storage.Provider, user string, in *model.HypitInput, l config.HypitLimits) error {
	if in == nil {
		return ErrHypitInput
	}
	assets := []*model.HypitAsset{}
	if in.Reference != nil {
		assets = append(assets, in.Reference)
	}
	for i := range in.SourceAssets {
		assets = append(assets, &in.SourceAssets[i])
	}
	var total int64
	for _, a := range assets {
		if a.TaskFileID != "" {
			continue
		}
		asset, err := resolveHypitUploadAsset(ctx, repo, store, user, a.URL)
		if err != nil {
			return err
		}
		if asset != nil {
			if !montageSourceTaskFileTypeMatches(a.Type, asset.ContentType) {
				return fmt.Errorf("%w: asset MIME type mismatch", ErrHypitInput)
			}
			a.FileSize = asset.Size
			a.FileName = asset.FileName
			a.MimeType = asset.ContentType
		}
		size := a.FileSize
		if size > l.MaxAssetBytes {
			return fmt.Errorf("%w: asset too large", ErrHypitInput)
		}
		total += size
	}
	if total > l.MaxInputBytes {
		return fmt.Errorf("%w: input byte budget exceeded", ErrHypitInput)
	}
	return nil
}

func (s *PlanService) validateHypitPlanUploads(ctx context.Context, user string, in *model.HypitInput) error {
	var store storage.Provider
	if s.referenceAssets != nil {
		store = s.referenceAssets.store
	}
	return validateHypitUploadInputs(ctx, s.repo, store, user, in, s.hypitCapabilities.config.Limits)
}

// Persist only the Pack contract; TaskExecution JSON intentionally hides these contracts.
type hypitPackSnapshot struct {
	ID       string          `json:"id"`
	Version  string          `json:"version"`
	Digest   string          `json:"digest"`
	Delivery json.RawMessage `json:"delivery"`
	Required json.RawMessage `json:"required"`
	Adapter  string          `json:"adapter"`
	Profile  string          `json:"profile"`
}

func freezeHypitPack(ex *model.TaskExecution) *hypitPackSnapshot {
	return &hypitPackSnapshot{ID: ex.AgentPackID, Version: ex.AgentPackVersion, Digest: ex.AgentPackDigest, Delivery: append(json.RawMessage(nil), ex.AgentPackDeliveryContract...), Required: append(json.RawMessage(nil), ex.AgentPackRequiredArtifactContract...), Adapter: ex.RuntimeAdapter, Profile: ex.RuntimeProfile}
}
func (p *hypitPackSnapshot) execution() *model.TaskExecution {
	if p == nil {
		return nil
	}
	return &model.TaskExecution{AgentPackID: p.ID, AgentPackVersion: p.Version, AgentPackDigest: p.Digest, AgentPackDeliveryContract: datatypes.JSON(p.Delivery), AgentPackRequiredArtifactContract: datatypes.JSON(p.Required), RuntimeAdapter: p.Adapter, RuntimeProfile: p.Profile}
}

func (s *TaskService) hypitAdmissionCapabilities(ctx context.Context, p CreateManualParams) (*HypitCapabilityService, error) {
	if s.hypitCapabilities == nil {
		return nil, ErrHypitInput
	}
	cfg := s.hypitCapabilities.config
	if p.InputSourceTaskID != "" {
		src, err := s.repo.Tasks().FindByID(ctx, p.InputSourceTaskID)
		if err != nil || src == nil || src.UserID != p.UserID || src.Type != model.PlatformHypit {
			return nil, fmt.Errorf("%w: invalid source task", ErrHypitInput)
		}
		frozen := readHypitSnapshot(src)
		if frozen.Profile == nil || frozen.Limits.MaxDurationSeconds <= 0 {
			return nil, fmt.Errorf("%w: source runtime snapshot unavailable", ErrHypitInput)
		}
		cfg.RuntimeProfile = frozen.Profile
		cfg.Limits = frozen.Limits
	}
	return NewHypitCapabilityService(cfg), nil
}

// Each clone pins the immediately previous project version. Authorize media only
// through that owner's persisted ancestor chain, not unrelated same-user tasks.
func hypitSourceTrust(ctx context.Context, repo repository.Repository, user, source string) ([]montageSourceTaskTrust, error) {
	trust := []montageSourceTaskTrust{}
	seen := map[string]bool{}
	for source != "" {
		if seen[source] || len(trust) >= 64 {
			return nil, fmt.Errorf("%w: invalid source lineage", ErrHypitInput)
		}
		seen[source] = true
		t, err := repo.Tasks().FindByID(ctx, source)
		if err != nil || t == nil || t.UserID != user || t.Type != model.PlatformHypit {
			return nil, fmt.Errorf("%w: source lineage not owned", ErrHypitInput)
		}
		trust = append(trust, montageSourceTaskTrust{taskID: t.ID, projectID: t.ProjectID})
		source = t.InputSourceTaskID
	}
	return trust, nil
}
