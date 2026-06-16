# syntax=docker/dockerfile:1.7

ARG GIT_VERSION=dev

FROM node:24-alpine AS frontend
WORKDIR /src/web

RUN corepack enable && corepack prepare pnpm@10.33.0 --activate

COPY web/package.json web/pnpm-lock.yaml ./
RUN --mount=type=cache,id=pnpm-store,target=/pnpm/store \
    pnpm config set store-dir /pnpm/store && \
    pnpm install --frozen-lockfile && \
    pnpm approve-builds --all && \
    pnpm rebuild

COPY web/ ./
ARG GIT_VERSION
RUN env NEXT_PUBLIC_APP_VERSION=${GIT_VERSION} pnpm run build

FROM golang:1.24.4-alpine AS backend
WORKDIR /src

RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . ./
RUN rm -rf static/out && mkdir -p static/out
COPY --from=frontend /src/web/out ./static/out

ARG GIT_VERSION
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -tags=jsoniter -ldflags="-s -w -X 'github.com/bestruirui/octopus/internal/conf.Version=${GIT_VERSION}' -X 'github.com/bestruirui/octopus/internal/conf.Commit=docker'" -o /out/octopus .

FROM alpine:3.22

ENV TZ=Asia/Shanghai
WORKDIR /app

RUN apk add --no-cache ca-certificates su-exec tzdata && \
    cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && \
    echo Asia/Shanghai > /etc/timezone && \
    mkdir -p /app/data

COPY --from=backend /out/octopus /app/octopus
COPY scripts/dockerfiles/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh /app/octopus

EXPOSE 8080
VOLUME ["/app/data"]
CMD ["/entrypoint.sh"]
