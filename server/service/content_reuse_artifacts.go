package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
)

type contentReuseContentRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type contentReuseArtifactIdentity struct {
	Source                string                     `json:"source"`
	DataAt                json.RawMessage            `json:"data_at"`
	Missing               []string                   `json:"missing"`
	EvidencePaths         []string                   `json:"evidence_paths"`
	Confidence            string                     `json:"confidence"`
	SourceContent         *contentReuseContentRef    `json:"source_content"`
	TargetContent         *contentReuseContentRef    `json:"target_content"`
	Relation              string                     `json:"relation"`
	Angle                 string                     `json:"angle"`
	SemanticKernel        string                     `json:"semantic_kernel"`
	CorePoints            []string                   `json:"core_points"`
	ReplaceableExamples   []string                   `json:"replaceable_examples"`
	FormatRequirements    map[string]json.RawMessage `json:"format_requirements"`
	CostLevel             string                     `json:"cost_level"`
	MaterialRequirements  []string                   `json:"material_requirements"`
	ComplianceConstraints []string                   `json:"compliance_constraints"`
	SchemaVersion         int                        `json:"schema_version"`
	Status                string                     `json:"status"`
	AssetID               string                     `json:"asset_id"`
	ReuseRunID            string                     `json:"reuse_run_id"`
	TaskID                string                     `json:"task_id"`
	TargetPlatform        string                     `json:"target_platform"`
	Checks                []struct {
		ID            string   `json:"id"`
		Status        string   `json:"status"`
		Reason        string   `json:"reason"`
		EvidencePaths []string `json:"evidence_paths"`
	} `json:"checks"`
}

// Reuse reports supplement each Pack's existing delivery and semantic gates.
// They are untrusted evidence, never authority to change run/publication state.
func (s *TaskService) validateContentReuseReports(ctx context.Context, task *model.Task, execution *model.TaskExecution) error {
	qualityPath := "output/quality-report.json"
	if model.IsHypitPlatform(task.Type) {
		qualityPath = "output/semantic-report.json"
	}
	for _, path := range []string{"output/content-reuse-brief.json", "output/content-lineage.json", qualityPath} {
		body, found, err := s.readExecutionArtifact(ctx, execution.ID, path, maxTaskDeliveryJSONBytes)
		if err != nil {
			return fmt.Errorf("read content reuse report %s: %w", path, err)
		}
		if !found {
			return fmt.Errorf("content reuse report %s missing", path)
		}
		if path == qualityPath {
			var envelope map[string]json.RawMessage
			if err := json.Unmarshal(body, &envelope); err != nil {
				return fmt.Errorf("invalid content reuse report %s", path)
			}
			if nested, ok := envelope["content_reuse"]; ok {
				body = nested
			} else if model.IsHypitPlatform(task.Type) {
				return fmt.Errorf("content reuse semantic review missing")
			}
		}
		var report contentReuseArtifactIdentity
		if err := json.Unmarshal(body, &report); err != nil {
			return fmt.Errorf("invalid content reuse report %s: %w", path, err)
		}
		if report.SchemaVersion != 1 || report.AssetID != task.ContentAssetID || report.ReuseRunID != task.ReuseRunID || report.TaskID != task.ID || report.TargetPlatform != contentReuseTaskPlatform(task) {
			return fmt.Errorf("content reuse report %s identity or version mismatch", path)
		}
		if report.Status != "ready" && report.Status != "warning" {
			return fmt.Errorf("content reuse report %s is not ready", path)
		}
		if err := validateReuseReportFields(report, task, path); err != nil {
			return fmt.Errorf("content reuse report %s: %w", path, err)
		}
		if path == qualityPath {
			if len(report.Checks) != 5 {
				return fmt.Errorf("content reuse quality checks missing")
			}
			seen := map[string]bool{}
			for _, check := range report.Checks {
				if seen[check.ID] {
					return fmt.Errorf("duplicate content reuse quality check")
				}
				seen[check.ID] = true
				if check.Status == "warning" && report.Status != "warning" {
					return fmt.Errorf("content reuse quality warning omitted from report status")
				}
				if strings.TrimSpace(check.Reason) == "" || !reuseStringArray(check.EvidencePaths, false) || check.ID == "" || (check.Status != "passed" && check.Status != "warning") {
					return fmt.Errorf("content reuse quality check is blocked or invalid")
				}
			}
			for _, required := range []string{"fact_sources", "core_consistency", "platform_fit", "compliance", "material_rights"} {
				if !seen[required] {
					return fmt.Errorf("content reuse quality check %s missing", required)
				}
			}

		}
	}
	return nil
}

