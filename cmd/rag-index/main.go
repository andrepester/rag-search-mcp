package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/andrepester/rag-search-mcp/internal/config"
	"github.com/andrepester/rag-search-mcp/internal/ingest"
	"github.com/andrepester/rag-search-mcp/internal/observability"
	"github.com/andrepester/rag-search-mcp/internal/ollama"
	"github.com/andrepester/rag-search-mcp/internal/store"
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
	logger := observability.NewFallbackLogger(os.Stdout, os.Getenv("RAG_LOG_LEVEL"), os.Getenv("RAG_LOG_FORMAT"))
	if err := run(context.Background(), logger); err != nil {
		logReindexError(context.Background(), componentLogger(logger, "rag-index"), err)
		os.Exit(1)
	}

}

func run(ctx context.Context, logger *slog.Logger) error {
	logger = componentLogger(logger, "rag-index")

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	ollamaClient := ollama.New(cfg.OllamaHost)
	chromaClient := store.NewChromaClient(cfg.ChromaURL, cfg.ChromaTenant, cfg.ChromaDatabase)
	ingestSvc := newIndexer(&cfg, ollamaClient, chromaClient)

	retryCtx, cancel := context.WithTimeout(ctx, reindexInitTimeout)
	defer cancel()

	start := time.Now()
	logger.InfoContext(ctx, "reindex started",
		slog.String("event", "reindex_start"),
		slog.String("trigger", "cli"),
		slog.String("docs_dir", cfg.DocsDir),
		slog.String("code_dir", cfg.CodeDir),
		slog.Bool("code_ingest", cfg.EnableCodeIngest),
		slog.String("collection", cfg.CollectionName),
	)

	stats, err := reindexWithRetry(retryCtx, ingestSvc)
	if err != nil {
		return err
	}

	logger.InfoContext(ctx, "reindex complete",
		slog.String("event", "reindex_complete"),
		slog.String("trigger", "cli"),
		slog.Int("files", stats.Files),
		slog.Int("docs_files", stats.DocsFiles),
		slog.Int("code_files", stats.CodeFiles),
		slog.Int("chunks", stats.Chunks),
		slog.Int64("duration_ms", time.Since(start).Milliseconds()),
	)
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

func componentLogger(logger *slog.Logger, component string) *slog.Logger {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return logger.With(slog.String("component", component))
}

func dependencyForReindexError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "ollama") || strings.Contains(msg, "embed") {
		return "ollama"
	}
	if strings.Contains(msg, "chroma") || strings.Contains(msg, "collection") {
		return "chroma"
	}
	return ""
}

func logReindexError(ctx context.Context, logger *slog.Logger, err error) {
	attrs := []slog.Attr{
		slog.String("event", "reindex_error"),
		slog.String("error", err.Error()),
	}
	if dependency := dependencyForReindexError(err); dependency != "" {
		attrs = append(attrs,
			slog.String("dependency", dependency),
			slog.String("hint", observability.DependencyHint(dependency)),
		)
	}
	logger.LogAttrs(ctx, slog.LevelError, "reindex failed", attrs...)
}
