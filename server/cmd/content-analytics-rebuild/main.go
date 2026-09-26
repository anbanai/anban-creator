package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// content-analytics-rebuild starts and executes a durable analytics migration
// for one project. The server's Asynq recovery loop can resume queued/running
// jobs after a process restart; this command is also useful for an explicit
// one-time cutover or a manual retry after reviewing the persisted report.
func main() {
	fs := flag.NewFlagSet("content-analytics-rebuild", flag.ExitOnError)
	configPath := fs.String("config", "server/config.yaml", "server config path")
	projectID := fs.String("project-id", "", "project UUID to migrate")
	fs.Parse(os.Args[1:])
	if strings.TrimSpace(*projectID) == "" {
		fmt.Fprintln(os.Stderr, "-project-id is required")
		os.Exit(2)
	}
	cfg, err := config.NewConfig(strings.TrimSpace(*configPath))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	db, err := gorm.Open(mysql.Open(cfg.Database.DSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	sqlDB, err := db.DB()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer sqlDB.Close()
	repo := repository.New(db)
	mgr := service.NewAnalyticsRebuildManager(repo)
	job, err := mgr.Start(context.Background(), strings.TrimSpace(*projectID))
	if err == nil {
		err = mgr.Run(context.Background(), job.ID)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	status, err := mgr.Status(context.Background(), job.ID)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("job_id=%s status=%s processed=%d\n", status.ID, status.Status, status.Processed)
}
