// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package unit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var emailTemplates = []string{
	"verification.valid.body.gotmpl",
	"verification.valid.subject.gotmpl",
	"verification.invalid.body.gotmpl",
	"verification.invalid.subject.gotmpl",
	"recovery.valid.body.gotmpl",
	"recovery.valid.subject.gotmpl",
	"recovery.invalid.body.gotmpl",
	"recovery.invalid.subject.gotmpl",
}

func TestEmailTemplatesExist(t *testing.T) {
	dir := filepath.Join(helmDir(), "ory-kratos", "email-templates")
	for _, tmpl := range emailTemplates {
		path := filepath.Join(dir, tmpl)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("email template missing: %s", tmpl)
		}
	}
}

func TestEmailTemplatesNoInternalTerms(t *testing.T) {
	dir := filepath.Join(helmDir(), "ory-kratos", "email-templates")
	internalTerms := []string{"Kratos", "kratos", "Hydra", "hydra", "OIDC", "ArgoCD", "Gitea", "MinIO", "tenant_id"}

	for _, tmpl := range emailTemplates {
		data, err := os.ReadFile(filepath.Join(dir, tmpl))
		if err != nil {
			continue
		}
		content := string(data)
		for _, term := range internalTerms {
			if strings.Contains(content, term) {
				t.Errorf("template %s contains internal term %q", tmpl, term)
			}
		}
	}
}

func TestEmailTemplatesHaveBranding(t *testing.T) {
	dir := filepath.Join(helmDir(), "ory-kratos", "email-templates")

	// Body templates must mention "LoopRuntime"
	bodyTemplates := []string{
		"verification.valid.body.gotmpl",
		"verification.invalid.body.gotmpl",
		"recovery.valid.body.gotmpl",
		"recovery.invalid.body.gotmpl",
	}
	for _, tmpl := range bodyTemplates {
		data, err := os.ReadFile(filepath.Join(dir, tmpl))
		if err != nil {
			continue
		}
		if !strings.Contains(string(data), "LoopRuntime") {
			t.Errorf("template %s missing LoopRuntime branding", tmpl)
		}
	}

	// Subject templates must mention "LoopRuntime"
	subjectTemplates := []string{
		"verification.valid.subject.gotmpl",
		"recovery.valid.subject.gotmpl",
	}
	for _, tmpl := range subjectTemplates {
		data, err := os.ReadFile(filepath.Join(dir, tmpl))
		if err != nil {
			continue
		}
		if !strings.Contains(string(data), "LoopRuntime") {
			t.Errorf("subject template %s missing LoopRuntime branding", tmpl)
		}
	}
}

func TestEmailTemplatesValidURLs(t *testing.T) {
	dir := filepath.Join(helmDir(), "ory-kratos", "email-templates")

	// Verification template must use {{ .VerificationURL }}
	data, err := os.ReadFile(filepath.Join(dir, "verification.valid.body.gotmpl"))
	if err == nil {
		if !strings.Contains(string(data), "{{ .VerificationURL }}") {
			t.Error("verification template missing {{ .VerificationURL }}")
		}
	}

	// Recovery template must use {{ .RecoveryURL }}
	data, err = os.ReadFile(filepath.Join(dir, "recovery.valid.body.gotmpl"))
	if err == nil {
		if !strings.Contains(string(data), "{{ .RecoveryURL }}") {
			t.Error("recovery template missing {{ .RecoveryURL }}")
		}
	}
}

func TestKratosHTTPCourierConfigured(t *testing.T) {
	// F27 (M23 audit 2026-07-10): use helmValues() to tolerate .gotmpl variant.
	data, _, err := helmValues("ory-kratos")
	if err != nil {
		t.Fatalf("ory-kratos values.yaml{,.gotmpl} missing: %v", err)
	}
	content := string(data)
	// MEET-036: DigitalOcean blocks SMTP — using Resend HTTP API instead.
	if !strings.Contains(content, "delivery_strategy: http") {
		t.Error("Kratos values.yaml missing HTTP courier delivery_strategy")
	}
	if !strings.Contains(content, "api.resend.com") {
		t.Error("Kratos values.yaml missing Resend API URL")
	}
	if !strings.Contains(content, "Authorization:") {
		t.Error("Kratos values.yaml missing Authorization header for Resend API")
	}
}
