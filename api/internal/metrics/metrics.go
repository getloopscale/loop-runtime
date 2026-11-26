// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package metrics provides Prometheus instrumentation for the LoopRuntime API.
//
// All metric names follow the spec §15.2 naming convention: loop_runtime_<subsystem>_<name>.
// Labels follow Prometheus best practices: lowercase, snake_case, bounded cardinality.
package metrics
