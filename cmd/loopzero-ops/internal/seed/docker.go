// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// errContainerAbsent marks a docker inspect of a container that does not
// exist — the one docker failure a caller may treat as "skip".
var errContainerAbsent = errors.New("container does not exist")

// defaultDockerInspect returns the first non-empty IPAddress from
// `docker inspect --format '{{...}}' <container>`. D1 exemption applies:
// this is READ-ONLY container introspection, not shell-out for mutation.
func defaultDockerInspect(container string) (string, error) {
	// docker inspect exits non-zero if the container is missing.
	out, err := exec.Command("docker", "inspect",
		container, "--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && strings.Contains(string(ee.Stderr), "No such object") {
			return "", fmt.Errorf("%s: %w", container, errContainerAbsent)
		}
		return "", fmt.Errorf("docker inspect %s: %w", container, err)
	}
	for _, ip := range strings.Fields(string(out)) {
		if ip != "" && ip != "<no value>" {
			return ip, nil
		}
	}
	return "", fmt.Errorf("no IPAddress found for %s (State.Running?)", container)
}
