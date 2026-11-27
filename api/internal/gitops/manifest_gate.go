// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package gitops

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// forbiddenKinds is the set of top-level Kubernetes resource kinds that MUST
// NOT appear in tenant GitOps repos. Each of these is either cluster-scoped
// infrastructure managed by the platform (ClusterIssuer, ClusterRole*, CRDs)
// or tenant-namespace primitives that the controller owns (Namespace).
//
// Namespaced Issuer is intentionally NOT blocked — a tenant may legitimately
// need a per-namespace issuer in advanced scenarios. ClusterIssuer is blocked
// because it is cluster-scoped and platform-managed.
//
// Certificate CRs are allowed: the renderer emits them for platform-managed
// TLS and the YAML-aware walk only inspects the document's TOP-LEVEL
// apiVersion+kind — it does NOT match on nested fields like issuerRef.kind.
// This fixes the substring-matching false positive from MEET-2026-04-09-042
// that blocked every new tenant deploy (see roadmap 01, Correction G).
var forbiddenKinds = map[string]string{
	"Namespace":                "platform-managed (controller creates tenant namespaces)",
	"ClusterRole":              "cluster-scoped RBAC is not permitted in tenant gitops",
	"ClusterRoleBinding":       "cluster-scoped RBAC is not permitted in tenant gitops",
	"ClusterIssuer":            "cluster-scoped issuers are platform-managed",
	"CustomResourceDefinition": "CRDs are platform-managed and must not appear in tenant gitops",
}

// documentHeader is the minimum shape we need to classify a YAML document.
// yaml.v3 KnownFields=false allows any other fields to be present without
// erroring — we only care about the two we decode here.
type documentHeader struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
}

// validateManifests is the tenant gitops safety net. It parses a multi-doc
// YAML string and rejects any document whose TOP-LEVEL kind is in the
// forbidden set.
//
// Failure modes:
//   - Nil error: all documents pass. Safe to push.
//   - Non-nil error, kind in message: a forbidden resource was found.
//   - Non-nil error, "parse": the manifest is malformed. Fail closed so we
//     never push unparseable YAML that could confuse ArgoCD or hide an
//     injection attempt.
//
// Empty/whitespace-only documents and the empty input itself are treated as
// no-ops (the renderer may legitimately emit a trailing `---` separator).
func validateManifests(manifest string) error {
	if strings.TrimSpace(manifest) == "" {
		return nil
	}

	dec := yaml.NewDecoder(bytes.NewReader([]byte(manifest)))
	docIdx := 0
	for {
		var hdr documentHeader
		err := dec.Decode(&hdr)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			// Fail closed on malformed YAML. The downstream consumer (ArgoCD,
			// kubectl apply) would reject this anyway; doing it here gives a
			// clearer error trail and prevents corrupt commits in gitops.
			return fmt.Errorf("manifest gate: parse document %d: %w", docIdx, err)
		}
		docIdx++

		// An empty document (e.g. a bare `---` separator) decodes as a zero
		// header. Skip it rather than treating it as a resource.
		if hdr.Kind == "" && hdr.APIVersion == "" {
			continue
		}

		if reason, forbidden := forbiddenKinds[hdr.Kind]; forbidden {
			return fmt.Errorf(
				"manifest gate: document %d: top-level kind %q is forbidden in tenant gitops: %s",
				docIdx-1, hdr.Kind, reason,
			)
		}
	}
}
