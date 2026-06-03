FROM golang:1.25.11-alpine@sha256:c05ba4b73604069d376c4f41346b05374335b5ca0c46fb6dfede5a59f5196931 AS base

WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

FROM base AS go-runner

RUN chmod -R a+rwX /go

WORKDIR /workspace

FROM go-runner AS test

WORKDIR /src

RUN go test -count=1 ./...

FROM base AS builder

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/rag-mcp ./cmd/rag-mcp
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/rag-index ./cmd/rag-index

FROM alpine:3.22@sha256:310c62b5e7ca5b08167e4384c68db0fd2905dd9c7493756d356e893909057601

RUN adduser -D -H app
USER app

WORKDIR /app
COPY --from=builder /out/rag-mcp /app/rag-mcp
COPY --from=builder /out/rag-index /app/rag-index

EXPOSE 8765

ENTRYPOINT ["/app/rag-mcp"]
