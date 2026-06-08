# ADR: Container Statelessness and Host Persistence

| Field | Content |
|---|---|
| ID | RAG-SEARCH-MCP-ADR-2026-05-31-CONTAINER-PERSISTENCE |
| Name | Keep runtime containers stateless and persist operational data on host-mounted paths |
| Status | Accepted |
| Decision Question | Which data should be treated as durable in `rag-search-mcp`, and where should that data live relative to Docker containers? |
| Context / Constraints | `rag-search-mcp` is a small, privately operated Docker-first project. Runtime services are started, stopped, rebuilt, and replaced through Docker Compose and Make targets. The important data boundaries must stay simple: source material lives on the host, index artifacts are host-mounted operational data, Ollama is provided by a shared external host, and containers should remain disposable. |
| Decision & Rationale | Runtime containers are treated as stateless and replaceable. Durable data is kept outside container writable layers: `HOST_DOCS_DIR` and `HOST_CODE_DIR` are read-only host-mounted sources, and `HOST_INDEX_DIR` is host-mounted Chroma index state. `.env` is local host configuration for source/index paths and the shared `OLLAMA_HOST`. `make down` only stops containers. `make clean-install` recreates the stack while preserving the index path by default. `make clean-install FULL_RESET=1` is the explicit destructive reset path for `HOST_INDEX_DIR`. This keeps the operating model understandable without adding application-managed backup, restore, migration, or model-cache management. |
| Alternatives | 1) Store durable state inside container writable layers: rejected because rebuilds and container replacement would make data retention unclear. 2) Use application-managed backup/restore for v1: rejected because source directories and host persistence paths are under the operator's control and normal host backup tooling is sufficient for this private project. 3) Use only Docker-managed volumes with no visible host paths: rejected because explicit host paths make reset, inspection, and backup responsibilities easier to understand. 4) Bundle and persist an Ollama model cache in this stack: rejected because Ollama is now operated as a shared external dependency. |
| Date / Documentation | 2026-05-31; README operations and troubleshooting; Vikunja backlog item `#18` |
| Actors | User (product and operating-model decision), OpenCode Assistant (documentation) |

## Assumptions

- Documentation and code sources are the primary source of truth and remain outside the runtime containers.
- Index artifacts are operational state derived from mounted sources, runtime configuration, chunking rules, and the embedding model.
- Embedding models are managed on the shared Ollama host and have a lifecycle outside this stack.
- The operator is responsible for backing up source directories and any host persistence paths they care about.
- Container-local writable files are not treated as durable application state unless they are explicitly mounted through documented host paths.

## Consequences / Operational Implications

- `rag-mcp` and `chroma` containers can be stopped, rebuilt, or recreated without treating their writable layers as authoritative data stores.
- `HOST_DOCS_DIR` and `HOST_CODE_DIR` are mounted read-only into `rag-mcp`; the app does not own, mutate, or back up those sources.
- `HOST_INDEX_DIR` persists Chroma index state across container stop/start and non-destructive reinstall flows.
- `HOST_INDEX_DIR/rag-state` persists the active index generation pointer used by atomic reindex switching.
- `.env` is host-local configuration and should be treated as operator-managed setup state, not generated application data.
- `make clean-install FULL_RESET=1` intentionally deletes the resolved `HOST_INDEX_DIR` path before reinstalling; source mounts are not reset by this command.
- Backup and restore are outside the application boundary for v1. Operators should back up host source directories and persistence paths with normal host-level tooling if they need recovery guarantees.
- Future work that introduces additional active pointers, build collections, or other index metadata must store required durable artifacts under the documented host persistence boundary or explicitly revisit this ADR.

## Validation / Evidence

- `docker/docker-compose.yml` bind-mounts `HOST_INDEX_DIR` to Chroma's data path.
- `docker/docker-compose.yml` bind-mounts `HOST_INDEX_DIR/rag-state` to `rag-mcp` as the active-generation pointer state path.
- `docker/docker-compose.yml` bind-mounts `HOST_DOCS_DIR` and `HOST_CODE_DIR` read-only into `rag-mcp`.
- `make down` maps to `docker compose stop`, so it does not remove containers or host-mounted data.
- `shell/clean-install.sh` preserves `HOST_INDEX_DIR` by default and deletes it only when `FULL_RESET=1` is set.
- `shell/install-bootstrap.sh` resolves host paths from process environment, `.env`, and defaults, and creates the configured host directories when needed.
