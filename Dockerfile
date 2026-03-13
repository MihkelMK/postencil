FROM golang:1.26-alpine3.23 AS builder

WORKDIR /build

# Copy module files first for layer caching
COPY go.mod ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
  -ldflags="-w -s" \
  -o postencil \
  ./cmd/postencil

# ── Final image ────────────────────────────────────────────────────────────────
# scratch + manually copied certs keeps the image minimal while still supporting
# TLS connections to the target.
FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /build/postencil /postencil

EXPOSE 8080

ENTRYPOINT ["/postencil"]