func contentReuseTaskPlatform(task *model.Task) string {
	if task.Type == model.TaskTypeWechatArticle || task.Type == model.TaskTypeWechatPicture {
		return model.PlatformWechat
	}
	return task.Type
}

func reuseStringArray(values []string, nonempty bool) bool {
	if values == nil || (nonempty && len(values) == 0) {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}

func validateReuseReportFields(report contentReuseArtifactIdentity, task *model.Task, path string) error {
	if strings.TrimSpace(report.Source) == "" || !reuseStringArray(report.Missing, false) || !reuseStringArray(report.EvidencePaths, true) {
		return fmt.Errorf("source and evidence metadata required")
	}
	if report.Confidence != "low" && report.Confidence != "medium" && report.Confidence != "high" {
		return fmt.Errorf("invalid confidence")
	}
	if len(report.Missing) > 0 && (report.Confidence == "high" || report.Status != "warning") {
		return fmt.Errorf("missing evidence requires warning and reduced confidence")
	}
	if len(report.DataAt) == 0 {
		return fmt.Errorf("data_at required")
	}
	if string(report.DataAt) == "null" {
		if !slices.Contains(report.Missing, "data_at") {
			return fmt.Errorf("unknown data date must be disclosed")
		}
	} else {
		var date time.Time
		if err := json.Unmarshal(report.DataAt, &date); err != nil || date.IsZero() || date.After(time.Now().UTC()) {
			return fmt.Errorf("invalid evidence date")
		}
	}
	if path == "output/content-reuse-brief.json" || path == "output/content-lineage.json" {
		if task.SourceContentRef == "" {
			if report.SourceContent != nil {
				return fmt.Errorf("unexpected source content")
			}
		} else {
			ref := strings.SplitN(task.SourceContentRef, ":", 2)
			if len(ref) != 2 || report.SourceContent == nil || report.SourceContent.Kind != ref[0] || report.SourceContent.ID != ref[1] {
				return fmt.Errorf("source content identity mismatch")
			}
		}
	}
	switch path {
	case "output/content-reuse-brief.json":
		if strings.TrimSpace(report.Angle) == "" || strings.TrimSpace(report.SemanticKernel) == "" || !reuseStringArray(report.CorePoints, true) || !reuseStringArray(report.ReplaceableExamples, false) || !reuseStringArray(report.MaterialRequirements, false) || !reuseStringArray(report.ComplianceConstraints, false) {
			return fmt.Errorf("platform brief incomplete")
		}
		if !slices.Contains([]string{"L1", "L2", "L3", "L4"}, report.CostLevel) {
			return fmt.Errorf("invalid cost level")
		}
		for _, field := range []string{"form", "title_strategy", "opening_strategy", "length", "visual_or_spoken_requirements"} {
			var text string
			if err := json.Unmarshal(report.FormatRequirements[field], &text); err != nil || strings.TrimSpace(text) == "" {
				return fmt.Errorf("format requirement %s missing or invalid", field)
			}
		}
	case "output/content-lineage.json":
		if report.Relation != "derived_from" || report.TargetContent == nil || report.TargetContent.Kind != "task" || report.TargetContent.ID != task.ID {
			return fmt.Errorf("target content identity mismatch")
		}
	}
	return nil
}
