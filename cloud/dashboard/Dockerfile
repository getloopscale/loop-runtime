FROM node:20-alpine3.21 AS builder
WORKDIR /app
COPY package.json package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci
COPY . .
# No VITE_* build args — config is injected at runtime via docker-entrypoint.sh.
# This makes the image environment-independent (same commit = same artifact).
RUN npm run build

FROM nginxinc/nginx-unprivileged:1.28-alpine3.21
LABEL org.opencontainers.image.source="https://github.com/loopzero/loopzero"
LABEL org.opencontainers.image.title="loopzero-dashboard"

# Upgrade Alpine packages to get latest security patches (openssl, musl).
USER root
RUN apk upgrade --no-cache
USER 101

COPY --from=builder /app/dist /usr/share/nginx/html
COPY docker-entrypoint.sh /docker-entrypoint.d/40-runtime-config.sh
RUN echo 'server { listen 8080; root /usr/share/nginx/html; location / { try_files $uri /index.html; } }' > /etc/nginx/conf.d/default.conf
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --retries=3 \
  CMD wget -qO- http://localhost:8080/ || exit 1
