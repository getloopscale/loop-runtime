// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package shellscript

import (
	"reflect"
	"testing"
)

func TestEffectfulCLI(t *testing.T) {
	for _, body := range []string{"kubectl get pods", "k3d cluster list", "helmfile sync", "terraform apply", "cosign verify x"} {
		if !EffectfulCLI.MatchString(body) {
			t.Errorf("%q must be effectful", body)
		}
	}
	for _, body := range []string{"grep -r x .", "echo kubectlish", "gh pr list"} {
		if EffectfulCLI.MatchString(body) {
			t.Errorf("%q must not be effectful", body)
		}
	}
}

func TestPythonHeredoc(t *testing.T) {
	for _, body := range []string{"python3 - <<PY\nprint(1)\nPY", "python <<'EOF'", "cat <<'PYEOF'", "x <<\"PYTHON\""} {
		if !PythonHeredoc.MatchString(body) {
			t.Errorf("%q must be a Python heredoc", body)
		}
	}
	if PythonHeredoc.MatchString("python3 script.py") {
		t.Error("calling a Python file is not a heredoc")
	}
}

func TestParseAllowlistRow(t *testing.T) {
	cases := []struct {
		line   string
		want   AllowlistRow
		wantOK bool
	}{
		{"| infra/scripts/foo.sh | 145 | KEEP-OPERATOR — live |", AllowlistRow{Path: "infra/scripts/foo.sh", Cells: []string{"145", "KEEP-OPERATOR — live"}}, true},
		{"|infra/scripts/foo.sh|145|reason|", AllowlistRow{Path: "infra/scripts/foo.sh", Cells: []string{"145", "reason"}}, true},
		{"| infra/scripts/foo.sh | 145", AllowlistRow{Path: "infra/scripts/foo.sh", Cells: []string{"145"}}, true},
		{"| <script-path>.sh | <LOC> | <reason> |", AllowlistRow{}, false},
		{"| Script | LOC | Why |", AllowlistRow{}, false},
		{"|---|---|---|", AllowlistRow{}, false},
		{"prose mentioning foo.sh", AllowlistRow{}, false},
		{"| infra/scripts/foo.py | 1 | x |", AllowlistRow{}, false},
	}
	for _, c := range cases {
		got, ok := ParseAllowlistRow(c.line)
		if ok != c.wantOK || !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseAllowlistRow(%q) = (%+v, %v), want (%+v, %v)", c.line, got, ok, c.want, c.wantOK)
		}
	}
}
