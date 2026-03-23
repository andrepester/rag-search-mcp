#!/usr/bin/env bash
# lib/install.sh - main installer for the local RAG + MCP setup
# Usage: ./lib/install.sh [--dry-run] [--yes] [--skip-ollama] [--skip-config]
set -euo pipefail

# -- Parse flags ------------------------------------------------------
export DRY_RUN=0
export AUTO_YES=0
SKIP_OLLAMA=0
SKIP_CONFIG=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run)     DRY_RUN=1;     shift ;;
    --yes|-y)      AUTO_YES=1;    shift ;;
    --skip-ollama) SKIP_OLLAMA=1; shift ;;
    --skip-config) SKIP_CONFIG=1; shift ;;
    -h|--help)
      cat <<'USAGE'
Usage: ./lib/install.sh [OPTIONS]

Options:
  --dry-run       Show what would be done without executing
  --yes, -y       Skip all confirmation prompts
  --skip-ollama   Skip ollama installation, service start, and model pull
  --skip-config   Skip opencode.json generation
  -h, --help      Show this help message
USAGE
      exit 0
      ;;
    *)
      echo "[error] Unknown option: $1" >&2
      exit 1
      ;;
  esac
done

# -- Source library scripts -------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# shellcheck source=lib/common.sh
source "$SCRIPT_DIR/common.sh"
# shellcheck source=lib/bootstrap.sh
source "$SCRIPT_DIR/bootstrap.sh"
# shellcheck source=lib/config.sh
source "$SCRIPT_DIR/config.sh"

# -- Header -----------------------------------------------------------
echo ""
echo "╔═══════════════════════════════════════════════════╗"
echo "║  Local RAG + MCP Installer                       ║"
echo "╚═══════════════════════════════════════════════════╝"
echo ""
info "Repo root:  $REPO_ROOT"
info "OS:         $OS"
info "Dry run:    $( [[ $DRY_RUN == 1 ]] && echo yes || echo no )"
echo ""

if [[ "$OS" == "unknown" ]]; then
  die "Unsupported operating system. Only macOS and Linux are supported."
fi

# -- Step 1: Create .env from template if needed ---------------------
info "── Step 1/5: Environment file ──"
ensure_env_file
load_env_file
init_rag_env
info "Docs dir:      $RAG_DOCS_DIR"
info "Index dir:     $RAG_CHROMA_DIR"
info "Ollama host:   $OLLAMA_HOST"
info "Embed model:   $EMBED_MODEL"
info "Collection:    $COLLECTION_NAME"
echo ""

# -- Step 2: Install uv ----------------------------------------------
info "── Step 2/5: uv ──"
install_uv
echo ""

# -- Step 3: Install ollama + model ----------------------------------
info "── Step 3/5: ollama ──"
if [[ $SKIP_OLLAMA == 1 ]]; then
  info "Skipping ollama (--skip-ollama)"
else
  install_ollama
fi
echo ""

# -- Step 4: Python dependencies -------------------------------------
info "── Step 4/5: Python dependencies ──"
install_python_deps
echo ""

# -- Step 5: Generate opencode.json ----------------------------------
info "── Step 5/5: OpenCode config ──"
if [[ $SKIP_CONFIG == 1 ]]; then
  info "Skipping config generation (--skip-config)"
else
  generate_opencode_config
fi
echo ""

# -- Create dirs if missing ------------------------------------------
run mkdir -p "$RAG_DOCS_DIR" "$RAG_CHROMA_DIR"

# -- Done -------------------------------------------------------------
echo "╔═══════════════════════════════════════════════════╗"
echo "║  Installation complete!                          ║"
echo "╚═══════════════════════════════════════════════════╝"
