package skiplink

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A `t.Skip(` inside a Go string literal is DATA, not a skip. The Go detector is a
// regexp over raw text and could not tell the two apart, so a test that builds Go
// source as a fixture was reported as carrying unlinked skips.
//
// Measured 2026-09-05: the B-180 gate's own test file, which feeds fake test source
// to an AST detector, produced ten violations this way and failed the whole chain
// at the seventh gate.

func writeGo(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

func scan(t *testing.T, dir, path string) []violation {
	t.Helper()
	return scanFile(path, dir, goSkipRe, regexp.MustCompile(`https://github\.com/\S+/issues/\d+`))
}

func TestSkipInsideAStringLiteralIsNotASkip(t *testing.T) {
	dir := t.TempDir()
	src := "package p\n" +
		"\n" +
		"const fixture = `\n" +
		"func TestGenerated(t *testing.T) {\n" +
		"\tif _, err := os.Stat(\"x\"); os.IsNotExist(err) {\n" +
		"\t\tt.Skipf(\"absent\")\n" +
		"\t}\n" +
		"\tt.Fatal(\"dead\")\n" +
		"}\n" +
		"`\n"
	p := writeGo(t, dir, "fixture_test.go", src)
	if got := scan(t, dir, p); len(got) != 0 {
		t.Fatalf("a skip inside a raw string is data, not a skip; got %d violation(s): %v", len(got), got)
	}
}

func TestARealSkipIsStillReported(t *testing.T) {
	dir := t.TempDir()
	src := "package p\n\nimport \"testing\"\n\nfunc TestReal(t *testing.T) {\n\tt.Skip(\"no reason given\")\n}\n"
	p := writeGo(t, dir, "real_test.go", src)
	got := scan(t, dir, p)
	if len(got) != 1 {
		t.Fatalf("an unlinked skip in code must still be reported; got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0].display, "no reason given") {
		t.Fatalf("the violation must show the line a person wrote, got %q", got[0].display)
	}
}

func TestBothInOneFileAreToldApart(t *testing.T) {
	dir := t.TempDir()
	src := "package p\n" +
		"\n" +
		"import \"testing\"\n" +
		"\n" +
		"const fixture = `t.Skip(\"data\")`\n" +
		"\n" +
		"func TestReal(t *testing.T) {\n" +
		"\tt.Skip(\"code\")\n" +
		"}\n"
	p := writeGo(t, dir, "mixed_test.go", src)
	got := scan(t, dir, p)
	if len(got) != 1 {
		t.Fatalf("expected exactly the code skip, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0].display, "code") || strings.Contains(got[0].display, "data") {
		t.Fatalf("told the wrong one apart: %q", got[0].display)
	}
}

func TestLineNumbersDoNotMoveWhenLiteralsAreMasked(t *testing.T) {
	dir := t.TempDir()
	// A multi-line raw string BEFORE the real skip: masking must preserve the
	// newlines inside it, or every line number after it shifts.
	src := "package p\n" +
		"\n" +
		"import \"testing\"\n" +
		"\n" +
		"const fixture = `\nline\nline\nline\n`\n" +
		"\n" +
		"func TestReal(t *testing.T) {\n" +
		"\tt.Skip(\"here\")\n" +
		"}\n"
	p := writeGo(t, dir, "offset_test.go", src)
	got := scan(t, dir, p)
	if len(got) != 1 {
		t.Fatalf("expected one violation, got %d: %v", len(got), got)
	}
	want := ":12:"
	if !strings.Contains(got[0].display, want) {
		t.Fatalf("line number moved: wanted %s in %q", want, got[0].display)
	}
}

func TestAFileTheScannerCannotReadIsStillMatched(t *testing.T) {
	// Fails toward MORE detection. The hazard is concrete: an unterminated raw
	// string EARLIER in the file makes the scanner emit one STRING token that runs
	// to EOF, swallowing every real skip after it. Masking that token would hide
	// them. So a file the scanner could not read cleanly is matched unmasked.
	//
	// The first version of this test put the malformed literal AFTER the skip,
	// where `t.Skip(` is never inside a literal either way — it passed with the
	// fail-open removed, and mutation said so.
	dir := t.TempDir()
	src := "package p\n" +
		"\n" +
		"const bad = `unterminated\n" +
		"\n" +
		"func TestBroken(t *testing.T) {\n" +
		"\tt.Skip(\"swallowed by the bad literal\")\n" +
		"}\n"
	p := writeGo(t, dir, "broken_test.go", src)
	got := scan(t, dir, p)
	if len(got) != 1 {
		t.Fatalf("an unparseable file must still be matched; got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0].display, "swallowed") {
		t.Fatalf("wrong line reported: %q", got[0].display)
	}
}

func TestANonGoFileIsUnaffected(t *testing.T) {
	dir := t.TempDir()
	p := writeGo(t, dir, "spec.ts", "it.skip('x', () => {})\n")
	got := scanFile(p, dir, tsSkipRe, regexp.MustCompile(`https://github\.com/\S+/issues/\d+`))
	if len(got) != 1 {
		t.Fatalf("the TypeScript path must be untouched by Go masking; got %d", len(got))
	}
}
