package wiz

import "errors"

var (
	ErrDeviceOffline  = errors.New("device is offline")
	ErrUnauthorized   = errors.New("authentication failed")
	ErrTimeout        = errors.New("request timed out")
	ErrNetworkFailure = errors.New("network failure")
)

type WizError struct {
	Type    string
	Message string
	Err     error
}

func (e *WizError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "unknown wiz error"
}

func (e *WizError) Unwrap() error {
	return e.Err
}

func NewError(errType string, err error) *WizError {
	return &WizError{
		Type: errType,
		Err:  err,
	}
}

func ErrorType(err error) string {
	if wizErr, ok := err.(*WizError); ok {
		return wizErr.Type
	}
	return ""
}

func IsDeviceOffline(err error) bool {
	if wizErr, ok := err.(*WizError); ok {
		return wizErr.Type == "offline"
	}
	return false
}

func IsTimeout(err error) bool {
	if wizErr, ok := err.(*WizError); ok {
		return wizErr.Type == "timeout"
	}
	return false
}

func IsUnauthorized(err error) bool {
	if wizErr, ok := err.(*WizError); ok {
		return wizErr.Type == "unauthorized"
	}
	return false
}
