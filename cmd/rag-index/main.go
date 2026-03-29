package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"local-rag/internal/config"
	"local-rag/internal/ingest"
	"local-rag/internal/ollama"
	"local-rag/internal/store"
)

const (
	reindexInitTimeout    = 45 * time.Second
	reindexInitMinBackoff = 250 * time.Millisecond
	reindexInitMaxBackoff = 3 * time.Second
)

type indexer interface {
	Reindex(ctx context.Context) (ingest.Stats, error)
}

var (
	loadConfig = config.Load
	newIndexer = func(cfg *config.Config, ollamaClient *ollama.Client, chromaClient *store.ChromaClient) indexer {
		return &ingest.Service{Config: cfg, Ollama: ollamaClient, Chroma: chromaClient}
	}
)

func main() {
	if err := run(context.Background(), log.Printf); err != nil {
		log.Fatalf("reindex failed: %v", err)
	}

}

func run(ctx context.Context, logf func(string, ...any)) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	ollamaClient := ollama.New(cfg.OllamaHost)
	chromaClient := store.NewChromaClient(cfg.ChromaURL, cfg.ChromaTenant, cfg.ChromaDatabase)
	ingestSvc := newIndexer(&cfg, ollamaClient, chromaClient)

	retryCtx, cancel := context.WithTimeout(ctx, reindexInitTimeout)
	defer cancel()

	stats, err := reindexWithRetry(retryCtx, ingestSvc)
	if err != nil {
		return err
	}

	logf("reindex complete: files=%d docs_files=%d code_files=%d chunks=%d", stats.Files, stats.DocsFiles, stats.CodeFiles, stats.Chunks)
	return nil
}

func reindexWithRetry(ctx context.Context, idx indexer) (ingest.Stats, error) {
	backoff := reindexInitMinBackoff
	var lastErr error

	for {
		stats, err := idx.Reindex(ctx)
		if err == nil {
			return stats, nil
		}
		lastErr = err

		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return ingest.Stats{}, fmt.Errorf("reindex timeout after retries: %w", lastErr)
			}
			return ingest.Stats{}, fmt.Errorf("reindex canceled: %w", ctx.Err())
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > reindexInitMaxBackoff {
			backoff = reindexInitMaxBackoff
		}
	}
}
