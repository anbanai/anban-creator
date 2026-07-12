package agent

import "testing"

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
	for _, name := range []string{"", "../secret.txt", "a/b.txt", `a\\b.txt`, " spaced.txt ", "bad?.txt", "."} {
		if _, err := CanonicalResumeAttachmentFilename(name); err == nil {
			t.Fatalf("non-canonical filename %q accepted", name)
		}
	}
}
