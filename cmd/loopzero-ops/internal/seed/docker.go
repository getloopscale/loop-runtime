// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"encoding/json"
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
		if errors.As(err, &ee) && inspectSaysContainerIsAbsent(string(ee.Stderr)) {
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

// inspectSaysContainerIsAbsent reports whether `docker inspect` failed because the
// container does not exist. Docker changed the message's case — "Error: No such
// object" in older releases, "error: no such object" now — and matching one spelling
// turned the absent OPTIONAL secrets cluster into a fatal setup error (#398). Any
// other failure (daemon down, permission) stays fatal.
func inspectSaysContainerIsAbsent(stderr string) bool {
	return strings.Contains(strings.ToLower(stderr), "no such object")
}

// defaultDockerNetworkInspect returns the IPv4 address (no CIDR) of the given
// container inside the docker network. D1 exemption: read-only introspection.
//
// A container address shuffles on every restart, so a consumer of it must be
// re-run after one. The peer kubeconfigs still need it: the k3s API server
// certificate carries the node IP in its SANs, and no stable host alias.
func defaultDockerNetworkInspect(network, container string) (string, error) {
	out, err := exec.Command("docker", "network", "inspect", network).Output()
	if err != nil {
		return "", fmt.Errorf("docker network inspect %s: %w", network, err)
	}
	// Response is `[{...}]` (an array of network entries).
	var nets []struct {
		Containers map[string]struct {
			Name string `json:"Name"`
			IPv4 string `json:"IPv4Address"`
		} `json:"Containers"`
	}
	if err := json.Unmarshal(out, &nets); err != nil {
		return "", fmt.Errorf("parse docker network inspect: %w", err)
	}
	for _, n := range nets {
		for _, c := range n.Containers {
			if c.Name == container {
				ip, _, _ := strings.Cut(c.IPv4, "/")
				if ip != "" {
					return ip, nil
				}
			}
		}
	}
	return "", fmt.Errorf("container %s not found in network %s", container, network)
}
