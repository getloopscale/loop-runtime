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
const imageCrane = "gcr.io/go-containerregistry/crane:v0.20.2"

// harborMirrorTemplate generates an inline container template that copies an image
// from the primary registry (DOCR) to Harbor using crane copy.
//
// MEET-048: This is a best-effort operation during the DOCR→Harbor migration.
// The DAG task uses ContinueOn{Failed: true} so a mirror failure does not block the build.
// Harbor credentials are mounted from buildkit-harbor-registry-config secret.
func harborMirrorTemplate(appName, sourceTag, harborTag string) *forge.Container {
	return &forge.Container{
		Name:  fmt.Sprintf("mirror-harbor-%s", appName),
		Image: imageCrane,
		Command: []string{"sh", "-c"},
		Args: []string{
			fmt.Sprintf(
				`echo "Mirroring %s → %s" && crane copy "%s" "%s" && echo "Mirror OK"`,
				sourceTag, harborTag, sourceTag, harborTag,
			),
		},
		VolumeMounts: []forge.VolumeBuilder{
			// Mount both registry credential secrets for crane auth.
			&forge.SecretVolume{BaseVolume: forge.BaseVolume{
				Name: secretRegistryConfig, MountPath: "/root/.docker", ReadOnly: true,
			}, SecretName: secretRegistryConfig},
		},
		Resources: &forge.ResourceRequirements{
			Requests: forge.ResourceList{CPU: "50m", Memory: "64Mi"},
			Limits:   forge.ResourceList{CPU: "200m", Memory: "256Mi"},
		},
		SecurityContext: &model.SecurityContext{
			RunAsNonRoot: ptr(true),
			RunAsUser:    ptr(int64(65534)),
			RunAsGroup:   ptr(int64(65534)),
		},
	}
}
