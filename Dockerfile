# Multi-stage build: compile both binaries from a single image.
# The API and Worker share the same base image; each Deployment selects
# which binary to run via the `command` field in its Kubernetes manifest.

# --- Build stage ---
FROM golang:1.24-alpine AS build
WORKDIR /src

# Cache deps: copy mod files first, download, then copy source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/worker ./cmd/worker

# --- Runtime stage ---
FROM alpine:3.20
RUN apk add --no-cache ca-certificates curl
COPY --from=build /out/api    /usr/local/bin/api
COPY --from=build /out/worker /usr/local/bin/worker
EXPOSE 3000
