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

type outputMode string

const (
	outputLogs  outputMode = "logs"
	outputHuman outputMode = "human"
	outputJSON  outputMode = "json"
)

type indexer interface {
	Reindex(ctx context.Context) (ingest.Stats, error)
}

type commandOptions struct {
	showStatus bool
	output     outputMode
}

type reindexResult struct {
	Job      reindexjob.Job
	Stats    ingest.Stats
	Duration time.Duration
}

type reindexOutput struct {
	OK             bool                     `json:"ok"`
	Status         string                   `json:"status"`
	JobID          string                   `json:"job_id,omitempty"`
	Trigger        string                   `json:"trigger,omitempty"`
	DurationMillis int64                    `json:"duration_ms"`
	Stats          ingest.Stats             `json:"stats,omitempty"`
	Error          string                   `json:"error,omitempty"`
	Dependency     string                   `json:"dependency,omitempty"`
	Hint           string                   `json:"hint,omitempty"`
	ActiveJob      *reindexjob.Job          `json:"active_job,omitempty"`
	BlockedStart   *reindexjob.BlockedStart `json:"blocked_start,omitempty"`
}

var (
	loadConfig = config.Load
	newIndexer = func(cfg *config.Config, ollamaClient *ollama.Client, chromaClient *store.ChromaClient) indexer {
		return &ingest.Service{Config: cfg, Ollama: ollamaClient, Chroma: chromaClient}
	}
)

func main() {
	if code := runCLI(context.Background(), os.Args[1:], os.Stdout, os.Stderr); code != 0 {
		os.Exit(code)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	_, err := runReindex(ctx, logger)
	return err
}

func runCommand(ctx context.Context, logger *slog.Logger, args []string, stdout io.Writer) error {
	opts, err := parseCommandArgs(args)
	if err != nil {
		return err
	}
	return runCommandWithOptions(ctx, logger, opts, stdout)
}

func runCLI(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	opts, err := parseCommandArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "rag-index: %v\n", err)
		return 1
	}

	logWriter := stdout
	if !opts.showStatus && opts.output != outputLogs {
		logWriter = io.Discard
	}
	logger := observability.NewFallbackLogger(logWriter, os.Getenv("RAG_LOG_LEVEL"), os.Getenv("RAG_LOG_FORMAT"))

	if err := runCommandWithOptions(ctx, logger, opts, stdout); err != nil {
		renderCommandError(ctx, logger, opts.output, stderr, err)
		if reindexjob.IsBusy(err) {
			return exitReindexBusy
		}
		return 1
	}
	return 0
}

