package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaultsAndOverrides(t *testing.T) {
	t.Setenv("RAG_DOCS_DIR", "./data/docs")
	t.Setenv("RAG_CODE_DIR", "./data/code")
	t.Setenv("RAG_SCOPE_DEFAULT", "all")
	t.Setenv("RAG_CHUNK_SIZE", "500")
	t.Setenv("RAG_CHUNK_OVERLAP", "50")
	t.Setenv("RAG_MAX_SEARCH_DISTANCE", "0.35")
	t.Setenv("RAG_ENABLE_CODE_INGEST", "false")
	t.Setenv("FRESH_INDEX", "true")
	t.Setenv("RAG_INDEX_LIMIT", "10")
	t.Setenv("RAG_INDEX_SUBDIR", "docs/demo//technology")
	t.Setenv("RAG_EMBED_CONCURRENCY", "3")
	t.Setenv("RAG_EMBED_NUM_THREADS", "4")
	t.Setenv("RAG_REINDEX_TIMEOUT", "45m")
	t.Setenv("OLLAMA_HOST", "http://ollama.example.internal:11434")

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
	if cfg.MaxSearchDistance != 0.35 {
		t.Fatalf("MaxSearchDistance = %f, want 0.35", cfg.MaxSearchDistance)
	}
	if cfg.EnableCodeIngest {
		t.Fatal("EnableCodeIngest = true, want false")
	}
	if !cfg.FreshIndex {
		t.Fatal("FreshIndex = false, want true")
	}
	if cfg.IndexLimit != 10 {
		t.Fatalf("IndexLimit = %d, want 10", cfg.IndexLimit)
	}
	if cfg.IndexSubdir != "docs/demo/technology" {
		t.Fatalf("IndexSubdir = %q, want docs/demo/technology", cfg.IndexSubdir)
	}
	if cfg.EmbedConcurrency != 3 {
		t.Fatalf("EmbedConcurrency = %d, want 3", cfg.EmbedConcurrency)
	}
	if cfg.EmbedNumThreads != 4 {
		t.Fatalf("EmbedNumThreads = %d, want 4", cfg.EmbedNumThreads)
	}
	if cfg.ReindexTimeout != 45*time.Minute {
		t.Fatalf("ReindexTimeout = %s, want 45m", cfg.ReindexTimeout)
	}
	if !filepath.IsAbs(cfg.DocsDir) || !filepath.IsAbs(cfg.CodeDir) || !filepath.IsAbs(cfg.IndexStateDir) {
		t.Fatal("expected absolute docs/code/index state paths")
	}
	if cfg.Host != "127.0.0.1" {
		t.Fatalf("Host = %q, want 127.0.0.1", cfg.Host)
	}
	if cfg.Port != 8765 {
		t.Fatalf("Port = %d, want 8765", cfg.Port)
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("LogLevel = %q, want info", cfg.LogLevel)
	}
	if cfg.LogFormat != "json" {
		t.Fatalf("LogFormat = %q, want json", cfg.LogFormat)
	}
}

