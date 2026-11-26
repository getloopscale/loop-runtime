// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package postgresrestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/shellrun"
)

// ErrNotBackedUp: the cluster is not configured to archive through the plugin, so there
// is nothing for a drill to recover from.
var ErrNotBackedUp = errors.New("cluster has no backup configured")

// Kubectl is the Platform over kubectl: the cluster, its backups and psql in its pods.
type Kubectl struct {
	Runner    shellrun.Runner
	Context   string
	Namespace string
	Cluster   string
}

func (k Kubectl) run(ctx context.Context, args ...string) (string, error) {
	full := []string{"--namespace", k.Namespace}
	if k.Context != "" {
		full = append([]string{"--context", k.Context}, full...)
	}
	full = append(full, args...)
	res, err := k.Runner.Run(ctx, "kubectl", full)
	if err != nil {
		return "", fmt.Errorf("kubectl %s: %w", strings.Join(args, " "), err)
	}
	if !res.IsSuccess() {
		return "", fmt.Errorf("kubectl %s: exit %d: %s", strings.Join(args, " "), res.ExitCode, strings.TrimSpace(res.Stderr))
	}
	return strings.TrimSpace(res.Stdout), nil
}

type clusterObject struct {
	Spec struct {
		ImageName string `json:"imageName"`
		Storage   struct {
			Size         string `json:"size"`
			StorageClass string `json:"storageClass"`
		} `json:"storage"`
		Plugins []struct {
			Name       string            `json:"name"`
			Parameters map[string]string `json:"parameters"`
		} `json:"plugins"`
		ServiceAccountTemplate struct {
			Metadata struct {
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
		} `json:"serviceAccountTemplate"`
	} `json:"spec"`
	Status struct {
		CurrentPrimary string `json:"currentPrimary"`
	} `json:"status"`
}

// Source reads the running cluster.
func (k Kubectl) Source(ctx context.Context) (Source, error) {
	out, err := k.run(ctx, "get", "clusters.postgresql.cnpg.io", k.Cluster, "-o", "json")
	if err != nil {
		return Source{}, err
	}
	return sourceFromJSON(k.Cluster, k.Namespace, []byte(out))
}

func sourceFromJSON(name, namespace string, raw []byte) (Source, error) {
	var c clusterObject
	if err := json.Unmarshal(raw, &c); err != nil {
		return Source{}, fmt.Errorf("decode cluster %s: %w", name, err)
	}
	src := Source{
		Name: name, Namespace: namespace,
		Image:       c.Spec.ImageName,
		StorageSize: c.Spec.Storage.Size, StorageClass: c.Spec.Storage.StorageClass,
		RoleARN: c.Spec.ServiceAccountTemplate.Metadata.Annotations["eks.amazonaws.com/role-arn"],
		Primary: c.Status.CurrentPrimary,
	}
	for _, p := range c.Spec.Plugins {
		if p.Name == pluginName {
			src.ObjectStore = p.Parameters["barmanObjectName"]
			src.ServerName = p.Parameters["serverName"]
		}
	}
	if src.ObjectStore == "" {
		return Source{}, fmt.Errorf("%w: cluster %s has no %s plugin", ErrNotBackedUp, name, pluginName)
	}
	if src.ServerName == "" {
		src.ServerName = name
	}
	if src.Primary == "" {
		return Source{}, fmt.Errorf("cluster %s reports no current primary", name)
	}
	return src, nil
}

// HasCompletedBackup reports whether any Backup of the cluster completed.
func (k Kubectl) HasCompletedBackup(ctx context.Context) (bool, error) {
	out, err := k.run(ctx, "get", "backups.postgresql.cnpg.io", "-o",
		`jsonpath={range .items[*]}{.spec.cluster.name}={.status.phase}{"\n"}{end}`)
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(out, "\n") {
		if line == k.Cluster+"=completed" {
			return true, nil
		}
	}
	return false, nil
}

// SQL runs one statement with psql as the superuser, over the pod's local socket.
func (k Kubectl) SQL(ctx context.Context, pod, database, query string) (string, error) {
	return k.run(ctx, "exec", pod, "-c", "postgres", "--",
		"psql", "-U", "postgres", "-d", database, "-At", "-v", "ON_ERROR_STOP=1", "-c", query)
}

// Apply creates or updates the objects of a manifest.
func (k Kubectl) Apply(ctx context.Context, manifest []byte) error {
	f, err := os.CreateTemp("", "restore-drill-*.yaml")
	if err != nil {
		return fmt.Errorf("write the manifest: %w", err)
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(manifest); err != nil {
		return fmt.Errorf("write the manifest: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write the manifest: %w", err)
	}
	_, err = k.run(ctx, "apply", "-f", f.Name())
	return err
}

// Phase is the Cluster's status phase.
func (k Kubectl) Phase(ctx context.Context, cluster string) (string, error) {
	return k.run(ctx, "get", "clusters.postgresql.cnpg.io", cluster, "-o", "jsonpath={.status.phase}")
}

// Delete removes the Cluster; the operator removes its pods and volume claims with it.
func (k Kubectl) Delete(ctx context.Context, cluster string) error {
	_, err := k.run(ctx, "delete", "clusters.postgresql.cnpg.io", cluster,
		"--ignore-not-found", "--wait=true", "--timeout=180s")
	return err
}
