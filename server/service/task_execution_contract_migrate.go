package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anbanai/anban-creator/server/agentpack"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

type TaskExecutionContractMigrationStats struct {
	Backfilled int
	Unmatched  int
}

type frozenPackContractKey struct {
	ID       string
	Version  string
	Digest   string
	TaskType string
}

type frozenPackContracts struct {
	Delivery []agentpack.DeliverySpec
	Required []agentpack.ArtifactSpec
}

type taskExecutionContractCandidate struct {
	ID                                string
	TaskType                          string
	AgentPackID                       string
	AgentPackVersion                  string
	AgentPackDigest                   string
	AgentPackDeliveryContract         []byte
	AgentPackRequiredArtifactContract []byte
}

// MigrateTaskExecutionContracts backfills execution-owned contracts only when
// the task type and complete immutable Pack identity match an audited entry.
// Unknown identities remain empty so all runtime authorization stays closed.
func MigrateTaskExecutionContracts(ctx context.Context, db *gorm.DB, logger *zerolog.Logger) (TaskExecutionContractMigrationStats, error) {
	var stats TaskExecutionContractMigrationStats
	if db == nil {
		return stats, nil
	}
	registry, err := taskExecutionContractMigrationRegistry()
	if err != nil {
		return stats, err
	}

	var candidates []taskExecutionContractCandidate
	if err := db.WithContext(ctx).
		Table("task_executions AS e").
		Select("e.id, tasks.type AS task_type, e.agent_pack_id, e.agent_pack_version, e.agent_pack_digest, e.agent_pack_delivery_contract, e.agent_pack_required_artifact_contract").
		Joins("LEFT JOIN tasks ON tasks.id = e.task_id").
		Where("e.agent_pack_delivery_contract IS NULL OR e.agent_pack_delivery_contract = '' OR e.agent_pack_required_artifact_contract IS NULL OR e.agent_pack_required_artifact_contract = ''").
		Order("e.id ASC").
		Scan(&candidates).Error; err != nil {
		return stats, fmt.Errorf("list task executions missing frozen contracts: %w", err)
	}

	for _, candidate := range candidates {
		key := frozenPackContractKey{
			ID:       strings.TrimSpace(candidate.AgentPackID),
			Version:  strings.TrimSpace(candidate.AgentPackVersion),
			Digest:   strings.TrimSpace(candidate.AgentPackDigest),
			TaskType: strings.TrimSpace(candidate.TaskType),
		}
		contracts, ok := registry[key]
		if !ok {
			stats.Unmatched++
			continue
		}
		backfilled, backfillErr := backfillTaskExecutionContracts(ctx, db, candidate, key, contracts)
		if backfillErr != nil {
			return stats, backfillErr
		}
		if backfilled {
			stats.Backfilled++
		}
	}

	if logger != nil {
		logger.Info().Int("backfilled", stats.Backfilled).Int("unmatched", stats.Unmatched).
			Msg("task execution contract migration completed")
	}
	return stats, nil
}