func parseCommandArgs(args []string) (commandOptions, error) {
	flags := flag.NewFlagSet("rag-index", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	showStatus := flags.Bool("status", false, "print reindex job status as JSON")
	outputRaw := flags.String("output", string(outputLogs), "reindex output mode: logs, human, or json")
	if err := flags.Parse(args); err != nil {
		return commandOptions{}, err
	}
	if flags.NArg() > 0 {
		return commandOptions{}, fmt.Errorf("unexpected argument: %s", flags.Arg(0))
	}
	output, err := parseOutputMode(*outputRaw)
	if err != nil {
		return commandOptions{}, err
	}
	return commandOptions{showStatus: *showStatus, output: output}, nil
}

func parseOutputMode(raw string) (outputMode, error) {
	switch outputMode(strings.ToLower(strings.TrimSpace(raw))) {
	case outputLogs:
		return outputLogs, nil
	case outputHuman:
		return outputHuman, nil
	case outputJSON:
		return outputJSON, nil
	default:
		return "", fmt.Errorf("invalid output %q: must be one of logs, human, json", raw)
	}
}

func runCommandWithOptions(ctx context.Context, logger *slog.Logger, opts commandOptions, stdout io.Writer) error {
	if opts.showStatus {
		return writeReindexStatus(ctx, stdout)
	}

	runLogger := logger
	if opts.output != outputLogs {
		runLogger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	result, err := runReindex(ctx, runLogger)
	if err != nil {
		return err
	}

	switch opts.output {
	case outputHuman:
		return renderHumanResult(stdout, result)
	case outputJSON:
		return renderJSONResult(stdout, result)
	default:
		return nil
	}
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

func runReindex(ctx context.Context, logger *slog.Logger) (reindexResult, error) {
	logger = componentLogger(logger, "rag-index")

	cfg, err := loadConfig()
	if err != nil {
		return reindexResult{}, fmt.Errorf("invalid configuration: %w", err)
	}

	ollamaClient := ollama.New(cfg.OllamaHost)
	chromaClient := store.NewChromaClient(cfg.ChromaURL, cfg.ChromaTenant, cfg.ChromaDatabase)
	ingestSvc := newIndexer(&cfg, ollamaClient, chromaClient)

	retryCtx, cancel := context.WithTimeout(ctx, reindexTimeout)
	defer cancel()

	run, err := reindexjob.New(cfg.IndexStateDir).Start(ctx, reindexjob.TriggerCLI)
	if err != nil {
		return reindexResult{}, err
	}

	start := time.Now()
	result := reindexResult{Job: run.Job}
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
	result.Stats = stats
	result.Duration = time.Since(start)
	if finishErr := run.Finish(ctx, stats, err); finishErr != nil {
		if err != nil {
			return result, errors.Join(err, finishErr)
		}
		return result, finishErr
	}
	if err != nil {
		return result, err
	}
	result.Duration = time.Since(start)

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
		slog.Int64("duration_ms", result.Duration.Milliseconds()),
	)
	return result, nil
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

func renderHumanResult(stdout io.Writer, result reindexResult) error {
	_, err := fmt.Fprintf(stdout,
		"index: complete\njob: %s\nduration: %s\nfiles: %d total, %d docs, %d code\nchunks: %d total, %d embedded, %d reused\nchanges: %d changed, %d deleted, %d reused files\ngeneration: %s\n",
		result.Job.ID,
		result.Duration.Round(time.Millisecond),
		result.Stats.Files,
		result.Stats.DocsFiles,
		result.Stats.CodeFiles,
		result.Stats.Chunks,
		result.Stats.EmbeddedChunks,
		result.Stats.ReusedChunks,
		result.Stats.ChangedFiles,
		result.Stats.DeletedFiles,
		result.Stats.ReusedFiles,
		result.Stats.Generation,
	)
	return err
}

func renderJSONResult(stdout io.Writer, result reindexResult) error {
	encoder := json.NewEncoder(stdout)
	return encoder.Encode(reindexOutput{
		OK:             true,
		Status:         reindexjob.StatusSucceeded,
		JobID:          result.Job.ID,
		Trigger:        result.Job.Trigger,
		DurationMillis: result.Duration.Milliseconds(),
		Stats:          result.Stats,
	})
}

func renderCommandError(ctx context.Context, logger *slog.Logger, mode outputMode, stderr io.Writer, err error) {
	if mode == outputLogs {
		logger := componentLogger(logger, "rag-index")
		if reindexjob.IsBusy(err) {
			logReindexBlocked(ctx, logger, err)
			return
		}
		logReindexError(ctx, logger, err)
		return
	}

	output := reindexOutput{
		OK:     false,
		Status: reindexjob.StatusFailed,
		Error:  err.Error(),
	}
	if busy, ok := reindexjob.Busy(err); ok {
		blocked := busy.BlockedStart
		output.Status = reindexjob.StatusBlocked
		output.Error = blocked.Error
		output.Trigger = blocked.Trigger
		output.ActiveJob = blocked.ActiveJob
		output.BlockedStart = &blocked
	}
	if dependency := dependencyForReindexError(err); dependency != "" {
		output.Dependency = dependency
		output.Hint = observability.DependencyHint(dependency)
	}

	if mode == outputJSON {
		if encodeErr := json.NewEncoder(stderr).Encode(output); encodeErr == nil {
			return
		}
	}
	renderHumanError(stderr, output)
}

func renderHumanError(stderr io.Writer, output reindexOutput) {
	if output.Status == reindexjob.StatusBlocked {
		fmt.Fprintln(stderr, "index: blocked")
	} else {
		fmt.Fprintln(stderr, "index: failed")
	}
	if output.Error != "" {
		fmt.Fprintf(stderr, "error: %s\n", output.Error)
	}
	if output.Trigger != "" {
		fmt.Fprintf(stderr, "trigger: %s\n", output.Trigger)
	}
	if output.ActiveJob != nil {
		fmt.Fprintf(stderr, "active_job: %s\n", output.ActiveJob.ID)
		if output.ActiveJob.Trigger != "" {
			fmt.Fprintf(stderr, "active_trigger: %s\n", output.ActiveJob.Trigger)
		}
	}
	if output.Dependency != "" {
		fmt.Fprintf(stderr, "dependency: %s\n", output.Dependency)
	}
	if output.Hint != "" {
		fmt.Fprintf(stderr, "hint: %s\n", output.Hint)
	}
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
