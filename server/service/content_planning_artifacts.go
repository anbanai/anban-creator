package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/agentpack"
	"github.com/anbanai/anban-creator/server/model"
)

func planningHash(body []byte) string { sum := sha256.Sum256(body); return hex.EncodeToString(sum[:]) }

// validateContentPlanningArtifacts validates actual bytes after manifest sealing.
// Agent semantic judgments remain evidence, never a market-performance claim.
func (s *TaskService) validateContentPlanningArtifacts(ctx context.Context, task *model.Task, execution *model.TaskExecution) error {
	pack, ok := agentpack.Default().ForTaskType(task.Type)
	if !ok {
		return fmt.Errorf("planning Pack missing")
	}
	bodies := map[string][]byte{}
	for _, spec := range pack.ArtifactsForTaskType(task.Type) {
		if !spec.Required {
			continue
		}
		body, found, err := s.readExecutionArtifact(ctx, execution.ID, spec.Path, 2<<20)
		if err != nil {
			return err
		}
		if !found || len(strings.TrimSpace(string(body))) == 0 {
			return fmt.Errorf("missing planning artifact %s", spec.Path)
		}
		bodies[spec.Path] = body
		if spec.MIMEType != "application/json" {
			continue
		}
		var value any
		if json.Unmarshal(body, &value) != nil {
			return fmt.Errorf("invalid planning JSON %s", spec.Path)
		}
		if err := agentpack.ValidateOutput(pack, value); err != nil {
			return err
		}
		var identity model.PlanningIdentity
		if json.Unmarshal(body, &identity) != nil || identity.SchemaVersion != "1.0" || identity.TaskID != task.ID || identity.ExecutionID != execution.ID || identity.TaskKind != task.TaskKind || (identity.Status != "ready" && !(task.Type == model.TaskTypeContentResearch && identity.Status == "data_insufficient")) {
			return fmt.Errorf("planning artifact identity/readiness mismatch: %s", spec.Path)
		}
	}
	input, err := decodePlanningInput(task.TaskKind, task.AgentInput.Data())
	if err != nil {
		return err
	}
	if task.Type == model.TaskTypeContentResearch {
		var research model.PlanningResearchResult
		var evidence model.PlanningResearchEvidence
		_ = json.Unmarshal(bodies["output/content-research.json"], &research)
		_ = json.Unmarshal(bodies["output/research-evidence.json"], &evidence)
		if research.Status != evidence.Status {
			return fmt.Errorf("research/evidence status mismatch")
		}
		if research.Status == "data_insufficient" {
			if len(research.Candidates) != 0 || !planningInsufficientReasons(research.Missing) || !planningInsufficientReasons(evidence.Missing) {
				return fmt.Errorf("invalid data_insufficient research result")
			}
		} else if len(research.Candidates) == 0 || len(evidence.Sources) == 0 {
			return fmt.Errorf("data_insufficient: no cited proposals")
		}
		records, err := s.repo.SearchOperations().ListSucceeded(ctx, task.UserID, task.ProjectID, task.ID, execution.ID)
		if err != nil {
			return err
		}
		receipts := map[string]*model.SearchOperation{}
		for _, r := range records {
			receipts[r.ID] = r
		}
		sourceIDs := map[string]bool{}
		for _, source := range evidence.Sources {
			if source.ID == "" || sourceIDs[source.ID] || len(source.Facts) == 0 || !(planningSourceMatchesReceipt(source, receipts[source.OperationID]) || planningSourceMatchesInternal(source, input.InternalEvidence)) {
				return fmt.Errorf("research source has no matching execution receipt")
			}
			sourceIDs[source.ID] = true
		}
		allowedMerge := map[string]bool{}
		for _, candidate := range input.Candidates {
			if candidate.AssetID != "" {
				allowedMerge[candidate.AssetID] = true
			}
		}
		proposalIDs := map[string]bool{}
		for _, candidate := range research.Candidates {
			if candidate.ID == "" || proposalIDs[candidate.ID] || strings.TrimSpace(candidate.Title) == "" || strings.TrimSpace(candidate.SemanticKernel) == "" || len(candidate.Angles) == 0 || candidate.ProductionCost == "" || candidate.MaterialRisk == "" || candidate.ComplianceRisk == "" {
				return fmt.Errorf("research proposal incomplete")
			}
			proposalIDs[candidate.ID] = true
			if err := validatePlanningSourceRefs(candidate.SourceRefs, sourceIDs); err != nil {
				return err
			}
			for _, assessment := range []model.PlanningAssessment{candidate.TargetFit, candidate.Transferability} {
				if math.IsNaN(assessment.Score) || math.IsInf(assessment.Score, 0) || assessment.Score < 0 || assessment.Score > 100 || strings.TrimSpace(assessment.Rationale) == "" {
					return fmt.Errorf("invalid semantic assessment")
				}
				if err := validatePlanningSourceRefs(assessment.SourceRefs, sourceIDs); err != nil {
					return err
				}
			}
			for _, id := range candidate.MergeAssetIDs {
				if !allowedMerge[id] {
					return fmt.Errorf("merge suggestion outside frozen candidate batch")
				}
			}
		}
		return nil
	}
	sources := map[string]bool{}
	for _, ref := range input.AcceptedArtifacts {
		if ref.Path != "output/research-evidence.json" {
			continue
		}
		_, file, err := planningAcceptedArtifact(ctx, s.repo, s.store, task.UserID, task.ProjectID, ref)
		if err != nil {
			return err
		}
		body, found, err := s.readExecutionArtifact(ctx, file.ExecutionID, file.FilePath, 2<<20)
		if err != nil || !found {
			return fmt.Errorf("accepted evidence missing")
		}
		var evidence model.PlanningResearchEvidence
		if json.Unmarshal(body, &evidence) != nil {
			return fmt.Errorf("invalid accepted evidence")
		}
		for _, source := range evidence.Sources {
			sources[source.ID] = true
		}
	}
	if len(sources) == 0 {
		return fmt.Errorf("accepted sources missing")
	}
	if task.Type == model.TaskTypeContentValidation {
		article := bodies["output/validation-article.md"]
		if len([]rune(strings.TrimSpace(string(article)))) < 100 {
			return fmt.Errorf("validation article is not substantive text")
		}
		return validatePlanningReview(bodies["output/validation-review.json"], article, sources, false)
	}
	var script model.PlanningVideoScript
	if json.Unmarshal(bodies["output/video-script.json"], &script) != nil {
		return fmt.Errorf("invalid script")
	}
	target := task.ProjectSnapshot.Data().Platform
	if input.TargetRequirements != nil {
		target = input.TargetRequirements.Platform
	}
	if script.Platform != target || len(script.Scenes) == 0 || strings.TrimSpace(script.SpokenText) == "" {
		return fmt.Errorf("script target/text missing")
	}
	if err := validatePlanningSourceRefs(script.SourceRefs, sources); err != nil {
		return err
	}
	var spoken, srt strings.Builder
	end := 0.0
	for i, scene := range script.Scenes {
		if scene.Index != i+1 || math.Abs(scene.StartSeconds-end) > 0.0001 || scene.EndSeconds <= scene.StartSeconds || math.Round(scene.EndSeconds*1000) <= math.Round(scene.StartSeconds*1000) || scene.EndSeconds > 3600 || math.IsNaN(scene.EndSeconds) || strings.TrimSpace(scene.SpokenText) == "" || strings.TrimSpace(scene.Visuals) == "" || len(scene.MaterialRequirements) == 0 {
			return fmt.Errorf("invalid script scene timing or content")
		}
		if err := validatePlanningSourceRefs(scene.SourceRefs, sources); err != nil {
			return err
		}
		if i > 0 {
			spoken.WriteString("\n")
		}
		spoken.WriteString(strings.TrimSpace(scene.SpokenText))
		fmt.Fprintf(&srt, "%d\n%s --> %s\n%s\n\n", i+1, planningSRTTime(scene.StartSeconds), planningSRTTime(scene.EndSeconds), strings.TrimSpace(scene.SpokenText))
		end = scene.EndSeconds
	}
	if strings.TrimSpace(script.SpokenText) != spoken.String() {
		return fmt.Errorf("spoken text does not match ordered scenes")
	}
	if input.TargetRequirements != nil && input.TargetRequirements.DurationSeconds > 0 && end > input.TargetRequirements.DurationSeconds+0.001 {
		return fmt.Errorf("script exceeds frozen duration")
	}
	if strings.TrimSpace(strings.ReplaceAll(string(bodies["output/video-subtitles.srt"]), "\r\n", "\n")) != strings.TrimSpace(srt.String()) {
		return fmt.Errorf("subtitles do not match script timing/text")
	}
	readableScript := string(bodies["output/video-script.md"])
	for _, scene := range script.Scenes {
		if !strings.Contains(readableScript, strings.TrimSpace(scene.SpokenText)) {
			return fmt.Errorf("readable script does not contain spoken text")
		}
	}
	return validatePlanningReview(bodies["output/script-review.json"], bodies["output/video-script.json"], sources, true)
}

