// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package englishonly

import (
	"go/scanner"
	"go/token"
	"path"
	"strings"
	"unicode"
)

// Names are the half of the repository the prose detector cannot read: it
// skips identifiers and paths on purpose, because a word-level guess over
// code mistakes names for prose. So a package named in Portuguese, its files,
// its types and its verdict constants passed the gate untouched (#380).
//
// Names are judged differently from prose. A name is split into its parts
// (camelCase, snake_case, kebab-case, digits), and a part is Portuguese only
// when it is a whole word from words/names.txt — a list of Portuguese words no
// English dictionary lists. Two sources of names are read, and only these two,
// because they are the ones that can be split without guessing:
//
//   - Go identifiers, taken from the Go tokenizer, so a string or a comment
//     is never mistaken for a name;
//   - the components of every tracked path.

// Finding kinds for names.
const (
	KindPortugueseIdentifier = "portuguese-identifier"
	KindPortuguesePath       = "portuguese-path"
)

var nameWords = mustLoadWords("words/names.txt")

// portugueseNameWords returns the parts of name that are Portuguese words.
func portugueseNameWords(name string) []string {
	var out []string
	for _, part := range splitName(name) {
		if nameWords[part] {
			out = append(out, part)
		}
	}
	return out
}

// splitName returns the lower-cased word parts of a name. Anything that is not
// a letter separates parts, and so does a case change: "HTTPProbeURL" is
// http, probe, url.
func splitName(name string) []string {
	var (
		out   []string
		runes = []rune(name)
		start = -1
	)
	flush := func(end int) {
		if start >= 0 && end > start {
			out = append(out, strings.ToLower(string(runes[start:end])))
		}
		start = -1
	}
	for i, r := range runes {
		if !unicode.IsLetter(r) {
			flush(i)
			continue
		}
		if start < 0 {
			start = i
			continue
		}
		prev := runes[i-1]
		lowerToUpper := unicode.IsLower(prev) && unicode.IsUpper(r)
		// The last capital of an acronym starts the next word: "HTTPProbe".
		acronymEnd := unicode.IsUpper(prev) && unicode.IsUpper(r) &&
			i+1 < len(runes) && unicode.IsLower(runes[i+1])
		if lowerToUpper || acronymEnd {
			flush(i)
			start = i
		}
	}
	flush(len(runes))
	return out
}

// scanGoIdentifiers reports the Portuguese identifiers of a Go file. Lines in
// exempt carry an exemption marker with a reason and are skipped; the prose
// scan has already counted them.
func scanGoIdentifiers(rel string, data []byte, exempt map[int]bool) []Finding {
	fset := token.NewFileSet()
	file := fset.AddFile(rel, fset.Base(), len(data))
	var s scanner.Scanner
	// A nil error handler: a file the tokenizer cannot fully read still yields
	// the identifiers it could, and compiling it is another gate's job.
	s.Init(file, data, nil, 0)
	type seenKey struct {
		line int
		name string
	}
	seen := map[seenKey]bool{}
	var findings []Finding
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			return findings
		}
		if tok != token.IDENT {
			continue
		}
		line := fset.Position(pos).Line
		key := seenKey{line, lit}
		if exempt[line] || seen[key] {
			continue
		}
		seen[key] = true
		if words := portugueseNameWords(lit); len(words) > 0 {
			findings = append(findings, Finding{Path: rel, Line: line, Kind: KindPortugueseIdentifier, Evidence: words, Text: lit})
		}
	}
}

// pathFindings reports the Portuguese components of rel. A directory is
// reported once, however many tracked files it holds; seen carries the
// prefixes already judged across calls.
func pathFindings(rel string, seen map[string]bool) []Finding {
	var findings []Finding
	parts := strings.Split(rel, "/")
	for i := range parts {
		prefix := path.Join(parts[:i+1]...)
		if seen[prefix] {
			continue
		}
		seen[prefix] = true
		if words := portugueseNameWords(parts[i]); len(words) > 0 {
			findings = append(findings, Finding{Path: prefix, Kind: KindPortuguesePath, Evidence: words, Text: parts[i]})
		}
	}
	return findings
}
