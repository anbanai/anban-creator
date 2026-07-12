package agent

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"unicode"

	appconfig "github.com/anbanai/anban-creator/app/config"
)

var resumeRootRelativePath = path.Join(appconfig.ConfigDir, "resume")

// SanitizeResumeFilename is the single filename contract used when resume
// inputs are persisted and later materialized by every executor.
func SanitizeResumeFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = "attachment"
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsControl(r), unicode.IsSpace(r), strings.ContainsRune(`/\:*?"<>|`, r):
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	cleaned := strings.Trim(b.String(), "._ ")
	if cleaned == "" {
		return "attachment"
	}
	return cleaned
}

func UniqueResumeFilename(name string, used map[string]int) string {
	used[name]++
	if used[name] == 1 {
		return name
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	return fmt.Sprintf("%s_%d%s", base, used[name], ext)
}

func CanonicalResumeAttachmentFilename(name string) (string, error) {
	if name == "" || strings.TrimSpace(name) != name || filepath.Base(name) != name || SanitizeResumeFilename(name) != name {
		return "", fmt.Errorf("resume attachment filename is not canonical")
	}
	return name, nil
}

func ResumeAttachmentReferencePath(name string) (string, error) {
	name, err := CanonicalResumeAttachmentFilename(name)
	if err != nil {
		return "", err
	}
	return path.Join("attachments", name), nil
}

func ResumeAttachmentWorkspacePath(name string) (string, error) {
	rel, err := ResumeAttachmentReferencePath(name)
	if err != nil {
		return "", err
	}
	return path.Join(resumeRootRelativePath, rel), nil
}
