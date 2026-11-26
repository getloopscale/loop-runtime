// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package unit

import (
	"os"
	"regexp"
	"testing"
)

// The doctor must look for the Ory services where setup installs them. It kept
// checking loop-runtime-system after Ory moved to loop-runtime-auth, so a healthy install printed four
// "not installed (required for cloud auth)" warnings — a doctor that cries wolf on a
// clean run is one nobody reads (#407).
func TestTheDoctorLooksForOryWhereTheHelmfileInstallsIt(t *testing.T) {
	helmfile, err := os.ReadFile("../../../infra/helmfile/core.helmfile.yaml.gotmpl")
	if err != nil {
		t.Fatalf("read the core helmfile: %v", err)
	}
	release := regexp.MustCompile(`(?m)^  - name: ory-kratos\n(?:    [^\n]*\n)*?    namespace: (\S+)`).FindSubmatch(helmfile)
	if release == nil {
		t.Fatal("the core helmfile declares no ory-kratos release namespace")
	}
	installed := string(release[1])

	doctor, err := os.ReadFile("../../../taskfiles/dev.yml")
	if err != nil {
		t.Fatalf("read taskfiles/dev.yml: %v", err)
	}
	block := regexp.MustCompile(`(?s)for SVC in ory-kratos-public[^\n]*\n\s*NS="([^"]+)"`).FindSubmatch(doctor)
	if block == nil {
		t.Fatal("taskfiles/dev.yml has no doctor loop over the Ory services")
	}
	if checked := string(block[1]); checked != installed {
		t.Errorf("the doctor checks Ory in %q; the helmfile installs it in %q", checked, installed)
	}
}
