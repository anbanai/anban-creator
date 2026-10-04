// runtime-cleanup prints private blocked-cleanup evidence for operator review,
// or reopens exactly that reviewed revision. It never mutates task/billing state.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("runtime-cleanup", flag.ContinueOnError)
	configPath := flags.String("config", "server/config.yaml", "Server config path")
	executionID := flags.String("execution-id", "", "print this blocked execution's private review evidence as JSON")
	reopen := flags.Bool("reopen", false, "reopen cleanup using an exact reviewed evidence file")
	evidencePath := flags.String("evidence", "", "JSON evidence previously printed by this command")
	reviewed := flags.Bool("reviewed-identity", false, "confirm workload ownership, frozen image, scope, and instance evidence was independently reviewed and the conflict resolved")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if *reopen && (!*reviewed || *evidencePath == "") {
		return fmt.Errorf("reopen requires reviewed-identity and evidence")
	}
	if !*reopen && (*executionID == "" || *reviewed || *evidencePath != "") {
		return fmt.Errorf("inspection requires execution-id only; reopening requires reopen, reviewed-identity and evidence")
	}
	var review model.CleanupReview
	if *reopen {
		if *executionID != "" {
			return fmt.Errorf("reopen uses the execution ID from the reviewed evidence")
		}
		raw, err := os.ReadFile(*evidencePath)
		if err != nil {
			return fmt.Errorf("read evidence: %w", err)
		}
		review, err = parseReview(raw)
		if err != nil {
			return err
		}
	}
	cfg, err := config.NewConfig(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	db, err := gorm.Open(mysql.Open(cfg.Database.DSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	executions := repository.New(db).TaskExecutions()
	if *reopen {
		won, err := executions.ReopenBlockedCleanup(ctx, review)
		if err != nil {
			return fmt.Errorf("reopen cleanup: %w", err)
		}
		if !won {
			return fmt.Errorf("cleanup evidence changed or execution is no longer blocked; inspect and review again")
		}
		fmt.Println("cleanup reopened; frozen identity, task outcome, and billing unchanged")
		return nil
	}
	execution, err := executions.FindByID(ctx, *executionID)
	if err != nil {
		return fmt.Errorf("find execution: %w", err)
	}
	if execution.CleanupStatus != model.TaskExecutionCleanupBlocked {
		return fmt.Errorf("execution cleanup is not blocked")
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(execution.CleanupReview())
}

func parseReview(raw []byte) (model.CleanupReview, error) {
	var review model.CleanupReview
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&review); err != nil {
		return review, fmt.Errorf("invalid evidence: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return review, fmt.Errorf("evidence must contain exactly one JSON object")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return review, err
	}
	for _, key := range []string{"execution_id", "task_id", "target", "runtime_profile", "runtime_image", "scope", "workload", "instance_id", "attempts", "diagnostic"} {
		value, exists := fields[key]
		if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return review, fmt.Errorf("evidence must explicitly contain %s, including any empty identity member", key)
		}
	}
	if review.ExecutionID == "" || review.TaskID == "" || review.Target == "" || review.Attempts <= 0 || review.Diagnostic == "" {
		return review, fmt.Errorf("evidence must identify a blocked execution revision")
	}
	return review, nil
}
