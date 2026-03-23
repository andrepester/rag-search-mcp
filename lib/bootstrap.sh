#!/usr/bin/env bash
# lib/bootstrap.sh — bootstrap/install functions for the main installer
# Sourced by lib/install.sh; expects lib/common.sh to be loaded first.

# ── uv ───────────────────────────────────────────────────────────────
install_uv() {
  if has_cmd uv; then
    ok "uv already installed ($(uv --version))"
    return 0
  fi

  info "Installing uv..."
  case "$OS" in
    macos)
      run brew install uv
      ;;
    linux)
      run curl -LsSf https://astral.sh/uv/install.sh | run sh
      ;;
    *)
      die "Unsupported OS for uv installation: $OS"
      ;;
  esac

  has_cmd uv || die "uv installation failed — not found on PATH"
  ok "uv installed ($(uv --version))"
}

# ── Ollama ───────────────────────────────────────────────────────────
install_ollama() {
  if has_cmd ollama; then
    ok "ollama already installed ($(ollama --version 2>/dev/null || echo 'version unknown'))"
  else
    info "Installing ollama..."
    case "$OS" in
      macos)
        run brew install ollama
        ;;
      linux)
        run curl -fsSL https://ollama.com/install.sh | run sh
        ;;
      *)
        die "Unsupported OS for ollama installation: $OS"
        ;;
    esac
    has_cmd ollama || die "ollama installation failed — not found on PATH"
    ok "ollama installed"
  fi

  start_ollama
  pull_embed_model
}

start_ollama() {
  info "Ensuring ollama is running at $OLLAMA_HOST..."

  # Check if ollama is already responding
  if curl -sf "$OLLAMA_HOST/" &>/dev/null; then
    ok "ollama is already running"
    return 0
  fi

  if ! ollama_host_is_local; then
    die "Configured OLLAMA_HOST is not reachable: $OLLAMA_HOST"
  fi

  case "$OS" in
    macos)
      run brew services start ollama 2>/dev/null || true
      ;;
    linux)
      if has_cmd systemctl; then
        run sudo systemctl start ollama 2>/dev/null || true
      else
        info "Starting ollama serve in background..."
        run ollama serve &>/dev/null &
        disown
      fi
      ;;
  esac

  # Wait for ollama to become responsive (up to 30 seconds)
  info "Waiting for ollama to start..."
  local attempts=0
  while ! curl -sf "$OLLAMA_HOST/" &>/dev/null; do
    sleep 1
    attempts=$((attempts + 1))
    if [[ $attempts -ge 30 ]]; then
      die "ollama did not start within 30 seconds at $OLLAMA_HOST"
    fi
  done
  ok "ollama is running"
}

pull_embed_model() {
  local model="$EMBED_MODEL"
  info "Pulling embedding model: $model"
  run ollama pull "$model"
  ok "Model $model ready"
}

# ── Python dependencies via uv ───────────────────────────────────────
install_python_deps() {
  info "Setting up Python project and dependencies..."

  if [[ ! -f "$REPO_ROOT/pyproject.toml" ]]; then
    die "pyproject.toml not found in $REPO_ROOT"
  fi

  if [[ ! -f "$REPO_ROOT/uv.lock" ]]; then
    die "uv.lock not found in $REPO_ROOT"
  fi

  info "Syncing locked dependencies..."
  run uv sync --directory "$REPO_ROOT"

  ok "Python dependencies installed"
}
