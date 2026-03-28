#!/usr/bin/env bash
# lib/doctor.sh — health checks for the local RAG setup
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# shellcheck source=lib/common.sh
source "$SCRIPT_DIR/common.sh"
load_env_file
init_rag_env

passed=0
failed=0
warned=0

check() {
  local label="$1"
  shift
  if "$@" &>/dev/null; then
    printf '[ok]    %s\n' "$label"
    passed=$((passed + 1))
  else
    printf '[FAIL]  %s\n' "$label"
    failed=$((failed + 1))
  fi
}

warn_check() {
  local label="$1"
  shift
  if "$@" &>/dev/null; then
    printf '[ok]    %s\n' "$label"
    passed=$((passed + 1))
  else
    printf '[warn]  %s\n' "$label"
    warned=$((warned + 1))
  fi
}

check_index_has_data() {
  uv --directory "$REPO_ROOT" run python - <<'PY'
from __future__ import annotations

import os
from pathlib import Path

import chromadb

index_dir = Path(os.environ["RAG_CHROMA_DIR"])
collection_name = os.environ["COLLECTION_NAME"]
client = chromadb.PersistentClient(path=str(index_dir))
collection = client.get_collection(name=collection_name)
count = collection.count()
raise SystemExit(0 if count > 0 else 1)
PY
}

check_opencode_config() {
  uv --directory "$REPO_ROOT" run python - <<'PY'
from __future__ import annotations

import json
import os
from pathlib import Path

repo_root = Path(os.environ["REPO_ROOT"])
config_path = repo_root / "opencode.json"
config = json.loads(config_path.read_text(encoding="utf-8"))
block = config["mcp"]["local_rag"]


def to_repo_relative(path_value: str) -> str:
    path = Path(path_value).expanduser()
    resolved = path.resolve() if path.is_absolute() else (repo_root / path).resolve()
    try:
        return str(resolved.relative_to(repo_root))
    except ValueError:
        return str(resolved)


expected_command = ["uv", "run", "lib/server.py"]
expected_env = {
    "RAG_CHROMA_DIR": to_repo_relative(os.environ["RAG_CHROMA_DIR"]),
    "OLLAMA_HOST": os.environ["OLLAMA_HOST"],
    "EMBED_MODEL": os.environ["EMBED_MODEL"],
    "COLLECTION_NAME": os.environ["COLLECTION_NAME"],
}

raise SystemExit(0 if block.get("command") == expected_command and block.get("environment") == expected_env else 1)
PY
}

check_server_imports() {
  uv --directory "$REPO_ROOT" run python - <<'PY'
from __future__ import annotations

import importlib.util
import os
from pathlib import Path

server_path = Path(os.environ["REPO_ROOT"]) / "lib/server.py"
spec = importlib.util.spec_from_file_location("local_rag_server", server_path)
module = importlib.util.module_from_spec(spec)
assert spec and spec.loader
spec.loader.exec_module(module)

module.list_sources()
PY
}

check_server_starts() {
  uv --directory "$REPO_ROOT" run python - <<'PY'
from __future__ import annotations

import os
import subprocess
import time

cmd = ["uv", "--directory", os.environ["REPO_ROOT"], "run", "lib/server.py"]
proc = subprocess.Popen(
    cmd,
    stdin=subprocess.PIPE,
    stdout=subprocess.DEVNULL,
    stderr=subprocess.DEVNULL,
)
time.sleep(1)
alive = proc.poll() is None

if alive:
    proc.terminate()
    try:
        proc.wait(timeout=5)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.wait(timeout=5)

raise SystemExit(0 if alive else 1)
PY
}

check_index_model_matches_config() {
  uv --directory "$REPO_ROOT" run python - <<'PY'
from __future__ import annotations

import json
import os
from pathlib import Path

index_dir = Path(os.environ["RAG_CHROMA_DIR"])
configured_model = os.environ["EMBED_MODEL"].strip()
metadata_path = index_dir / ".rag_index_meta.json"

if not metadata_path.exists():
    raise SystemExit(1)

try:
    metadata = json.loads(metadata_path.read_text(encoding="utf-8"))
except json.JSONDecodeError:
    raise SystemExit(1)

index_model = str(metadata.get("embed_model", "")).strip()
raise SystemExit(0 if index_model and index_model == configured_model else 1)
PY
}

