// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package gitearepo

// DefaultInternalURL is the in-cluster address of the platform's Gitea HTTP
// API when nothing configures one. The platform installs Gitea into the
// loopzero-system namespace in every environment (infra/helmfile/
// core.helmfile.yaml.gotmpl), so this is the address a component reaches when
// it is installed with its defaults.
//
// It is the single definition of the value in Go: the api configuration and
// the operators' constants both read it from here, because pkg is the one
// module every other module already depends on. It used to be written in each
// of them as gitea-http.gitea.svc — a namespace no environment creates — so a
// component that relied on its default could not reach Gitea at all.
//
// The same value is also written where a Go constant cannot reach: the
// kubebuilder default on PlatformBootstrap's Gitea InternalURL (and the CRD
// copies generated from it) and config.giteaUrl in the loopzero-api chart. Tests
// pin each of those to this constant, so they cannot drift from it again.
const DefaultInternalURL = "http://gitea-http.loopzero-system.svc.cluster.local:3000"
