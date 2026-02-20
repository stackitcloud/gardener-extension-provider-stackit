package stackit

import (
	"github.com/google/go-cmp/cmp"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"
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
