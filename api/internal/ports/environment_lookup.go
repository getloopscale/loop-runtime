// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package ports

// FindEnvironment returns the entry for the exact (project, name) tuple, or
// nil. Runtime environments are per-(project, env) since M2; a legacy entry
// carries an empty Project and therefore matches only an empty project.
// Matching by name alone reported another project's env of the same name as
// present, and the new tuple was never added to the Tenant CR (m37, #37).
func FindEnvironment(entries []EnvironmentEntry, project, name string) *EnvironmentEntry {
	for i := range entries {
		if entries[i].Name == name && entries[i].Project == project {
			return &entries[i]
		}
	}
	return nil
}
