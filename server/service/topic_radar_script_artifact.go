package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/anbanai/anban-creator/server/model"
)

func (s *TopicRadarService) GetScriptArtifact(ctx context.Context, userID, id string) (json.RawMessage, error) {
	job, err := s.GetScript(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if job.Status != model.TopicScriptJobSucceeded || job.ExecutionID == nil {
		return nil, fmt.Errorf("%w: script artifact is not ready", ErrTopicSubscriptionInvalid)
	}
	data, err := s.topicExecutionArtifact(ctx, *job.ExecutionID, "output/topic-script.json")
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(data)
	if len(data) == 0 || !json.Valid(data) || hex.EncodeToString(hash[:]) != job.ArtifactSHA256 {
		return nil, errors.New("script artifact integrity check failed")
	}
	return json.RawMessage(data), nil
}
