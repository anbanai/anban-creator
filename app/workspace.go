package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

var validContentTypes = map[string]bool{
	"rednote":  true,
	"articles": true,
	"xls":      true,
}

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

类型:
  rednote   小红书内容 (output/rednote/staging/)
  articles  微信文章 (output/articles/staging/)
  xls     微信小绿书图片帖 (output/xls/staging/)`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			contentType := args[0]
			if !validContentTypes[contentType] {
				return fmt.Errorf("invalid content type %q: must be one of rednote, articles, xls", contentType)
			}

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
	return &cobra.Command{
		Use:   "archive <type>",
		Short: "归档 staging 目录为带日期编号的目录",
		Long: `将 output/<type>/staging/ 目录归档为 output/<type>/YYYYMMDD-NNN/ 格式。

类型:
  rednote   小红书内容 (output/rednote/staging/)
  articles  微信文章 (output/articles/staging/)
  xls     微信小绿书图片帖 (output/xls/staging/)`,
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

			archiveDir, err := nextArchiveDir(contentType)
			if err != nil {
				return fmt.Errorf("compute archive dir: %w", err)
			}

			// 记录归档前的状态（用于调试）
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
}

// copyDir 递归复制目录内容（跨文件系统兼容）
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 计算目标路径
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		return copyFile(path, dstPath, info.Mode())
	})
}

// copyFile 复制单个文件
func copyFile(src, dst string, mode os.FileMode) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	return dstFile.Sync()
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
