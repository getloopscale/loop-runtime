// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package servicesacyclic

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrRegistryCycle means the allow-list itself permits a cycle, so a tree that
// realises it would pass a gate whose one promise is acyclicity.
var ErrRegistryCycle = errors.New("the services allow-list permits an import cycle")

// ErrRegistryUnknownTarget means an allowed edge points at a context the
// allow-list does not declare — a node the cycle check could not see.
var ErrRegistryUnknownTarget = errors.New("the services allow-list permits an edge to an undeclared context")

// checkRegistryAcyclic verifies the allow-list is a DAG over declared contexts.
// Iteration is sorted so the reported cycle is the same on every run.
func checkRegistryAcyclic(registry map[string]map[string]bool) error {
	contexts := sortedKeys(registry)
	for _, from := range contexts {
		for _, to := range sortedKeys(registry[from]) {
			if _, declared := registry[to]; !declared {
				return fmt.Errorf("%w: %s -> %s", ErrRegistryUnknownTarget, from, to)
			}
		}
	}

	const (
		unvisited = iota
		onPath
		done
	)
	state := make(map[string]int, len(registry))
	var path []string
	var visit func(string) error
	visit = func(ctx string) error {
		state[ctx] = onPath
		path = append(path, ctx)
		for _, next := range sortedKeys(registry[ctx]) {
			switch state[next] {
			case onPath:
				return fmt.Errorf("%w: %s", ErrRegistryCycle, cycleFrom(path, next))
			case unvisited:
				if err := visit(next); err != nil {
					return err
				}
			}
		}
		path = path[:len(path)-1]
		state[ctx] = done
		return nil
	}
	for _, ctx := range contexts {
		if state[ctx] == unvisited {
			if err := visit(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

// cycleFrom renders the part of the DFS path that closes back on start.
func cycleFrom(path []string, start string) string {
	for i, ctx := range path {
		if ctx == start {
			return strings.Join(append(append([]string{}, path[i:]...), start), " -> ")
		}
	}
	return start + " -> " + start
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
