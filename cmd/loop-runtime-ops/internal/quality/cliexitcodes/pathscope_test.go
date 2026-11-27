package cliexitcodes

import (
	"path/filepath"
	"testing"
)

// WHERE a checkout sits must not decide whether its files are read.
//
// The exclusion rule matched the whole ABSOLUTE path for "/node_modules/", ".test."
// and ".spec.", so a loop-runtime-cli reached through a node_modules directory — the
// ordinary way a CLI is consumed — excluded every file beneath it, and the gate
// reported the scan as done over zero files. Inherited from the parent, where the
// only candidate was <repo>/cli and it could never fire; B-185 is what added an
// operator-supplied, out-of-repo candidate and made it reachable.

func TestACheckoutUnderNodeModulesIsStillScanned(t *testing.T) {
	root := t.TempDir()
	cli := filepath.Join(root, "app", "node_modules", "loop-runtime-cli")
	write(t, cli, "src/main.ts", "process.exit(42)\n")

	o := check(t, Config{RepoRoot: root, CLIRoot: cli})

	if o.ScannedFiles != 1 {
		t.Fatalf("the file is under node_modules and was skipped; ScannedFiles=%d", o.ScannedFiles)
	}
	if len(o.Violations) != 1 {
		t.Fatalf("process.exit(42) is outside the contract and must be reported: %v", o.Violations)
	}
}

func TestADirectoryNamedLikeATestFileDoesNotSilenceTheTree(t *testing.T) {
	root := t.TempDir()
	cli := filepath.Join(root, "pkg.test.fixtures", "loop-runtime-cli")
	write(t, cli, "src/main.ts", "process.exit(9)\n")

	o := check(t, Config{RepoRoot: root, CLIRoot: cli})

	if o.ScannedFiles != 1 {
		t.Fatalf("a parent directory's NAME excluded the file; ScannedFiles=%d", o.ScannedFiles)
	}
}

func TestTheFileLevelExemptionStillHolds(t *testing.T) {
	// The rule is about what a FILE is, not where its tree lives. Narrowing it to
	// the relative path must not delete the exemption it exists for.
	root := t.TempDir()
	cli := filepath.Join(root, "cli")
	write(t, cli, "src/main.test.ts", "process.exit(9)\n")
	write(t, cli, "src/vendor/node_modules/dep/index.ts", "process.exit(9)\n")
	write(t, cli, "src/main.ts", "process.exit(0)\n")

	o := check(t, Config{RepoRoot: root, CLIRoot: cli})

	if len(o.Violations) != 0 {
		t.Fatalf("an exempt file was reported: %v", o.Violations)
	}
	if o.ScannedFiles != 1 {
		t.Fatalf("exactly the one real source file should be read; got %d", o.ScannedFiles)
	}
}

// The __tests__ exemption had the same defect as the other three rules: it
// matched the ABSOLUTE path, so a checkout sitting under any __tests__
// directory was skipped whole.
func TestAParentDirectoryNamedTestsDoesNotSilenceTheTree(t *testing.T) {
	root := t.TempDir()
	cli := filepath.Join(root, "__tests__", "loop-runtime-cli")
	write(t, cli, "src/main.ts", "process.exit(9)\n")

	o := check(t, Config{RepoRoot: root, CLIRoot: cli})

	if o.ScannedFiles != 1 || len(o.Violations) != 1 {
		t.Fatalf("a parent directory named __tests__ excluded the file; ScannedFiles=%d violations=%v",
			o.ScannedFiles, o.Violations)
	}
}
