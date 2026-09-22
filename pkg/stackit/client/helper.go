package client

import (
	"context"
	"net/http"

	"github.com/stackitcloud/stackit-sdk-go/core/runtime"
)

const (
	XRequestIDHeader = "X-Request-Id"
	XTraceIDHeader   = "X-Trace-Id"
)

func execute[T any](ctx context.Context, call func(context.Context) (T, error)) (T, error) {
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	resp, err := call(ctx)
	if err != nil {
		var zero T
		err = WrapError(err, XTraceIDHeader, runtime.GetTraceId(ctx))
		if httpResp != nil {
			reqID := httpResp.Header.Get(XRequestIDHeader)
			err = WrapError(err, XRequestIDHeader, reqID)
		}
		return zero, err
	}

	return resp, nil
}