func backfillTaskExecutionContracts(ctx context.Context, db *gorm.DB, candidate taskExecutionContractCandidate, key frozenPackContractKey, contracts frozenPackContracts) (bool, error) {
	delivery, err := json.Marshal(contracts.Delivery)
	if err != nil {
		return false, fmt.Errorf("marshal delivery contract for execution %s: %w", candidate.ID, err)
	}
	required, err := json.Marshal(contracts.Required)
	if err != nil {
		return false, fmt.Errorf("marshal required artifact contract for execution %s: %w", candidate.ID, err)
	}
	fields := []struct {
		column   string
		current  []byte
		expected []byte
	}{
		{column: "agent_pack_delivery_contract", current: candidate.AgentPackDeliveryContract, expected: delivery},
		{column: "agent_pack_required_artifact_contract", current: candidate.AgentPackRequiredArtifactContract, expected: required},
	}
	backfilled := false
	for _, field := range fields {
		if len(field.current) > 0 {
			continue
		}
		result := db.WithContext(ctx).Model(&model.TaskExecution{}).
			Where("id = ? AND agent_pack_id = ? AND agent_pack_version = ? AND agent_pack_digest = ?", candidate.ID, key.ID, key.Version, key.Digest).
			Where("task_id IN (SELECT id FROM tasks WHERE type = ?)", key.TaskType).
			Where("("+field.column+" IS NULL OR "+field.column+" = '')").
			Update(field.column, field.expected)
		if result.Error != nil {
			return backfilled, fmt.Errorf("backfill %s for execution %s: %w", field.column, candidate.ID, result.Error)
		}
		if result.RowsAffected == 1 {
			backfilled = true
			continue
		}

		latest, loadErr := loadTaskExecutionContractCandidate(ctx, db, candidate.ID)
		if loadErr != nil {
			return backfilled, fmt.Errorf("re-read execution %s after contract race: %w", candidate.ID, loadErr)
		}
		if strings.TrimSpace(latest.AgentPackID) != key.ID || strings.TrimSpace(latest.AgentPackVersion) != key.Version ||
			strings.TrimSpace(latest.AgentPackDigest) != key.Digest || strings.TrimSpace(latest.TaskType) != key.TaskType {
			return backfilled, fmt.Errorf("backfill frozen contracts for execution %s lost identity race", candidate.ID)
		}
		actual := latest.AgentPackDeliveryContract
		if field.column == "agent_pack_required_artifact_contract" {
			actual = latest.AgentPackRequiredArtifactContract
		}
		if !bytes.Equal(actual, field.expected) {
			return backfilled, fmt.Errorf("backfill frozen contracts for execution %s found conflicting %s", candidate.ID, field.column)
		}
	}
	return backfilled, nil
}

func loadTaskExecutionContractCandidate(ctx context.Context, db *gorm.DB, executionID string) (taskExecutionContractCandidate, error) {
	var result taskExecutionContractCandidate
	err := db.WithContext(ctx).
		Table("task_executions AS e").
		Select("e.id, tasks.type AS task_type, e.agent_pack_id, e.agent_pack_version, e.agent_pack_digest, e.agent_pack_delivery_contract, e.agent_pack_required_artifact_contract").
		Joins("LEFT JOIN tasks ON tasks.id = e.task_id").
		Where("e.id = ?", executionID).
		Take(&result).Error
	return result, err
}

func taskExecutionContractMigrationRegistry() (map[frozenPackContractKey]frozenPackContracts, error) {
	registry := historicalTaskExecutionContracts()
	for _, pack := range agentpack.Default().Packs {
		for _, taskType := range pack.Bindings.TaskTypes {
			required, err := pack.RequiredArtifactsForTaskType(taskType)
			if err != nil {
				return nil, fmt.Errorf("resolve current Pack %s required artifacts for migration: %w", pack.ID, err)
			}
			registry[frozenPackContractKey{ID: pack.ID, Version: pack.Version, Digest: pack.Digest, TaskType: taskType}] = frozenPackContracts{
				Delivery: append([]agentpack.DeliverySpec(nil), pack.DeliveryForTaskType(taskType)...),
				Required: required,
			}
		}
	}
	return registry, nil
}

