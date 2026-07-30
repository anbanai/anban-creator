package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/migrations"
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
	flags := flag.NewFlagSet("agent-profile-envs-backfill", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	configPath := flags.String("config", "server/config.yaml", "server config path")
	batchSize := flags.Int("batch-size", 500, "positive task batch size")
	dryRun := flags.Bool("dry-run", false, "validate conversions without writing")
	verifyOnly := flags.Bool("verify-only", false, "verify completed v3 rows without writing")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *batchSize <= 0 {
		return fmt.Errorf("batch-size must be positive")
	}
	if *dryRun && *verifyOnly {
		return fmt.Errorf("dry-run and verify-only are mutually exclusive")
	}

	cfg, err := config.NewConfig(strings.TrimSpace(*configPath))
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	db, err := gorm.Open(mysql.Open(cfg.Database.DSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("open database pool: %w", err)
	}
	defer sqlDB.Close()

	counts, err := tableCounts(ctx, db)
	if err != nil {
		return err
	}
	fmt.Printf("tasks=%d plans=%d task_executions=%d\n", counts.tasks, counts.plans, counts.executions)
	if err := migrations.BackfillAgentProfileEnvs(ctx, db, migrations.AgentProfileEnvsBackfillOptions{
		BatchSize: *batchSize, Profiles: cfg.Claude.ExecutionProfiles, DryRun: *dryRun, VerifyOnly: *verifyOnly,
	}); err != nil {
		return err
	}
	mode := "backfill"
	if *dryRun {
		mode = "dry-run"
	} else if *verifyOnly {
		mode = "verify-only"
	}
	fmt.Printf("mode=%s status=ok\n", mode)
	return nil
}

type migrationTableCounts struct {
	tasks, plans, executions int64
}

func tableCounts(ctx context.Context, db *gorm.DB) (migrationTableCounts, error) {
	var counts migrationTableCounts
	for _, item := range []struct {
		table string
		value *int64
	}{{"tasks", &counts.tasks}, {"plans", &counts.plans}, {"task_executions", &counts.executions}} {
		if err := db.WithContext(ctx).Table(item.table).Count(item.value).Error; err != nil {
			return migrationTableCounts{}, fmt.Errorf("count %s: %w", item.table, err)
		}
	}
	return counts, nil
}
