// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package github

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DeployDescriptorPath is the file, at the repository root, that makes a
// GitHub repository deployable by push. It is the GitHub path's descriptor
// only: a CLI upload carries `.loopzero/services.json` instead, validated at the
// upload seam (docs/loopzerokit-loopzerocloud-seam.md). A repository never carries
// that file — it is build output — so the two paths read different files and
// neither accepts the other's.
const DeployDescriptorPath = "loopscale.yaml"

// DeployDescriptorPaths lists every name the descriptor has had, in the order
// they are looked up. The names after the first are the ones earlier releases
// of the API and of the CLI wrote; a repository that still carries one keeps
// deploying. When a repository has more than one, the first in this list wins.
var DeployDescriptorPaths = []string{DeployDescriptorPath, "loopzero.yaml", "theo.yaml"}

// InvalidDeployDescriptorError says why a descriptor cannot be deployed. The
// message is written for the person who owns the repository.
type InvalidDeployDescriptorError struct {
	// Path is the file that was read. Empty means DeployDescriptorPath.
	Path   string
	Reason string
}

func (e *InvalidDeployDescriptorError) Error() string {
	path := e.Path
	if path == "" {
		path = DeployDescriptorPath
	}
	return path + " " + e.Reason
}

// ParseDeployDescriptorFile applies ParseDeployDescriptor to the content of
// path, so that a refusal names the file the repository has.
func ParseDeployDescriptorFile(path string, content []byte) (*LoopZeroYamlMinimal, error) {
	descriptor, err := ParseDeployDescriptor(content)
	var invalid *InvalidDeployDescriptorError
	if errors.As(err, &invalid) {
		invalid.Path = path
	}
	return descriptor, err
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
	if err := checkAppsAreBuildable(descriptor.Apps); err != nil {
		return nil, err
	}
	return &descriptor, nil
}

// checkAppsAreBuildable refuses an app the build cannot make. Every app is built
// from the canonical Dockerfile, which needs a runtime of the fixed stack and a
// start command; without them the push is accepted and the build fails later
// with an error that names neither field.
func checkAppsAreBuildable(apps map[string]map[string]interface{}) error {
	names := make([]string, 0, len(apps))
	for name := range apps {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		app := apps[name]
		runtime, _ := app["runtime"].(string)
		if runtime != "node" && runtime != "python" {
			return &InvalidDeployDescriptorError{Reason: fmt.Sprintf(
				"app %q needs `runtime: node` or `runtime: python` (got %q).", name, runtime)}
		}
		if start, _ := app["start"].(string); strings.TrimSpace(start) == "" {
			return &InvalidDeployDescriptorError{Reason: fmt.Sprintf(
				"app %q needs `start`, the command that runs it (for example `start: node server.js`).", name)}
		}
	}
	return nil
}
