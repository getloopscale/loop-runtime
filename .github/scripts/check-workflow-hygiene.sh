#!/usr/bin/env bash
# check-workflow-hygiene.sh — duas propriedades dos workflows deste repo.
#
# This line said "verificadas a cada PR" — checked on every PR. workflow-hygiene.yml has never had a
# pull_request trigger: it runs on PUSH to develop/main, post-merge. Corrected 2026-09-06 (B-192). A
# sentence promising a moment of verification that does not exist is the defect family that item measured.
#
# POR QUE EXISTE. Os dois defeitos abaixo ja foram corrigidos A MAO uma vez (#233 e #250) e nada
# impedia a reintroducao. Este repo hospeda `build-publish.yml`, o reusable chamado por CINCO repos
# consumidores — um `uses: foo@v4` ou um `${{ }}` dentro de `run:` aqui nao fica aqui: propaga para
# todos eles no proximo build. Corrigir sem guardar e trocar um defeito por um intervalo ate ele voltar.
#
# G1 — toda `uses:` de acao de terceiro fixada por SHA de 40 caracteres.
#      Uma tag e um ponteiro MOVEL: quem controla o repo da acao pode reapontar `v4` para outro
#      commit depois da revisao, e o build seguinte executa codigo que ninguem leu. `tj-actions/
#      changed-files` (marco/2025) foi exatamente isso, em ~23 mil repos. Um SHA e imutavel.
#      `./.github/...` (reusable local) fica de fora: nao ha terceiro, e o conteudo vem do mesmo commit.
#
# G2 — nenhum `${{ ... }}` dentro de um bloco `run:`.
#      `${{ }}` e substituido NO TEXTO do script antes do shell existir. Um valor com aspas, `;` ou
#      `$(...)` deixa de ser argumento e vira comando, com acesso aos segredos do job. A forma segura
#      e passar pelo `env:` do step e usar `"${VAR}"` — ai o valor chega como variavel de ambiente e
#      nenhum conteudo dele consegue virar sintaxe.
#
#      O caso mais perigoso aqui e o `workflow_call`: um input e, por definicao, controlado pelo
#      CALLER, e o conjunto de callers muda sem que este arquivo mude.
#
# LIMITE HONESTO: G2 delimita o bloco `run:` por INDENTACAO, nao por um parser YAML. Um `run:` com
# indentacao exotica escaparia. E uma aproximacao textual barata que cobre a forma que este repo usa;
# a alternativa fiel exigiria uma dependencia so para isto.
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.." || exit 1

fail=0
note() { fail=1; printf '  %s\n' "$*" >&2; }

# ---- G1: acoes de terceiro fixadas por SHA -------------------------------------------------------
while IFS= read -r hit; do
  ref="${hit#*uses:}"; ref="${ref%%#*}"; ref="$(printf '%s' "${ref}" | tr -d ' \t')"
  case "${ref}" in
    ./*) continue ;;                                    # reusable local — mesmo commit, sem terceiro
  esac
  sha="${ref##*@}"
  if ! printf '%s' "${sha}" | grep -qE '^[0-9a-f]{40}$'; then
    [ "${fail}" -eq 0 ] && echo "workflow-hygiene: acao de terceiro NAO fixada por SHA (G1, #233):" >&2
    note "${hit}"
  fi
done < <(grep -rnE '^\s*-?\s*uses:' .github/workflows/ 2>/dev/null)

# ---- G2: nenhuma interpolacao dentro de `run:` ---------------------------------------------------
g2=$(python3 - <<'PY'
import glob, re
hits = []
for path in sorted(glob.glob('.github/workflows/*.yml') + glob.glob('.github/workflows/*.yaml')):
    in_run, run_indent = False, 0
    for lineno, line in enumerate(open(path), 1):
        line = line.rstrip('\n')
        stripped = line.strip()
        if re.match(r'^-?\s*run:', stripped):
            in_run, run_indent = True, len(line) - len(line.lstrip())
            if '${{' in line:
                hits.append(f'{path}:{lineno}: {stripped[:110]}')
            continue
        if in_run:
            if stripped and (len(line) - len(line.lstrip())) <= run_indent:
                in_run = False
                continue
            if '${{' in line:
                hits.append(f'{path}:{lineno}: {stripped[:110]}')
print('\n'.join(hits))
PY
)
if [ -n "${g2}" ]; then
  [ "${fail}" -eq 0 ] && echo >&2
  echo "workflow-hygiene: \${{ }} interpolado dentro de \`run:\` (G2, #250):" >&2
  printf '%s\n' "${g2}" | sed 's/^/  /' >&2
  fail=1
fi

if [ "${fail}" -ne 0 ]; then
  cat >&2 <<'MSG'

Como corrigir:
  G1  troque a tag pelo SHA de 40 caracteres do commit revisado, com a versao em comentario:
        uses: actions/checkout@9c091bb...  # v7.0.0
  G2  mova o valor para o `env:` do step e use a variavel entre aspas:
        env: { IMAGE_REF: "${{ inputs.registry }}/${{ inputs.image }}" }
        run: cosign sign --yes "${IMAGE_REF}"
MSG
  exit 1
fi
echo "workflow-hygiene: acoes fixadas por SHA e nenhum \${{ }} dentro de run: — ok."
