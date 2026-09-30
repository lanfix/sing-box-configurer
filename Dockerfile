# Образ sing-box-configurer.
#
# Локально образ собирается из исходников (docker build . или docker compose build). В релизе
# (.github/workflows/release.yml) стадия binaries подменяется бинарниками, собранными scripts/build.sh
# (--build-context binaries=dist), поэтому в образе и в архивах релиза GitHub одни и те же файлы.

FROM --platform=$BUILDPLATFORM docker.io/library/node:24-alpine AS view

WORKDIR /opt/view

COPY view/package.json view/package-lock.json ./
RUN npm ci --no-audit --no-fund

COPY view/ ./
RUN npm run build


FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.26.4-alpine AS builder

ARG VERSION=dev
ARG TARGETPLATFORM

WORKDIR /opt/src

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
COPY --from=view /opt/view/dist ./view/dist

RUN DIST=/opt/dist sh scripts/build.sh "$VERSION" "$TARGETPLATFORM"


# Бинарники по каталогам платформ: /linux_amd64, /linux_arm64, /linux_armv7.
FROM scratch AS binaries

COPY --from=builder /opt/dist/ /


FROM docker.io/library/alpine:3.22

ARG VERSION=dev
ARG CHANGELOG=""
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

LABEL org.opencontainers.image.title="sing-box-configurer" \
      org.opencontainers.image.version="${VERSION}" \
      io.lanfix.changelog="${CHANGELOG}"

WORKDIR /app

COPY --from=binaries --chmod=755 /${TARGETOS}_${TARGETARCH}${TARGETVARIANT}/ /app/

CMD ["/app/sing-box-configurer"]
