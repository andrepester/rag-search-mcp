FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/rag-mcp ./cmd/rag-mcp
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/rag-index ./cmd/rag-index

FROM alpine:3.22

RUN adduser -D -H app
USER app

WORKDIR /app
COPY --from=builder /out/rag-mcp /app/rag-mcp
COPY --from=builder /out/rag-index /app/rag-index

EXPOSE 8080

ENTRYPOINT ["/app/rag-mcp"]
