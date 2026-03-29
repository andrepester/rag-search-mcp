package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsAndOverrides(t *testing.T) {
	t.Setenv("RAG_DOCS_DIR", "./docs")
	t.Setenv("RAG_CODE_DIR", "./.empty-code")
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
}

func TestLoadValidation(t *testing.T) {
	for _, key := range []string{"RAG_CHUNK_SIZE", "RAG_CHUNK_OVERLAP", "RAG_SCOPE_DEFAULT"} {
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
}
