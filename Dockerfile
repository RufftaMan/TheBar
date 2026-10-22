# syntax=docker/dockerfile:1

# 1) frontend (Svelte) -> backend/web/dist
FROM node:22-alpine AS web
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
RUN mkdir -p ../backend/web/dist && npm run build

# 2) backend (Go), with the frontend embedded
FROM golang:1.26-alpine AS go
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY --from=web /src/backend/web/dist ./web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /thebar .

# 3) docker CLI + compose plugin (used for config validation and stack actions)
FROM docker:29-cli AS cli

# 4) runtime
FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata
COPY --from=cli /usr/local/bin/docker /usr/local/bin/docker
COPY --from=cli /usr/local/libexec/docker/cli-plugins/docker-compose /usr/local/libexec/docker/cli-plugins/docker-compose
COPY --from=go /thebar /usr/local/bin/thebar
ENV THEBAR_DATA=/data
VOLUME /data
EXPOSE 8800
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD ["thebar", "health"]
ENTRYPOINT ["thebar"]
