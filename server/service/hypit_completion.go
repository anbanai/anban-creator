package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/storage"
	"regexp"
	"strings"
)

var hypitRevisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

var hypitRequired = map[string]string{"output/final.mp4": "final_video", "output/cover.png": "cover", "output/project.json": "project_manifest", "output/project.zip": "project_archive", "output/delivery-manifest.json": "delivery_manifest", "output/quality-report.json": "quality_report"}

func validateHypitCompletionArtifacts(files []*model.TaskFile) agent.ArtifactValidation {
	found := map[string]bool{}
	for _, f := range files {
		if f != nil && f.FileSize > 0 && f.OSSKey != "" && lowercaseSHA256.MatchString(f.ContentHash) && (f.State == model.TaskFileStatePending || f.State == model.TaskFileStateDelivered) && hypitRequired[f.FilePath] == f.Role {
			found[f.FilePath] = true
		}
	}
	v := agent.ArtifactValidation{Valid: true, MeaningfulFileCount: len(found)}
	for p := range hypitRequired {
		if !found[p] {
			v.Valid = false
			v.Missing = append(v.Missing, p)
		}
	}
	return v
}
func (s *TaskService) validateHypitReports(ctx context.Context, t *model.Task, files []*model.TaskFile) error {
	l := readHypitSnapshot(t).Limits
	revision := ""
	for _, f := range files {
		if f.FilePath == "output/final.mp4" && f.FileSize > l.MaxVideoBytes || f.FilePath == "output/project.zip" && f.FileSize > l.MaxProjectBytes {
			return fmt.Errorf("%w: hypit output exceeds frozen limits", ErrTaskDeliveryObjectInvalid)
		}
		if f.FilePath != "output/quality-report.json" && f.FilePath != "output/project.json" {
			continue
		}
		b, err := storage.ReadObject(ctx, s.store, f.OSSKey, 2<<20)
		if err != nil {
			return fmt.Errorf("%w: read hypit report: %v", ErrTaskDeliveryObjectInvalid, err)
		}
		var report struct {
			SchemaVersion int  `json:"schema_version"`
			Passed        bool `json:"passed"`
			Upstream      struct {
				Repository string `json:"repository"`
				Revision   string `json:"revision"`
			} `json:"upstream"`
			Checks map[string]bool `json:"checks"`
			Media  struct {
				Duration float64 `json:"duration_seconds"`
				Width    int     `json:"width"`
				Height   int     `json:"height"`
			} `json:"media"`
			ProjectRoot string `json:"project_root"`
			RunPath     string `json:"run_path"`
		}
		if json.Unmarshal(b, &report) != nil || report.SchemaVersion != 1 || !hypitRevisionPattern.MatchString(report.Upstream.Revision) || strings.TrimSuffix(report.Upstream.Repository, ".git") != "https://github.com/hypit-ai/hypit" {
			return fmt.Errorf("%w: invalid hypit report identity", ErrTaskDeliveryObjectInvalid)
		}
		if revision != "" && revision != report.Upstream.Revision {
			return fmt.Errorf("%w: inconsistent hypit upstream revisions", ErrTaskDeliveryObjectInvalid)
		}
		revision = report.Upstream.Revision
		if f.FilePath == "output/project.json" {
			if report.ProjectRoot != "/workspace/project" || report.RunPath != "productions/main/runs/main.svrun" {
				return fmt.Errorf("%w: invalid hypit project paths", ErrTaskDeliveryObjectInvalid)
			}
			continue
		}
		if !report.Passed || report.Media.Duration <= 0 || report.Media.Duration > float64(l.MaxDurationSeconds) || report.Media.Width <= 0 || report.Media.Height <= 0 {
			return fmt.Errorf("%w: hypit objective quality failed", ErrTaskDeliveryObjectInvalid)
		}
		for _, k := range []string{"semantic", "video_probe", "video_full_decode", "cover_decode", "project_references", "official_check", "official_plan", "project_archive"} {
			if !report.Checks[k] {
				return fmt.Errorf("%w: hypit check %s not passed", ErrTaskDeliveryObjectInvalid, k)
			}
		}
	}
	return nil
}
func taskArtifactByteLimit(t *model.Task, p string) int64 {
	if t != nil && model.IsHypitPlatform(t.Type) {
		l := readHypitSnapshot(t).Limits
		switch p {
		case "output/project.zip":
			if l.MaxProjectBytes > 0 {
				return l.MaxProjectBytes
			}
		case "output/final.mp4":
			if l.MaxVideoBytes > 0 {
				return l.MaxVideoBytes
			}
		}
	}
	return maxTaskArtifactUploadBytes
}
