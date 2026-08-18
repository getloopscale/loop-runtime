// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package trufflehogscan

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrUnparseable means a line that IS JSON failed to parse.
//
// Pular em silêncio o que não parseia converte PERDA DE DADOS em "não achei nada" — a
// forma exata do defeito que este pacote existe para matar (EC-3). Ruído de log, que
// nem começa com `{`, é outra coisa e segue ignorado.
var ErrUnparseable = errors.New("saída do scanner ilegível — perda de dados não é ausência de achado")

// maxLine — uma linha de achado carrega DetectorDescription e ExtraData; o padrão de
// 64KiB de bufio.Scanner corta EM SILÊNCIO (EC-6).
const maxLine = 1024 * 1024

// Finding is one trufflehog candidate.
type Finding struct {
	Detector string
	File     string
	// Live=true means the providing service confirmed the credential works.
	Live bool

	id string
}

// ID is the stable identity written to the versioned baseline.
//
// É `sha256(detector + "\0" + bruto)` truncado, e NUNCA o bruto: o arquivo é versionado,
// então gravar o valor vazaria o que o gate existe para impedir. O detector entra no hash
// porque o mesmo texto reconhecido por dois detectores são dois candidatos.
func (f Finding) ID() string { return f.id }

// String renders a finding without ever exposing the secret.
func (f Finding) String() string {
	where := f.File
	if where == "" {
		where = "(mensagem de commit)"
	}
	return fmt.Sprintf("%s [%s] %s", where, f.Detector, f.id)
}

// rawFinding mirrors only the fields this gate reads. Everything else in trufflehog's
// JSON — inclusive o bruto — morre neste struct e não alcança nenhum arquivo.
type rawFinding struct {
	DetectorName   string `json:"DetectorName"`
	Raw            string `json:"Raw"`
	Verified       bool   `json:"Verified"`
	SourceMetadata struct {
		Data struct {
			Git struct {
				File string `json:"file"`
			} `json:"Git"`
		} `json:"Data"`
	} `json:"SourceMetadata"`
}

// ParseFindings reads trufflehog's `--json` stream.
func ParseFindings(r io.Reader) ([]Finding, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, maxLine), maxLine)

	var out []Finding
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "{") {
			continue // log noise, not a finding
		}
		var rf rawFinding
		if err := json.Unmarshal([]byte(line), &rf); err != nil {
			return nil, fmt.Errorf("%w: linha %d: %w", ErrUnparseable, n, err)
		}
		if rf.DetectorName == "" {
			continue // JSON válido que não é achado
		}
		out = append(out, Finding{
			Detector: rf.DetectorName,
			File:     rf.SourceMetadata.Data.Git.File,
			Live:     rf.Verified,
			id:       identity(rf.DetectorName, rf.Raw),
		})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnparseable, err)
	}
	return out, nil
}

func identity(detector, raw string) string {
	sum := sha256.Sum256([]byte(detector + "\x00" + raw))
	return hex.EncodeToString(sum[:])[:16]
}
