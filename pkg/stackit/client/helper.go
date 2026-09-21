package client

import (
	"context"
	"net/http"

	"github.com/stackitcloud/stackit-sdk-go/core/runtime"
	sdkWait "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api/wait"
)

func execute[T any](ctx context.Context, call func(context.Context) (T, error)) (T, error) {
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	resp, err := call(ctx)
	if err != nil {
		var zero T
		err = WrapError(err, "X-Trace-Id", runtime.GetTraceId(ctx))
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			err = WrapError(err, sdkWait.XRequestIDHeader, reqID)
		}
		return zero, err
	}

	return resp, nil
}
