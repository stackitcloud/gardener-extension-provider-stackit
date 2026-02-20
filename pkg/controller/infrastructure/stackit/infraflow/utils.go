package infraflow

import (
	"context"
	"fmt"
)

// ErrorMultipleMatches is returned when the findExisting finds multiple resources matching a name.
var ErrorMultipleMatches = fmt.Errorf("error multiple matches")

func (fctx *FlowContext) workerCIDR() string {
	s := fctx.config.Networks.Worker
	if workers := fctx.config.Networks.Workers; workers != "" {
		s = workers
	}

	return s
}
func (fctx *FlowContext) defaultSecurityGroupName() string {
	return fctx.technicalID
}

func (fctx *FlowContext) defaultNetworkName() string {
	return fctx.technicalID
}

func (fctx *FlowContext) defaultSSHKeypairName() string {
	return fctx.technicalID
}

func findExisting[T any](ctx context.Context, id *string, name string,
	getter func(ctx context.Context, id string) (*T, error),
	finder func(ctx context.Context, name string) ([]T, error)) (*T, error) {
	if id != nil {
		found, err := getter(ctx, *id)
		if err != nil {
			return nil, err
		}
		if found != nil {
			return found, nil
		}
	}

	found, err := finder(ctx, name)
	if len(found) == 0 {
		return nil, nil
	}
	if len(found) > 1 {
		return nil, fmt.Errorf("%w: found %d matches for name %q", ErrorMultipleMatches, len(found), name)
	}
	if err != nil {
		return nil, err
	}
	return &found[0], nil
}
