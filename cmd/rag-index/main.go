package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
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
	"github.com/andrepester/rag-search-mcp/internal/reindexjob"
	"github.com/andrepester/rag-search-mcp/internal/store"
)

const (
	reindexTimeout        = 5 * time.Minute
	reindexInitMinBackoff = 250 * time.Millisecond
	reindexInitMaxBackoff = 3 * time.Second
	exitReindexBusy       = 2
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
	if err := runCommand(context.Background(), logger, os.Args[1:], os.Stdout); err != nil {
		logger := componentLogger(logger, "rag-index")
		if reindexjob.IsBusy(err) {
			logReindexBlocked(context.Background(), logger, err)
			os.Exit(exitReindexBusy)
		}
		logReindexError(context.Background(), logger, err)
		os.Exit(1)
	}

}

func run(ctx context.Context, logger *slog.Logger) error {
	return runReindex(ctx, logger)
}

func runCommand(ctx context.Context, logger *slog.Logger, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("rag-index", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	showStatus := flags.Bool("status", false, "print reindex job status as JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected argument: %s", flags.Arg(0))
	}
	if *showStatus {
		return writeReindexStatus(ctx, stdout)
	}
	return runReindex(ctx, logger)
}

func writeReindexStatus(ctx context.Context, stdout io.Writer) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}
	status, err := reindexjob.New(cfg.IndexStateDir).Status(ctx)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(status)
}

func runReindex(ctx context.Context, logger *slog.Logger) error {
	logger = componentLogger(logger, "rag-index")

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	ollamaClient := ollama.New(cfg.OllamaHost)
	chromaClient := store.NewChromaClient(cfg.ChromaURL, cfg.ChromaTenant, cfg.ChromaDatabase)
	ingestSvc := newIndexer(&cfg, ollamaClient, chromaClient)

	retryCtx, cancel := context.WithTimeout(ctx, reindexTimeout)
	defer cancel()

	run, err := reindexjob.New(cfg.IndexStateDir).Start(ctx, reindexjob.TriggerCLI)
	if err != nil {
		return err
	}

	start := time.Now()
	logger.InfoContext(ctx, "reindex started",
		slog.String("event", "reindex_start"),
		slog.String("trigger", "cli"),
		slog.String("job_id", run.Job.ID),
		slog.String("docs_dir", cfg.DocsDir),
		slog.String("code_dir", cfg.CodeDir),
		slog.Bool("code_ingest", cfg.EnableCodeIngest),
		slog.Bool("fresh_index", cfg.FreshIndex),
		slog.String("collection", cfg.CollectionName),
	)

	stats, err := reindexWithRetry(retryCtx, ingestSvc)
	if finishErr := run.Finish(ctx, stats, err); finishErr != nil {
		if err != nil {
			return errors.Join(err, finishErr)
		}
		return finishErr
	}
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
		slog.Int("changed_files", stats.ChangedFiles),
		slog.Int("deleted_files", stats.DeletedFiles),
		slog.Int("reused_files", stats.ReusedFiles),
		slog.Int("embedded_chunks", stats.EmbeddedChunks),
		slog.Int("reused_chunks", stats.ReusedChunks),
		slog.String("generation", stats.Generation),
		slog.String("job_id", run.Job.ID),
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

func logReindexBlocked(ctx context.Context, logger *slog.Logger, err error) {
	attrs := []slog.Attr{
		slog.String("event", "reindex_blocked"),
		slog.String("error", err.Error()),
	}
	if busy, ok := reindexjob.Busy(err); ok {
		attrs = append(attrs,
			slog.String("trigger", busy.BlockedStart.Trigger),
			slog.String("status", busy.BlockedStart.Status),
		)
		if busy.BlockedStart.ActiveJob != nil {
			attrs = append(attrs,
				slog.String("active_job_id", busy.BlockedStart.ActiveJob.ID),
				slog.String("active_trigger", busy.BlockedStart.ActiveJob.Trigger),
			)
		}
	}
	logger.LogAttrs(ctx, slog.LevelWarn, "reindex already running", attrs...)
}
