package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"syscall"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

var safeArtifactRequestID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type ArtifactDiagnosticFields struct {
	Operation   string `json:"operation"`
	Code        string `json:"code"`
	HTTPStatus  int    `json:"http_status,omitempty"`
	RequestID   string `json:"request_id,omitempty"`
	NetworkCode string `json:"network_code,omitempty"`
}

func NewArtifactDiagnosticFields(operation string, err error) ArtifactDiagnosticFields {
	fields := ArtifactDiagnosticFields{Operation: operation, Code: "service_unavailable"}
	switch {
	case errors.Is(err, ErrTaskArtifactInvalid):
		fields.Code = "invalid_request"
	case errors.Is(err, ErrTaskArtifactExecutionConflict):
		fields.Code = "execution_conflict"
	case errors.Is(err, context.Canceled):
		fields.Code = "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		fields.Code = "deadline_exceeded"
	case errors.Is(err, syscall.ECONNRESET):
		fields.Code = "connection_reset"
		fields.NetworkCode = "ECONNRESET"
	case errors.Is(err, syscall.ETIMEDOUT):
		fields.Code = "network_timeout"
		fields.NetworkCode = "ETIMEDOUT"
	case errors.Is(err, syscall.ECONNREFUSED):
		fields.Code = "network_unavailable"
		fields.NetworkCode = "ECONNREFUSED"
	default:
		var serviceErr oss.ServiceError
		if errors.As(err, &serviceErr) {
			applyOSSArtifactDiagnostic(&fields, serviceErr)
			break
		}
		var serviceErrPtr *oss.ServiceError
		if errors.As(err, &serviceErrPtr) && serviceErrPtr != nil {
			applyOSSArtifactDiagnostic(&fields, *serviceErrPtr)
			break
		}
		var networkErr net.Error
		if errors.As(err, &networkErr) {
			if networkErr.Timeout() {
				fields.Code = "network_timeout"
			} else {
				fields.Code = "network_unavailable"
			}
		}
	}
	return fields
}

func applyOSSArtifactDiagnostic(fields *ArtifactDiagnosticFields, serviceErr oss.ServiceError) {
	fields.HTTPStatus = serviceErr.StatusCode
	if safeArtifactRequestID.MatchString(serviceErr.RequestID) {
		fields.RequestID = serviceErr.RequestID
	}
	fields.Code = artifactHTTPErrorCode(serviceErr.StatusCode)
}

func artifactHTTPErrorCode(status int) string {
	switch status {
	case http.StatusRequestTimeout:
		return "network_timeout"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusUnauthorized, http.StatusForbidden:
		return "unauthorized"
	case http.StatusConflict:
		return "execution_conflict"
	case http.StatusBadRequest, http.StatusNotFound:
		return "invalid_request"
	default:
		return "service_unavailable"
	}
}

func (f ArtifactDiagnosticFields) Error() string {
	return fmt.Sprintf("artifact %s failed: %s", f.Operation, f.Code)
}

func (f ArtifactDiagnosticFields) String() string { return f.Error() }
