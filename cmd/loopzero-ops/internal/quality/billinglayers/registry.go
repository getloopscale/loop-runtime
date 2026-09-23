// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package billinglayers

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ErrRegistryMalformed means the registry does not declare what it must.
//
// Um registro que aceita lixo não declara nada. É a mesma razão pela qual a isenção do
// `publishhygiene` exige razão E prazo: sem os dois ela não declara, só cala.
var ErrRegistryMalformed = errors.New("registro de camadas malformado")

// GuardKind is how a layer is protected from degrading in silence.
type GuardKind string

const (
	// GuardGate — um gate do loopzero-ops cobre a camada. Só resolve se o gate for RODADO.
	GuardGate GuardKind = "gate"
	// GuardTest — um teste afirma o invariante.
	GuardTest GuardKind = "test"
	// GuardOpen — dívida declarada, com dono. Resolve enquanto o item estiver ABERTO.
	GuardOpen GuardKind = "open"
)

// Guard is the declared protection of one layer.
type Guard struct {
	Kind   GuardKind
	Target string
}

// Layer is one independent place where missing billing configuration is tolerated.
type Layer struct {
	ID    string
	Repo  string
	Files []string
	Guard Guard
	Why   string
}

// Reference is a file that touches a billing input without being a layer.
type Reference struct {
	File   string
	Reason string
}

// Debt is one declared open item an `open:` guard points at. It lives in the
// versioned registry so the ratchet reads the same answer on every clone; the
// personal BACKLOG.md it used to read is gitignored and absent in CI.
type Debt struct {
	ID     string
	Status string
	Closes string // what delivering the item puts in place of the debt
}

// Registry is the declared state of the billing chain.
type Registry struct {
	// Anchor lives HERE, as data, and not as literals in Go — see the package doc.
	Anchor     []string
	Floor      int
	Layers     []Layer
	References []Reference
	Debts      map[string]Debt
}

// ParseRegistry reads the declared registry.
func ParseRegistry(r io.Reader) (Registry, error) {
	var reg Registry
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 256*1024), 256*1024)

	secao := ""
	pisoLido := false
	vistos := map[string]bool{}
	n := 0
	for sc.Scan() {
		n++
		linha := strings.TrimSpace(sc.Text())
		if linha == "" || strings.HasPrefix(linha, "#") {
			continue
		}
		if strings.HasPrefix(linha, "[") && strings.HasSuffix(linha, "]") {
			secao = strings.ToLower(strings.Trim(linha, "[]"))
			continue
		}

		switch secao {
		case "ancora":
			reg.Anchor = append(reg.Anchor, linha)
		case "piso":
			v, err := strconv.Atoi(linha)
			if err != nil || v < 0 {
				return reg, fmt.Errorf("%w: piso inválido na linha %d: %q", ErrRegistryMalformed, n, linha)
			}
			reg.Floor = v
			pisoLido = true
		case "camadas":
			l, err := parseLayer(linha, n)
			if err != nil {
				return reg, err
			}
			if vistos[l.ID] {
				return reg, fmt.Errorf("%w: camada %q declarada duas vezes (linha %d) — uma esconde a outra e o piso segue satisfeito",
					ErrRegistryMalformed, l.ID, n)
			}
			vistos[l.ID] = true
			reg.Layers = append(reg.Layers, l)
		case "referencias":
			ref, err := parseReference(linha, n)
			if err != nil {
				return reg, err
			}
			reg.References = append(reg.References, ref)
		case "debts":
			d, err := parseDebt(linha, n)
			if err != nil {
				return reg, err
			}
			if reg.Debts == nil {
				reg.Debts = map[string]Debt{}
			}
			if _, dup := reg.Debts[d.ID]; dup {
				return reg, fmt.Errorf("%w: debt %s declared twice (line %d)", ErrRegistryMalformed, d.ID, n)
			}
			reg.Debts[d.ID] = d
		default:
			return reg, fmt.Errorf("%w: linha %d fora de qualquer seção: %q", ErrRegistryMalformed, n, linha)
		}
	}
	if err := sc.Err(); err != nil {
		return reg, fmt.Errorf("%w: %w", ErrRegistryMalformed, err)
	}

	// A âncora ausente não é um registro mais simples — é um registro que não consegue
	// detectar camada nova, que é metade do que ele existe para fazer.
	if len(reg.Anchor) == 0 {
		return reg, fmt.Errorf("%w: seção [ancora] vazia — sem ela, uma camada nova nasce invisível", ErrRegistryMalformed)
	}
	// EC-4: sem piso declarado, apagar linhas faz o gate passar.
	if !pisoLido {
		return reg, fmt.Errorf("%w: seção [piso] ausente — é ela que impede o registro de se auto-silenciar", ErrRegistryMalformed)
	}
	return reg, nil
}

