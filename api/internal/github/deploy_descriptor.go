// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package github

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// DeployDescriptorPath is the file, at the repository root, that makes a
// GitHub repository deployable by push. It is the GitHub path's descriptor
// only: a CLI upload carries `.loopzero/services.json` instead, validated at the
// upload seam (docs/loopzerokit-loopzerocloud-seam.md). A repository never carries
// that file — it is build output — so the two paths read different files and
// neither accepts the other's.
const DeployDescriptorPath = "loopzero.yaml"

// InvalidDeployDescriptorError says why a descriptor cannot be deployed. The
// message is written for the person who owns the repository.
type InvalidDeployDescriptorError struct {
	Reason string
}

func (e *InvalidDeployDescriptorError) Error() string {
	return DeployDescriptorPath + " " + e.Reason
}

// ParseDeployDescriptor is the one rule that decides whether a repository can
// be deployed from GitHub. Connect refuses what it refuses, and a push deploys
// only what it accepts — so a repository can never be linked and then build
// nothing on every push.
//
// A deployable descriptor is a YAML mapping that declares at least one app
// under `apps`, each app itself a mapping: the build is made from those apps
// and from nothing else.
func ParseDeployDescriptor(content []byte) (*LoopZeroYamlMinimal, error) {
	if len(bytes.TrimSpace(content)) == 0 {
		return nil, &InvalidDeployDescriptorError{Reason: "is empty. Declare the apps to deploy under `apps:`."}
	}
	var document interface{}
	if err := yaml.Unmarshal(content, &document); err != nil {
		return nil, &InvalidDeployDescriptorError{Reason: fmt.Sprintf("is not valid YAML: %v", err)}
	}
	if _, isMapping := document.(map[string]interface{}); !isMapping {
		return nil, &InvalidDeployDescriptorError{Reason: "must be a mapping of fields, with the apps to deploy under `apps:`."}
	}
	var descriptor LoopZeroYamlMinimal
	if err := yaml.Unmarshal(content, &descriptor); err != nil {
		return nil, &InvalidDeployDescriptorError{Reason: "must declare `apps:` as a mapping of app name to its settings."}
	}
	if len(descriptor.Apps) == 0 {
		return nil, &InvalidDeployDescriptorError{Reason: "declares no apps. Add at least one app under `apps:`."}
	}
	return &descriptor, nil
}