func TestLoadValidation(t *testing.T) {
	for _, key := range []string{"RAG_CHUNK_SIZE", "RAG_CHUNK_OVERLAP", "RAG_SCOPE_DEFAULT", "RAG_HTTP_PORT", "RAG_MAX_TOP_K", "RAG_MAX_SEARCH_DISTANCE", "RAG_ENABLE_CODE_INGEST", "FRESH_INDEX", "RAG_INDEX_LIMIT", "RAG_INDEX_SUBDIR", "RAG_EMBED_CONCURRENCY", "RAG_EMBED_NUM_THREADS", "RAG_REINDEX_TIMEOUT", "RAG_LOG_LEVEL", "RAG_LOG_FORMAT", "OLLAMA_HOST"} {
		_ = os.Unsetenv(key)
	}
	t.Setenv("OLLAMA_HOST", "http://ollama.example.internal:11434")

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
	t.Setenv("RAG_MAX_SEARCH_DISTANCE", "not-a-number")
	if _, err := Load(); err == nil {
		t.Fatal("expected validation error for max search distance number")
	}

	t.Setenv("RAG_MAX_SEARCH_DISTANCE", "2.01")
	if _, err := Load(); err == nil {
		t.Fatal("expected validation error for max search distance range")
	}

	t.Setenv("RAG_MAX_SEARCH_DISTANCE", "0.50")
	t.Setenv("RAG_ENABLE_CODE_INGEST", "not-bool")
	if _, err := Load(); err == nil {
		t.Fatal("expected validation error for bool")
	}

	t.Setenv("RAG_ENABLE_CODE_INGEST", "true")
	t.Setenv("FRESH_INDEX", "not-bool")
	if _, err := Load(); err == nil {
		t.Fatal("expected validation error for fresh index bool")
	}

	t.Setenv("FRESH_INDEX", "false")
	t.Setenv("RAG_INDEX_LIMIT", "not-a-number")
	if _, err := Load(); err == nil {
		t.Fatal("expected validation error for index limit integer")
	}

	t.Setenv("RAG_INDEX_LIMIT", "-1")
	if _, err := Load(); err == nil {
		t.Fatal("expected validation error for index limit range")
	}

	t.Setenv("RAG_INDEX_LIMIT", "0")
	t.Setenv("RAG_EMBED_CONCURRENCY", "not-a-number")
	if _, err := Load(); err == nil {
		t.Fatal("expected validation error for embed concurrency integer")
	}

	t.Setenv("RAG_EMBED_CONCURRENCY", "0")
	if _, err := Load(); err == nil {
		t.Fatal("expected validation error for embed concurrency range")
	}

	t.Setenv("RAG_EMBED_CONCURRENCY", "2")
	t.Setenv("RAG_EMBED_NUM_THREADS", "not-a-number")
	if _, err := Load(); err == nil {
		t.Fatal("expected validation error for embed num threads integer")
	}

	t.Setenv("RAG_EMBED_NUM_THREADS", "-1")
	if _, err := Load(); err == nil {
		t.Fatal("expected validation error for embed num threads range")
	}

	t.Setenv("RAG_EMBED_NUM_THREADS", "0")
	t.Setenv("RAG_REINDEX_TIMEOUT", "not-a-duration")
	if _, err := Load(); err == nil {
		t.Fatal("expected validation error for reindex timeout duration")
	}

	t.Setenv("RAG_REINDEX_TIMEOUT", "0s")
	if _, err := Load(); err == nil {
		t.Fatal("expected validation error for reindex timeout range")
	}

	t.Setenv("RAG_REINDEX_TIMEOUT", "30m")
	t.Setenv("RAG_LOG_LEVEL", "invalid")
	if _, err := Load(); err == nil {
		t.Fatal("expected validation error for log level")
	}

	t.Setenv("RAG_LOG_LEVEL", "info")
	t.Setenv("RAG_LOG_FORMAT", "yaml")
	if _, err := Load(); err == nil {
		t.Fatal("expected validation error for log format")
	}

	t.Setenv("RAG_LOG_FORMAT", "json")
	t.Setenv("OLLAMA_HOST", " ")
	if _, err := Load(); err == nil {
		t.Fatal("expected validation error for missing ollama host")
	}
}

func TestLoadIndexSubdirValidation(t *testing.T) {
	tests := []struct {
		name             string
		raw              string
		enableCodeIngest string
		want             string
		wantErr          bool
	}{
		{name: "docs subdir", raw: "docs/demo/technology", enableCodeIngest: "true", want: "docs/demo/technology"},
		{name: "code subdir", raw: "code/internal\\ingest", enableCodeIngest: "true", want: "code/internal/ingest"},
		{name: "empty", raw: "  ", enableCodeIngest: "true", wantErr: true},
		{name: "absolute", raw: "/data/docs", enableCodeIngest: "true", wantErr: true},
		{name: "traversal", raw: "docs/../secrets", enableCodeIngest: "true", wantErr: true},
		{name: "missing scope prefix", raw: "demo/technology", enableCodeIngest: "true", wantErr: true},
		{name: "scope root only", raw: "docs/", enableCodeIngest: "true", wantErr: true},
		{name: "unknown scope", raw: "assets/demo", enableCodeIngest: "true", wantErr: true},
		{name: "code disabled", raw: "code/internal/ingest", enableCodeIngest: "false", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OLLAMA_HOST", "http://ollama.example.internal:11434")
			t.Setenv("RAG_ENABLE_CODE_INGEST", tt.enableCodeIngest)
			t.Setenv("RAG_INDEX_SUBDIR", tt.raw)

			cfg, err := Load()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Load() succeeded, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() failed: %v", err)
			}
			if cfg.IndexSubdir != tt.want {
				t.Fatalf("IndexSubdir = %q, want %q", cfg.IndexSubdir, tt.want)
			}
		})
	}
}
