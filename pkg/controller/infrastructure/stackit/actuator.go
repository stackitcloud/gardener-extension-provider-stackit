package stackit

import (
	"encoding/json"

	"github.com/gardener/gardener/extensions/pkg/controller/infrastructure"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/apis/stackit/v1alpha1"
)

type actuator struct {
	client            client.Client
	restConfig        *rest.Config
	customLabelDomain string
}

// NewActuator creates a new Actuator that updates the status of the handled Infrastructure resources.
func NewActuator(mgr manager.Manager, customLabelDomain string) infrastructure.Actuator {
	return &actuator{
		client:            mgr.GetClient(),
		restConfig:        mgr.GetConfig(),
		customLabelDomain: customLabelDomain,
	}
}

func infrastructureStateFromRaw(infra *extensionsv1alpha1.Infrastructure) (*stackitv1alpha1.InfrastructureState, error) {
	state := &stackitv1alpha1.InfrastructureState{}
	raw := infra.Status.State

	if raw != nil {
		jsonBytes, err := raw.MarshalJSON()
		if err != nil {
			return nil, err
		}

		// todo(ka): for now we won't use the actuator decoder because the flow state kind was registered as "FlowState" and not "InfrastructureState". So we
		// shall use the simple json unmarshal for this release.
		if err := json.Unmarshal(jsonBytes, state); err != nil {
			return nil, err
		}
	}

	return state, nil
}
