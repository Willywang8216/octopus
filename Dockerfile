# Build-from-source Dockerfile.
# Multi-stage build: Node 20 stage compiles the Next.js frontend into
# `web/out`, Go 1.24 stage embeds it into the binary, and a tiny alpine
# runtime serves it.
#
# Use this with docker-compose.dev.yml:
#   docker compose -f docker-compose.dev.yml up -d --build

# -----------------------------------------------------------------------------
# Stage 1: build the frontend
# -----------------------------------------------------------------------------
FROM node:20-alpine AS frontend
WORKDIR /src

# pnpm via corepack is part of every recent Node image
RUN corepack enable

# Copy only what the frontend build needs first so the layer caches well.
COPY web/package.json web/pnpm-lock.yaml ./web/
RUN cd web && pnpm install --frozen-lockfile

# Bring in the rest of the frontend source.
COPY web/ ./web/

# Build the static export. The output ends up in web/out/ thanks to next.config.ts.
RUN cd web && pnpm run build

# -----------------------------------------------------------------------------
# Stage 2: build the Go binary (with the frontend embedded via static.go)
# -----------------------------------------------------------------------------
FROM golang:1.24-alpine AS backend
WORKDIR /src

# git is required because go build reads VCS info into the binary metadata.
RUN apk add --no-cache git

# Cache go.mod/go.sum first so dependency downloads are layer-cached.
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source.
COPY . .

# The frontend assets the Go binary embeds live under static/out.
RUN rm -rf static/out
COPY --from=frontend /src/web/out ./static/out

# Static binary, no CGO, jsoniter for performance.
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -tags=jsoniter -o /out/octopus .

# -----------------------------------------------------------------------------
# Stage 3: minimal runtime
# -----------------------------------------------------------------------------
FROM alpine:3.20
ENV TZ=Asia/Shanghai
RUN apk add --no-cache ca-certificates tzdata && \
    cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && \
    echo "Asia/Shanghai" > /etc/timezone && \
    apk del tzdata && \
    mkdir -p /app/data

WORKDIR /app
COPY --from=backend /out/octopus /app/octopus
RUN chmod +x /app/octopus

EXPOSE 8080
VOLUME ["/app/data"]

ENTRYPOINT ["/app/octopus"]
CMD ["start"]
