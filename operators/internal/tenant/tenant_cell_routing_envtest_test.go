// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenant_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
)

var _ = Describe("Cell routing", Ordered, func() {
	const (
		tenantName = "tn-cell-routed"
		cellID     = "cell-9"
	)

	BeforeAll(func() {
		tenant := newTenant(tenantName, loopzerov1.PlanStarter, []string{"production"})
		tenant.Spec.CellID = cellID
		Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
	})

	AfterAll(func() {
		cleanupGoldenTenant(tenantName, []string{"production"})
	})

	It("resolves the runtime cluster of the tenant's cell for every runtime write", func() {
		// Tenant writes the PriorityClass and the Environment writes the
		// namespace, RBAC, NetworkPolicy and quota: both reach the runtime
		// cluster through the factory, keyed by the tenant's cell.
		Eventually(func(g Gomega) {
			g.Expect(cellFactory.Calls()).To(ContainElement(cellID))
		}, timeout, interval).Should(Succeed())
	})
})
