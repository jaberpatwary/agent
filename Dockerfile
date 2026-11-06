# ---- Build stage ----
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install git for module fetching and ca-certificates for HTTPS calls
# (AI APIs, RSS feeds) made at build/runtime.
RUN apk add --no-cache git ca-certificates

COPY go.mod ./
RUN go mod download

COPY . .

# Static binary, no CGO, so it runs on a minimal final image.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/server ./cmd/server

# ---- Final stage ----
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Binary
COPY --from=builder /app/server /app/server

# Server-rendered templates and static assets must ship with the binary,
# since html/template loads them from disk at startup (web/templates/**).
COPY --from=builder /app/web ./web

ENV PORT=8080
EXPOSE 8080

# Render.com (and most PaaS) inject DATABASE_URL, JWT_SECRET, GROQ_API_KEY,
# GEMINI_API_KEY etc. as environment variables — see .env.example.
ENTRYPOINT ["/app/server"]
