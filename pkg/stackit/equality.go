package stackit

import (
	"github.com/google/go-cmp/cmp"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
	"k8s.io/apimachinery/pkg/conversion"
)

// Equality can do semantic deep equality checks for STACKIT SDK objects, similar to apiequality.Semantic.
// Example: stackit.Equality.DeepEqual(securityGroupRuleA, securityGroupRuleB) == true
var Equality = conversion.EqualitiesOrDie(
	func(a, b iaas.Protocol) bool {
		// ignore the protocol number, only care about name for equality
		return a.GetName() == b.GetName()
	},
)

var ProtocolComparison = cmp.Comparer(func(a, b iaas.Protocol) bool {
	// ignore the protocol number, only care about name for equality
	return a.GetName() == b.GetName()
})

var MapStringAnyComparison = cmp.Comparer(func(a, b map[string]any) bool {
	// go-cmp differentiate between a nil map and an empty map
	// len() of a nil map returns 0
	// this check returns true when comparing an empty map with a nil map
	if len(a) == 0 && len(b) == 0 {
		return true
	}

	return cmp.Equal(a, b)
})
