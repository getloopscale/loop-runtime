package main

// Every rule-file path cited in an operators Go comment resolves on disk.
//
// Written RED to pin a defect and left untracked, "not to ship". It ships now, because a
// check that does not run protects nobody: untracked, it reached one checkout and the
// four citations it found would have come back with nothing to say so.
//
// What it found on 2026-09-16: four comments in production code citing a design rule
// deleted in `9a4a29755`. Two were repointed at `CLAUDE.md`, which is versioned and
// declares itself the contract rather than a summary of one; three stated the rule
// instead of pointing at it, because it fits in a sentence and a stated rule cannot rot.
//
// The obvious repoint — the error-handling rule under `.claude/` — was measured and
// refused: that tree is gitignored, so the citation would have been dead again one
// directory over.

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// pathCitation matches a rules-directory path written in a comment, with or
// without the ".claude/" prefix. Both spellings occur in operators/.
var pathCitation = regexp.MustCompile(`(\.claude/)?rules/([a-z0-9._-]+\.md)`)

func repoRootFromOperators(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Dir(wd)
}

func TestOperatorsCommentRuleCitationsResolveOnDisk(t *testing.T) {
	root := repoRootFromOperators(t)
	operatorsDir := filepath.Join(root, "operators")

	checked := 0
	fset := token.NewFileSet()

	err := filepath.Walk(operatorsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "vendor" || info.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if perr != nil {
			return nil // unparseable file is not this test's subject
		}
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				for _, m := range pathCitation.FindAllString(c.Text, -1) {
					checked++
					// Resolve against the repo root, the way a reader would.
					rel := strings.TrimPrefix(m, ".claude/")
					candidates := []string{
						filepath.Join(root, m),
						filepath.Join(root, ".claude", rel),
					}
					found := false
					for _, cand := range candidates {
						if _, serr := os.Stat(cand); serr == nil {
							found = true
							break
						}
					}
					if !found {
						pos := fset.Position(c.Pos())
						relPath, _ := filepath.Rel(root, pos.Filename)
						t.Errorf("%s:%d cites %q, which does not resolve on disk",
							relPath, pos.Line, m)
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	// Floor: a matcher that enumerated nothing must FAIL, not pass silently.
	if checked == 0 {
		t.Fatal("enumerated ZERO rule-path citations under operators/ — " +
			"the matcher is broken, which is not the same as the tree being clean")
	}
	t.Logf("enumerated %d rule-path citation(s) under operators/", checked)
}
