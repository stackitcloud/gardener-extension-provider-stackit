package bastion

import (
	"context"

	"github.com/gardener/gardener/extensions/pkg/controller/bastion"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/stackit"
)

// DefaultAddOptions are the default AddOptions for AddToManager.
var DefaultAddOptions = AddOptions{}

// AddOptions are Options to apply when adding the Openstack bastion controller to the manager.
type AddOptions struct {
	// Controller are the controller.Options.
	Controller controller.Options
	// IgnoreOperationAnnotation specifies whether to ignore the operation annotation or not.
	IgnoreOperationAnnotation bool
	// ExtensionClasses defines the extension class this extension is responsible for.
	ExtensionClasses []extensionsv1alpha1.ExtensionClass
	// CustomLabelDomain is the domain prefix for custom labels applied to STACKIT infrastructure resources.
	CustomLabelDomain string
}

// AddToManagerWithOptions adds a controller with the given Options to the given manager.
// The opts.Reconciler is being set with a newly instantiated Actuator.
func AddToManagerWithOptions(mgr manager.Manager, opts AddOptions) error {
	return bastion.Add(mgr, bastion.AddArgs{
		Actuator:          (&Actuator{CustomLabelDomain: opts.CustomLabelDomain}).WithManager(mgr),
		ControllerOptions: opts.Controller,
		Predicates:        bastion.DefaultPredicates(opts.IgnoreOperationAnnotation),
		Type:              stackit.Type,
		ExtensionClasses:  opts.ExtensionClasses,
	})
}

// AddToManager adds a controller with the default Options.
func AddToManager(_ context.Context, mgr manager.Manager) error {
	return AddToManagerWithOptions(mgr, DefaultAddOptions)
}
