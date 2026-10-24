FROM golang:1.27.3-alpine@sha256:b8c9b82affbbb82a020bbcf998dad2ba9516944ac7e883524642a481a9505e9c AS base

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

FROM alpine:3.24@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a

RUN adduser -D -H app && apk del apk-tools
USER app

WORKDIR /app
COPY --from=base /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /out/rag-mcp /app/rag-mcp
COPY --from=builder /out/rag-index /app/rag-index

EXPOSE 8765

ENTRYPOINT ["/app/rag-mcp"]
