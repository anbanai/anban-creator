package agent

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"unicode"

	appconfig "github.com/anbanai/anban-creator/app/config"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

var resumeRootRelativePath = path.Join(appconfig.ConfigDir, "resume")

const (
	maxPortableFilenameBytes      = 255
	maxPortableFilenameUTF16Units = 255
)

var windowsReservedResumeStems = map[string]struct{}{
	"con": {}, "prn": {}, "aux": {}, "nul": {}, "conin$": {}, "conout$": {}, "clock$": {},
	"com1": {}, "com2": {}, "com3": {}, "com4": {}, "com5": {}, "com6": {}, "com7": {}, "com8": {}, "com9": {},
	"lpt1": {}, "lpt2": {}, "lpt3": {}, "lpt4": {}, "lpt5": {}, "lpt6": {}, "lpt7": {}, "lpt8": {}, "lpt9": {},
	"com¹": {}, "com²": {}, "com³": {}, "lpt¹": {}, "lpt²": {}, "lpt³": {},
}

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
	for suffix := 1; ; suffix++ {
		candidate := boundedResumeFilename(name, suffix)
		key := PortableFilenameKey(candidate)
		if used[key] == 0 {
			used[key] = 1
			return candidate
		}
	}
}

func boundedResumeFilename(name string, suffix int) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	suffixText := ""
	if suffix > 1 {
		suffixText = fmt.Sprintf("_%d", suffix)
	}
	suffixBytes, suffixUnits := portableFilenameSize(suffixText)
	ext = truncatePortableFilenamePart(ext, maxPortableFilenameBytes-suffixBytes-1, maxPortableFilenameUTF16Units-suffixUnits-1)
	if ext == "." {
		ext = ""
	}
	extBytes, extUnits := portableFilenameSize(ext)
	base = truncatePortableFilenamePart(base, maxPortableFilenameBytes-suffixBytes-extBytes, maxPortableFilenameUTF16Units-suffixUnits-extUnits)
	if base == "" {
		base = truncatePortableFilenamePart("attachment", maxPortableFilenameBytes-suffixBytes-extBytes, maxPortableFilenameUTF16Units-suffixUnits-extUnits)
	}
	return base + suffixText + ext
}

func truncatePortableFilenamePart(value string, maxBytes, maxUnits int) string {
	if maxBytes <= 0 || maxUnits <= 0 {
		return ""
	}
	var b strings.Builder
	units := 0
	for _, r := range value {
		runeBytes := len(string(r))
		runeUnits := 1
		if r > 0xffff {
			runeUnits = 2
		}
		if b.Len()+runeBytes > maxBytes || units+runeUnits > maxUnits {
			break
		}
		b.WriteRune(r)
		units += runeUnits
	}
	return b.String()
}

func portableFilenameSize(name string) (int, int) {
	units := 0
	for _, r := range name {
		units++
		if r > 0xffff {
			units++
		}
	}
	return len(name), units
}

// PortableFilenameKey matches names across case-folding and Unicode-
// normalizing filesystems without changing the user-visible filename.
func PortableFilenameKey(name string) string {
	return norm.NFC.String(cases.Fold().String(name))
}

func CanonicalResumeAttachmentFilename(name string) (string, error) {
	if name == "" || strings.TrimSpace(name) != name || strings.TrimRight(name, ". ") != name || filepath.Base(name) != name || SanitizeResumeFilename(name) != name || isWindowsReservedResumeFilename(name) {
		return "", fmt.Errorf("resume attachment filename is not canonical")
	}
	if err := ValidatePortableFilenameComponent(name); err != nil {
		return "", err
	}
	return name, nil
}

func ValidatePortableFilenameComponent(name string) error {
	bytes, units := portableFilenameSize(name)
	if name == "" || bytes > maxPortableFilenameBytes || units > maxPortableFilenameUTF16Units {
		return fmt.Errorf("filename component exceeds portable limits")
	}
	return nil
}

// PrepareResumeAttachmentFilename is the producer-side half of the shared
// portable filename contract. It rejects names that cannot be represented on
// every supported executor filesystem, then allocates a case-folded unique name.
func PrepareResumeAttachmentFilename(raw string, used map[string]int) (string, error) {
	base := filepath.Base(raw)
	if base == "" || strings.TrimSpace(base) != base || strings.TrimRight(base, ". ") != base {
		return "", fmt.Errorf("resume attachment filename is not portable")
	}
	name := SanitizeResumeFilename(base)
	if isWindowsReservedResumeFilename(name) {
		return "", fmt.Errorf("resume attachment filename is not canonical")
	}
	name = UniqueResumeFilename(name, used)
	if _, err := CanonicalResumeAttachmentFilename(name); err != nil {
		return "", err
	}
	return name, nil
}

func isWindowsReservedResumeFilename(name string) bool {
	stem, _, _ := strings.Cut(name, ".")
	_, reserved := windowsReservedResumeStems[PortableFilenameKey(stem)]
	return reserved
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
