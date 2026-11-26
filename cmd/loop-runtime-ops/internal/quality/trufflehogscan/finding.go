// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package trufflehogscan

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrUnparseable means a line that IS JSON failed to parse.
//
// Silently skipping what does not parse turns DATA LOSS into "found nothing" — the
// exact shape of the defect this package exists to kill (EC-3). Log noise, which
// does not even start with `{`, is something else and is still ignored.
var ErrUnparseable = errors.New("unreadable scanner output — data loss is not the absence of a finding")

// maxLine — a finding line carries DetectorDescription and ExtraData; bufio.Scanner's
// 64KiB default cuts SILENTLY (EC-6).
const maxLine = 1024 * 1024

// Finding is one trufflehog candidate.
type Finding struct {
	Detector string
	File     string
	// Live=true means the providing service confirmed the credential works.
	Live bool

	id string
}

// ID is the stable identity written to the versioned baseline.
//
// It is a truncated `sha256(detector + "\0" + raw)`, and NEVER the raw value: the file is versioned,
// so writing the value would leak what the gate exists to prevent. The detector goes into the hash
// because the same text recognized by two detectors is two candidates.
func (f Finding) ID() string { return f.id }

// String renders a finding without ever exposing the secret.
func (f Finding) String() string {
	where := f.File
	if where == "" {
		where = "(commit message)"
	}
	return fmt.Sprintf("%s [%s] %s", where, f.Detector, f.id)
}

// rawFinding mirrors only the fields this gate reads. Everything else in trufflehog's
// JSON — including the raw value — dies in this struct and reaches no file.
type rawFinding struct {
	DetectorName   string `json:"DetectorName"`
	Raw            string `json:"Raw"`
	Verified       bool   `json:"Verified"`
	SourceMetadata struct {
		Data struct {
			Git struct {
				File string `json:"file"`
			} `json:"Git"`
		} `json:"Data"`
	} `json:"SourceMetadata"`
}

// ParseFindings reads trufflehog's `--json` stream.
func ParseFindings(r io.Reader) ([]Finding, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, maxLine), maxLine)

	var out []Finding
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "{") {
			continue // log noise, not a finding
		}
		var rf rawFinding
		if err := json.Unmarshal([]byte(line), &rf); err != nil {
			return nil, fmt.Errorf("%w: line %d: %w", ErrUnparseable, n, err)
		}
		if rf.DetectorName == "" {
			continue // valid JSON that is not a finding
		}
		out = append(out, Finding{
			Detector: rf.DetectorName,
			File:     rf.SourceMetadata.Data.Git.File,
			Live:     rf.Verified,
			id:       identity(rf.DetectorName, rf.Raw),
		})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnparseable, err)
	}
	return out, nil
}

func identity(detector, raw string) string {
	sum := sha256.Sum256([]byte(detector + "\x00" + raw))
	return hex.EncodeToString(sum[:])[:16]
}
