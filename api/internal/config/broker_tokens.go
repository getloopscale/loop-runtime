// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package config

import (
	"fmt"
	"strings"

	"github.com/getloopscale/loop-runtime/pkg/domain"
)

// BrokerCellTokens returns the secret-fetch broker's bearer per cell: the cell a request
// comes from is the cell whose token it presented (#456). LOOP_RUNTIME_BROKER_TOKENS lists
// `cell=token` pairs, comma-separated; LOOP_RUNTIME_BROKER_TOKEN, the single-cell form, is the
// default cell's token. An empty map disables the broker. The error never carries a token.
func (c Config) BrokerCellTokens() (map[string]string, error) {
	tokens := map[string]string{}
	owners := map[string]string{} // token → cell, to refuse a token two cells share
	add := func(cell, token string) error {
		if prev, ok := tokens[cell]; ok && prev != token {
			return fmt.Errorf("broker token for cell %q is set twice (LOOP_RUNTIME_BROKER_TOKEN and LOOP_RUNTIME_BROKER_TOKENS, or twice in LOOP_RUNTIME_BROKER_TOKENS)", cell)
		}
		if other, ok := owners[token]; ok && other != cell {
			return fmt.Errorf("LOOP_RUNTIME_BROKER_TOKENS gives cells %q and %q the same token; a request could not be bound to one", other, cell)
		}
		tokens[cell], owners[token] = token, cell
		return nil
	}
	if c.BrokerToken != "" {
		if err := add(domain.DefaultCellID, c.BrokerToken); err != nil {
			return nil, err
		}
	}
	listed := map[string]bool{}
	for i, entry := range strings.Split(c.BrokerTokens, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		cell, token, ok := strings.Cut(entry, "=")
		cell, token = strings.TrimSpace(cell), strings.TrimSpace(token)
		if !ok || cell == "" || token == "" {
			return nil, fmt.Errorf("LOOP_RUNTIME_BROKER_TOKENS entry %d is not cell=token", i+1)
		}
		if listed[cell] {
			return nil, fmt.Errorf("LOOP_RUNTIME_BROKER_TOKENS names cell %q twice", cell)
		}
		listed[cell] = true
		if err := add(cell, token); err != nil {
			return nil, err
		}
	}
	return tokens, nil
}

func (c Config) validateBrokerTokens() error {
	_, err := c.BrokerCellTokens()
	return err
}
