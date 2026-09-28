// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package platforminstall guards how the platform is installed from
// taskfiles/infra.yml and the auth chart values:
//
//   - every cluster is installed by `helmfile sync`, never a bare
//     `helm upgrade --install`, and infra:install:core syncs the whole
//     helmfile so `needs:` ordering holds;
//   - secret reads decode base64 portably (BSD and GNU);
//   - the Kratos and Hydra DSNs use single-quoted YAML so the embedded
//     gotmpl call survives;
//   - no task pins the Helmfile environment, infra:install:cell takes its
//     cell as a parameter, and every cell in infra/k3d appears in every task
//     that enumerates the cells.
//
// Test-only package: it reads repository files and has no production code.
package platforminstall
