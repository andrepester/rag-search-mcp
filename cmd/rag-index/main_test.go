package main

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"local-rag/internal/config"
	"local-rag/internal/ingest"
	"local-rag/internal/ollama"
	"local-rag/internal/store"
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

	err := run(context.Background(), func(string, ...any) {})
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

	logged := ""
	err := run(context.Background(), func(format string, args ...any) {
		logged = format
	})
	if err != nil {
		t.Fatalf("run() failed: %v", err)
	}
	if !strings.Contains(logged, "reindex complete") {
		t.Fatalf("expected completion log, got %q", logged)
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
