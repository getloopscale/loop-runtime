// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package shellgen

import (
	"strings"
	"testing"
)

func TestScriptBuilderQuotesCdAndEchoArguments(t *testing.T) {
	script := New().
		SetE().
		Cd("/workspace").
		Echo("Building...").
		String()

	want := "set -euo pipefail\ncd '/workspace'\necho 'Building...'"
	if script != want {
		t.Errorf("script mismatch:\ngot:  %q\nwant: %q", script, want)
	}
}

func TestScriptBuilderKeepsRawLinesVerbatim(t *testing.T) {
	script := New().Raw("while true; do sleep 1; done").Blank().String()
	if script != "while true; do sleep 1; done\n" {
		t.Errorf("unexpected script: %q", script)
	}
}

func TestScriptBuilderQuotesAnInjectionAttemptInCd(t *testing.T) {
	const attempt = "$(whoami); echo pwned"
	script := New().Cd(attempt).String()

	if script != "cd '"+attempt+"'" {
		t.Errorf("command substitution not quoted: %s", script)
	}
	if strings.Count(script, "'") != 2 {
		t.Errorf("expected exactly one quoted argument: %s", script)
	}
}

func TestScriptBuilderQuotesEnvValuesAndMkdirPaths(t *testing.T) {
	script := New().Env("NODE_ENV", "production").MkdirP("/out dir").Comment("done").String()

	want := "export NODE_ENV='production'\nmkdir -p '/out dir'\n# done"
	if script != want {
		t.Errorf("script mismatch:\ngot:  %q\nwant: %q", script, want)
	}
}
