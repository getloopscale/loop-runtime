// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ServicesReservedEnvKeyAccepted counts reserved env keys (LD_PRELOAD, PATH,
// LOOPRUNTIME_*...) that a services.json upload set and that were ACCEPTED because
// the blocklist runs in warn mode. It is the signal an operator watches
// before switching the blocklist to enforce.
var ServicesReservedEnvKeyAccepted = promauto.NewCounter(prometheus.CounterOpts{
	Name: "loopruntime_services_reserved_env_key_accepted_total",
	Help: "Reserved env keys accepted from services.json because the blocklist is in warn mode.",
})
