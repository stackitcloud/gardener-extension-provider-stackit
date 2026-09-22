package client

import (
	"errors"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackitcloud/stackit-sdk-go/core/oapierror"
)

var _ = Describe("Errors", func() {
	Describe("Error", func() {
		It("should return the error message", func() {
			Expect((&Error{Message: "foo"}).Error()).To(Equal("foo"))
		})

		It("should return the status code", func() {
			Expect((&Error{StatusCode: 404}).StatusCode).To(Equal(404))
		})
	})

	Describe("GetStatusCode", func() {
		It("should work with Error", func() {
			Expect(GetStatusCode(&Error{StatusCode: 404})).To(Equal(404))
		})

		It("should work with GenericOpenAPIError", func() {
			Expect(GetStatusCode(&oapierror.GenericOpenAPIError{StatusCode: 404})).To(Equal(404))
		})
	})

	Describe("IsNotFound", func() {
		It("should work with Error and NewNotFoundError", func() {
			Expect(IsNotFound(NewNotFoundError("server", "foo"))).To(BeTrue())
			Expect(IsNotFound(&Error{StatusCode: 404})).To(BeTrue())
			Expect(IsNotFound(&Error{StatusCode: 200})).To(BeFalse())
		})

		It("should work with GenericOpenAPIError", func() {
			Expect(IsNotFound(&oapierror.GenericOpenAPIError{StatusCode: 404})).To(BeTrue())
			Expect(IsNotFound(&oapierror.GenericOpenAPIError{StatusCode: 200})).To(BeFalse())
		})

		It("should return false for other errors", func() {
			Expect(IsNotFound(fmt.Errorf("404"))).To(BeFalse())
			Expect(IsNotFound(nil)).To(BeFalse())
		})
	})

	Describe("IsConflictError", func() {
		It("should work with Error", func() {
			Expect(IsConflictError(&Error{StatusCode: 409})).To(BeTrue())
			Expect(IsConflictError(&Error{StatusCode: 200})).To(BeFalse())
		})

		It("should work with GenericOpenAPIError", func() {
			Expect(IsConflictError(&oapierror.GenericOpenAPIError{StatusCode: 409})).To(BeTrue())
			Expect(IsConflictError(&oapierror.GenericOpenAPIError{StatusCode: 200})).To(BeFalse())
		})

		It("should return false for other errors", func() {
			Expect(IsConflictError(fmt.Errorf("409"))).To(BeFalse())
			Expect(IsConflictError(nil)).To(BeFalse())
		})
	})

	Describe("WrapError", func() {
		It("wraps the error with the provided identifier", func() {
			err := errors.New("test error")
			expected := fmt.Errorf("[X-Trace-Id:12345]: %w", err)
			Expect(WrapError(err, XTraceIDHeader, "12345")).To(Equal(expected))
		})

		It("returns the original error when the identifier is empty", func() {
			err := errors.New("test error")
			Expect(WrapError(err, "trace-id", "")).To(Equal(err))
		})

		It("returns nil when the error is nil", func() {
			Expect(WrapError(nil, "trace-id", "12345")).To(Succeed())
		})
	})
})
