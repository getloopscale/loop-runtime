// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package transitcoverage is the Go home for the vault-prefix-format gate
// (T3.7 of plan 2026-06-02-openbao-adoption v1.3). Enforces the FAANG
// dual-path invariant that ALL ciphertext writes go through the
// encryptForStore dispatcher (which routes to Vault Transit when configured,
// AES legacy otherwise) — NEVER directly via s.encrypt() bypassing the dispatcher.
//
// Three invariants asserted statically:
//
//  1. vaultPrefix constant in secret_wrapper.go matches "vault:v1:"
//     (canonical OpenBao Transit ciphertext prefix).
//
//  2. Set() method calls s.encryptForStore (NOT raw s.encrypt).
//     Direct s.encrypt outside helper methods + dispatcher = regression.
//
//  3. ListForResync (and any other public read path) calls s.decryptFromStore
//     (NOT raw s.decrypt). Direct s.decrypt outside helper methods = regression.
//
// Rationale: post T3.5, the dispatcher chooses Vault Transit or AES based on
// runtime config. Bypassing the dispatcher would silently fall back to AES
// even when Vault was configured — exactly the regression vector we built
// the dual-path to prevent.
package transitcoverage

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Config struct {
	RepoRoot string
}

type Outcome struct {
	Pass       bool
	Violations []string
	Findings   map[string]string // metric name → value
}

// target file paths (relative to RepoRoot)
const (
	secretWrapperPath = "api/internal/db/store/secret_wrapper.go"
)

// canonical constant we MUST find in secret_wrapper.go
var vaultPrefixLine = regexp.MustCompile(`vaultPrefix\s*=\s*\[\]byte\("vault:v1:"\)`)

// direct call patterns that bypass the dispatcher (regression vector)
var (
	rawEncryptCall = regexp.MustCompile(`\bs\.encrypt\(`)
	rawDecryptCall = regexp.MustCompile(`\bs\.decrypt\(`)
	dispatcherEnc  = regexp.MustCompile(`\bs\.encryptForStore\(`)
	dispatcherDec  = regexp.MustCompile(`\bs\.decryptFromStore\(`)
)

// Methods that are EXPECTED to call s.encrypt / s.decrypt directly because
// they ARE the dispatcher's underlying primitive. Calls OUTSIDE these methods
// (e.g., directly from Set() or ListForResync()) are violations.
var dispatcherInternalMethods = map[string]bool{
	"encryptForStore":  true,
	"decryptFromStore": true,
	"encrypt":          true, // the AES primitive itself
	"decrypt":          true,
}

// Check runs all three invariants and returns Outcome.
func Check(cfg Config) (*Outcome, error) {
	out := &Outcome{
		Findings: make(map[string]string),
	}

	target := filepath.Join(cfg.RepoRoot, secretWrapperPath)
	// #nosec G304 -- target is a fixed source-tree path.
	data, err := os.ReadFile(target)
	if err != nil {
		out.Violations = append(out.Violations,
			fmt.Sprintf("cannot read %s: %v", secretWrapperPath, err))
		out.Pass = false
		return out, nil
	}
	content := string(data)

	// Invariant 1: vaultPrefix constant exists + matches canonical value
	if !vaultPrefixLine.MatchString(content) {
		out.Violations = append(out.Violations,
			fmt.Sprintf("%s: vaultPrefix constant missing or does not match `[]byte(\"vault:v1:\")`",
				secretWrapperPath))
	} else {
		out.Findings["vault_prefix"] = "found"
	}

	// Invariants 2+3: walk Set + ListForResync method bodies + flag raw
	// s.encrypt / s.decrypt calls. Also count dispatcher uses for sanity.
	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	curMethod := ""
	depth := 0
	lineNo := 0
	rawEnc, rawDec, dispEnc, dispDec := 0, 0, 0, 0

	methodHeader := regexp.MustCompile(`^func\s+\(s\s+\*?PostgresSecretStore\)\s+(\w+)\s*\(`)

	for scanner.Scan() {
		lineNo++
		line := scanner.Text()

		if depth == 0 {
			if m := methodHeader.FindStringSubmatch(line); m != nil {
				curMethod = m[1]
			}
		}
		// Track brace depth to know when we're inside a method body
		for _, ch := range line {
			if ch == '{' {
				depth++
			} else if ch == '}' {
				depth--
				if depth == 0 {
					curMethod = ""
				}
			}
		}

		if curMethod == "" || dispatcherInternalMethods[curMethod] {
			// Outside any tracked method OR inside the dispatcher itself
			// (encrypt/decrypt primitives ARE expected to be called from
			// encryptForStore/decryptFromStore).
			continue
		}

		if rawEncryptCall.MatchString(line) {
			rawEnc++
			out.Violations = append(out.Violations,
				fmt.Sprintf("%s:%d: %s() calls s.encrypt() directly — must use s.encryptForStore() dispatcher (T3.5)",
					secretWrapperPath, lineNo, curMethod))
		}
		if rawDecryptCall.MatchString(line) {
			rawDec++
			out.Violations = append(out.Violations,
				fmt.Sprintf("%s:%d: %s() calls s.decrypt() directly — must use s.decryptFromStore() dispatcher (T3.4)",
					secretWrapperPath, lineNo, curMethod))
		}
		if dispatcherEnc.MatchString(line) {
			dispEnc++
		}
		if dispatcherDec.MatchString(line) {
			dispDec++
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	out.Findings["raw_encrypt_outside_dispatcher"] = fmt.Sprintf("%d", rawEnc)
	out.Findings["raw_decrypt_outside_dispatcher"] = fmt.Sprintf("%d", rawDec)
	out.Findings["dispatcher_encrypt_uses"] = fmt.Sprintf("%d", dispEnc)
	out.Findings["dispatcher_decrypt_uses"] = fmt.Sprintf("%d", dispDec)

	// At least 1 dispatcher use each is expected (Set must call encryptForStore;
	// ListForResync must call decryptFromStore).
	if dispEnc < 1 {
		out.Violations = append(out.Violations,
			fmt.Sprintf("%s: no s.encryptForStore() call found outside dispatcher — Set() likely bypasses dual-path (T3.5)",
				secretWrapperPath))
	}
	if dispDec < 1 {
		out.Violations = append(out.Violations,
			fmt.Sprintf("%s: no s.decryptFromStore() call found outside dispatcher — read path likely bypasses dual-path (T3.4)",
				secretWrapperPath))
	}

	out.Pass = len(out.Violations) == 0
	return out, nil
}

// FormatReport returns a human-readable summary suitable for CLI output.
func (o *Outcome) FormatReport() string {
	var b strings.Builder
	if o.Pass {
		fmt.Fprintf(&b, "transit-coverage: PASS\n")
		for k, v := range o.Findings {
			fmt.Fprintf(&b, "  %s: %s\n", k, v)
		}
		return b.String()
	}
	fmt.Fprintf(&b, "transit-coverage: FAIL\n")
	for _, v := range o.Violations {
		fmt.Fprintf(&b, "  %s\n", v)
	}
	for k, v := range o.Findings {
		fmt.Fprintf(&b, "  %s: %s\n", k, v)
	}
	return b.String()
}
