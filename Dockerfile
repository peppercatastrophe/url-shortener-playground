# Multi-stage build: compile three binaries (api, worker, grpc) from one image.
# The API, Worker, and gRPC server share the same base image; each Deployment
# selects which binary to run via the `command` field in its Kubernetes manifest.

# --- Build stage ---
FROM golang:1.24-alpine AS build
WORKDIR /src

# Cache deps: copy mod files first, download, then copy source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/worker ./cmd/worker
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/grpc ./cmd/grpc

# --- Runtime stage ---
FROM alpine:3.20
RUN apk add --no-cache ca-certificates curl
COPY --from=build /out/api    /usr/local/bin/api
COPY --from=build /out/worker /usr/local/bin/worker
COPY --from=build /out/grpc    /usr/local/bin/grpc
EXPOSE 3000