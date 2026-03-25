// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

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
