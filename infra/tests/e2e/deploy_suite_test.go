package e2e

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestDeployHappyPath(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Deploy Happy Path E2E Suite")
}
