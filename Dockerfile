FROM docker.io/library/golang:1.26.4-alpine AS builder

ARG VERSION=dev

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /opt

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .

RUN for app in sing-box-configurer:cmd updater:cmd/updater; do \
        CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
            -ldflags="-w -s -extldflags '-static' -X github.com/lanfix/sing-box-configurer/internal/version.Version=${VERSION}" \
            -o "/opt/${app%%:*}" "/opt/${app#*:}" || exit 1; \
    done


FROM docker.io/library/debian:bookworm-slim

ARG VERSION=dev
ARG CHANGELOG=""

LABEL org.opencontainers.image.title="sing-box-configurer" \
      org.opencontainers.image.version="${VERSION}" \
      io.lanfix.changelog="${CHANGELOG}"

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder --chmod=755 /opt/sing-box-configurer /opt/updater /app/

CMD ["/app/sing-box-configurer"]
