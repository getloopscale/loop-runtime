// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

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

	// Body templates must mention "LoopZero"
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
		if !strings.Contains(string(data), "LoopZero") {
			t.Errorf("template %s missing LoopZero branding", tmpl)
		}
	}

	// Subject templates must mention "LoopZero"
	subjectTemplates := []string{
		"verification.valid.subject.gotmpl",
		"recovery.valid.subject.gotmpl",
	}
	for _, tmpl := range subjectTemplates {
		data, err := os.ReadFile(filepath.Join(dir, tmpl))
		if err != nil {
			continue
		}
		if !strings.Contains(string(data), "LoopZero") {
			t.Errorf("subject template %s missing LoopZero branding", tmpl)
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

func TestKratosSMTPConfigured(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(helmDir(), "ory-kratos", "values.yaml"))
	if err != nil {
		t.Fatalf("values.yaml missing: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "smtp:") {
		t.Error("Kratos values.yaml missing SMTP configuration")
	}
	if !strings.Contains(content, "from_address:") {
		t.Error("Kratos values.yaml missing SMTP from_address")
	}
	if !strings.Contains(content, "noreply@loopzero.example") {
		t.Error("Kratos values.yaml SMTP from_address should be noreply@loopzero.example")
	}
}
