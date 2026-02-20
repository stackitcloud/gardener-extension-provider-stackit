package client

import (
	"errors"
	"fmt"
	"net/http"
)

// StatusCodeError is a common interface implemented by Error and the SDK's GenericOpenAPIError.
type StatusCodeError interface {
	error
	GetStatusCode() int
}

// Error is an error returned by SDK helper functions in this package.
type Error struct {
	Message    string
	Resource   string
	Name       string
	StatusCode int
}

func (e *Error) Error() string {
	return e.Message
}

func (e *Error) GetStatusCode() int {
	return e.StatusCode
}

func NewNotFoundError(resource, name string) *Error {
	return &Error{
		Message:    fmt.Sprintf("%s %q not found", resource, name),
		Resource:   resource,
		Name:       name,
		StatusCode: http.StatusNotFound,
	}
}

// GetStatusCode returns the attached error code if the given error implements StatusCodeError or 0 otherwise.
func GetStatusCode(err error) int {
	var statusCodeError StatusCodeError
	if ok := errors.As(err, &statusCodeError); !ok {
		return 0
	}

	return statusCodeError.GetStatusCode()
}

func IsNotFound(err error) bool {
	return GetStatusCode(err) == http.StatusNotFound
}

// IgnoreNotFoundError ignore not found error
func IgnoreNotFoundError(err error) error {
	if IsNotFound(err) {
		return nil
	}
	return err
}

func IsConflictError(err error) bool {
	return GetStatusCode(err) == http.StatusConflict
}
