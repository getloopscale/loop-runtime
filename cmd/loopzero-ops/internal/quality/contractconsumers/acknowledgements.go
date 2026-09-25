// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package contractconsumers

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DefaultAcknowledgementsFile is where drops decided on purpose are recorded.
const DefaultAcknowledgementsFile = "infra/quality/contract-drops.txt"

// ErrMalformedAcknowledgement means a line is not `tag | file | reason` with
// all three present: an acknowledgement nobody justified is a rubber stamp.
var ErrMalformedAcknowledgement = errors.New("contract-drops: an acknowledgement must read `tag | file | reason`, all three non-empty")

// acknowledgement is one decided drop: the tag left this file on purpose.
type acknowledgement struct {
	tag, file, reason string
}

func (a acknowledgement) ident() string { return a.tag + " | " + a.file }

// loadAcknowledgements reads the file. Blank lines and `#` comments are
// ignored; an absent file acknowledges nothing.
func loadAcknowledgements(root, path string) ([]acknowledgement, error) {
	if path == "" {
		path = DefaultAcknowledgementsFile
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	f, err := os.Open(path) // #nosec G304 -- a repository-declared registry path
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("contract-drops: %w", err)
	}
	defer func() { _ = f.Close() }()

	var acks []acknowledgement
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("%w (line %d: %q)", ErrMalformedAcknowledgement, n, line)
		}
		a := acknowledgement{
			tag:    strings.TrimSpace(parts[0]),
			file:   strings.TrimSpace(parts[1]),
			reason: strings.TrimSpace(parts[2]),
		}
		if a.tag == "" || a.file == "" || a.reason == "" {
			return nil, fmt.Errorf("%w (line %d: %q)", ErrMalformedAcknowledgement, n, line)
		}
		acks = append(acks, a)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("contract-drops: %w", err)
	}
	return acks, nil
}

// applyAcknowledgements splits the dropped tags into those still to be judged
// by their consumers and those whose every net removal is acknowledged, and
// returns the acknowledgements no current drop matches.
//
// A tag is acknowledged when the losses nobody acknowledged are covered by
// declarations that appeared elsewhere: a declaration that left one file and
// arrived in another moved, it did not leave the contract. The count is the
// same measure the drop itself uses (structsDeclaringTagAt), so a move is never
// a drop here either — whether or not git's rename detection paired the files.
func applyAcknowledgements(root, base, head string, dropped []string, acks []acknowledgement, renamed map[string]string) (remaining, acknowledged, stale []string, err error) {
	declared := map[string]bool{}
	for _, a := range acks {
		declared[a.ident()] = true
	}
	used := map[string]bool{}
	for _, tag := range dropped {
		change, ferr := tagChangePerFile(root, base, head, tag, renamed)
		if ferr != nil {
			return nil, nil, nil, ferr
		}
		unacknowledged := 0
		acknowledgedLosses := 0
		for file, lost := range change.lost {
			id := acknowledgement{tag: tag, file: file}.ident()
			if declared[id] {
				used[id] = true
				acknowledgedLosses += lost
			} else {
				unacknowledged += lost
			}
		}
		if acknowledgedLosses > 0 && unacknowledged <= change.gained {
			acknowledged = append(acknowledged, tag)
		} else {
			remaining = append(remaining, tag)
		}
	}
	for _, a := range acks {
		if !used[a.ident()] {
			stale = append(stale, a.ident())
		}
	}
	sort.Strings(stale)
	return remaining, acknowledged, stale, nil
}

// tagChange is how one tag's declarations moved between base and head: the
// declarations each base file lost, and the total that files gained.
type tagChange struct {
	lost   map[string]int
	gained int
}

// tagChangePerFile compares the tag's declarations file by file. A file git
// reports as renamed is compared with its new path, so a move it paired is
// neither a loss nor a gain; a loss is reported under the base path, which is
// what an acknowledgement names.
func tagChangePerFile(root, base, head, tag string, renamed map[string]string) (tagChange, error) {
	change := tagChange{lost: map[string]int{}}
	before, err := declarationsPerFileAt(root, base, tag)
	if err != nil {
		return change, err
	}
	after, err := declarationsPerFileAt(root, head, tag)
	if err != nil {
		return change, err
	}
	accounted := map[string]int{}
	for file, n := range before {
		now := file
		if to, ok := renamed[file]; ok {
			now = to
		}
		accounted[now] += n
		if after[now] < n {
			change.lost[file] = n - after[now]
		}
	}
	for file, n := range after {
		if n > accounted[file] {
			change.gained += n - accounted[file]
		}
	}
	return change, nil
}
