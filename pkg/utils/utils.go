// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"fmt"
	"strings"

	utilsnet "k8s.io/utils/net"
)

// IsEmptyString checks whether a string is empty
func IsEmptyString(s *string) bool {
	return s == nil || *s == ""
}

// IsStringPtrValueEqual checks whether the value of string pointer `a` is equal to value of string `b`.
func IsStringPtrValueEqual(a *string, b string) bool {
	return a != nil && *a == b
}

// StringEqual compares to strings
func StringEqual(a, b *string) bool {
	return a == b || (a != nil && b != nil && *a == *b)
}

// SetStringValue sets an optional string value in a string map
// if the value is defined and not empty
func SetStringValue(values map[string]any, key string, value *string) {
	if !IsEmptyString(value) {
		values[key] = *value
	}
}

// SimpleMatch returns whether the given pattern matches the given text.
// It also returns a score indicating the match between `pattern` and `text`. The higher the score the higher the match.
// Only simple wildcard patterns are supposed to be passed, e.g. '*', 'tex*'.
func SimpleMatch(pattern, text string) (bool, int) {
	const wildcard = "*"
	if pattern == wildcard {
		return true, 0
	}
	if pattern == text {
		return true, len(text)
	}
	if strings.HasSuffix(pattern, wildcard) && strings.HasPrefix(text, pattern[:len(pattern)-1]) {
		s := strings.SplitAfterN(text, pattern[:len(pattern)-1], 2)
		return true, len(s[0])
	}
	if strings.HasPrefix(pattern, wildcard) && strings.HasSuffix(text, pattern[1:]) {
		i := strings.LastIndex(text, pattern[1:])
		return true, len(text) - i
	}

	return false, 0
}

// ComputeEgressCIDRs converts an IP to a CIDR depending on the IP family.
func ComputeEgressCIDRs(ips []string) []string {
	var result []string
	for _, ip := range ips {
		switch {
		case utilsnet.IsIPv4String(ip):
			result = append(result, fmt.Sprintf("%s/32", ip))
		case utilsnet.IsIPv6String(ip):
			result = append(result, fmt.Sprintf("%s/128", ip))
		}
	}
	return result
}

// BuildLabelKey constructs a label key from a custom domain and suffix.
// If customDomain is empty, it defaults to "kubernetes.io".
// Example: BuildLabelKey("ske.stackit.cloud", "cluster") returns "ske.stackit.cloud/cluster"
func BuildLabelKey(customDomain, suffix string) string {
	domain := strings.TrimSuffix(strings.TrimSpace(customDomain), "/")
	suffix = strings.TrimPrefix(suffix, "/")
	return fmt.Sprintf("%s/%s", domain, suffix)
}

// ClusterLabelKey returns the cluster label key using the provided custom domain.
// It is a convenience wrapper around BuildLabelKey with suffix "cluster".
// Example: ClusterLabelKey("kubernetes.io" returns "kubernetes.io/cluster"
func ClusterLabelKey(customDomain string) string {
	return BuildLabelKey(customDomain, "cluster")
}
