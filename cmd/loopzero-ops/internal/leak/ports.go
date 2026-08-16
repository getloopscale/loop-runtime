// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package leak

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// A superfície de rede do host é declarada, e fechada por padrão.
//
// MEDIDO em 2026-08-10, da internet e não do droplet: 39 portas escutavam em 0.0.0.0, o
// firewall bloqueava 20, e das 19 restantes apenas 2 tinham justificativa. As outras 17
// não estavam em lista nenhuma — entre elas o ArgoCD, o serverlb do cluster de segredos e
// a identidade servida SEM TLS na porta crua, enquanto a borda já a publica em HTTPS.
//
// A causa não foi descuido. A lista de bloqueio do firewall foi escrita quando os
// clusters se chamavam mgmt/workload/runtime-2/vault — os comentários dela ainda citam
// esses nomes — e tudo que subiu depois nasceu fora dela. Uma lista de NEGAÇÃO envelhece
// para o lado inseguro: o que ninguém lembrou de bloquear fica aberto. Uma lista de
// PERMISSÃO envelhece para o lado seguro, e é a única polaridade aceitável aqui.

// PortClass é o que decide se a porta pode ser alcançada da internet.
type PortClass string

const (
	// ClassExternal é a ÚNICA classe que permanece alcançável de fora, e exige o
	// consumidor nomeado com evidência de medição.
	ClassExternal PortClass = "external"
	ClassEdge     PortClass = "edge"     // a borda publica em HTTPS; a porta crua fecha
	ClassHost     PortClass = "host"     // consumido pelo próprio host
	ClassAdmin    PortClass = "admin"    // painel ou API de infraestrutura
	ClassPendente PortClass = "pendente" // registrada, consumidor ainda não medido
)

// PortDecl é uma linha do registro.
type PortDecl struct {
	Port   int
	Class  PortClass
	Reason string
	Line   int
}

// PortFinding é uma porta em desacordo com o registro.
type PortFinding struct {
	Port int
	Why  string
}

// ParsePorts lê o registro declarado.
//
// Uma linha malformada é ERRO e não é ignorada: num registro de segurança, a linha que o
// parser pula em silêncio é exatamente a porta que ninguém revisou.
func ParsePorts(caminho string) (map[int]PortDecl, error) {
	f, err := os.Open(caminho) // #nosec G304 -- caminho vem do operador, não de rede
	if err != nil {
		return nil, fmt.Errorf("ler registro de portas %s: %w", caminho, err)
	}
	defer func() { _ = f.Close() }()

	decls := map[int]PortDecl{}
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
		linha := strings.TrimSpace(sc.Text())
		if linha == "" || strings.HasPrefix(linha, "#") {
			continue
		}
		campos := strings.SplitN(linha, "|", 3)
		if len(campos) != 3 {
			return nil, fmt.Errorf("%s:%d: linha malformada, esperado `porta | classe | razão`: %q",
				caminho, n, linha)
		}
		porta, err := strconv.Atoi(strings.TrimSpace(campos[0]))
		if err != nil {
			return nil, fmt.Errorf("%s:%d: porta inválida %q", caminho, n, strings.TrimSpace(campos[0]))
		}
		classe := PortClass(strings.TrimSpace(campos[1]))
		switch classe {
		case ClassExternal, ClassEdge, ClassHost, ClassAdmin, ClassPendente:
		default:
			return nil, fmt.Errorf("%s:%d: classe desconhecida %q (use external|edge|host|admin|pendente)",
				caminho, n, classe)
		}
		razao := strings.TrimSpace(campos[2])
		if razao == "" {
			return nil, fmt.Errorf("%s:%d: porta %d sem razão declarada — quem consome, e como foi medido",
				caminho, n, porta)
		}
		if anterior, dup := decls[porta]; dup {
			return nil, fmt.Errorf("%s:%d: porta %d declarada duas vezes (antes na linha %d)",
				caminho, n, porta, anterior.Line)
		}
		decls[porta] = PortDecl{Port: porta, Class: classe, Reason: razao, Line: n}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("ler %s: %w", caminho, err)
	}
	return decls, nil
}

// AuditPorts compara o que escuta com o que foi declarado.
//
// Devolve duas listas, e a distinção importa para quem lê o relatório:
//   - naoDeclaradas: escutam e não estão no registro. É o modo de falha que este
//     verificador existe para impedir — uma porta nova alcançável sem ninguém decidir.
//   - obsoletas: declaradas e não escutam mais. Não é risco, mas um registro que acumula
//     entradas mortas deixa de ser revisável, e foi assim que a lista anterior degradou.
func AuditPorts(escutando []int, decls map[int]PortDecl) (naoDeclaradas, obsoletas []PortFinding) {
	viva := map[int]bool{}
	for _, p := range escutando {
		viva[p] = true
		if _, ok := decls[p]; !ok {
			naoDeclaradas = append(naoDeclaradas, PortFinding{
				Port: p,
				Why: "escuta em 0.0.0.0 e não está declarada — o registro é fechado por padrão, " +
					"então uma porta fora dele é decisão que ninguém tomou",
			})
		}
	}
	for p, d := range decls {
		if !viva[p] {
			obsoletas = append(obsoletas, PortFinding{
				Port: p,
				Why:  fmt.Sprintf("declarada como %s e não escuta mais — entrada morta torna o registro menos revisável", d.Class),
			})
		}
	}
	sort.Slice(naoDeclaradas, func(i, j int) bool { return naoDeclaradas[i].Port < naoDeclaradas[j].Port })
	sort.Slice(obsoletas, func(i, j int) bool { return obsoletas[i].Port < obsoletas[j].Port })
	return naoDeclaradas, obsoletas
}

// PortsToClose devolve as portas que escutam e cuja classe NÃO admite alcance externo.
//
// `pendente` fica de fora de propósito: fechar sem medir o consumidor é como as duas
// portas de CI foram derrubadas em 2026-07-30, quebrando os gates que detectam deploy.
// Uma pendência explícita é honesta; um fechamento cego troca exposição por cegueira.
func PortsToClose(escutando []int, decls map[int]PortDecl) []PortDecl {
	var fechar []PortDecl
	for _, p := range escutando {
		d, ok := decls[p]
		if !ok {
			continue // não declarada — reportada por AuditPorts, não fechada às cegas
		}
		if d.Class == ClassExternal || d.Class == ClassPendente {
			continue
		}
		fechar = append(fechar, d)
	}
	sort.Slice(fechar, func(i, j int) bool { return fechar[i].Port < fechar[j].Port })
	return fechar
}
