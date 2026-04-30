package stackit

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSelfHostedShootExposure(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "STACKIT SelfHostedShootExposure Controller Suite")
}
