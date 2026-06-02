package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/andrepester/rag-search-mcp/internal/config"
	"github.com/andrepester/rag-search-mcp/internal/ingest"
	"github.com/andrepester/rag-search-mcp/internal/ollama"
	"github.com/andrepester/rag-search-mcp/internal/store"
)

type fakeIndexer struct {
	reindex func(context.Context) (ingest.Stats, error)
}

func (f fakeIndexer) Reindex(ctx context.Context) (ingest.Stats, error) {
	return f.reindex(ctx)
}

func TestRunConfigError(t *testing.T) {
	originalLoadConfig := loadConfig
	originalNewIndexer := newIndexer
	t.Cleanup(func() {
		loadConfig = originalLoadConfig
		newIndexer = originalNewIndexer
	})

	loadConfig = func() (config.Config, error) {
		return config.Config{}, errors.New("bad env")
	}

	err := run(context.Background(), discardLogger())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "invalid configuration") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunSuccess(t *testing.T) {
	originalLoadConfig := loadConfig
	originalNewIndexer := newIndexer
	t.Cleanup(func() {
		loadConfig = originalLoadConfig
		newIndexer = originalNewIndexer
	})

	loadConfig = func() (config.Config, error) {
		return config.Config{
			OllamaHost:     "http://127.0.0.1:11434",
			ChromaURL:      "http://127.0.0.1:8000",
			ChromaTenant:   "default_tenant",
			ChromaDatabase: "default_database",
		}, nil
	}

	newIndexer = func(*config.Config, *ollama.Client, *store.ChromaClient) indexer {
		return fakeIndexer{reindex: func(context.Context) (ingest.Stats, error) {
			return ingest.Stats{Files: 1, DocsFiles: 1, CodeFiles: 0, Chunks: 3}, nil
		}}
	}

	var logs bytes.Buffer
	err := run(context.Background(), slog.New(slog.NewJSONHandler(&logs, nil)))
	if err != nil {
		t.Fatalf("run() failed: %v", err)
	}

	var completion map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("unmarshal log line: %v\n%s", err, line)
		}
		if record["event"] == "reindex_complete" {
			completion = record
			break
		}
	}
	if completion == nil {
		t.Fatalf("missing reindex_complete log in %s", logs.String())
	}
	if completion["chunks"] != float64(3) {
		t.Fatalf("chunks = %v, want 3", completion["chunks"])
	}
}

func TestReindexWithRetryEventuallySucceeds(t *testing.T) {
	var attempts atomic.Int32

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	stats, err := reindexWithRetry(ctx, fakeIndexer{reindex: func(context.Context) (ingest.Stats, error) {
		if attempts.Add(1) < 3 {
			return ingest.Stats{}, errors.New("not ready")
		}
		return ingest.Stats{Chunks: 7}, nil
	}})
	if err != nil {
		t.Fatalf("reindexWithRetry() failed: %v", err)
	}
	if stats.Chunks != 7 {
		t.Fatalf("Chunks = %d, want 7", stats.Chunks)
	}
	if attempts.Load() < 3 {
		t.Fatalf("attempts = %d, want >= 3", attempts.Load())
	}
}

func TestReindexWithRetryTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	if _, err := reindexWithRetry(ctx, fakeIndexer{reindex: func(context.Context) (ingest.Stats, error) {
		return ingest.Stats{}, errors.New("still failing")
	}}); err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestDependencyForReindexError(t *testing.T) {
	if got := dependencyForReindexError(errors.New("embed batch: failed")); got != "ollama" {
		t.Fatalf("dependency = %q, want ollama", got)
	}
	if got := dependencyForReindexError(errors.New("ensure collection before reset: failed")); got != "chroma" {
		t.Fatalf("dependency = %q, want chroma", got)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
