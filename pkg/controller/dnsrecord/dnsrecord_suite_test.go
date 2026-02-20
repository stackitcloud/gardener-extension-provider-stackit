package dnsrecord

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestDNSRecord(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DNSRecord Suite")
}