func validatePlanningSourceRefs(refs []string, allowed map[string]bool) error {
	if len(refs) == 0 {
		return fmt.Errorf("source references required")
	}
	for _, ref := range refs {
		if !allowed[ref] {
			return fmt.Errorf("unknown source reference %s", ref)
		}
	}
	return nil
}
func validatePlanningReview(body, subject []byte, sources map[string]bool, script bool) error {
	var review model.PlanningReview
	if json.Unmarshal(body, &review) != nil || review.ValidationKind != "semantic_only" || review.ContentSHA256 != planningHash(subject) || len(review.Missing) > 0 {
		return fmt.Errorf("invalid semantic review or content hash")
	}
	if err := validatePlanningSourceRefs(review.SourceRefs, sources); err != nil {
		return err
	}
	required := map[string]bool{"facts": false, "evidence": false, "compliance": false, "target_fit": false}
	if script {
		required["timing"] = false
		required["materials"] = false
	}
	for _, check := range review.Checks {
		if done, ok := required[check.ID]; !ok || done || check.Status != "pass" || strings.TrimSpace(check.Rationale) == "" {
			return fmt.Errorf("review check invalid")
		}
		if err := validatePlanningSourceRefs(check.SourceRefs, sources); err != nil {
			return err
		}
		required[check.ID] = true
	}
	for id, done := range required {
		if !done {
			return fmt.Errorf("missing review check %s", id)
		}
	}
	return nil
}
func planningSourceMatchesReceipt(source model.PlanningSource, record *model.SearchOperation) bool {
	if record == nil || source.InternalReference != nil {
		return false
	}
	if record.Operation == "mcp.search_web" {
		var response WebSearchResponse
		if json.Unmarshal(record.ResponseSnapshot, &response) != nil {
			return false
		}
		for _, item := range response.Results {
			fetched := item.FetchedAt
			if fetched.IsZero() {
				fetched = response.FetchedAt
			}
			if item.URL == source.URL && item.PublishedAt == source.PublishedAt && planningSameTime(source.FetchedAt, fetched) && planningFactsIn(source.Facts, item.Title+"\n"+item.Snippet+"\n"+item.Summary+"\n"+item.Content) {
				return true
			}
		}
	}
	if record.Operation == "mcp.fetch_web_page" {
		var response PageFetchResponse
		if json.Unmarshal(record.ResponseSnapshot, &response) == nil && response.URL == source.URL && source.PublishedAt == "" && planningSameTime(source.FetchedAt, response.FetchedAt) && planningFactsIn(source.Facts, response.Title+"\n"+response.ContentMarkdown) {
			return true
		}
	}
	return false
}
func planningSameTime(value string, expected time.Time) bool {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && !expected.IsZero() && parsed.Equal(expected)
}
func planningSRTTime(seconds float64) string {
	ms := int64(math.Round(seconds * 1000))
	return fmt.Sprintf("%02d:%02d:%02d,%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}

func planningSourceMatchesInternal(source model.PlanningSource, frozen []model.PlanningInternalEvidence) bool {
	if source.InternalReference == nil || source.OperationID != "" {
		return false
	}
	for _, item := range frozen {
		if *source.InternalReference == item.Reference && source.URL == item.Content.URL && planningSameTime(source.FetchedAt, item.DataAt) {
			published := ""
			if item.Content.Date != nil {
				published = item.Content.Date.UTC().Format(time.RFC3339Nano)
			}
			if source.PublishedAt != published {
				return false
			}
			for _, fact := range source.Facts {
				if strings.TrimSpace(fact) == "" || !strings.Contains(item.Content.Title, fact) {
					return false
				}
			}
			return true
		}
	}
	return false
}

func planningFactsIn(facts []string, evidence string) bool {
	if len(facts) == 0 {
		return false
	}
	for _, fact := range facts {
		if strings.TrimSpace(fact) == "" || !strings.Contains(evidence, fact) {
			return false
		}
	}
	return true
}

func planningInsufficientReasons(missing []string) bool {
	if len(missing) == 0 {
		return false
	}
	for _, reason := range missing {
		switch reason {
		case "no_relevant_sources", "source_evidence_insufficient", "internal_history_unavailable":
		default:
			return false
		}
	}
	return true
}
