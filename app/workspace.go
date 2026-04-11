package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/spf13/cobra"
)

func workspaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "工作区管理",
	}
	cmd.AddCommand(workspaceArchiveCmd())
	cmd.AddCommand(workspacePrepareCmd())
	return cmd
}

func workspacePrepareCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prepare <type>",
		Short: "归档残留 staging 并创建干净的工作目录",
		Long: `原子性完成：归档残留 staging（若非空）→ 创建干净 staging 目录。

任意类型均可使用，将在 output/<type>/staging/ 下创建工作目录。`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			contentType := args[0]
			stagingDir := filepath.Join("output", contentType, "staging")
			result := map[string]string{
				"path":     stagingDir,
				"archived": "",
			}

			if info, err := os.Stat(stagingDir); err == nil && info.IsDir() {
				entries, err := os.ReadDir(stagingDir)
				if err != nil {
					return fmt.Errorf("read staging dir: %w", err)
				}
				if len(entries) > 0 {
					archiveDir, err := nextArchiveDir(contentType)
					if err != nil {
						return fmt.Errorf("compute archive dir: %w", err)
					}
					// 记录归档前的状态（用于调试）
					entriesBefore, _ := os.ReadDir(stagingDir)
					if err := os.Rename(stagingDir, archiveDir); err != nil {
						return fmt.Errorf("archive staging: %w", err)
					}
					result["archived"] = filepath.Base(archiveDir)
					result["files_count"] = fmt.Sprintf("%d", len(entriesBefore))
				}
			}

			if err := os.MkdirAll(stagingDir, 0o755); err != nil {
				return fmt.Errorf("create staging dir: %w", err)
			}

			responseSuccess(result)
			return nil
		},
	}
}

func workspaceArchiveCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "archive <type>",
		Short: "归档 staging 目录",
		Long: `将 output/<type>/staging/ 目录归档。

使用 --name 按笔记标题命名归档目录（推荐），否则使用 YYYYMMDD-NNN 格式。

任意类型均可使用。`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			contentType := args[0]
			stagingDir := filepath.Join("output", contentType, "staging")

			if _, err := os.Stat(stagingDir); os.IsNotExist(err) {
				responseSuccess(map[string]string{
					"message": "no staging directory",
				})
				return nil
			}

			var archiveDir string
			var err error
			if name != "" {
				archiveDir, err = namedArchiveDir(contentType, name)
			} else {
				archiveDir, err = nextArchiveDir(contentType)
			}
			if err != nil {
				return fmt.Errorf("compute archive dir: %w", err)
			}

			entriesBefore, _ := os.ReadDir(stagingDir)
			if err := os.Rename(stagingDir, archiveDir); err != nil {
				return fmt.Errorf("archive staging: %w", err)
			}

			responseSuccess(map[string]string{
				"from":        stagingDir,
				"to":          archiveDir,
				"archived":    filepath.Base(archiveDir),
				"files_count": fmt.Sprintf("%d", len(entriesBefore)),
			})
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "归档目录名（如笔记标题），不指定时使用日期格式")
	return cmd
}

// sanitizeDirName 将标题清理为合法目录名：去除非法字符，截断至 50 字符
func sanitizeDirName(name string) string {
	illegal := strings.ContainsAny
	var b strings.Builder
	for _, r := range name {
		if illegal(string(r), `/\:*?"<>|`) || unicode.IsControl(r) {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	result := strings.TrimSpace(b.String())
	// 截断至 50 字符（按 rune 计算）
	runes := []rune(result)
	if len(runes) > 50 {
		runes = runes[:50]
	}
	return string(runes)
}

// namedArchiveDir 计算按标题命名的归档目录路径，重名时追加 -2、-3 后缀
func namedArchiveDir(contentType, name string) (string, error) {
	base := filepath.Join("output", contentType)
	clean := sanitizeDirName(name)
	if clean == "" {
		return nextArchiveDir(contentType)
	}
	path := filepath.Join(base, clean)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path, nil
	}
	for n := 2; n <= 999; n++ {
		candidate := filepath.Join(base, fmt.Sprintf("%s-%d", clean, n))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no available archive slot for name %q under %s", name, base)
}

// nextArchiveDir 计算下一个可用的归档目录路径，格式为 output/<type>/YYYYMMDD-NNN
func nextArchiveDir(contentType string) (string, error) {
	base := filepath.Join("output", contentType)
	today := time.Now().Format("20060102")

	for n := 1; n <= 999; n++ {
		name := fmt.Sprintf("%s-%03d", today, n)
		path := filepath.Join(base, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path, nil
		}
	}
	return "", fmt.Errorf("no available archive slot for %s on %s", contentType, today)
}
