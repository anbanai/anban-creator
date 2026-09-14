package model

// TaskOutcome is the public, deterministic terminal result of a task.
type TaskOutcome struct {
	CoreDelivery TaskCoreDeliveryOutcome `json:"core_delivery"`
	Visual       TaskVisualOutcome       `json:"visual"`
	Review       TaskReviewOutcome       `json:"review"`
	Publication  TaskPublicationOutcome  `json:"publication"`
	Warnings     []TaskOutcomeWarning    `json:"warnings"`
	Diagnostic   *ExecutionDiagnostic    `json:"diagnostic,omitempty"`
}

type TaskCoreDeliveryStatus string
type TaskVisualStatus string
type TaskReviewStatus string
type TaskPublicationStatus string

type TaskCoreDeliveryOutcome struct {
	Status TaskCoreDeliveryStatus `json:"status"`
}

type TaskVisualOutcome struct {
	Status TaskVisualStatus `json:"status"`
}

type TaskReviewOutcome struct {
	Status TaskReviewStatus `json:"status"`
}

type TaskPublicationOutcome struct {
	Status TaskPublicationStatus `json:"status"`
}

type TaskOutcomeWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Stage   string `json:"stage,omitempty"`
}

// ExecutionDiagnostic is the sanitized provider/runtime diagnostic exposed to
// task owners. Raw execution results and contexts remain internal.
type ExecutionDiagnostic struct {
	Provider         string `json:"provider,omitempty"`
	ProviderCode     string `json:"provider_code,omitempty"`
	HTTPStatus       int    `json:"http_status,omitempty"`
	Stage            string `json:"stage,omitempty"`
	ContentDirection string `json:"content_direction,omitempty"`
	Recoverable      bool   `json:"recoverable"`
	ResumePoint      string `json:"resume_point,omitempty"`
	RequestID        string `json:"request_id,omitempty"`
	Summary          string `json:"summary"`
}

const (
	TaskCoreDeliveryComplete TaskCoreDeliveryStatus = "complete"
	TaskCoreDeliveryNone     TaskCoreDeliveryStatus = "none"

	TaskVisualComplete     TaskVisualStatus = "complete"
	TaskVisualPartial      TaskVisualStatus = "partial"
	TaskVisualNotRequested TaskVisualStatus = "not_requested"

	TaskReviewPassed      TaskReviewStatus = "passed"
	TaskReviewWarning     TaskReviewStatus = "warning"
	TaskReviewUnavailable TaskReviewStatus = "unavailable"

	TaskPublicationSucceeded    TaskPublicationStatus = "succeeded"
	TaskPublicationSkipped      TaskPublicationStatus = "skipped"
	TaskPublicationFailed       TaskPublicationStatus = "failed"
	TaskPublicationAmbiguous    TaskPublicationStatus = "ambiguous"
	TaskPublicationNotRequested TaskPublicationStatus = "not_requested"
)
