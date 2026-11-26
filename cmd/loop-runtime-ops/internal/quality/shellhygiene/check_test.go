// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package shellhygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestCheck_HasDirective_Pass(t *testing.T) {
	root := t.TempDir()
	write(t, root, "infra/scripts/a.sh", "#!/usr/bin/env bash\nset -euo pipefail\necho hi\n")
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass {
		t.Fatalf("script with set -euo pipefail must PASS: %+v", out)
	}
}

func TestCheck_MissingDirective_Fails(t *testing.T) {
	root := t.TempDir()
	write(t, root, "infra/scripts/a.sh", "#!/usr/bin/env bash\necho hi\n")
	out, _ := Check(Config{RepoRoot: root})
	if out.Pass || len(out.Violations) != 1 {
		t.Fatalf("script without directive must FAIL: %+v", out)
	}
	if !strings.Contains(out.Violations[0], "a.sh") {
		t.Fatalf("violation must name the file: %v", out.Violations)
	}
}

func TestCheck_LibExempt(t *testing.T) {
	root := t.TempDir()
	write(t, root, "infra/scripts/lib/helper.sh", "#!/usr/bin/env bash\necho sourced lib, no set -e\n")
	out, _ := Check(Config{RepoRoot: root})
	if !out.Pass {
		t.Fatalf("**/lib/*.sh must be exempt (set -e propagates to callers): %+v", out)
	}
}

func TestCheck_AllowedVariants_Pass(t *testing.T) {
	root := t.TempDir()
	write(t, root, "infra/scripts/a.sh", "#!/usr/bin/env bash\nset -uo pipefail\n")
	write(t, root, "infra/scripts/b.sh", "#!/usr/bin/env bash\nset -eo pipefail\n")
	out, _ := Check(Config{RepoRoot: root})
	if !out.Pass {
		t.Fatalf("set -uo / set -eo pipefail variants must PASS: %+v", out)
	}
}

func TestCheck_DirectiveBeyondWindow_Fails(t *testing.T) {
	root := t.TempDir()
	var b strings.Builder
	b.WriteString("#!/usr/bin/env bash\n")
	for i := 0; i < 61; i++ {
		b.WriteString("# filler\n")
	}
	b.WriteString("set -euo pipefail\n")
	write(t, root, "infra/scripts/a.sh", b.String())
	out, _ := Check(Config{RepoRoot: root})
	if out.Pass {
		t.Fatalf("directive beyond top-60 lines must FAIL: %+v", out)
	}
}

func TestCheck_ScansAllConfiguredDirs(t *testing.T) {
	root := t.TempDir()
	write(t, root, "infra/tests/x.sh", "#!/usr/bin/env bash\necho no directive\n")
	out, _ := Check(Config{RepoRoot: root})
	if out.Pass {
		t.Fatalf("infra/tests/*.sh missing directive must FAIL: %+v", out)
	}
}
