package model

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

const agentFeedbackBusinessKeyIndex = "idx_agent_feedback_task_agent"

// MigrateAgentFeedbackIdempotencySchema removes historical duplicate rows and
// creates the unique business-key index before returning.
func MigrateAgentFeedbackIdempotencySchema(db *gorm.DB) error {
	if !db.Migrator().HasTable(&AgentFeedback{}) {
		return nil
	}
	exact, _, err := inspectAgentFeedbackBusinessKeyIndex(db)
	if err != nil {
		return fmt.Errorf("inspect agent feedback business-key index: %w", err)
	}
	if exact {
		return nil
	}

	const maxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		migrate := func(tx *gorm.DB) error {
			exact, named, err := inspectAgentFeedbackBusinessKeyIndex(tx)
			if err != nil {
				return fmt.Errorf("inspect agent feedback business-key index: %w", err)
			}
			if exact {
				return nil
			}
			if named {
				if err := tx.Migrator().DropIndex(&AgentFeedback{}, agentFeedbackBusinessKeyIndex); err != nil {
					return fmt.Errorf("drop malformed agent feedback index: %w", err)
				}
			}
			if err := deduplicateAgentFeedback(tx); err != nil {
				return err
			}
			if err := tx.Migrator().CreateIndex(&AgentFeedback{}, agentFeedbackBusinessKeyIndex); err != nil {
				exact, _, inspectErr := inspectAgentFeedbackBusinessKeyIndex(tx)
				if inspectErr == nil && exact {
					return nil
				}
				return fmt.Errorf("create unique agent feedback index: %w", err)
			}
			exact, _, err = inspectAgentFeedbackBusinessKeyIndex(tx)
			if err != nil {
				return fmt.Errorf("verify agent feedback business-key index: %w", err)
			}
			if !exact {
				return fmt.Errorf("unique agent feedback index %s was not created", agentFeedbackBusinessKeyIndex)
			}
			return nil
		}

		if db.Dialector.Name() == "sqlite" {
			lastErr = db.Transaction(migrate)
		} else {
			// MySQL DDL auto-commits. Retrying the complete dedupe/create/verify
			// sequence closes the bounded race with legacy concurrent writers.
			lastErr = migrate(db)
		}
		if lastErr == nil {
			return nil
		}
	}
	return fmt.Errorf("migrate agent feedback idempotency schema after %d attempts: %w", maxAttempts, lastErr)
}

func inspectAgentFeedbackBusinessKeyIndex(db *gorm.DB) (exact bool, named bool, err error) {
	indexes, err := db.Migrator().GetIndexes(&AgentFeedback{})
	if err != nil {
		return false, false, err
	}
	for _, index := range indexes {
		if !strings.EqualFold(index.Name(), agentFeedbackBusinessKeyIndex) {
			continue
		}
		named = true
		unique, uniqueKnown := index.Unique()
		columns := index.Columns()
		for i := range columns {
			columns[i] = strings.ToLower(strings.TrimSpace(columns[i]))
		}
		exact = uniqueKnown && unique && len(columns) == 2 && columns[0] == "task_id" && columns[1] == "agent_name"
		return exact, named, nil
	}
	return false, false, nil
}

func deduplicateAgentFeedback(db *gorm.DB) error {
	var rows []AgentFeedback
	if err := db.Order("created_at DESC").Order("id DESC").Find(&rows).Error; err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(rows))
	duplicateIDs := make([]string, 0)
	for _, row := range rows {
		key := row.TaskID + "\x00" + row.AgentName
		if _, exists := seen[key]; exists {
			duplicateIDs = append(duplicateIDs, row.ID)
			continue
		}
		seen[key] = struct{}{}
	}
	if len(duplicateIDs) == 0 {
		return nil
	}
	return db.Where("id IN ?", duplicateIDs).Delete(&AgentFeedback{}).Error
}
