FROM golang:1.26.1-alpine3.22@sha256:07e91d24f6330432729082bb580983181809e0a48f0f38ecde26868d4568c6ac AS builder
WORKDIR /workspace

ENV GOWORK=off
ENV GOPRIVATE=github.com/getloopscale/*,github.com/getloopscale/*

# loopzero-contracts is a PINNED published module (M5 version-pin cutover — api/go.mod
# requires github.com/getloopscale/loopzero-contracts v0.4.0, NO replace). The private module
#
# CORRIGIDO 2026-08-08: este arquivo autenticava para `getloopscale/` enquanto api/go.mod
# passou a pedir `getloopscale/` no commit f813b367 (2026-08-06). A reescrita de URL nao casava
# com o modulo pedido, entao o git buscava loopzero-contracts SEM autenticacao — e o GitHub
# responde a repo privado sem auth com "not found", a mesma mensagem de repo inexistente.
# O build da imagem loopzero-api quebrou no primeiro push para develop apos a migracao. As duas
# orgs ficam cobertas enquanto o redirect existir.
# is fetched via a short-lived token mounted as a buildx secret (id=gomod_token); the
# token is NEVER written to an image layer. git is required for the module VCS fetch.
RUN apk add --no-cache git openssh-client

COPY pkg/go.mod ./pkg/go.mod
COPY operators/api/go.mod operators/api/go.sum ./operators/api/
COPY operators/api/v1 ./operators/api/v1
COPY api/go.mod api/go.sum ./api/

# `awk 1` e nao `cp` ao gravar a chave: OpenSSH exige newline FINAL no PEM, e o
# transporte por `secret-envs` (variavel de ambiente) o remove. Sem esse unico
# byte a chave chega sintaticamente invalida e o erro e
# `Load key: error in libcrypto` — que soa como chave corrompida, nao como um
# terminador faltando. Reproduzido localmente: `printf %s "$(cat chave)"` produz
# exatamente esse erro. `awk 1` reemite cada linha com terminador, normalizando
# para um unico newline final seja qual for a forma de chegada.
RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=secret,id=gomod_token \
    --mount=type=secret,id=gomod_ssh_key \
    sh -c 'if [ -s /run/secrets/gomod_ssh_key ]; then \
      mkdir -p /root/.ssh && chmod 700 /root/.ssh; \
      awk 1 /run/secrets/gomod_ssh_key > /root/.ssh/gomod_key && chmod 600 /root/.ssh/gomod_key; \
      ssh-keyscan -t ed25519 github.com >> /root/.ssh/known_hosts 2>/dev/null; \
      printf "Host github.com\n  IdentityFile /root/.ssh/gomod_key\n  IdentitiesOnly yes\n  IdentityAgent none\n" > /root/.ssh/config; \
      git config --global url."git@github.com:getloopscale/".insteadOf "https://github.com/getloopscale/"; \
      git config --global url."git@github.com:getloopscale/".insteadOf "https://github.com/getloopscale/"; \
    elif [ -s /run/secrets/gomod_token ]; then \
      git config --global url."https://x-access-token:$(cat /run/secrets/gomod_token)@github.com/getloopscale/".insteadOf "https://github.com/getloopscale/"; \
      git config --global url."https://x-access-token:$(cat /run/secrets/gomod_token)@github.com/getloopscale/".insteadOf "https://github.com/getloopscale/"; \
    fi' \
    && cd api && { go mod download || { \
      echo "" >&2; \
      echo "FATAL: go mod download falhou." >&2; \
      if [ ! -s /run/secrets/gomod_token ]; then \
        echo "  CAUSA PROVAVEL: o secret gomod_token chegou VAZIO, entao a reescrita de URL acima foi" >&2; \
        echo "  PULADA (o if e silencioso por desenho) e o git buscou loopzero-contracts SEM autenticacao." >&2; \
        echo "  O GitHub responde a repo PRIVADO sem auth com Repository-not-found — a MESMA mensagem" >&2; \
        echo "  de repo inexistente. Por isso o erro manda procurar um repositorio que EXISTE, e este" >&2; \
        echo "  build falhou 10 vezes seguidas sem a causa aparecer." >&2; \
        echo "  ACAO: GOMOD_TOKEN precisa de um PAT com Contents:read em getloopscale/loopzero-contracts" >&2; \
        echo "  (o modulo NAO migrou junto com este repositorio)." >&2; \
      else \
        echo "  O token esta presente e nao-vazio, entao NAO e ausencia de credencial." >&2; \
        echo "  Verifique Contents:read em getloopscale/loopzero-contracts e se o PAT nao expirou." >&2; \
      fi; \
      exit 1; \
    }; }

COPY pkg/ ./pkg/
COPY api/ ./api/

# Proveniencia do binario. Sem estas tres o endpoint de versao reporta os
# defaults declarados no codigo (`dev` / `unknown`) — honesto, mas inutil: o
# painel /status mostrava "dev" como versao de producao, que nao demonstra versao
# alguma. O CI as passa via `build_args` do reusable build-publish.
#
# Defaults preservados de proposito: um build local sem args continua compilando,
# e o endpoint reporta `unknown` em vez de um sha plausivel-porem-falso, que um
# verificador externo leria como "em dia".
ARG LOOPZERO_VERSION=dev
ARG LOOPZERO_COMMIT=unknown
ARG LOOPZERO_BUILD_DATE=unknown

RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    # B-053 — o caminho e copiado de `api/internal/version/versionhttp/version.go`, que
    # e a referencia unica e se MOVE JUNTO com o codigo. Estas tres linhas tinham
    # dois erros compostos: a org era `getloopscale` (o modulo e `getloopscale`) e o
    # pacote era o `internal/routes` plano, que a decomposicao esvaziou.
    #
    # `-X` sobre simbolo inexistente e um NO-OP que sai 0 — o linker nao avisa —
    # entao o build passava e o binario ficava com o default. Medido ao vivo em
    # 2026-08-16: o motor respondia `{"version":"dev","built_at":"unknown"}`, e o
    # contrato de status da frota do loopzero-cloud le exatamente esses dois valores
    # como sentinela de "a camada de deploy nao injetou a proveniencia".
    #
    # O gate `ldflag-symbols` resolve estes caminhos contra os go.mod e o AST, entao
    # a proxima mudanca de pacote reprova em vez de quebrar em silencio.
    LDFLAGS="-s -w \
      -X github.com/getloopscale/loopzero/api/internal/version/versionhttp.versionTag=${LOOPZERO_VERSION} \
      -X github.com/getloopscale/loopzero/api/internal/version/versionhttp.versionCommit=${LOOPZERO_COMMIT} \
      -X github.com/getloopscale/loopzero/api/internal/version/versionhttp.versionBuildDate=${LOOPZERO_BUILD_DATE}" \
    && cd api && CGO_ENABLED=0 go build -trimpath -ldflags="$LDFLAGS" -o /loopzero-api ./cmd/ \
    && CGO_ENABLED=0 go build -trimpath -ldflags="$LDFLAGS" -o /loopzero-controller ./cmd/controller/

# Build migrate from source (same Go toolchain) to avoid CVEs in pre-compiled binary.
FROM golang:1.26.1-alpine3.22@sha256:07e91d24f6330432729082bb580983181809e0a48f0f38ecde26868d4568c6ac AS migrate-builder
RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go install -tags 'postgres' -trimpath -ldflags="-s -w" \
    github.com/golang-migrate/migrate/v4/cmd/migrate@v4.19.1

FROM alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce

LABEL org.opencontainers.image.source="https://github.com/getloopscale/loopzero"
LABEL org.opencontainers.image.title="loopzero-api"
LABEL org.opencontainers.image.description="LoopZero PaaS API server"

RUN apk add --no-cache ca-certificates git && adduser -D -u 1000 loopzero
COPY --from=builder /loopzero-api /loopzero-api
COPY --from=builder /loopzero-controller /loopzero-controller
COPY --from=migrate-builder /go/bin/migrate /usr/local/bin/migrate
COPY --from=builder /workspace/api/internal/db/migrations/ /migrations/
USER 1000
EXPOSE 8080 9090 9091

HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- http://localhost:8080/healthz || exit 1

CMD ["/loopzero-api"]
