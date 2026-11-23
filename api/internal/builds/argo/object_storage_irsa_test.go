// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/deploycontracts"
)

// workflowJSON builds the workflow for spec and returns it as JSON, the shape the
// build cluster receives.
func workflowJSON(t *testing.T, params InfraParams, spec deploycontracts.WorkflowSpec) string {
	t.Helper()
	wf, err := NewWorkflowBuilder(params).Build(spec)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	dict, err := wf.ToDict()
	if err != nil {
		t.Fatalf("ToDict: %v", err)
	}
	out, err := json.Marshal(dict)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return string(out)
}

// On the AWS profile the build steps reach S3 as the IRSA role of the workflow
// ServiceAccount and no loopzero-minio-credentials Secret exists: a step that reads it
// never starts (CreateContainerConfigError), as the SBOM step of the first tenant build
// on staging would have.
func TestWorkflowBuilder_IRSAObjectStoreReadsNoMinIOSecret(t *testing.T) {
	params := testInfraParams()
	params.ObjectStoreIRSA = true
	params.StaticAssetsBucket = "loopzero-staging-static-assets-000000000000"

	for name, render := range renderers() {
		t.Run(name, func(t *testing.T) {
			wf := render(t, params)
			if strings.Contains(wf, secretMinIOCreds) {
				t.Errorf("the workflow references %s on an IRSA object store:\n%s", secretMinIOCreds, wf)
			}
			if strings.Contains(wf, "$MINIO_ENDPOINT") {
				t.Errorf("the workflow points aws at $MINIO_ENDPOINT, which only the MinIO Secret sets")
			}
			if !strings.Contains(wf, "s3://loopzero-staging-static-assets-000000000000/") {
				t.Errorf("the workflow does not upload to the configured static assets bucket")
			}
		})
	}
}

// Without IRSA the steps keep reading the MinIO Secret and the default bucket: the
// local k3d profile has MinIO and no IRSA.
func TestWorkflowBuilder_SecretObjectStoreKeepsTheMinIOSecret(t *testing.T) {
	for name, render := range renderers() {
		t.Run(name, func(t *testing.T) {
			wf := render(t, testInfraParams())
			if !strings.Contains(wf, secretMinIOCreds) {
				t.Errorf("the workflow no longer reads %s on a MinIO object store", secretMinIOCreds)
			}
			if !strings.Contains(wf, "s3://"+s3BucketStaticAssets+"/") {
				t.Errorf("the workflow does not upload to %s by default", s3BucketStaticAssets)
			}
		})
	}
}

// renderers render what the build cluster receives for a container app (the whole
// workflow, with its SBOM step) and for a static app (its upload step).
func renderers() map[string]func(*testing.T, InfraParams) string {
	return map[string]func(*testing.T, InfraParams) string{
		"server app": func(t *testing.T, params InfraParams) string {
			return workflowJSON(t, params, testSpec())
		},
		"static app": func(t *testing.T, params InfraParams) string {
			spec := testSpec()
			plan := deploycontracts.AppBuildPlan{AppName: "site", AppPath: ".", DeliveryModel: "static"}
			tpl, err := NewWorkflowBuilder(params).uploadAssetsTemplate(spec, plan).BuildTemplate()
			if err != nil {
				t.Fatalf("BuildTemplate: %v", err)
			}
			out, err := json.Marshal(tpl)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			return string(out)
		},
	}
}
