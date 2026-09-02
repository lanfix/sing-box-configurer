FROM docker.io/library/golang:1.26.4-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /opt

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -extldflags '-static'" \
    -o /opt/sing-box-configurer /opt/cmd


FROM docker.io/library/debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y ca-certificates

WORKDIR /app

COPY --from=builder --chmod=755 /opt/sing-box-configurer /app

CMD ["/app/sing-box-configurer"]
