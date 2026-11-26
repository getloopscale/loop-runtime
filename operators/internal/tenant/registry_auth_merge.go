// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package tenant

import (
	"encoding/json"
	"fmt"
)

// mergedAuthEntry represents one registry's credentials inside a Docker
// config.json `auths` map. Username/Password are kept when the source document
// carries them; `auth` is the base64(username:password) field that every OCI
// tooling (BuildKit, crane, cosign) consumes.
type mergedAuthEntry struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Auth     string `json:"auth,omitempty"`
}

// mergedDockerConfig is the on-disk shape of `config.json` consumed by BuildKit,
// crane, and cosign. Only the `auths` map is relevant for our merge logic.
type mergedDockerConfig struct {
	Auths map[string]mergedAuthEntry `json:"auths"`
}

// parseDockerConfigJSON tolerates empty/nil input and returns an initialized
// struct. We never want a nil map downstream because every caller writes into it.
func parseDockerConfigJSON(data []byte) (mergedDockerConfig, error) {
	cfg := mergedDockerConfig{Auths: map[string]mergedAuthEntry{}}
	if len(data) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return mergedDockerConfig{}, fmt.Errorf("parse docker config.json: %w", err)
	}
	if cfg.Auths == nil {
		cfg.Auths = map[string]mergedAuthEntry{}
	}
	return cfg, nil
}

// upsertAuth replaces (or adds) the auth entry for `host`, preserving every
// other host already present, so the operator never erases a registry entry it
// did not write.
func (c *mergedDockerConfig) upsertAuth(host string, entry mergedAuthEntry) {
	if c.Auths == nil {
		c.Auths = map[string]mergedAuthEntry{}
	}
	c.Auths[host] = entry
}

// marshalDockerConfigJSON returns canonical config.json bytes.
func marshalDockerConfigJSON(cfg mergedDockerConfig) ([]byte, error) {
	if cfg.Auths == nil {
		cfg.Auths = map[string]mergedAuthEntry{}
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal docker config.json: %w", err)
	}
	return data, nil
}

// mergeAuthsByHost reads an existing config.json (`existing`) and applies the
// `incoming` upserts on top: every host present in `incoming` overrides the
// same host in `existing`; hosts only present in `existing` are preserved.
//
// On corrupt/partial existing JSON we start from an empty map rather than
// refusing to write — the next reconcile pass will then converge.
func mergeAuthsByHost(existing []byte, incoming map[string]mergedAuthEntry) ([]byte, error) {
	current, err := parseDockerConfigJSON(existing)
	if err != nil {
		current = mergedDockerConfig{Auths: map[string]mergedAuthEntry{}}
	}
	for host, entry := range incoming {
		current.upsertAuth(host, entry)
	}
	return marshalDockerConfigJSON(current)
}
