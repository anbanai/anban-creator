package agent

import (
	"strings"
	"testing"
	"unicode/utf16"
)

func TestResumeAttachmentPathsUseCanonicalFilename(t *testing.T) {
	used := map[string]int{}
	for _, tc := range []struct {
		raw       string
		wantName  string
		wantRef   string
		wantLocal string
	}{
		{raw: " ../客户 反馈?.txt ", wantName: "客户_反馈_.txt", wantRef: "attachments/客户_反馈_.txt", wantLocal: ".anban-creator/resume/attachments/客户_反馈_.txt"},
		{raw: "../客户 反馈?.txt", wantName: "客户_反馈__2.txt", wantRef: "attachments/客户_反馈__2.txt", wantLocal: ".anban-creator/resume/attachments/客户_反馈__2.txt"},
		{raw: "...", wantName: "attachment", wantRef: "attachments/attachment", wantLocal: ".anban-creator/resume/attachments/attachment"},
	} {
		name := UniqueResumeFilename(SanitizeResumeFilename(tc.raw), used)
		if name != tc.wantName {
			t.Fatalf("canonical name for %q = %q, want %q", tc.raw, name, tc.wantName)
		}
		ref, err := ResumeAttachmentReferencePath(name)
		if err != nil || ref != tc.wantRef {
			t.Fatalf("reference path for %q = %q, %v; want %q", name, ref, err, tc.wantRef)
		}
		local, err := ResumeAttachmentWorkspacePath(name)
		if err != nil || local != tc.wantLocal {
			t.Fatalf("workspace path for %q = %q, %v; want %q", name, local, err, tc.wantLocal)
		}
	}
}

func TestCanonicalResumeAttachmentFilenameRejectsNonCanonicalNames(t *testing.T) {
	for _, name := range []string{"", "../secret.txt", "a/b.txt", `a\\b.txt`, " spaced.txt ", "trailing.", "trailing ", "bad?.txt", ".", "CON", "con.txt", "con.foo.txt", "PrN.pdf", "AUX", "nul.json", "COM1.log", "com9", "LPT1.txt", "lpt9.md", "CONIN$.txt", "conout$", "CLOCK$.log", "COM¹.txt", "lpt²", "LPT³.md"} {
		if _, err := CanonicalResumeAttachmentFilename(name); err == nil {
			t.Fatalf("non-canonical filename %q accepted", name)
		}
	}
}

func TestUniqueResumeFilenameUsesPortableCaseFoldedCollisions(t *testing.T) {
	used := map[string]int{}
	for i, raw := range []string{"Foo.txt", "foo.txt", "FOO_2.txt", "foo.TXT", "Straße.txt", "STRASSE.txt", "Résumé.txt", "Re\u0301sume\u0301.txt"} {
		name, err := PrepareResumeAttachmentFilename(raw, used)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"Foo.txt", "foo_2.txt", "FOO_2_2.txt", "foo_3.TXT", "Straße.txt", "STRASSE_2.txt", "Résumé.txt", "Re\u0301sume\u0301_2.txt"}[i]
		if name != want {
			t.Fatalf("portable name[%d] = %q, want %q", i, name, want)
		}
	}
}

func TestPrepareResumeAttachmentFilenameRejectsNonPortableOriginal(t *testing.T) {
	for _, raw := range []string{"CON.txt", "aux", "report. ", "trailing.", "trailing ", "CONIN$.txt", "CLOCK$", "COM¹.txt", "LPT³.md"} {
		if _, err := PrepareResumeAttachmentFilename(raw, map[string]int{}); err == nil {
			t.Fatalf("non-portable original filename %q accepted", raw)
		}
	}
}

func TestValidatePortableFilenameComponentRejectsReservedAndTrailingNames(t *testing.T) {
	for _, name := range []string{"CON", "con.txt", "LPT1.log", "file.", "file ", "bad?.txt", "bad\x00.txt"} {
		if err := ValidatePortableFilenameComponent(name); err == nil {
			t.Fatalf("portable component %q accepted", name)
		}
	}
	for _, name := range []string{".claude", "settings.json", "Stra\u00dfe.txt"} {
		if err := ValidatePortableFilenameComponent(name); err != nil {
			t.Fatalf("portable component %q rejected: %v", name, err)
		}
	}
}

func TestPrepareResumeAttachmentFilenameBoundsLongNamesAndCollisionSuffixes(t *testing.T) {
	for _, raw := range []string{
		strings.Repeat("a", 300) + ".txt",
		strings.Repeat("界", 100) + ".md",
	} {
		used := map[string]int{}
		first, err := PrepareResumeAttachmentFilename(raw, used)
		if err != nil {
			t.Fatal(err)
		}
		second, err := PrepareResumeAttachmentFilename(raw, used)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{first, second} {
			utf16Len := len(utf16.Encode([]rune(name)))
			if len(name) > 255 || utf16Len > 255 {
				t.Fatalf("portable filename exceeds component limit: bytes=%d utf16=%d name=%q", len(name), utf16Len, name)
			}
			if _, err := CanonicalResumeAttachmentFilename(name); err != nil {
				t.Fatalf("prepared filename is not canonical: %v", err)
			}
		}
		if PortableFilenameKey(first) == PortableFilenameKey(second) {
			t.Fatalf("collision suffix did not create a unique portable name: %q", first)
		}
	}
}

func TestCanonicalResumeAttachmentFilenameRejectsOverlongPersistedName(t *testing.T) {
	if _, err := CanonicalResumeAttachmentFilename(strings.Repeat("a", 256)); err == nil {
		t.Fatal("overlong persisted resume filename accepted")
	}
}

func TestExecutionResumePathsAreImmutablePerExecution(t *testing.T) {
	firstContext, err := ExecutionResumeContextPath("execution-1")
	if err != nil {
		t.Fatal(err)
	}
	secondContext, err := ExecutionResumeContextPath("execution-2")
	if err != nil {
		t.Fatal(err)
	}
	attachment, err := ExecutionResumeAttachmentPath("execution-1", "feedback.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if firstContext == secondContext || firstContext != ".anban-creator/resume/executions/execution-1/latest.md" || attachment != ".anban-creator/resume/executions/execution-1/attachments/feedback.pdf" {
		t.Fatalf("execution resume paths = %q %q %q", firstContext, secondContext, attachment)
	}
	for _, invalid := range []string{"", "../escape", "execution/escape", " execution-1"} {
		if _, err := ExecutionResumeContextPath(invalid); err == nil {
			t.Fatalf("invalid execution ID %q accepted", invalid)
		}
	}
}
