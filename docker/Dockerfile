FROM golang:1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS base

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

FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf

RUN adduser -D -H app && apk del apk-tools
USER app

WORKDIR /app
COPY --from=base /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /out/rag-mcp /app/rag-mcp
COPY --from=builder /out/rag-index /app/rag-index

EXPOSE 8765

ENTRYPOINT ["/app/rag-mcp"]
