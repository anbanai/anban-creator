package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

// freezePlanningInternalSources resolves references against Server analytics at
// admission. The derived snapshot is injected only after user-schema validation.
func (s *TaskService) freezePlanningInternalSources(ctx context.Context, userID string, input map[string]any) error {
	body, _ := json.Marshal(input)
	var value model.ContentPlanningInput
	if err := json.Unmarshal(body, &value); err != nil {
		return err
	}
	frozen := []model.PlanningInternalEvidence{}
	if len(value.InternalSources) > 50 {
		return fmt.Errorf("%w: too many internal sources", ErrInvalidAgentInput)
	}
	for _, ref := range value.InternalSources {
		project, err := s.repo.Projects().FindByID(ctx, ref.ProjectID)
		if err != nil || project.UserID != userID {
			return fmt.Errorf("%w: internal source project is not owned", ErrInvalidAgentInput)
		}
		family, err := model.AnalyticsMetricFamilyForPlatform(project.Platform)
		if err != nil {
			return err
		}
		analytics := s.repo.Analytics().WithMetricFamily(family)
		var state *model.AnalyticsState
		var content *model.AnalyticsContent
		err = analytics.Snapshot(ctx, func(snapshot *repository.AnalyticsRepository) error {
			var err error
			state, err = snapshot.State(ctx, project.ID)
			if err != nil || state == nil || state.Revision != ref.AnalyticsRevision || state.Revision <= 0 || state.Status != "ready" {
				return fmt.Errorf("%w: internal analytics revision unavailable", ErrInvalidAgentInput)
			}
			content, err = snapshot.ResolveContent(ctx, project.ID, ref.ContentID)
			if err != nil || content.ProjectID != project.ID || content.ID != ref.ContentID {
				return fmt.Errorf("%w: internal content unavailable", ErrInvalidAgentInput)
			}
			return nil
		})
		if err != nil {
			return err
		}

		if content.TaskID != "" {
			task, err := s.repo.Tasks().FindByID(ctx, content.TaskID)
			if err != nil || task.UserID != userID || task.ProjectID != project.ID {
				return fmt.Errorf("%w: internal content task ownership", ErrInvalidAgentInput)
			}
		}
		frozen = append(frozen, model.PlanningInternalEvidence{Reference: ref, Content: *content, DataAt: state.UpdatedAt})
	}
	if len(frozen) > 0 {
		input["internal_evidence"] = frozen
	}
	return nil
}
