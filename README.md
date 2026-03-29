# RAG MCP Service (Go + Chroma + Ollama)

This repository provides a Go-first MCP service named `rag` for semantic
retrieval over documentation and code.

- OpenCode connects via remote MCP (`type: "remote"`)
- Runtime can run local, in Docker, or elsewhere on the network/cloud
- One Docker Compose file configures docs and code through mounts
- Default local setup runs Ollama, Chroma, and rag-mcp in one Compose project
- Default query scope is `all`
- Local binary default bind is loopback (`127.0.0.1`) for safer defaults

## Architecture

```mermaid
flowchart TD
    A["Mounted data/docs/"] --> B["rag-index"]
    C["Mounted data/code/ (can be empty)"] --> B
    B --> D["Ollama embeddings"]
    D --> E["Chroma collection: rag"]

    F["OpenCode"] -->|remote MCP| G["rag-mcp /mcp"]
    G --> E
```

## Tools exposed to OpenCode

If the MCP server is configured as `rag`, OpenCode sees these tools:

- `rag_search` - semantic search (`scope=all|docs|code`, default `all`)
- `rag_get_chunk` - fetch one chunk by `chunk_id`
- `rag_list_sources` - list indexed source paths
- `rag_reindex` - rebuild index from mounted sources

## Scope behavior

- `scope=all` (default): searches docs and code
- `scope=docs`: searches docs only
- `scope=code`: searches code only
- If `data/code` is empty (or code ingest is disabled), `scope=all` behaves like docs-only

## Docker Compose (single file)

Compose file path: `docker/docker-compose.yml`

- `HOST_DOCS_DIR` mount is required (defaults to `./data/docs`)
- `HOST_CODE_DIR` mount is required (defaults to `./data/code`, can be empty)
- `ollama` service is included in the same Compose stack
- Chroma persistence is stored on the host at `./data/index` via bind mount
- Ollama persistence is managed by the `ollama_data` volume
- Published container ports are bound to `127.0.0.1` (localhost-only)
- Compose sets `RAG_HTTP_HOST=0.0.0.0` inside container so host port publishing still works

### One-command local install

```bash
make install
```

`make install` creates `.env` from `.env.example` (if missing), upserts local
`opencode.json`, ensures host mount directories (`docs`, `code`, `index`) exist,
starts the Compose stack, pulls the embedding model in the Ollama container,
runs reindexing, and verifies indexed data.

### Start service

```bash
docker compose --project-directory . -f docker/docker-compose.yml up -d --build
```

### Reindex mounted data

```bash
docker compose --project-directory . -f docker/docker-compose.yml run --rm --entrypoint /app/rag-index rag-mcp
```

You can also trigger reindexing from OpenCode via `rag_reindex`.

### Stop service

```bash
docker compose --project-directory . -f docker/docker-compose.yml down
```

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `RAG_HTTP_HOST` | `127.0.0.1` | HTTP bind address (local default is loopback) |
| `RAG_HTTP_PORT` | `8765` | MCP HTTP port on host |
| `HOST_DOCS_DIR` | `./data/docs` | Host path mounted as docs source |
| `HOST_CODE_DIR` | `./data/code` | Host path mounted as code source (can be empty) |
| `RAG_ENABLE_CODE_INGEST` | `true` | Enable/disable code ingestion |
| `OLLAMA_HOST` | `http://ollama:11434` | Embedding endpoint for containerized runtime |
| `OLLAMA_PORT` | `11434` | Host port mapped to the Ollama container |
| `EMBED_MODEL` | `nomic-embed-text` | Embedding model name |
| `RAG_COLLECTION_NAME` | `rag` | Chroma collection name |
| `RAG_SCOPE_DEFAULT` | `all` | Default search scope |
| `RAG_CHUNK_SIZE` | `1200` | Chunk size in chars |
| `RAG_CHUNK_OVERLAP` | `200` | Chunk overlap in chars |
| `RAG_MAX_TOP_K` | `50` | Upper bound for search `top_k` |

## OpenCode configuration

`opencode.json` uses remote MCP and has no Docker command dependency:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "rag": {
      "type": "remote",
      "url": "http://127.0.0.1:8765/mcp",
      "enabled": true,
      "timeout": 10000
    }
  }
}
```

Run the runtime however you want (Compose, Kubernetes, VM, localhost binary)
as long as the MCP URL is reachable.

Note: `opencode.json` in this repository is local/machine-specific and ignored by git.

## Example prompts

- `Use rag_search with scope docs to explain installation.`
- `Use rag_search with scope code to find chunking logic.`
- `Use rag_search with scope all and summarize architecture from docs and code.`
- `Call rag_list_sources with scope all.`

## Local development (container-first)

```bash
make install
make mod
make test
make build
make doctor
```

`make build` performs a containerized compile check (`go build ./...`) and does not
write local binaries.

Start service:

```bash
make run
```

`make run` starts the Compose stack in detached mode.

Run reindex:

```bash
make reindex
```

All Go toolchain commands run inside containers via `Makefile` targets. No local Go
installation is required.

`make doctor` also starts the Compose stack, runs reindexing, and verifies that at
least one document chunk is indexed in Chroma.

## CI checks

GitHub Actions run:

- `ci-fast`: `gofmt` verification, `go vet`, containerized `go test` with coverage gate, `go build`, `docker compose config`
- `security-baseline`: gitleaks + `govulncheck`
- `integration-ollama`: full `make install` stack startup (Ollama + Chroma + rag-mcp) + reindex smoke test
- `supply-chain`: CycloneDX SBOM artifacts, license allowlist gate, Syft + Grype filesystem and image CVE scans

## Dependency automation

- Dependabot is enabled for `gomod`, `github-actions`, and `docker` updates via `.github/dependabot.yml`.
