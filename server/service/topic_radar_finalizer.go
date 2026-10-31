package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

type topicDigestArtifact struct {
	topicArtifactEnvelope
	SchemaVersion string                  `json:"schema_version"`
	GeneratedAt   time.Time               `json:"generated_at"`
	Suggestions   []topicDigestSuggestion `json:"suggestions"`
}

type topicDigestSuggestion struct {
	Title         string                 `json:"title"`
	Introduction  string                 `json:"introduction"`
	SourceSummary string                 `json:"source_summary"`
	SourceRefs    []model.TopicSourceRef `json:"source_refs"`
	Scores        map[string]float64     `json:"scores"`
	VideoFit      string                 `json:"video_fit"`
}

func (s *TopicRadarService) FinalizeDigest(ctx context.Context, digestID string, artifact []byte) error {
	if strings.TrimSpace(digestID) == "" {
		return errors.New("digest_id is required")
	}
	digest, err := s.repo.TopicDigests().FindByID(ctx, digestID)
	if err != nil {
		return err
	}
	return s.finalizeOwnedTopicDigest(ctx, digest, artifact)
}

func (s *TopicRadarService) finalizeOwnedTopicDigest(ctx context.Context, digest *model.TopicDigest, artifact []byte) error {
	if digest.Status == model.TopicDigestStatusSucceeded {
		return nil
	}
	if digest.Status == model.TopicDigestStatusFailed {
		if strings.TrimSpace(digest.Error) == "" {
			return errors.New("topic digest is already terminally failed")
		}
		return errors.New(digest.Error)
	}
	var parsed topicDigestArtifact
	if err := json.Unmarshal(artifact, &parsed); err != nil {
		return s.failDigest(ctx, digest, fmt.Errorf("invalid topic digest artifact: %w", err))
	}
	if err := parsed.topicArtifactEnvelope.validate("topic_digest", digest.AgentTaskID, digest.AgentExecutionID); err != nil {
		return s.failDigest(ctx, digest, err)
	}
	if parsed.SchemaVersion != "1.0" {
		return s.failDigest(ctx, digest, fmt.Errorf("unsupported topic digest schema version %q", parsed.SchemaVersion))
	}
	if parsed.GeneratedAt.IsZero() {
		return s.failDigest(ctx, digest, errors.New("topic digest generated_at is required"))
	}
	if len(parsed.Suggestions) == 0 {
		return s.failDigest(ctx, digest, errors.New("topic digest contains no suggestions"))
	}
	frozenRefs, _ := frozenTopicSourceRefs(digest.InputSnapshot)
	for i := range parsed.Suggestions {
		candidate := &parsed.Suggestions[i]
		if strings.TrimSpace(candidate.Title) == "" || strings.TrimSpace(candidate.Introduction) == "" || strings.TrimSpace(candidate.SourceSummary) == "" {
			return s.failDigest(ctx, digest, fmt.Errorf("suggestion %d is missing title, introduction, or source summary", i))
		}
		if len(candidate.SourceRefs) == 0 {
			return s.failDigest(ctx, digest, fmt.Errorf("suggestion %d has no source references", i))
		}
		for j, ref := range candidate.SourceRefs {
			if !ref.Resolvable() {
				return s.failDigest(ctx, digest, fmt.Errorf("suggestion %d contains an unresolvable source reference", i))
			}
			canonical, found := canonicalTopicSourceRef(ref, frozenRefs)
			if !found {
				return s.failDigest(ctx, digest, fmt.Errorf("suggestion %d contains a source outside the frozen snapshot", i))
			}
			candidate.SourceRefs[j] = canonical
		}
	}
	if err := rankTopicDigestSuggestions(digest.InputSnapshot, parsed.Suggestions); err != nil {
		return s.failDigest(ctx, digest, err)
	}

	err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		now := s.now()
		digest.Status = model.TopicDigestStatusSucceeded
		digest.DispatchStatus = model.TopicDigestDispatchClaimed
		digest.CandidateCount = len(parsed.Suggestions)
		digest.ActiveKey = nil
		digest.FinishedAt = &now
		digest.Error = ""
		won, err := tx.TopicDigests().UpdateIfNonterminal(ctx, digest)
		if err != nil {
			return err
		}
		if !won {
			return nil
		}
		suggestions := make([]*model.TopicSuggestion, 0, len(parsed.Suggestions))
		adoptions := make([]*model.TopicSuggestionAdoption, 0, len(parsed.Suggestions))
		for i, candidate := range parsed.Suggestions {
			sourceRefs, marshalErr := json.Marshal(candidate.SourceRefs)
			if marshalErr != nil {
				return marshalErr
			}
			scores, marshalErr := json.Marshal(candidate.Scores)
			if marshalErr != nil {
				return marshalErr
			}
			suggestion := &model.TopicSuggestion{
				ID:             uuid.NewString(),
				DigestID:       digest.ID,
				ProjectID:      digest.ProjectID,
				Title:          strings.TrimSpace(candidate.Title),
				Introduction:   strings.TrimSpace(candidate.Introduction),
				SourceSummary:  strings.TrimSpace(candidate.SourceSummary),
				SourceRefs:     sourceRefs,
				Scores:         scores,
				VideoFit:       strings.TrimSpace(candidate.VideoFit),
				Rank:           i + 1,
				AdoptionStatus: model.TopicAdoptionPending,
			}
			suggestions = append(suggestions, suggestion)
			adoptions = append(adoptions, &model.TopicSuggestionAdoption{
				ID:           uuid.NewString(),
				SuggestionID: suggestion.ID,
				DigestID:     digest.ID,
				UserID:       digest.UserID,
				ProjectID:    digest.ProjectID,
				Status:       model.TopicAdoptionPending,
			})
		}
		if err := tx.TopicSuggestions().CreateBatch(ctx, suggestions); err != nil {
			return err
		}
		if err := tx.TopicSuggestionAdoptions().CreateBatch(ctx, adoptions); err != nil {
			return err
		}
		return nil
	})
	return err
}

