package stackit

import (
	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
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
