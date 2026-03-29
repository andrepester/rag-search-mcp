package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsAndOverrides(t *testing.T) {
	t.Setenv("RAG_DOCS_DIR", "./data/docs")
	t.Setenv("RAG_CODE_DIR", "./data/code")
	t.Setenv("RAG_SCOPE_DEFAULT", "all")
	t.Setenv("RAG_CHUNK_SIZE", "500")
	t.Setenv("RAG_CHUNK_OVERLAP", "50")
	t.Setenv("RAG_ENABLE_CODE_INGEST", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.ChunkSize != 500 {
		t.Fatalf("ChunkSize = %d, want 500", cfg.ChunkSize)
	}
	if cfg.ChunkOverlap != 50 {
		t.Fatalf("ChunkOverlap = %d, want 50", cfg.ChunkOverlap)
	}
	if cfg.EnableCodeIngest {
		t.Fatal("EnableCodeIngest = true, want false")
	}
	if !filepath.IsAbs(cfg.DocsDir) || !filepath.IsAbs(cfg.CodeDir) {
		t.Fatal("expected absolute docs/code paths")
	}
	if cfg.Host != "127.0.0.1" {
		t.Fatalf("Host = %q, want 127.0.0.1", cfg.Host)
	}
	if cfg.Port != 8765 {
		t.Fatalf("Port = %d, want 8765", cfg.Port)
	}
}

func TestLoadValidation(t *testing.T) {
	for _, key := range []string{"RAG_CHUNK_SIZE", "RAG_CHUNK_OVERLAP", "RAG_SCOPE_DEFAULT", "RAG_HTTP_PORT", "RAG_MAX_TOP_K", "RAG_ENABLE_CODE_INGEST"} {
		_ = os.Unsetenv(key)
	}

	t.Setenv("RAG_CHUNK_SIZE", "10")
	t.Setenv("RAG_CHUNK_OVERLAP", "10")
	if _, err := Load(); err == nil {
		t.Fatal("expected validation error for overlap >= chunk size")
	}

	t.Setenv("RAG_CHUNK_SIZE", "10")
	t.Setenv("RAG_CHUNK_OVERLAP", "1")
	t.Setenv("RAG_SCOPE_DEFAULT", "invalid")
	if _, err := Load(); err == nil {
		t.Fatal("expected validation error for invalid scope")
	}

	t.Setenv("RAG_SCOPE_DEFAULT", "all")
	t.Setenv("RAG_HTTP_PORT", "0")
	if _, err := Load(); err == nil {
		t.Fatal("expected validation error for invalid port range")
	}

	t.Setenv("RAG_HTTP_PORT", "not-a-number")
	if _, err := Load(); err == nil {
		t.Fatal("expected validation error for invalid port")
	}

	t.Setenv("RAG_HTTP_PORT", "8765")
	t.Setenv("RAG_MAX_TOP_K", "0")
	if _, err := Load(); err == nil {
		t.Fatal("expected validation error for max top k")
	}

	t.Setenv("RAG_MAX_TOP_K", "50")
	t.Setenv("RAG_ENABLE_CODE_INGEST", "not-bool")
	if _, err := Load(); err == nil {
		t.Fatal("expected validation error for bool")
	}
}
