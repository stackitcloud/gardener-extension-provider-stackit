package feature

import (
	"strconv"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/component-base/featuregate"
)

const (
	// Every feature gate should add a method here following this template:
	//
	// // MyFeature enables Foo.
	// MyFeature featuregate.Feature = "MyFeature"

	// MutateDisableNTP enables the mutation that disables NTP if any worker's flatcar image version is greater than or eqaul to `FlatcarImageVersion`
	MutateDisableNTP featuregate.Feature = "MutateDisableNTP"
	// EnsureSTACKITLBDeletion enables the STACKIT LB deletion cleanup. The function checks for dangling/zombied LB's and then tries to delete them.
	EnsureSTACKITLBDeletion featuregate.Feature = "EnsureSTACKITLBDeletion"
	// UseSTACKITAPIInfrastructureController Uses the STACKIT API to create the shoot resources instead of OpenStack.
	UseSTACKITAPIInfrastructureController featuregate.Feature = "UseSTACKITAPIInfrastructureController"
	// UseSTACKITMachineControllerManager Uses the STACKIT machine controller Manager to manage nodes.
	UseSTACKITMachineControllerManager featuregate.Feature = "UseSTACKITMachineControllerManager"
	// ShootUseSTACKITMachineControllerManager Uses the STACKIT machine controller Manager to manage nodes for a specific Shoot.
	ShootUseSTACKITMachineControllerManager = "shoot.gardener.cloud/use-stackit-machine-controller-manager"
	// ShootUseSTACKITAPIInfrastructureController Uses the STACKIT API to create the shoot resources instead of OpenStack for a specific Shoot.
	ShootUseSTACKITAPIInfrastructureController = "shoot.gardener.cloud/use-stackit-api-infrastructure-controller"
)

var (
	// MutableGate is the central feature gate map for the gardener-extension-provider-stackit that can be
	// mutated. It is automatically initialized with all known features.
	// Use this only if you need to set the feature enablement state (i.e., in
	// the entrypoint or in tests). For determining whether a feature
	// gate is enabled, use Gate instead.
	MutableGate = featuregate.NewFeatureGate()

	// Gate is the central feature gate map.
	// Use this for checking if a feature is enabled, e.g.:
	//  if feature.Gate.Enabled(feature.MyFeature) { ... }
	Gate featuregate.FeatureGate = MutableGate

	allGates = map[featuregate.Feature]featuregate.FeatureSpec{
		MutateDisableNTP:                      {Default: true, PreRelease: featuregate.Alpha},
		EnsureSTACKITLBDeletion:               {Default: true, PreRelease: featuregate.Alpha},
		UseSTACKITAPIInfrastructureController: {Default: true, PreRelease: featuregate.Alpha},
		UseSTACKITMachineControllerManager:    {Default: true, PreRelease: featuregate.Alpha},
	}
)

func init() {
	utilruntime.Must(MutableGate.Add(allGates))
}

func UseStackitMachineControllerManager(cluster *extensionscontroller.Cluster) bool {
	if cluster != nil && cluster.Shoot != nil {
		annotation, ok := cluster.Shoot.Annotations[ShootUseSTACKITMachineControllerManager]
		if ok {
			enabledByAnnotation, err := strconv.ParseBool(annotation)
			if err == nil {
				return enabledByAnnotation
			}
		}
	}
	return Gate.Enabled(UseSTACKITMachineControllerManager)
}

func UseStackitAPIInfrastructureController(cluster *extensionscontroller.Cluster) bool {
	if cluster != nil && cluster.Shoot != nil {
		annotation, ok := cluster.Shoot.Annotations[ShootUseSTACKITAPIInfrastructureController]
		if ok {
			enabledByAnnotation, err := strconv.ParseBool(annotation)
			if err == nil {
				return enabledByAnnotation
			}
		}
	}
	return Gate.Enabled(UseSTACKITAPIInfrastructureController)
}
