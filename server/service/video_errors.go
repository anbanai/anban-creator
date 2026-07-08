package service

import "errors"

var ErrVideoGenerationConfig = errors.New("video generation config invalid")
var ErrVideoTaskInput = errors.New("videocreator/videoeditor input invalid")

type videoGenerationConfigError struct {
	cause error
}

func wrapVideoGenerationConfigError(err error) error {
	if err == nil {
		return nil
	}
	return videoGenerationConfigError{cause: err}
}

func (e videoGenerationConfigError) Error() string {
	return e.cause.Error()
}

func (e videoGenerationConfigError) Unwrap() error {
	return e.cause
}

func (e videoGenerationConfigError) Is(target error) bool {
	return target == ErrVideoGenerationConfig || errors.Is(e.cause, target)
}
