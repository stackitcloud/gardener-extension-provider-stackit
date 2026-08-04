package stackit

import (
	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
)

// DetermineRegion returns the STACKIT region (e.g., for IaaS API) of the shoot.
// It handles the legacy RegionOne value from the OpenStack CloudProfile and returns eu01 instead.
// TODO: Remove this once we migrated all Shoot specs from RegionOne to eu01.
func DetermineRegion(cluster *extensionscontroller.Cluster) string {
	region := cluster.Shoot.Spec.Region
	if region == "RegionOne" {
		return "eu01"
	}
	return region
}

// DetermineRegionFromWorkerSpec returns the STACKIT region (e.g., for IaaS API) of the WorkerSpec.
// It handles the legacy RegionOne value from the OpenStack CloudProfile and returns eu01 instead.
// TODO: Remove this once we migrated all Shoot specs from RegionOne to eu01.
func DetermineRegionFromWorkerSpec(worker *extensionsv1alpha1.Worker) string {
	region := worker.Spec.Region
	if region == "RegionOne" {
		return "eu01"
	}
	return region
}
