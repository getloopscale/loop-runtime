// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// deploymentPath is the loopzero-api Deployment template, relative to this package.
const deploymentPath = "../../helm/loopzero-api/templates/deployment.yaml"

// TestLoopZeroAPI_TodoEnvFromOptionalCarregaRazao closes the second DoD bullet of
// B-104.
//
// # Why this is worth a test
//
// `optional: true` is what makes the kubelet start the container even when the
// Secret or ConfigMap does not exist. It is therefore the exact mechanism that
// lets a dependency vanish without startup saying anything — and it is not
// hypothetical: measured against dev-public on 2026-08-16, SIX of the ten
// envFrom sources did not exist in the cluster while the API was up and serving
// (`loopzero-platform-topology`, `loopzero-broker-token`, `loopzero-api-minio`,
// `loopzero-api-internal`, `loopzero-api-stripe`, `loopzero-api-admin`).
//
// The item claimed no decision distinguished the ones that may be absent from
// the ones that may not. Measured, that was too strong: `loopzero-openbao-approle`
// carries an exemplary reason WITH a fail-loud complement, and the five ESO
// secrets carry a group reason. Two carried none — the wiring ConfigMap and the
// database credentials — and the second is the one that lets the API boot with
// no database at all.
//
// This test asserts the property that survives: an `optional: true` with no
// nearby prose is an exemption nobody decided, and exemptions nobody decided are
// how a deny-list ages toward the unsafe side.
func TestLoopZeroAPI_TodoEnvFromOptionalCarregaRazao(t *testing.T) {
	data, err := os.ReadFile(filepath.Clean(deploymentPath))
	if err != nil {
		t.Fatalf("read %s: %v", deploymentPath, err)
	}
	lines := strings.Split(string(data), "\n")

	inEnvFrom := false
	var undocumented []int
	var total int

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "envFrom:":
			inEnvFrom = true
			continue
		case inEnvFrom && trimmed != "" && !strings.HasPrefix(trimmed, "-") &&
			!strings.HasPrefix(trimmed, "#") && !strings.Contains(trimmed, ":") == false &&
			isBlockExit(trimmed):
			inEnvFrom = false
		}
		if !inEnvFrom || trimmed != "optional: true" {
			continue
		}
		total++
		if !hasReasonAbove(lines, i) {
			undocumented = append(undocumented, i+1)
		}
	}

	if total == 0 {
		t.Fatal("nenhum `optional: true` encontrado no bloco envFrom — ou o template " +
			"mudou de forma, ou este teste parou de o reconhecer. Um teste que não " +
			"mede nada não verifica nada")
	}
	if len(undocumented) > 0 {
		t.Errorf("%d de %d entradas `optional: true` do envFrom não têm razão escrita "+
			"acima (linhas %v). `optional: true` é o mecanismo que permite a dependência "+
			"sumir sem o arranque dizer nada — uma isenção que ninguém decidiu (B-104)",
			len(undocumented), total, undocumented)
	}
}

