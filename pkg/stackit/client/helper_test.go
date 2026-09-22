package client

import (
	"context"
	"errors"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
)

var _ = Describe("execute", func() {
	It("wraps API errors with trace and request IDs", func() {
		_, err := execute(context.Background(), func(ctx context.Context) (int, error) {
			response, ok := ctx.Value(sdkconfig.ContextHTTPResponse).(**http.Response)
			Expect(ok).To(BeTrue())
			*response = &http.Response{Header: http.Header{
				XTraceIDHeader:   {"trace-123"},
				XRequestIDHeader: {"request-456"},
			}}
			return 0, errors.New("api error")
		})

		Expect(err).To(MatchError("[X-Request-Id:request-456]: [X-Trace-Id:trace-123]: api error"))
	})

	It("wraps API errors with trace ID only", func() {
		_, err := execute(context.Background(), func(ctx context.Context) (int, error) {
			response, ok := ctx.Value(sdkconfig.ContextHTTPResponse).(**http.Response)
			Expect(ok).To(BeTrue())
			*response = &http.Response{Header: http.Header{
				XTraceIDHeader: {"trace-123"},
			}}
			return 0, errors.New("api error")
		})

		Expect(err).To(MatchError("[X-Trace-Id:trace-123]: api error"))
	})

	It("wraps API errors with request ID only", func() {
		_, err := execute(context.Background(), func(ctx context.Context) (int, error) {
			response, ok := ctx.Value(sdkconfig.ContextHTTPResponse).(**http.Response)
			Expect(ok).To(BeTrue())
			*response = &http.Response{Header: http.Header{
				XRequestIDHeader: {"request-456"},
			}}
			return 0, errors.New("api error")
		})

		Expect(err).To(MatchError("[X-Request-Id:request-456]: api error"))
	})

	It("returns original error when no IDs are present", func() {
		_, err := execute(context.Background(), func(ctx context.Context) (int, error) {
			return 0, errors.New("api error")
		})

		Expect(err).To(MatchError("api error"))
	})

	It("returns response and no error on success", func() {
		res, err := execute(context.Background(), func(ctx context.Context) (string, error) {
			return "ok", nil
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal("ok"))
	})
})
