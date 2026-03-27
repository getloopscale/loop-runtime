// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package controller_test

import (
	"github.com/loopzero/loopzero/operators/operatortest"
)

// Type aliases for operatortest fakes — eliminates duplicate implementations.
// The canonical fakes live in operatortest/ (exported for E2E). These aliases
// let unit tests use them without qualifying every reference.
type fakeGiteaProvisioner = operatortest.FakeGitProvisioner
type fakeArgoCDProvisioner = operatortest.FakeArgoCDProvisioner

// Factory functions delegate to operatortest constructors.
var (
	newFakeGiteaProvisioner  = operatortest.NewFakeGitProvisioner
	newFakeArgoCDProvisioner = operatortest.NewFakeArgoCDProvisioner
)
