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

LABEL org.opencontainers.image.source=https://github.com/MihkelMK/postencil
LABEL org.opencontainers.image.description="Webhook proxy that renders Go templates in request fields before forwarding to a target."
LABEL org.opencontainers.image.licenses="GPLv3"

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /build/postencil /postencil

EXPOSE 8080

ENTRYPOINT ["/postencil"]
