#!/usr/bin/env bash
# lib/reindex.sh — re-run document ingestion
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# shellcheck source=lib/common.sh
source "$SCRIPT_DIR/common.sh"
load_env_file
init_rag_env

echo "[info]  Re-indexing documents from $RAG_DOCS_DIR..."
uv --directory "$REPO_ROOT" run lib/ingest.py
echo "[ok]    Indexing complete"
