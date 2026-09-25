// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/deploycontracts"

	"sigs.k8s.io/yaml"
)

// Script-migration regression tests.
//
// Background: 4 templates (deliver-artifact, sbom, sign-image keyless,
// sign-image key-based) previously emitted as Argo `container:` blocks
// with `command: ["sh","-c","<script>"]`. The forge SDK ships a dedicated
// `forge.Script` builder that emits `script:` blocks — the semantically
// correct shape for "run a script source through an interpreter". These
// tests lock in the migration:
//
//  1. The Argo template emitted is `script:`, NOT `container:`.
//  2. The script source travels in the `source:` field, NOT in args.
//  3. The interpreter command is just `["sh"]` (no `-c`).
//
// Existing per-template tests (cosign_test.go, syft_test.go) cover
// the script content + output parameter wiring. These tests cover the
// SHAPE only — what Argo sees on the wire.

// assertScriptShape fails the test if the YAML does not match the
// expected forge.Script emission.
func assertScriptShape(t *testing.T, label string, marshaled string) {
	t.Helper()
	if !strings.Contains(marshaled, "script:") {
		t.Errorf("%s: emitted template lacks `script:` block — Script migration reverted?\n%s", label, marshaled)
	}
	if strings.Contains(marshaled, "container:\n") {
		t.Errorf("%s: emitted `container:` instead of `script:`\n%s", label, marshaled)
	}
	if strings.Contains(marshaled, `- -c`) {
		t.Errorf("%s: Script.Command still includes -c flag (legacy Container shape)\n%s", label, marshaled)
	}
	if !strings.Contains(marshaled, "source:") {
		t.Errorf("%s: missing `source:` field in emitted template\n%s", label, marshaled)
	}
}

func marshalTemplate(t *testing.T, tpl interface{}) string {
	t.Helper()
	out, err := yaml.Marshal(tpl)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return string(out)
}

func TestScriptMigration_DeliverArtifact_EmitsScriptBlock(t *testing.T) {
	b := NewWorkflowBuilder(testInfraParams())
	spec := deploycontracts.WorkflowSpec{
		TenantID:  "tn-1",
		ProjectID: "proj-1",
		BuildID:   "build-1",
		CommitSHA: "abc1234",
	}
	plan := deploycontracts.AppBuildPlan{
		AppName:  "api",
		AppPath:  ".",
		ImageTag: "registry.example.com/tn-1/api:abc1234",
	}
	s := b.uploadAssetsTemplate(spec, plan)
	tpl, err := s.BuildTemplate()
	if err != nil {
		t.Fatalf("BuildTemplate: %v", err)
	}
	assertScriptShape(t, "deliver-artifact", marshalTemplate(t, tpl))
}

func TestScriptMigration_SyftSBOM_EmitsScriptBlock(t *testing.T) {
	s := syftScanTemplate("tn-1", "proj-1", "abc1234", "api",
		"registry.example.com/tn-1/api:abc1234", false, "")
	tpl, err := s.BuildTemplate()
	if err != nil {
		t.Fatalf("BuildTemplate: %v", err)
	}
	assertScriptShape(t, "sbom", marshalTemplate(t, tpl))
}

func TestScriptMigration_CosignSign_KeyBased_EmitsScriptBlock(t *testing.T) {
	s := cosignSignTemplate("api", "registry.example.com/org/app:abc", false)
	tpl, err := s.BuildTemplate()
	if err != nil {
		t.Fatalf("BuildTemplate: %v", err)
	}
	assertScriptShape(t, "cosign-keybased", marshalTemplate(t, tpl))
}

func TestScriptMigration_CosignSign_Keyless_EmitsScriptBlock(t *testing.T) {
	s := cosignSignTemplateKeyless(CosignSignParams{
		AppName:          "api",
		ImageTag:         "registry.example.com/org/app:abc",
		InsecureRegistry: false,
		Keyless:          true,
	})
	tpl, err := s.BuildTemplate()
	if err != nil {
		t.Fatalf("BuildTemplate: %v", err)
	}
	assertScriptShape(t, "cosign-keyless", marshalTemplate(t, tpl))
}
