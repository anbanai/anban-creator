// repair-wechat-identities inspects identity pollution without starting Server.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/service"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	fs := flag.NewFlagSet("repair-wechat-identities", flag.ContinueOnError)
	path := fs.String("config", "config.yaml", "server config path; DSN is never printed")
	apply := fs.Bool("apply", false, "apply evidenced repairs and commit durable DB audit rows (stop writers first)")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	cfg, err := config.NewConfig(*path)
	if err != nil {
		return err
	}
	db, err := gorm.Open(mysql.Open(cfg.Database.DSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return fmt.Errorf("open repair database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	report, err := service.RepairWechatIdentities(context.Background(), db, *apply)
	if outputErr := json.NewEncoder(os.Stdout).Encode(report); outputErr != nil {
		return outputErr
	}
	return err
}
