// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package github

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The deploy descriptor is the single rule that decides whether a repository
// can be deployed from GitHub. Connect refuses what this refuses, and a push
// deploys only what this accepts.
func TestParseDeployDescriptor_AcceptsAMappingThatDeclaresApps(t *testing.T) {
	d, err := ParseDeployDescriptor([]byte(minimalLoopRuntimeYaml))

	if err != nil {
		t.Fatalf("ParseDeployDescriptor: %v", err)
	}
	if d.Project != "my-app" || len(d.Apps) != 1 {
		t.Errorf("descriptor = %+v, want project my-app with 1 app", d)
	}
}

func TestParseDeployDescriptor_RefusesWhatNoDeployCouldBuild(t *testing.T) {
	cases := map[string]string{
		"empty file":              "",
		"whitespace only":         "   \n\t  ",
		"not YAML":                "not: valid: yaml: ::\n  - broken",
		"a list, not a mapping":   "- api\n- web\n",
		"a mapping with no apps":  "name: my-app\nframework: node\n",
		"apps present but empty":  "project: my-app\napps: {}\n",
		"apps that is not a map":  "project: my-app\napps:\n  - api\n",
		"a scalar document":       "just-a-string\n",
		"an explicit null":        "~\n",
		"a mapping with no field": "{}\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			d, err := ParseDeployDescriptor([]byte(content))

			var invalid *InvalidDeployDescriptorError
			if !errors.As(err, &invalid) {
				t.Fatalf("ParseDeployDescriptor = (%+v, %v), want *InvalidDeployDescriptorError", d, err)
			}
			if invalid.Error() == "" {
				t.Error("the refusal must carry a message the user can act on")
			}
		})
	}
}

// A push whose loopzero.yaml declares no app used to submit a build with zero
// apps, while Connect accepted the same file. The push must now deploy
// nothing, exactly as Connect refuses it.
func TestHandlePush_DescriptorWithoutAppsSubmitsNoBuild(t *testing.T) {
	submitter := &fakeBuildSubmitter{}
	d := newTestDeployer(t, "project: my-app\n", AutoDeployConfig{
		ObjectStore: &fakeObjectUploader{}, BuildSubmitter: submitter,
	})

	err := d.HandlePush(context.Background(), PushRequest{
		RepoFullName: "owner/repo", Branch: "main", CommitSHA: "sha1234567", InstallationID: 1,
		TenantID: "tenant-1", ProjectName: "my-app",
	})

	if err != nil {
		t.Fatalf("HandlePush = %v, want nil (not deployable is not a failure)", err)
	}
	if submitter.submittedReq != nil {
		t.Errorf("a build was submitted for a descriptor with no apps: %+v", submitter.submittedReq)
	}
}

func TestHandlePush_EmptyDescriptorSubmitsNoBuild(t *testing.T) {
	submitter := &fakeBuildSubmitter{}
	d := newTestDeployer(t, "\n", AutoDeployConfig{
		ObjectStore: &fakeObjectUploader{}, BuildSubmitter: submitter,
	})

	if err := d.HandlePush(context.Background(), PushRequest{
		RepoFullName: "owner/repo", Branch: "main", CommitSHA: "sha1234567", InstallationID: 1,
		TenantID: "tenant-1", ProjectName: "my-app",
	}); err != nil {
		t.Fatalf("HandlePush = %v, want nil", err)
	}
	if submitter.submittedReq != nil {
		t.Errorf("a build was submitted for an empty descriptor: %+v", submitter.submittedReq)
	}
}

func TestPrepareSourceBundle_DescriptorWithoutAppsReturnsNoDescriptor(t *testing.T) {
	uploader := &fakeObjectUploader{}
	d := newTestDeployer(t, "project: my-app\n", AutoDeployConfig{ObjectStore: uploader})

	y, err := d.PrepareSourceBundle(context.Background(), "owner/repo", "sha4", 1, "tenant-1")

	if err != nil || y != nil {
		t.Fatalf("PrepareSourceBundle = (%+v, %v), want (nil, nil)", y, err)
	}
	if uploader.uploadedKey != "tenant-1/source/sha4.bundle" {
		t.Errorf("the bundle must still be staged; uploaded key = %q", uploader.uploadedKey)
	}
}

// Every app is built by the canonical Dockerfile, which needs a runtime on the
// fixed stack (node or python) and a start command. A descriptor without them
// was connected, and then every push failed in the build: `framework: node`
// (the CLI's theo.yaml field) built as runtime "unsupported", and an app with
// no `start` failed generating its Dockerfile. Both builds reported "missing
// image digest", which names neither field.
func TestParseDeployDescriptor_RefusesAppsTheBuildCannotMake(t *testing.T) {
	cases := map[string]struct{ content, mentions string }{
		"no runtime":          {"apps:\n  web:\n    framework: node\n    start: node server.js\n", "runtime"},
		"unsupported runtime": {"apps:\n  web:\n    runtime: go\n    start: ./server\n", "runtime"},
		"no start command":    {"apps:\n  web:\n    runtime: node\n", "start"},
		"one bad app of two":  {"apps:\n  api:\n    runtime: python\n    start: python app.py\n  web:\n    runtime: node\n", "web"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			d, err := ParseDeployDescriptor([]byte(tc.content))

			var invalid *InvalidDeployDescriptorError
			if !errors.As(err, &invalid) {
				t.Fatalf("ParseDeployDescriptor = (%+v, %v), want *InvalidDeployDescriptorError", d, err)
			}
			if !strings.Contains(invalid.Error(), tc.mentions) {
				t.Errorf("refusal %q does not name %q", invalid.Error(), tc.mentions)
			}
		})
	}
}

func TestParseDeployDescriptor_AcceptsNodeAndPythonAppsWithAStartCommand(t *testing.T) {
	content := "apps:\n  web:\n    runtime: node\n    start: node server.js\n  api:\n    runtime: python\n    start: python app.py\n"

	if _, err := ParseDeployDescriptor([]byte(content)); err != nil {
		t.Fatalf("ParseDeployDescriptor: %v", err)
	}
}
