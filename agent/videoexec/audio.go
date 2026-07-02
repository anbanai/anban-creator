package videoexec

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func ExtractAudio(sourcePath, outPath string) error {
	if strings.TrimSpace(sourcePath) == "" || strings.TrimSpace(outPath) == "" {
		return fmt.Errorf("source and output paths are required")
	}
	if info, err := os.Stat(outPath); err == nil && info.Size() > 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("create audio dir: %w", err)
	}
	cmd := exec.Command(
		"ffmpeg", "-y",
		"-i", sourcePath,
		"-vn", "-ac", "1", "-ar", "16000",
		"-codec:a", "pcm_s16le",
		outPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("extract audio: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
