// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"fmt"

	forge "github.com/getloopscale/loopzero-forge"
	"github.com/getloopscale/loopzero-forge/model"
)

// imageCrane is the crane image for registry operations.
// crane is a lightweight Go tool for interacting with remote container registries.
const imageCrane = "gcr.io/go-containerregistry/crane:v0.21.5"

// harborMirrorTemplate generates an inline container template that copies an image
// from the primary registry (DOCR) to Harbor using crane copy.
//
// MEET-048: This is a best-effort operation during the DOCR→Harbor migration.
// The DAG task uses ContinueOn{Failed: true} so a mirror failure does not block the build.
// Harbor credentials are mounted from buildkit-harbor-registry-config secret.
func harborMirrorTemplate(appName, sourceTag, harborTag string) *forge.Container {
	// crane v0.21+ is a distroless image with no /bin/sh. Call the binary
	// directly via Command + Args instead of wrapping in `sh -c`.
	// Entrypoint in the distroless image is /ko-app/crane (standard ko build).
	//
	// Registry auth: crane reads credentials from $DOCKER_CONFIG/config.json.
	// We mount the secret in /tmp/.docker (accessible by nonroot user 65534,
	// unlike /root/.docker which requires root permission) and point
	// DOCKER_CONFIG at it.
	return &forge.Container{
		Name:    fmt.Sprintf("mirror-harbor-%s", appName),
		Image:   imageCrane,
		Command: []string{"/ko-app/crane"},
		Args:    []string{"copy", sourceTag, harborTag},
		Env: []forge.EnvBuilder{
			forge.Env{Name: "DOCKER_CONFIG", Value: "/tmp/.docker"},
			forge.Env{Name: "HOME", Value: "/tmp"},
		},
		VolumeMounts: []forge.VolumeBuilder{
			// Mount the registry credentials secret at /tmp/.docker so user 65534
			// can read config.json (registry.digitalocean.com + harbor auth).
			&forge.SecretVolume{BaseVolume: forge.BaseVolume{
				Name: secretRegistryConfig, MountPath: "/tmp/.docker", ReadOnly: true,
			}, SecretName: secretRegistryConfig},
		},
		Resources: &forge.ResourceRequirements{
			Requests: forge.ResourceList{CPU: "50m", Memory: "64Mi"},
			Limits:   forge.ResourceList{CPU: "200m", Memory: "256Mi"},
		},
		SecurityContext: &model.SecurityContext{
			RunAsNonRoot: forge.Ptr(true),
			RunAsUser:    forge.Ptr(int64(65534)),
			RunAsGroup:   forge.Ptr(int64(65534)),
		},
	}
}
