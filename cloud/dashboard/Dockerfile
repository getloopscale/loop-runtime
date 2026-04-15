FROM node:20-alpine3.21 AS builder
WORKDIR /app
COPY package.json package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci
COPY . .
ARG VITE_API_URL=""
ARG VITE_ENGINE_URL=""
ARG VITE_AUTH_URL=""
ARG VITE_ACCOUNTS_URL=""
RUN npm run build

FROM nginxinc/nginx-unprivileged:1.28-alpine3.21
LABEL org.opencontainers.image.source="https://github.com/loopzero/loopzero"
LABEL org.opencontainers.image.title="loopzero-dashboard"
COPY --from=builder /app/dist /usr/share/nginx/html
RUN echo 'server { listen 8080; root /usr/share/nginx/html; location / { try_files $uri /index.html; } }' > /etc/nginx/conf.d/default.conf
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --retries=3 \
  CMD wget -qO- http://localhost:8080/ || exit 1
