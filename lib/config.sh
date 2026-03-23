#!/usr/bin/env bash
# lib/config.sh — project config/bootstrap file generation helpers
# Sourced by lib/install.sh; expects lib/common.sh to be loaded first.

generate_opencode_config() {
  local target="$REPO_ROOT/opencode.json"

  info "Updating project-local opencode.json..."
  if [[ "${DRY_RUN:-0}" == "1" ]]; then
    info "[dry-run] Would write project-local MCP config for $REPO_ROOT"
    return 0
  fi

  uv --directory "$REPO_ROOT" run python - <<'PY'
from __future__ import annotations

import json
import os
from pathlib import Path

repo_root = Path(os.environ["REPO_ROOT"])
target = repo_root / "opencode.json"

config: dict[str, object] = {}
if target.exists():
    try:
        config = json.loads(target.read_text(encoding="utf-8"))
    except json.JSONDecodeError:
        backup = target.with_suffix(target.suffix + ".invalid")
        target.replace(backup)
        print(f"[warn]  Existing opencode.json was invalid JSON; moved to {backup.name}")
        config = {}

mcp = config.setdefault("mcp", {})
if not isinstance(mcp, dict):
    config["mcp"] = {}
    mcp = config["mcp"]

mcp["local_rag"] = {
    "type": "local",
    "command": [
        "uv",
        "--directory",
        str(repo_root),
        "run",
        "lib/server.py",
    ],
    "enabled": True,
    "timeout": 10000,
    "environment": {
        "RAG_CHROMA_DIR": os.environ["RAG_CHROMA_DIR"],
        "OLLAMA_HOST": os.environ["OLLAMA_HOST"],
        "EMBED_MODEL": os.environ["EMBED_MODEL"],
        "COLLECTION_NAME": os.environ["COLLECTION_NAME"],
    },
}

target.write_text(json.dumps(config, indent=2) + "\n", encoding="utf-8")
PY

ok "Updated project-local opencode.json"
}

ensure_env_file() {
  local env_file="$REPO_ROOT/.env"
  local env_example="$REPO_ROOT/lib/.env.example"

  if [[ -f "$env_file" ]]; then
    ok "Existing .env preserved"
    return 0
  fi

  if [[ ! -f "$env_example" ]]; then
    warn "No .env or lib/.env.example found"
    return 0
  fi

  info "Creating .env from lib/.env.example..."
  run cp "$env_example" "$env_file"
  if [[ "${DRY_RUN:-0}" == "1" ]]; then
    ok "Dry run: .env would be created from lib/.env.example"
    return 0
  fi
  ok "Created .env from lib/.env.example"
}
