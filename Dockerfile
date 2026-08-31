# syntax=docker/dockerfile:1.6
# Multi-stage para Render Free (512MB) + Neon Free
# stage1: frontend (node 20) -> app/dist
# stage2: backend (go 1.26) -> /app/bin/artigos-ana
# stage3: runtime (debian slim + poppler-utils + ca-certificates) -> /app/www + bin

FROM node:20-bookworm-slim AS frontend
WORKDIR /src/app
COPY app/package.json app/package-lock.json* ./
RUN npm ci --ignore-scripts
COPY app/ ./
RUN npm run build

FROM golang:1.26-bookworm AS backend
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
# Build estatico sem CGO para slim
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /app/bin/artigos-ana .

FROM debian:bookworm-slim AS runtime
RUN apt-get update && apt-get install -y --no-install-recommends \
    poppler-utils ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && update-ca-certificates
WORKDIR /app
COPY --from=backend /app/bin/artigos-ana /app/bin/artigos-ana
COPY --from=frontend /src/app/dist /app/www
RUN mkdir -p /tmp/data/pdfs /tmp/data/paginas /tmp/data/export /tmp/data/tmp && chmod -R 755 /tmp/data
# Render injeta PORT=10000 e DATABASE_URL; BIND_ADDR=0.0.0.0 obrigatorio para web
ENV BIND_ADDR=0.0.0.0
ENV PORT=10000
EXPOSE 10000
# Nota: /tmp/data eh efemero no Render Free (apagado a cada restart/sleep).
# Persistencia real vem do Postgres (Neon) via pdf_data/imagem_data BYTEA; disco serve como cache.
CMD ["/app/bin/artigos-ana", "-bind=0.0.0.0", "-port=10000", "-www=/app/www", "-data=/tmp/data", "-poppler=/usr/bin"]
