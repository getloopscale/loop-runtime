// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"errors"
	"flag"
	"os"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/drillcredisolation"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "drill-creds-isolation",
		Summary: "Ensures DR-drill credentials are isolated from production.",
		Wiring: operatorRunOn(
			"DRILL_DO_TOKEN / DIGITALOCEAN_TOKEN / DO_TOKEN / DRILL_PULUMI_TOKEN in the environment"),
		NoRoot: true,
		Setup:  setupDrillCredsIsolation,
	})
}

// setupDrillCredsIsolation is the pure-logic port of
// infra/scripts/check-drill-credentials-isolation.sh (MIGRATE-GO-PUREFN). It
// reads the DRILL_*/prod token env vars and verifies no DRILL_* token equals
// its prod counterpart (catastrophic per D7). A shared token is a violation;
// missing tokens mean the isolation could not be checked.
func setupDrillCredsIsolation(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		r := drillcredisolation.Check(drillcredisolation.Env{
			DrillDO:         os.Getenv("DRILL_DO_TOKEN"),
			DigitalOcean:    os.Getenv("DIGITALOCEAN_TOKEN"),
			DO:              os.Getenv("DO_TOKEN"),
			DrillPulumi:     os.Getenv("DRILL_PULUMI_TOKEN"),
			PulumiAccess:    os.Getenv("PULUMI_ACCESS_TOKEN"),
			DrillCF:         os.Getenv("DRILL_CF_TOKEN"),
			CloudflareAPI:   os.Getenv("CLOUDFLARE_API_TOKEN"),
			RequireIsolated: os.Getenv("DRILL_REQUIRE_ISOLATED_TOKENS"),
		})
		for _, m := range r.Messages {
			inv.Stderr.Printf("[drill-creds-isolation] %s\n", m)
		}
		if r.Verdict() == verdict.NotVerified {
			return nil, errors.New("required drill tokens are not set, so their isolation from production could not be checked")
		}
		return r, nil
	}
}
