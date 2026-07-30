// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"regexp"
	"strings"
)

// metricCheckSetFor devolve o conjunto de valores admitidos pelo CHECK EFETIVO de
// `<table>.metric` — o ultimo CHECK que sobra depois de aplicar `sqls` na ordem dada.
//
// POR QUE ISTO E PARAMETRIZADO POR TABELA (#241). A versao anterior varria TODAS as migrations
// atras de `metric ... IN (...)` e ficava com a ultima ocorrencia, sem olhar a QUAL TABELA ela
// pertencia. Quando `lens_alert_rules` e `lens_fired_alerts` ganharam sua propria coluna `metric`
// (000054/000056/000058/000059), a "ultima ocorrencia" passou a ser a do lens — e o guard de drift
// do `cost_counters` passou a comparar o vocabulario de custo com o de observabilidade. Reprovava
// dizendo que `cpu_core_seconds` faltava no CHECK (esta la) e que `p95_latency` sobrava (nunca
// esteve, e de outra tabela).
//
// O guard nao estava detectando drift: estava lendo a tabela errada. E um teste vermelho que ninguem
// consegue deixar verde ensina o time a ignorar vermelho — `testing.md`: "once red tests are ignored,
// all tests lose value".
//
// LIMITE HONESTO: isto e um parser TEXTUAL, nao SQL. Ele cobre as duas formas que estas migrations
// usam — a coluna inline no `CREATE TABLE` e o par `DROP CONSTRAINT` / `ADD CONSTRAINT` — e ignora
// deliberadamente `UPDATE ... WHERE metric IN (...)` (existe em 000032 e nao e restricao alguma).
// Um CHECK escrito de terceira forma (funcao, dominio, trigger) escaparia. A alternativa fiel exigiria
// subir um Postgres e ler `pg_constraint`, o que este teste de unidade nao quer pagar.
func metricCheckSetFor(table string, sqls []string) map[string]bool {
	var last string
	for _, sql := range sqls {
		stripped := stripSQLComments(sql)
		if v, ok := lastMetricCheckInFile(table, stripped); ok {
			last = v
		}
	}
	if last == "" {
		return nil
	}
	set := map[string]bool{}
	for _, m := range reQuotedValue.FindAllStringSubmatch(last, -1) {
		set[m[1]] = true
	}
	return set
}

var (
	// `-- comentario` some antes de qualquer casamento: varias destas migrations descrevem em prosa
	// o CHECK que estao prestes a trocar, e uma dessas frases citada literalmente viraria a resposta.
	reLineComment = regexp.MustCompile(`(?m)--.*$`)
	reQuotedValue = regexp.MustCompile(`'([^']+)'`)
	// A lista de valores de um CHECK sobre a coluna `metric`. Ancorado em CHECK de proposito: um
	// `UPDATE ... WHERE metric IN (...)` nao casa, e nao deve — nao restringe nada.
	reMetricCheck = regexp.MustCompile(`(?is)CHECK\s*\(\s*metric\s+IN\s*\(([^)]*)\)`)
)

func stripSQLComments(sql string) string { return reLineComment.ReplaceAllString(sql, "") }

// lastMetricCheckInFile devolve a lista do ultimo CHECK de `<table>.metric` no arquivo, se houver.
//
// O recorte e o COMANDO (fatia entre `;`), e ele so conta quando o comando e um `CREATE TABLE
// <table>` ou um `ALTER TABLE <table>`. E o que separa as duas formas que estas migrations usam
//
//	CREATE TABLE cost_counters ( ... metric TEXT NOT NULL CHECK (metric IN (...)) ... );
//	ALTER TABLE  cost_counters ADD CONSTRAINT ... CHECK (metric IN (...));
//
// de uma citacao da tabela que nao restringe nada — uma FK para ela, um indice, um UPDATE.
//
// Fatiar por `;` fora de um lexer erraria se houvesse `;` dentro de literal; nao ha nestas
// migrations, e a alternativa e um parser SQL inteiro para um teste de unidade.
func lastMetricCheckInFile(table, sql string) (string, bool) {
	var last string
	found := false

	for _, stmt := range strings.Split(sql, ";") {
		lower := strings.ToLower(stmt)
		if !strings.Contains(lower, strings.ToLower(table)) {
			continue
		}
		isAlter := reAlterOf(table).MatchString(stmt)
		isCreate := reCreateOf(table).MatchString(stmt)
		if !isAlter && !isCreate {
			// A tabela e citada (FK, comentario ja removido, indice) mas o comando nao e dela.
			continue
		}
		for _, m := range reMetricCheck.FindAllStringSubmatch(stmt, -1) {
			last, found = m[1], true
		}
	}
	return last, found
}

func reAlterOf(table string) *regexp.Regexp {
	return regexp.MustCompile(`(?is)ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?"?` + regexp.QuoteMeta(table) + `"?\b`)
}

func reCreateOf(table string) *regexp.Regexp {
	return regexp.MustCompile(`(?is)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?"?` + regexp.QuoteMeta(table) + `"?\b`)
}
