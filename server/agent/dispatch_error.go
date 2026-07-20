package agent

import "errors"

type PermanentDispatchError struct {
	err error
}

func NewPermanentDispatchError(err error) error {
	if err == nil {
		return nil
	}
	return &PermanentDispatchError{err: err}
}

func (e *PermanentDispatchError) Error() string { return e.err.Error() }
func (e *PermanentDispatchError) Unwrap() error { return e.err }

func IsPermanentDispatchError(err error) bool {
	var permanent *PermanentDispatchError
	return errors.As(err, &permanent)
}
