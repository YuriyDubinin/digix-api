# syntax=docker/dockerfile:1.7

# ---- builder ----
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux \
    go build -ldflags="-s -w" -o /app/api ./cmd/api

# ---- runtime ----
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata wget \
 && adduser -D -u 1000 app

COPY --from=builder /app/api        /app/api
COPY --from=builder /build/migrations /app/migrations

WORKDIR /app
USER app

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://localhost:8080/api/ping >/dev/null || exit 1

CMD ["/app/api"]
