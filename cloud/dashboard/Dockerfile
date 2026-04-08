FROM node:20-alpine AS builder
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm install --legacy-peer-deps
COPY . .
# Build-time env vars for Vite (baked into the static output).
# Only VITE_API_URL is used. Empty in production (same-origin relative URLs).
ARG VITE_API_URL=http://localhost:13002
RUN npm run build

FROM nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
# SPA routing: serve index.html for all routes
RUN echo 'server { listen 80; root /usr/share/nginx/html; location / { try_files $uri /index.html; } }' > /etc/nginx/conf.d/default.conf
EXPOSE 80