func historicalTaskExecutionContracts() map[frozenPackContractKey]frozenPackContracts {
	delivery := func(role, path, mimeType string) agentpack.DeliverySpec {
		return agentpack.DeliverySpec{Role: role, Path: path, MIMEType: mimeType}
	}
	required := func(role, path, mimeType string) agentpack.ArtifactSpec {
		return agentpack.ArtifactSpec{Role: role, Path: path, MIMEType: mimeType, Required: true}
	}
	contracts := make(map[frozenPackContractKey]frozenPackContracts)
	add := func(id, digest, taskType string, visible []agentpack.DeliverySpec, mandatory []agentpack.ArtifactSpec) {
		contracts[frozenPackContractKey{ID: id, Version: "1.0.0", Digest: digest, TaskType: taskType}] = frozenPackContracts{
			Delivery: visible,
			Required: mandatory,
		}
	}

	add("article", "285f0c86e5a08d4d56d073f0f2cc89f389844e33cefbdfeb43dd308dcfd9fc0c", model.PlatformArticle,
		[]agentpack.DeliverySpec{
			delivery("final_markdown", "output/04-article-final.md", "text/markdown"),
			delivery("html", "output/05-article.html", "text/html"),
			delivery("cover", "output/cover*.png", "image/png"),
			delivery("image", "output/img_*.png", "image/png"),
		},
		[]agentpack.ArtifactSpec{
			required("final_markdown", "output/04-article-final.md", "text/markdown"),
			required("html", "output/05-article.html", "text/html"),
			required("review", "output/final-review.md", "text/markdown"),
		})
	add("ecommerce", "202c79302ce17d2c2585bbe0c861b8794025ad15b225fe373d33fe1113ef3c4d", model.PlatformEcommerce,
		[]agentpack.DeliverySpec{
			delivery("copywriting", "output/copywriting.md", "text/markdown"),
			delivery("manifest", "output/manifest.json", "application/json"),
			delivery("main_image", "output/main_*.png", "image/png"),
			delivery("detail_image", "output/detail_*.png", "image/png"),
			delivery("cover_image", "output/cover_*.png", "image/png"),
			delivery("share_image", "output/share_*.png", "image/png"),
			delivery("sku_image", "output/sku_*.png", "image/png"),
		},
		[]agentpack.ArtifactSpec{
			required("product_bible", "output/product-bible.md", "text/markdown"),
			required("copywriting", "output/copywriting.md", "text/markdown"),
			required("manifest", "output/manifest.json", "application/json"),
		})
	add("live-slicer", "2b8676fb0d8add71253eced5586e43b4fd0f9d835141a5b90e6d24154ac171a7", model.TaskTypeLiveSlicer,
		[]agentpack.DeliverySpec{
			delivery("summary", "output/summary.md", "text/markdown"),
			delivery("manifest", "output/clip-manifest.json", "application/json"),
			delivery("clip_video", "output/exports/*.mp4", "video/mp4"),
			delivery("clip_metadata", "output/exports/*.md", "text/markdown"),
		},
		[]agentpack.ArtifactSpec{
			required("summary", "output/summary.md", "text/markdown"),
			required("manifest", "output/clip-manifest.json", "application/json"),
			required("plan", "output/clip-plan.json", "application/json"),
		})
	add("moments", "978b45a25bf490b0c8292eb51df6f21fc47e41d78f3d3cc79db2e22ba952d25a", model.PlatformMoments,
		[]agentpack.DeliverySpec{
			delivery("final_markdown", "output/content.md", "text/markdown"),
			delivery("image", "output/moments-image.png", "image/png"),
		},
		[]agentpack.ArtifactSpec{
			required("analysis", "output/material-analysis.md", "text/markdown"),
			required("final_markdown", "output/content.md", "text/markdown"),
			required("image_prompt", "output/image-prompts.md", "text/markdown"),
			required("image", "output/moments-image.png", "image/png"),
			required("review", "output/quality-review.md", "text/markdown"),
		})
	add("montage", "0545c87a39d06108272fc36534b00837c74fe0e154865f914c1dbc0d2296a87b", model.PlatformMontage,
		[]agentpack.DeliverySpec{
			delivery("final_video", "output/final.mp4", "video/mp4"),
			delivery("project_manifest", "output/montage-project.json", "application/json"),
			delivery("delivery_manifest", "output/delivery-manifest.json", "application/json"),
		},
		[]agentpack.ArtifactSpec{
			required("project_manifest", "output/montage-project.json", "application/json"),
			required("delivery_manifest", "output/delivery-manifest.json", "application/json"),
			required("final_video", "output/final.mp4", "video/mp4"),
		})
	seednoteDigest := "84c58f8e77326bfc818bcd7e36cacab18b52f7e5d8d61e775516bfdabb817933"
	add("seednote", seednoteDigest, model.PlatformSeednote,
		[]agentpack.DeliverySpec{
			delivery("final_markdown", "output/content.md", "text/markdown"),
			delivery("cover", "output/cover.png", "image/png"),
			delivery("image", "output/image_*.png", "image/png"),
			delivery("tail", "output/tail.png", "image/png"),
		},
		[]agentpack.ArtifactSpec{
			required("final_markdown", "output/content.md", "text/markdown"),
			required("image_plan", "output/image-plan.md", "text/markdown"),
		})
	add("seednote", seednoteDigest, model.TaskTypeViralAnalysis,
		[]agentpack.DeliverySpec{
			delivery("analysis", "output/source-analysis.md", "text/markdown"),
			delivery("template", "output/viral-template.json", "application/json"),
		},
		[]agentpack.ArtifactSpec{
			required("analysis", "output/source-analysis.md", "text/markdown"),
			required("template", "output/viral-template.json", "application/json"),
		})
	return contracts
}
