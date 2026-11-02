package wework

import (
	"errors"
	"regexp"
)

var secretQueryParameter = regexp.MustCompile(`(?i)([?&](?:corpsecret|access_token|refresh_token|token|aeskey)=)[^&\s"'<>]+`)

// sanitizedRequestError retains error identity while redacting credential query
// values from the transport's otherwise-useful diagnostic message.
type sanitizedRequestError struct {
	cause   error
	message string
}

func (e sanitizedRequestError) Error() string { return e.message }
func (e sanitizedRequestError) Unwrap() error { return e.cause }

func sanitizeRequestError(err error) error {
	if err == nil {
		return nil
	}
	var safe sanitizedRequestError
	if errors.As(err, &safe) {
		return err
	}
	redacted := secretQueryParameter.ReplaceAllString(err.Error(), `${1}[REDACTED]`)
	if redacted == err.Error() {
		return err
	}
	return sanitizedRequestError{cause: err, message: redacted}
}