func frozenTopicSourceRefs(snapshot []byte) ([]model.TopicSourceRef, bool) {
	var root any
	if len(snapshot) == 0 || json.Unmarshal(snapshot, &root) != nil {
		return nil, false
	}
	refs := make([]model.TopicSourceRef, 0)
	var visit func(any)
	visit = func(value any) {
		switch item := value.(type) {
		case map[string]any:
			for key, child := range item {
				if key == "source_refs" || key == "sources" {
					if data, err := json.Marshal(child); err == nil {
						var parsed []model.TopicSourceRef
						if json.Unmarshal(data, &parsed) == nil {
							refs = append(refs, parsed...)
						}
					}
				}
				visit(child)
			}
		case []any:
			for _, child := range item {
				visit(child)
			}
		}
	}
	visit(root)
	return refs, len(refs) > 0
}

func topicSourceRefInSnapshot(candidate model.TopicSourceRef, frozen []model.TopicSourceRef) bool {
	_, found := canonicalTopicSourceRef(candidate, frozen)
	return found
}

func canonicalTopicSourceRef(candidate model.TopicSourceRef, frozen []model.TopicSourceRef) (model.TopicSourceRef, bool) {
	for _, ref := range frozen {
		if candidate.Type == "" || candidate.Type != ref.Type {
			continue
		}
		if candidate.Platform != "" && candidate.Platform != ref.Platform {
			continue
		}
		if candidate.RefID != "" && candidate.RefID != ref.RefID {
			continue
		}
		if candidate.URL != "" && candidate.URL != ref.URL {
			continue
		}
		if candidate.Resolvable() {
			return ref, true
		}
	}
	return model.TopicSourceRef{}, false
}

func (s *TopicRadarService) failDigest(ctx context.Context, digest *model.TopicDigest, cause error) error {
	digest.Status = model.TopicDigestStatusFailed
	digest.ActiveKey = nil
	digest.Error = cause.Error()
	now := s.now()
	digest.FinishedAt = &now
	if _, err := s.repo.TopicDigests().UpdateIfNonterminal(ctx, digest); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func (s *TopicRadarService) AdoptSuggestion(ctx context.Context, adoptionID string) error {
	adoption, err := s.repo.TopicSuggestionAdoptions().FindByID(ctx, adoptionID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err != nil {
		return err
	}
	if adoption.Status == model.TopicAdoptionAdopted || adoption.Status == model.TopicAdoptionDuplicate {
		return nil
	}
	token := uuid.NewString()
	until := s.now().Add(2 * time.Minute)
	claimed, err := s.repo.TopicSuggestionAdoptions().Claim(ctx, adoption.ID, token, until)
	if err != nil {
		return err
	}
	if !claimed {
		return errors.New("topic adoption is already claimed")
	}
	suggestion, err := s.repo.TopicSuggestions().FindByID(ctx, adoption.SuggestionID)
	if err != nil {
		return err
	}
	err = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		topic := &model.TopicPool{UserID: adoption.UserID, ProjectID: adoption.ProjectID, Topic: suggestion.Title, SourceDigestID: &adoption.DigestID, SourceSuggestionID: &suggestion.ID, SourceLabel: suggestion.SourceSummary, Status: model.TopicStatusUnused}
		if _, err := tx.TopicPools().AddWithSource(ctx, topic); err != nil {
			return err
		}
		adoption.TopicPoolID = &topic.ID
		adoption.Status = model.TopicAdoptionAdopted
		if topic.SourceSuggestionID == nil || *topic.SourceSuggestionID != suggestion.ID {
			adoption.Status = model.TopicAdoptionDuplicate
		}
		adoption.Error = ""
		if err := tx.TopicSuggestionAdoptions().UpdateClaimed(ctx, adoption, token); err != nil {
			return err
		}
		suggestion.AdoptionStatus = adoption.Status
		return tx.TopicSuggestions().Update(ctx, suggestion)
	})
	if err != nil {
		adoption.Status = model.TopicAdoptionFailed
		adoption.Error = err.Error()
		if updateErr := s.repo.TopicSuggestionAdoptions().UpdateClaimed(ctx, adoption, token); updateErr != nil {
			return errors.Join(err, updateErr)
		}
	}
	return err
}
