// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package englishonly

import (
	"bufio"
	"embed"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

//go:embed words/*.txt
var wordFiles embed.FS

// Word lists, loaded once from words/. They live in files, one word per line,
// so the gate's own vocabulary never forms a Portuguese line for the gate to
// report.
var (
	strongWords = mustLoadWords("words/strong.txt")
	weakWords   = mustLoadWords("words/weak.txt")
	englishWord = mustLoadWords("words/english.txt")
	loanwords   = mustLoadWords("words/loanwords.txt")
)

func mustLoadWords(name string) map[string]bool {
	data, err := wordFiles.ReadFile(name)
	if err != nil {
		panic(fmt.Sprintf("englishonly: embedded word list %s: %v", name, err))
	}
	words := map[string]bool{}
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		w := strings.TrimSpace(sc.Text())
		if w == "" || strings.HasPrefix(w, "#") {
			continue
		}
		words[w] = true
	}
	if len(words) == 0 {
		panic(fmt.Sprintf("englishonly: embedded word list %s is empty", name))
	}
	return words
}

// portugueseDiacritics are the marks Portuguese spelling uses. A word carrying
// one is Portuguese unless English borrowed it with the mark (loanwords.txt).
const portugueseDiacritics = "çãõáéíóúâêôàÇÃÕÁÉÍÓÚÂÊÔÀ"

// apostropheStandIns are words written with a trailing apostrophe in place of
// the accent a keyboard could not type ("e'" for "é", "so'" for "só"). No
// English word takes that shape.
var apostropheStandIns = map[string]bool{"so": true, "le": true, "ve": true}

// minWordsForOneStrongWord is the shortest line in which one strong word plus a
// connective reads as prose. Below it, the words are more likely identifiers
// ("ja, _ := json.Marshal(a)").
const minWordsForOneStrongWord = 4

// Verdict is Classify's answer for one line.
type Verdict struct {
	Portuguese bool
	// Evidence names the words that decided, so a finding can be checked by
	// reading it rather than by re-running the detector.
	Evidence []string
}

// codeSpans are removed before reading words: a URL, and inline code in
// backticks, name things rather than say them.
var codeSpans = regexp.MustCompile("https?://\\S+|`[^`]*`")

// Classify decides whether one line is Portuguese.
//
// The detector prefers missing a Portuguese line to flagging an English one:
// a gate that cries wolf is disabled, and one that misses a line leaves the
// next line of the same comment to be caught. So a line is Portuguese only
// when:
//
//   - it has a word with a Portuguese diacritic that English did not borrow; or
//   - it has two distinct words from words/strong.txt; or
//   - it has one such word, beside a connective from words/weak.txt, in a line
//     of at least minWordsForOneStrongWord words with no English function word.
//
// Paths, URLs, inline code, snake_case, camelCase and tokens with digits are
// not words, so identifiers do not trip it.
func Classify(line string) Verdict {
	var (
		accented []string
		strong   = map[string]bool{}
		weak     int
		english  int
		nWords   int
	)
	for _, tok := range words(line) {
		nWords++
		apostrophe := strings.HasSuffix(tok, "'")
		base := strings.Trim(tok, "'")
		if base == "" || !isAllLetters(base) {
			continue
		}
		switch {
		case strings.ContainsAny(base, portugueseDiacritics) && !loanwords[base]:
			accented = append(accented, base)
		case apostrophe && (strongWords[base] || weakWords[base] || apostropheStandIns[base]):
			strong[base+"'"] = true
		case strongWords[base]:
			strong[base] = true
		case weakWords[base]:
			weak++
		case englishWord[base]:
			english++
		}
	}
	evidence := append(accented, sortedKeys(strong)...)
	switch {
	case len(accented) > 0,
		len(strong) >= 2,
		len(strong) == 1 && weak >= 1 && english == 0 && nWords >= minWordsForOneStrongWord:
		return Verdict{Portuguese: true, Evidence: evidence}
	}
	return Verdict{}
}

// words returns the lower-cased prose words of a line.
func words(line string) []string {
	if strings.Contains(line, "`") || strings.Contains(line, "http") {
		line = codeSpans.ReplaceAllString(line, " ")
	}
	var out []string
	for _, raw := range strings.FieldsFunc(line, isTokenSeparator) {
		if strings.ContainsAny(raw, `/_\`) {
			continue // a path or an identifier
		}
		for _, part := range strings.Split(strings.Trim(raw, "."), "-") {
			if part == "" || strings.Contains(part, ".") || strings.ContainsAny(part, "0123456789") || isCamelCase(part) {
				continue
			}
			out = append(out, strings.ToLower(part))
		}
	}
	return out
}

// isTokenSeparator splits on whitespace and on punctuation that never sits
// inside a word.
func isTokenSeparator(r rune) bool {
	return unicode.IsSpace(r) || strings.ContainsRune(tokenSeparators, r)
}

const tokenSeparators = "()[]{}<>\"“”,;:!?|*#="

// isCamelCase marks an identifier: a lower-case letter followed by an
// upper-case one inside one token.
func isCamelCase(s string) bool {
	prevLower := false
	for _, r := range s {
		if prevLower && unicode.IsUpper(r) {
			return true
		}
		prevLower = unicode.IsLower(r)
	}
	return false
}

func isAllLetters(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
