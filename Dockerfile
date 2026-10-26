# Stage 1: Build Vue 3 + Vite Frontend
FROM node:22-alpine AS web-builder
WORKDIR /app/web
COPY web/package.json web/package-lock.json* ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Stage 2: Build Pure-Go Backend Binary (CGO_ENABLED=0 + embedded frontend)
FROM golang:1.26-alpine AS go-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY web/embed.go ./web/embed.go
COPY --from=web-builder /app/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /app/secret-santa ./cmd/server

# Stage 3: Minimal Production Runtime
FROM alpine:3.21
RUN apk --no-cache add ca-certificates tzdata su-exec && \
    addgroup -S santa && adduser -S -G santa santa && \
    mkdir -p /data && chown -h santa:santa /data && chmod 700 /data
WORKDIR /app
COPY --from=go-builder /app/secret-santa /app/secret-santa
VOLUME ["/data"]
EXPOSE 8080
ENV APP_ENV=production \
    PORT=8080 \
    DB_PATH=/data/secret-santa.db
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1:${PORT:-8080}/api/health || exit 1
ENTRYPOINT ["sh", "-c", "if [ \"$(id -u)\" = '0' ]; then mkdir -p /data && chown -h santa:santa /data && chmod 700 /data && exec su-exec santa /app/secret-santa; else exec /app/secret-santa; fi"]
