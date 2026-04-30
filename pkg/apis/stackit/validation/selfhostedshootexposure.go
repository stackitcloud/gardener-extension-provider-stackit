package validation

import (
	cidrvalidation "github.com/gardener/gardener/pkg/utils/validation/cidr"
	"k8s.io/apimachinery/pkg/util/validation/field"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
)

// ValidateSelfHostedShootExposureConfig validates a SelfHostedShootExposureConfig object.
func ValidateSelfHostedShootExposureConfig(config *stackitv1alpha1.SelfHostedShootExposureConfig, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if config == nil || config.LoadBalancer == nil {
		return allErrs
	}

	lbPath := fldPath.Child("loadBalancer")
	if config.LoadBalancer.AccessControl != nil {
		allErrs = append(allErrs, validateAllowedSourceRanges(config.LoadBalancer.AccessControl.AllowedSourceRanges, lbPath.Child("accessControl", "allowedSourceRanges"))...)
	}
	return allErrs
}

func validateAllowedSourceRanges(ranges []string, fldPath *field.Path) field.ErrorList {
	allErrs := make(field.ErrorList, 0, len(ranges))
	cidrs := make([]cidrvalidation.CIDR, 0, len(ranges))
	for i, r := range ranges {
		idxPath := fldPath.Index(i)
		cidrs = append(cidrs, cidrvalidation.NewCIDR(r, idxPath))
		allErrs = append(allErrs, cidrvalidation.ValidateCIDRIsCanonical(idxPath, r)...)
	}
	allErrs = append(allErrs, cidrvalidation.ValidateCIDRParse(cidrs...)...)
	return allErrs
}