func parseLayer(linha string, n int) (Layer, error) {
	campos := strings.Split(linha, "|")
	if len(campos) != 5 {
		return Layer{}, fmt.Errorf("%w: camada na linha %d tem %d campos, esperados 5 (id | repo | arquivos | guarda | por quê)",
			ErrRegistryMalformed, n, len(campos))
	}
	l := Layer{
		ID:   strings.TrimSpace(campos[0]),
		Repo: strings.TrimSpace(campos[1]),
		Why:  strings.TrimSpace(campos[4]),
	}
	for _, f := range strings.Split(campos[2], ",") {
		if f = strings.TrimSpace(f); f != "" {
			l.Files = append(l.Files, f)
		}
	}
	g, err := parseGuard(campos[3], n)
	if err != nil {
		return Layer{}, err
	}
	l.Guard = g

	if l.ID == "" || l.Repo == "" || len(l.Files) == 0 || l.Why == "" {
		return Layer{}, fmt.Errorf("%w: camada na linha %d tem campo vazio — uma camada sem arquivo ou sem razão não declara nada",
			ErrRegistryMalformed, n)
	}
	return l, nil
}

// parseGuard normaliza espaço e caixa (EC-7): `Gate: X` e `gate:X` são a mesma intenção,
// e recusar por caixa é ruído. Aceitar forma desconhecida é buraco.
func parseGuard(raw string, n int) (Guard, error) {
	tipo, alvo, ok := strings.Cut(strings.TrimSpace(raw), ":")
	if !ok {
		return Guard{}, fmt.Errorf("%w: guarda sem forma na linha %d: %q", ErrRegistryMalformed, n, raw)
	}
	g := Guard{
		Kind:   GuardKind(strings.ToLower(strings.TrimSpace(tipo))),
		Target: strings.TrimSpace(alvo),
	}
	switch g.Kind {
	case GuardGate, GuardTest, GuardOpen:
	default:
		return Guard{}, fmt.Errorf("%w: guarda %q desconhecida na linha %d — as formas aceitas são `gate`, `test` e `open`",
			ErrRegistryMalformed, g.Kind, n)
	}
	if g.Target == "" {
		return Guard{}, fmt.Errorf("%w: guarda sem alvo na linha %d", ErrRegistryMalformed, n)
	}
	return g, nil
}

func parseReference(linha string, n int) (Reference, error) {
	campos := strings.Split(linha, "|")
	if len(campos) != 2 {
		return Reference{}, fmt.Errorf("%w: referência na linha %d tem %d campos, esperados 2 (arquivo | por que não é camada)",
			ErrRegistryMalformed, n, len(campos))
	}
	ref := Reference{File: strings.TrimSpace(campos[0]), Reason: strings.TrimSpace(campos[1])}
	if ref.File == "" || ref.Reason == "" {
		return Reference{}, fmt.Errorf("%w: referência na linha %d sem arquivo ou sem razão — uma referência sem razão só cala",
			ErrRegistryMalformed, n)
	}
	return ref, nil
}

// parseDebt reads `<B-NNN> | <status> | <what closes it>`. The status must be
// one the ratchet can classify; anything else is refused rather than guessed.
func parseDebt(line string, n int) (Debt, error) {
	fields := strings.Split(line, "|")
	if len(fields) != 3 {
		return Debt{}, fmt.Errorf("%w: debt on line %d has %d fields, want 3 (id | status | what closes it)",
			ErrRegistryMalformed, n, len(fields))
	}
	d := Debt{
		ID:     strings.TrimSpace(fields[0]),
		Status: strings.TrimSpace(fields[1]),
		Closes: strings.TrimSpace(fields[2]),
	}
	if d.ID == "" || d.Closes == "" {
		return Debt{}, fmt.Errorf("%w: debt on line %d has an empty id or no closing criterion", ErrRegistryMalformed, n)
	}
	if !openStatuses[d.Status] && !terminalStatuses[d.Status] {
		return Debt{}, fmt.Errorf("%w: debt %s on line %d has status %q; known statuses are raw, triaged, approved, planned, shipped, killed",
			ErrRegistryMalformed, d.ID, n, d.Status)
	}
	return d, nil
}
