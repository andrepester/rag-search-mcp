#!/usr/bin/env bash
# lib/common.sh — shared helpers for installer modules
# Sourced by lib/install.sh and other lib/*.sh modules; not executed directly.

set -euo pipefail

# ── Repo root ────────────────────────────────────────────────────────
# Resolve the repo root relative to where the sourcing script lives.
# Assumes this file lives at <repo>/lib/common.sh
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export REPO_ROOT

# ── OS detection ─────────────────────────────────────────────────────
detect_os() {
  case "$(uname -s)" in
    Darwin*) echo "macos" ;;
    Linux*)  echo "linux" ;;
    *)       echo "unknown" ;;
  esac
}
OS="$(detect_os)"
export OS

# ── Default configuration ──────────────────────────────────────────────
DEFAULT_RAG_DOCS_DIR="docs"
DEFAULT_RAG_CHROMA_DIR="index"
DEFAULT_OLLAMA_HOST="http://127.0.0.1:11434"
DEFAULT_EMBED_MODEL="nomic-embed-text"
DEFAULT_COLLECTION_NAME="docs"
DEFAULT_RAG_CHUNK_SIZE="1200"
DEFAULT_RAG_CHUNK_OVERLAP="200"

# ── Logging helpers ──────────────────────────────────────────────────
_log()  { printf '%s\n' "$1"; }
info()  { _log "[info]  $*"; }
warn()  { _log "[warn]  $*"; }
err()   { _log "[error] $*" >&2; }
die()   { err "$@"; exit 1; }
ok()    { _log "[ok]    $*"; }

# ── Confirmation prompt ──────────────────────────────────────────────
# If AUTO_YES=1 is set (--yes flag), skip confirmation.
confirm() {
  local msg="${1:-Continue?}"
  if [[ "${AUTO_YES:-0}" == "1" ]]; then
    return 0
  fi
  printf '%s [y/N] ' "$msg"
  read -r answer
  case "$answer" in
    [yY]*) return 0 ;;
    *)     return 1 ;;
  esac
}

# ── Dry-run guard ────────────────────────────────────────────────────
# Wraps a command: in dry-run mode it prints instead of executing.
run() {
  if [[ "${DRY_RUN:-0}" == "1" ]]; then
    info "[dry-run] $*"
  else
    "$@"
  fi
}

# ── Command existence check ──────────────────────────────────────────
has_cmd() {
  command -v "$1" &>/dev/null
}

# ── Environment/config helpers ────────────────────────────────────────
load_env_file() {
  local env_file="${1:-$REPO_ROOT/.env}"

  if [[ ! -f "$env_file" ]]; then
    return 0
  fi

  set -a
  # shellcheck disable=SC1090
  source "$env_file"
  set +a
}

resolve_repo_path() {
  local raw="$1"

  if [[ -z "$raw" ]]; then
    return 1
  fi

  if [[ "$raw" == ~* ]]; then
    raw="${HOME}${raw#\~}"
  fi

  if [[ "$raw" == /* ]]; then
    printf '%s\n' "$raw"
  else
    printf '%s\n' "$REPO_ROOT/$raw"
  fi
}

init_rag_env() {
  local resolved_docs_dir
  local resolved_chroma_dir

  resolved_docs_dir="$(resolve_repo_path "${RAG_DOCS_DIR:-$DEFAULT_RAG_DOCS_DIR}")"
  resolved_chroma_dir="$(resolve_repo_path "${RAG_CHROMA_DIR:-$DEFAULT_RAG_CHROMA_DIR}")"

  export RAG_DOCS_DIR="$resolved_docs_dir"
  export RAG_CHROMA_DIR="$resolved_chroma_dir"
  export OLLAMA_HOST="${OLLAMA_HOST:-$DEFAULT_OLLAMA_HOST}"
  export EMBED_MODEL="${EMBED_MODEL:-$DEFAULT_EMBED_MODEL}"
  export COLLECTION_NAME="${COLLECTION_NAME:-$DEFAULT_COLLECTION_NAME}"
  export RAG_CHUNK_SIZE="${RAG_CHUNK_SIZE:-$DEFAULT_RAG_CHUNK_SIZE}"
  export RAG_CHUNK_OVERLAP="${RAG_CHUNK_OVERLAP:-$DEFAULT_RAG_CHUNK_OVERLAP}"
}

ollama_host_is_local() {
  case "$OLLAMA_HOST" in
    http://127.0.0.1:*|http://localhost:*|https://127.0.0.1:*|https://localhost:*)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}
