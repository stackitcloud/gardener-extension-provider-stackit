package client

import (
	"strings"

	gardenerutils "github.com/gardener/gardener/pkg/utils"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
)

const (
	// resourceNameHashLength is the number of hex chars from SHA-256(exposure.Name) appended when
	// the unabridged name would exceed validation.DNS1123LabelMaxLength. 8 hex chars = 32 bits,
	// plenty of collision resistance for "names sharing a prefix on the same shoot".
	resourceNameHashLength = 8
)

// BuildResourceName returns "<technicalID>-<resourceNameInfix>-<resourceName>" if it fits within
// apivalidation.DNS1123LabelMaxLength, otherwise truncates the exposure name and appends an
// 8-char SHA-256 suffix of the original exposure name to keep the result unique and DNS-compliant.
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
