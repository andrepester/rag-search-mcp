FROM golang:1.27.1-alpine@sha256:4cb7ac979db5fcc41cae44b2227ba5ab8a51e8807f40d9ba4dee20a0ad960b5b AS base

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

FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

RUN adduser -D -H app && apk del apk-tools
USER app

WORKDIR /app
COPY --from=base /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /out/rag-mcp /app/rag-mcp
COPY --from=builder /out/rag-index /app/rag-index

EXPOSE 8765

ENTRYPOINT ["/app/rag-mcp"]
