package client

import (
	"errors"
	"net/http"
)

// StatusCodeError is a common interface implemented by Error and the SDK's GenericOpenAPIError.
type StatusCodeError interface {
	error
	GetStatusCode() int
}

// GetStatusCode returns the attached error code if the given error implements StatusCodeError or 0 otherwise.
func GetStatusCode(err error) int {
	var statusCodeError StatusCodeError
	if ok := errors.As(err, &statusCodeError); !ok {
		return 0
	}

	return statusCodeError.GetStatusCode()
}

func IsConflict(err error) bool { return GetStatusCode(err) == http.StatusConflict }
