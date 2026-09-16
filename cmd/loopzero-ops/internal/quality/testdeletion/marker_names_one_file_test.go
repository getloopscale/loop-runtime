// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package testdeletion

import (
	"os"
	"testing"
)

// B-058 RED — a marker binds to the RANGE, not to the file it names.
//
// The justification below names exactly one file and explains exactly one
// deletion. A second, unrelated test file is deleted in the same range. The
// gate must not approve the second on the strength of a reason written for
// the first.
//
// Current behaviour: Check() reports Pass, because line 97 asks only whether
// the marker appears ANYWHERE in the concatenated commit log.
// RED POR DESENHO ate' B-058 ser implementado.
//
// Mede o defeito: um marcador que NOMEIA `api/foo_test.go` aprova a delecao de
// `api/unrelated_test.go`. O gate le a existencia do marcador e nao o alvo que ele
// nomeia, entao uma aprovacao para um arquivo vale para qualquer outro no mesmo commit.
//
// SKIP com o item e a medicao na mensagem, pelo mesmo argumento de B-001, B-048 e B-074:
// vermelho permanente e' ignorado numa semana e leva a suite junto (`rules/testing.md`).
// `LOOPZERO_RUN_B058_RED=1` o arma; armado, ainda falha.
//
// O arquivo chamava-se `grain_red_test.go` — renomeado porque `_red_` e' um ESTADO e nao
// uma identidade, e um arquivo assim continua chamado `red` depois de ficar verde.
func skipUnlessRedB058(t *testing.T) {
	t.Helper()
	if os.Getenv("LOOPZERO_RUN_B058_RED") == "" {
		t.Skip("DEBITO CONHECIDO B-058: o gate de delecao de testes le se existe marcador, " +
			"nao QUAL arquivo ele nomeia — entao uma aprovacao para um teste aprova a delecao " +
			"de qualquer outro no mesmo commit. Rode com LOOPZERO_RUN_B058_RED=1.")
	}
}

func TestMarkerNamingFileADoesNotApproveFileB(t *testing.T) {
	skipUnlessRedB058(t)
	diff := "api/foo_test.go\napi/unrelated_test.go\n"
	o := Check(Config{
		DiffOverride: &diff,
		CommitLog: "chore: drop the foo suite\n\n" +
			"Test deletion justification: api/foo_test.go covered a helper that\n" +
			"was removed in this same slice.\n",
	})
	if o.Pass {
		t.Errorf("a marker naming api/foo_test.go must not approve the deletion of "+
			"api/unrelated_test.go; got Pass with reason %q over deletions %v",
			o.Reason, o.DeletedTests)
	}
}

// B-058 RED — the PR-body channel has the same grain.
func TestPRBodyMarkerNamingFileADoesNotApproveFileB(t *testing.T) {
	skipUnlessRedB058(t)
	diff := "api/foo_test.go\napi/unrelated_test.go\n"
	o := Check(Config{
		DiffOverride: &diff,
		PRBody:       "Test deletion justification: api/foo_test.go is superseded.\n",
	})
	if o.Pass {
		t.Errorf("a PR-body marker naming api/foo_test.go must not approve "+
			"api/unrelated_test.go; got Pass with reason %q", o.Reason)
	}
}

// B-058 RED — a marker in a commit that deletes nothing approves every
// deletion made by every other commit in the range. This is the shape
// measured live on 2026-09-12: neither of the two markers in
// origin/main...workspace deletes a test file, and all 12 deletions passed.
func TestMarkerInACommitThatDeletesNothingApprovesNothing(t *testing.T) {
	skipUnlessRedB058(t)
	diff := "api/foo_test.go\n"
	o := Check(Config{
		DiffOverride: &diff,
		CommitLog: "refactor: move a helper\n\n(no deletions here)\n" +
			"\x00\x00" + // separator stand-in; %B concatenates bodies
			"chore: justify a deletion from another session\n\n" +
			"Test deletion justification: something else entirely.\n",
	})
	if o.Pass {
		t.Errorf("a marker in a commit that deletes no test file must not approve "+
			"a deletion made elsewhere in the range; got Pass with reason %q", o.Reason)
	}
}
