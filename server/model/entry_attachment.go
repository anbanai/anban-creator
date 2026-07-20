package model

// EntryAttachment is the reusable Studio/iLink AI-entry attachment descriptor.
// First-version AI parsing only consumes metadata and URLs; agents materialize
// the files into the task workspace before deeper understanding.
type EntryAttachment struct {
	Type        string `json:"type,omitempty"`
	URL         string `json:"url,omitempty"`
	Text        string `json:"text,omitempty"`
	FileName    string `json:"file_name,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Size        int64  `json:"size,omitempty"`
	Role        string `json:"role,omitempty"`
	UploadID    string `json:"upload_id,omitempty"`
	Key         string `json:"key,omitempty"`
	Instruction string `json:"instruction,omitempty"`
}

const (
	EntryAttachmentRoleResumeLatest = "resume_latest"
	EntryAttachmentRoleResumeFile   = "resume_file"
)

func IsResumeEntryAttachment(a EntryAttachment) bool {
	return a.Role == EntryAttachmentRoleResumeLatest || a.Role == EntryAttachmentRoleResumeFile
}
