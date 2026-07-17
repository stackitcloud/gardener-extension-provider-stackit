package client

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	gardenerutils "github.com/gardener/gardener/pkg/utils"
	"github.com/stackitcloud/stackit-sdk-go/core/runtime"
	sdkWait "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api/wait"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
)

const (
	// resourceNameHashLength is the number of hex chars from SHA-256 appended when
	// the unabridged name would exceed validation.DNS1123LabelMaxLength. 8 hex chars = 32 bits,
	// plenty of collision resistance for "names sharing a prefix on the same shoot".
	resourceNameHashLength = 8
)

// BuildResourceName returns "<technicalID>-<resourceNameInfix>-<resourceName>" if it fits within
// apivalidation.DNS1123LabelMaxLength, otherwise truncates the resource name and appends an
// 8-char SHA-256 suffix of the original resourceName name to keep the result unique and DNS-compliant.
func BuildResourceName(technicalID, resourceNameInfix, resourceName string) string {
	full := technicalID + resourceNameInfix + resourceName
	if len(full) <= utilvalidation.DNS1123LabelMaxLength {
		return full
	}

	hash := gardenerutils.ComputeSHA256Hex([]byte(resourceName))[:resourceNameHashLength]
	budget := max(0, utilvalidation.DNS1123LabelMaxLength-len(technicalID)-len(resourceNameInfix)-resourceNameHashLength-1)
	// strings.TrimRight strips trailing hyphens after truncation to keep the result DNS-1123 compliant.
	truncated := strings.TrimRight(resourceName[:budget], "-")
	return technicalID + resourceNameInfix + truncated + "-" + hash
}

// wrapErrorWithResponseID wraps the error with the X-Request-Id but only if the error is not nil
func wrapErrorWithResponseID(err error, reqID string) error {
	if err == nil {
		return nil
	}
	// if the request id is empty we don't wrap the error
	if reqID == "" {
		return err
	}
	return fmt.Errorf("[%s:%s]: %w", sdkWait.XRequestIDHeader, reqID, err)
}

func WithResponseID[T any](ctx context.Context, call func(context.Context) (T, error)) (T, error) {
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	resp, err := call(ctx)
	if err != nil {
		var zero T
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return zero, wrapErrorWithResponseID(err, reqID)
		}
		return zero, err
	}

	return resp, nil
}