index_embed_model() {
  uv --directory "$REPO_ROOT" run python - <<'PY'
from __future__ import annotations

import json
import os
from pathlib import Path

index_dir = Path(os.environ["RAG_CHROMA_DIR"])
metadata_path = index_dir / ".rag_index_meta.json"
if not metadata_path.exists():
    raise SystemExit(1)

try:
    metadata = json.loads(metadata_path.read_text(encoding="utf-8"))
except json.JSONDecodeError:
    raise SystemExit(1)

model = str(metadata.get("embed_model", "")).strip()
if not model:
    raise SystemExit(1)

print(model)
PY
}

echo "=== Local RAG Doctor ==="
echo ""
echo "Config: docs=$RAG_DOCS_DIR index=$RAG_CHROMA_DIR ollama=$OLLAMA_HOST model=$EMBED_MODEL collection=$COLLECTION_NAME"
echo ""

# ── System tools ─────────────────────────────────────────────────────
check "uv is installed"            command -v uv
check "ollama is installed"        command -v ollama
check "ollama endpoint is reachable" curl -sf "$OLLAMA_HOST/"

# ── Embedding model ──────────────────────────────────────────────────
model="$EMBED_MODEL"
check "ollama model '$model' available" ollama show "$model"

# ── Index/model compatibility ────────────────────────────────────────
warn_check "Index embed model matches EMBED_MODEL" check_index_model_matches_config
index_model=""
if index_model="$(index_embed_model 2>/dev/null)"; then
  if [[ "$index_model" != "$EMBED_MODEL" ]]; then
    echo "[warn]  Index uses '$index_model' but EMBED_MODEL is '$EMBED_MODEL'; server will fallback to index model"
    echo "[warn]  Reindex recommended: make reindex"
    warned=$((warned + 1))
  fi
  check "Index embed model '$index_model' available" ollama show "$index_model"
else
  echo "[warn]  Index metadata missing or invalid; cannot verify embed model compatibility"
  echo "[warn]  Reindex recommended: make reindex"
  warned=$((warned + 1))
fi

# ── Python dependencies ─────────────────────────────────────────────
check "pyproject.toml exists"      test -f "$REPO_ROOT/pyproject.toml"
check "uv.lock exists"             test -f "$REPO_ROOT/uv.lock"
check "Python: mcp importable"     uv --directory "$REPO_ROOT" run python -c "import mcp"
check "Python: chromadb importable" uv --directory "$REPO_ROOT" run python -c "import chromadb"
check "Python: httpx importable"   uv --directory "$REPO_ROOT" run python -c "import httpx"
check "Python: pypdf importable"   uv --directory "$REPO_ROOT" run python -c "import pypdf"

# ── Project structure ────────────────────────────────────────────────
check "docs directory exists"      test -d "$RAG_DOCS_DIR"
check "index directory exists"     test -d "$RAG_CHROMA_DIR"
check "lib/server.py exists"      test -f "$REPO_ROOT/lib/server.py"
check "lib/ingest.py exists"      test -f "$REPO_ROOT/lib/ingest.py"

# ── OpenCode config ──────────────────────────────────────────────────
if [[ -f "$REPO_ROOT/opencode.json" ]]; then
  check "opencode.json matches current config" check_opencode_config
else
  printf '[FAIL]  opencode.json not found (run make install to generate it)\n'
  failed=$((failed + 1))
fi

# ── Runtime smoke tests ──────────────────────────────────────────────
check "Chroma collection has indexed documents" check_index_has_data
check "MCP helpers can query the collection" check_server_imports
check "MCP server starts over stdio" check_server_starts

# ── Summary ──────────────────────────────────────────────────────────
echo ""
echo "=== Results: $passed passed, $warned warnings, $failed failed ==="

if [[ $failed -gt 0 ]]; then
  exit 1
fi