// TestHasReasonAbove pins the rule the assertion above rests on. Without it the
// helper is an assertion with no cases — widenable until it passes, which is the
// defect B-092 measured one directory over.
func TestHasReasonAbove(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		idx   int
		want  bool
	}{
		{
			name: "comentario imediatamente acima conta",
			lines: []string{"envFrom:", "# esta entrada e opcional porque X, Y e Z",
				"- secretRef:", "name: a", "optional: true"},
			idx: 4, want: true,
		},
		{
			// A regressao que a mutacao encontrou: creditar o comentario de
			// grupo a corrida inteira faz uma entrada acrescentada depois herdar
			// a razao por acidente — o mesmo defeito que o helm-drift tinha ao
			// aprovar por par de charts em vez de por arquivo.
			name: "entrada seguinte NAO herda o comentario da anterior",
			lines: []string{"envFrom:", "# as cinco abaixo vem do ESO e caem no legado",
				"- secretRef:", "name: a", "optional: true",
				"- secretRef:", "name: b", "optional: true"},
			idx: 7, want: false,
		},
		{
			name:  "sem comentario nenhum ate o topo do bloco nao conta",
			lines: []string{"envFrom:", "- secretRef:", "name: a", "optional: true"},
			idx:   3, want: false,
		},
		{
			name:  "rotulo curto nao e razao",
			lines: []string{"envFrom:", "# minio", "- secretRef:", "name: a", "optional: true"},
			idx:   4, want: false,
		},
		{
			// A regressao que este helper ja teve: subindo, a PRIMEIRA linha de
			// comentario encontrada e a ULTIMA do paragrafo, e a ultima linha de
			// um paragrafo costuma ser fragmento. Julgar so' ela reportou uma
			// entrada com sete linhas de justificativa como indocumentada.
			name: "bloco de varias linhas terminando em fragmento conta pelo todo",
			lines: []string{"envFrom:",
				"# esta entrada e opcional porque o bootstrap a cria depois,",
				"# e exigi-la aqui tornaria a ordem circular",
				"# (1:1).",
				"- secretRef:", "name: a", "optional: true"},
			idx: 6, want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasReasonAbove(tc.lines, tc.idx); got != tc.want {
				t.Errorf("hasReasonAbove = %v, esperado %v", got, tc.want)
			}
		})
	}
}

// isBlockExit reports whether a line at the same indent level as `envFrom:`
// closes the block. `resources:` / `livenessProbe:` and their siblings do.
func isBlockExit(trimmed string) bool {
	for _, k := range []string{"resources:", "livenessProbe:", "readinessProbe:",
		"volumeMounts:", "ports:", "securityContext:", "lifecycle:"} {
		if strings.HasPrefix(trimmed, k) {
			return true
		}
	}
	return false
}

// hasReasonAbove looks upward from an `optional: true` for prose explaining it.
//
// The comment has to introduce THIS entry: the walk stops at the previous
// entry's opener, so one entry's reason cannot be credited to the next.
//
// # Why per-entry and not per-group
//
// The first version credited a group comment to the whole contiguous run that
// followed it, reasoning that repeating the ESO block's shared reason five times
// was ceremony. Mutation-testing it showed the cost: a SIXTH entry appended to
// the run inherits the group's reason and is documented by accident.
//
// That is precisely the defect `helm-drift` carried — approving per chart-pair
// approved every present AND future divergence of that pair — and this session
// re-keyed that gate per file for the same reason. Keying per entry here is the
// same correction, and the five ESO reasons that replaced the group comment are
// not copies: each names its own secret and its own consequence, which is the
// information a group comment cannot carry.
//
// Substance is measured over the WHOLE contiguous block, not over the first line
// the walk happens to meet. Walking upward meets the block's LAST line, and a
// paragraph's last line is routinely a fragment — `loopzero-broker-token` carries
// seven lines of rationale ending in `1:1 mapping).`, thirteen characters, and
// judging that line alone reported a well-documented entry as undocumented.
func hasReasonAbove(lines []string, idx int) bool {
	seenOpener := 0
	for i := idx - 1; i >= 0 && idx-i < maxLookback; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "#") {
			return commentBlockSubstance(lines, i) >= minReasonChars
		}
		if strings.HasPrefix(trimmed, "- secretRef:") || strings.HasPrefix(trimmed, "- configMapRef:") {
			seenOpener++
			// The first opener is this entry's own. A second means the walk
			// reached the PREVIOUS entry without meeting a comment, so this
			// entry has none of its own.
			if seenOpener > 1 {
				return false
			}
			continue
		}
		if trimmed == "envFrom:" {
			return false
		}
	}
	return false
}

// commentBlockSubstance sums the prose in the contiguous comment block ending at
// `last`, walking upward.
func commentBlockSubstance(lines []string, last int) int {
	total := 0
	for i := last; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, "#") {
			break
		}
		total += len(strings.TrimSpace(strings.TrimPrefix(trimmed, "#")))
	}
	return total
}

const (
	// maxLookback bounds the walk so a comment far above cannot be credited to
	// an unrelated entry. Sized for the ESO group: five entries of three lines
	// each, plus slack.
	maxLookback = 30
	// minReasonChars is what separates a reason from a label.
	minReasonChars = 20
)
