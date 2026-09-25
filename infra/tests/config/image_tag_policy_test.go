// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package config

import (
	"regexp"
	"strings"
	"testing"
)

// ciSentinel is the immutable placeholder `charts/loopzero-api/values-bootstrap.yaml`
// ships as its default tag, decided under issue #232. It is schema-valid (the
// chart's own `values.schema.json` requires minLength 1), it renders, and if it
// ever reaches a cluster the pod fails with `ImagePullBackOff` naming it — which
// says exactly what went wrong: somebody forgot the override.
const ciSentinel = "SET-BY-CI"

var (
	// Anchored on the VALUE. The previous form matched `^\s*tag:\s*…` against
	// whole-file text, which cannot be unit-tested one shape at a time and
	// silently accepted a match anywhere in the file.
	tagSemverRE = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[a-z0-9.-]+)?$`)
	tagSHARE    = regexp.MustCompile(`^(sha-)?[a-f0-9]{8,40}$`)
	tagBranchRE = regexp.MustCompile(`^(chg|feat|fix|hotfix|achado)[a-z0-9-]*$`)
)

// imageTagVerdict decides whether a chart's default image tag is admissible
// under ADR-0020 D1, as amended by issue #232.
//
// It returns the reason on refusal because a gate that only says "no" teaches
// the reader to widen it. The reason names WHICH rule caught the value, so the
// next person can argue with that rule instead of deleting the check.
//
// Order matters: branch names and `latest` are refused BEFORE the accepting
// shapes are tried, so a value that is both plausible and banned is reported by
// the rule that bans it rather than by a generic "no shape matched".
func imageTagVerdict(tag string) (bool, string) {
	switch {
	case tag == "":
		return false, "tag vazia — o `values.schema.json` do próprio chart exige minLength 1, " +
			"e um default que o chart rejeita quebra `helm template` para toda ferramenta " +
			"que renderiza com defaults puros (issue #232)"
	case tagBranchRE.MatchString(tag):
		return false, "aponta para um nome de branch, que é mutável — banido pelo ADR-0020 D1"
	case tag == "latest":
		return false, "`latest` foi avaliado e rejeitado na issue #232: dá a falha mais " +
			"silenciosa possível — um deploy não-determinístico, ou uma rejeição de VAP " +
			"apontando para a admissão em vez de para o override que faltou. A forma " +
			"decidida é a sentinela `" + ciSentinel + "`"
	case tag == ciSentinel:
		return true, ""
	case tagSemverRE.MatchString(tag), tagSHARE.MatchString(tag):
		return true, ""
	}
	return false, "não corresponde a nenhuma forma aceita (semver, sha-*, ou a sentinela `" +
		ciSentinel + "`) — ADR-0020 D1 + issue #232"
}

// TestImageTagVerdict is the table that makes the tag contract of ADR-0020 D1
// reviewable — and that corrects it.
//
// # What the old assertion said, and why it was wrong
//
// `TestB31ImageTagsPinned` accepted `semver | sha-* | latest` and rejected
// branch names. The real value in `charts/loopzero-api/values-bootstrap.yaml` is the
// sentinel `SET-BY-CI`, chosen under issue #232 with the reasoning written into
// the file itself (`values.yaml:17-33`): three candidates were weighed, and
// `latest` was rejected BY NAME because it produced the quietest failure — a
// non-deterministic deploy, or a VAP rejection pointing at admission instead of
// at the override that was forgotten.
//
// So the assertion permitted what the decision rejected, and rejected what the
// decision chose. It froze a superseded reading of ADR-0020 D1 — and nothing
// caught it, because the package it lives in was wired into no task target
// (B-092). An un-run test does not merely fail to protect: it preserves an
// expired contract, and would block the correct design on the day it was wired.
//
// # Why a table instead of a wider regex
//
// A regex inside an `if` is an assertion with no cases: it cannot be reviewed,
// only widened until it passes. Each row below states one accepted or refused
// shape WITH its reason, so the next change to this contract is a line someone
// can argue with.
func TestImageTagVerdict(t *testing.T) {
	cases := []struct {
		name       string
		tag        string
		wantOK     bool
		wantReason string // substring esperado quando recusado
	}{
		{name: "semver simples", tag: "v1.2.3", wantOK: true},
		{name: "semver com pre-release", tag: "v1.2.3-rc.1", wantOK: true},
		{name: "sha com prefixo", tag: "sha-a1b2c3d4", wantOK: true},
		{name: "sha nu de 40", tag: "a1b2c3d4e5f6a7b8c9d0a1b2c3d4e5f6a7b8c9d0", wantOK: true},
		{
			// A linha que corrige o contrato. Ver values.yaml:17-33 + issue #232.
			name: "sentinela SET-BY-CI e a forma decidida na issue #232",
			tag:  "SET-BY-CI", wantOK: true,
		},
		{
			// A outra metade da correcao: o que era aceito passa a ser recusado.
			name: "latest e recusado pela razao registrada na #232, nao por gosto",
			tag:  "latest", wantOK: false, wantReason: "#232",
		},
		{name: "nome de branch feat", tag: "feat-multi-aud", wantOK: false, wantReason: "branch"},
		{name: "nome de branch hotfix", tag: "hotfix-abc", wantOK: false, wantReason: "branch"},
		{
			// O schema do proprio chart exige minLength 1; uma tag vazia quebra
			// `helm template` para toda ferramenta que renderiza com defaults.
			name: "tag vazia e recusada — o schema do chart a rejeita",
			tag:  "", wantOK: false, wantReason: "vazia",
		},
		{name: "texto arbitrario nao e nenhuma das formas aceitas", tag: "multi-aud-fix", wantOK: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, reason := imageTagVerdict(tc.tag)
			if ok != tc.wantOK {
				t.Fatalf("imageTagVerdict(%q) = %v (%q), esperado %v", tc.tag, ok, reason, tc.wantOK)
			}
			if ok {
				return
			}
			if reason == "" {
				t.Fatal("recusa sem razao: quem le a falha precisa saber qual regra pegou")
			}
			if tc.wantReason != "" && !strings.Contains(reason, tc.wantReason) {
				t.Errorf("razao da recusa = %q, esperado conter %q", reason, tc.wantReason)
			}
		})
	}
}
