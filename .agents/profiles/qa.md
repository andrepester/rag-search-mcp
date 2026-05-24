# QA Profile

Use this profile for runtime behavior, install/bootstrap, MCP endpoint, Docker, config, or post-merge smoke checks.

## Preflight

- `docker compose --project-directory . -f docker/docker-compose.yml config`
- `make test`

## Launch

- Start or rebuild the stack with `make up`.
- Verify containers with `docker compose --project-directory . -f docker/docker-compose.yml ps`.

## Runtime Smoke

Health endpoint:

- `curl -fsS http://127.0.0.1:8765/healthz`

Full runtime doctor:

- `make doctor`

MCP HTTP smoke:

- Initialize a session via `POST /mcp`.
- Send `notifications/initialized`.
- Call `tools/list` and verify these tools exist: `get_chunk`, `list_sources`, `reindex`, `search`.
- Call `list_sources` with `scope=all`.
- Call `search` with a small query and `top_k=1`.

## Log Check

Inspect recent logs:

- `docker compose --project-directory . -f docker/docker-compose.yml logs --tail=80 rag-mcp chroma ollama`

## Rules

- Leave the stack running if it was already expected to run; otherwise report final state clearly.
- Do not use `make install` unless testing bootstrap or first-run behavior.
- Do not run destructive reset flows without explicit user approval.
- Browser QA is not required unless a web UI is added later.
