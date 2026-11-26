package noaesremaining

import "testing"

// A TRAILING COMMENT HID LIVE AES FROM THE GATE THAT EXISTS TO FIND IT.
//
// The line scanner set inComment on any line holding `/*` and then `continue`d on any
// line holding `*/`, so a line carrying BOTH discarded itself — live code included.
//
// Measured 2026-09-06 by an audit, inside the scan root this item had just widened: a
// complete AES-256-GCM implementation whose three marker lines each ended in a block
// comment compiled, was gofmt-clean, and returned PASS. One token.

// seedRoots gives every scan root a file, so the per-root "read nothing" floor
// (added by B-195 itself) does not fire and stand in for the verdict under test.
func seedRoots(t *testing.T, root string) {
	t.Helper()
	writeGo(t, root, "pkg/keep.go", "package pkg\n\nvar Keep = 1\n")
	writeGo(t, root, "operators/api/keep.go", "package api\n\nvar Keep = 1\n")
}

func TestAMarkerWithATrailingBlockCommentIsStillFound(t *testing.T) {
	root := t.TempDir()
	seedRoots(t, root)
	writeGo(t, root, "api/internal/db/store/legacy.go",
		"package store\n\n"+
			"import \"crypto/aes\" /* legacy */\n\n"+
			"func f(key, data []byte) {\n"+
			"\tblock, _ := aes.NewCipher(key) /* AES-256 */\n"+
			"\t_, _ = cipher.NewGCM(block) /* GCM */\n"+
			"}\n")

	out, err := Check(Config{RepoRoot: root, NowOverride: enforcedNow()})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if out.Pass {
		t.Fatalf("three live AES markers, each with a trailing comment, and the gate passed")
	}
	if len(out.Hits) != 3 {
		t.Fatalf("want all three markers found, got %d: %+v", len(out.Hits), out.Hits)
	}
}

func TestAMarkerWhollyInsideACommentIsStillIgnored(t *testing.T) {
	// The property the old code got right, which the fix must not lose.
	root := t.TempDir()
	seedRoots(t, root)
	writeGo(t, root, "api/internal/db/store/notes.go",
		"package store\n\n"+
			"// import \"crypto/aes\"\n"+
			"/*\n"+
			"aes.NewCipher(key)\n"+
			"cipher.NewGCM(block)\n"+
			"*/\n\n"+
			"var X = 1\n")

	out, err := Check(Config{RepoRoot: root, NowOverride: enforcedNow()})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !out.Pass {
		t.Fatalf("markers wholly inside comments are prose, not code: %+v", out.Hits)
	}
}

func TestTheImportPathIsNotMaskedAwayAsAString(t *testing.T) {
	// `import "crypto/aes"` IS a string literal. Masking strings the way the
	// sibling gate does would hide the primary marker, which is why this gate
	// masks comments only.
	root := t.TempDir()
	seedRoots(t, root)
	writeGo(t, root, "api/internal/db/store/imp.go",
		"package store\n\nimport \"crypto/aes\"\n\nvar _ = aes.NewCipher\n")

	out, err := Check(Config{RepoRoot: root, NowOverride: enforcedNow()})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if out.Pass {
		t.Fatalf("the import marker was masked away: %+v", out)
	}
}

func TestAFileTheScannerCannotReadIsStillMatched(t *testing.T) {
	// Fails toward MORE detection. An unterminated block comment makes the scanner
	// emit one COMMENT token running to EOF; masking it would hide every marker
	// after it — in a gate whose subject is that the marker is gone.
	//
	// Added after a mutant that removed the fail-open SURVIVED: nothing exercised
	// the branch, so its only evidence was that it had been written.
	root := t.TempDir()
	seedRoots(t, root)
	writeGo(t, root, "api/internal/db/store/broken.go",
		"package store\n\n"+
			"/* unterminated\n\n"+
			"import \"crypto/aes\"\n\n"+
			"var _ = aes.NewCipher\n")

	out, err := Check(Config{RepoRoot: root, NowOverride: enforcedNow()})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if out.Pass {
		t.Fatalf("a file the scanner could not read cleanly must be matched unmasked, "+
			"or an unterminated comment hides everything after it: %+v", out)
	}
}
