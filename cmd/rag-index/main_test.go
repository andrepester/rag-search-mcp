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
	"github.com/andrepester/rag-search-mcp/internal/reindexjob"
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
		return testConfig(t.TempDir()), nil
	}

	newIndexer = func(*config.Config, *ollama.Client, *store.ChromaClient) indexer {
		return fakeIndexer{reindex: func(context.Context) (ingest.Stats, error) {
			return ingest.Stats{Files: 1, DocsFiles: 1, CodeFiles: 0, Chunks: 3}, nil
		}}
	}

	var logs bytes.Buffer
	var out bytes.Buffer
	err := runCommand(context.Background(), slog.New(slog.NewJSONHandler(&logs, nil)), []string{"--output=logs"}, &out)
	if err != nil {
		t.Fatalf("runCommand(--output=logs) failed: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q, want empty in logs mode", out.String())
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
	if completion["job_id"] == "" {
		t.Fatalf("missing job_id in completion log: %+v", completion)
	}
}

func TestRunCommandHumanOutput(t *testing.T) {
	originalLoadConfig := loadConfig
	originalNewIndexer := newIndexer
	t.Cleanup(func() {
		loadConfig = originalLoadConfig
		newIndexer = originalNewIndexer
	})

	loadConfig = func() (config.Config, error) {
		return testConfig(t.TempDir()), nil
	}

	newIndexer = func(*config.Config, *ollama.Client, *store.ChromaClient) indexer {
		return fakeIndexer{reindex: func(context.Context) (ingest.Stats, error) {
			return ingest.Stats{
				Files:          106,
				DocsFiles:      105,
				CodeFiles:      1,
				Chunks:         176,
				ChangedFiles:   0,
				DeletedFiles:   0,
				ReusedFiles:    106,
				EmbeddedChunks: 0,
				ReusedChunks:   176,
				Generation:     "gen-human",
				IndexSubdir:    "docs/demo/technology",
			}, nil
		}}
	}

	var out bytes.Buffer
	var logs bytes.Buffer
	err := runCommand(context.Background(), slog.New(slog.NewJSONHandler(&logs, nil)), []string{"--output=human"}, &out)
	if err != nil {
		t.Fatalf("runCommand(--output=human) failed: %v", err)
	}

	output := out.String()
	for _, want := range []string{
		"index: complete",
		"job: reindex-",
		"files: 106 total, 105 docs, 1 code",
		"chunks: 176 total, 0 embedded, 176 reused",
		"changes: 0 changed, 0 deleted, 106 reused files",
		"generation: gen-human",
		"index_subdir: docs/demo/technology",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("human output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, `"event":"reindex_complete"`) {
		t.Fatalf("human output contains raw JSON log:\n%s", output)
	}
	if strings.Contains(logs.String(), "reindex_complete") {
		t.Fatalf("human mode wrote runtime logs: %s", logs.String())
	}
}

func TestRunCommandJSONOutput(t *testing.T) {
	originalLoadConfig := loadConfig
	originalNewIndexer := newIndexer
	t.Cleanup(func() {
		loadConfig = originalLoadConfig
		newIndexer = originalNewIndexer
	})

	loadConfig = func() (config.Config, error) {
		return testConfig(t.TempDir()), nil
	}

	newIndexer = func(*config.Config, *ollama.Client, *store.ChromaClient) indexer {
		return fakeIndexer{reindex: func(context.Context) (ingest.Stats, error) {
			return ingest.Stats{
				Files:       2,
				DocsFiles:   1,
				CodeFiles:   1,
				Chunks:      4,
				Generation:  "gen-json",
				IndexSubdir: "code/internal/ingest",
			}, nil
		}}
	}

	var out bytes.Buffer
	var logs bytes.Buffer
	if err := runCommand(context.Background(), slog.New(slog.NewJSONHandler(&logs, nil)), []string{"--output=json"}, &out); err != nil {
		t.Fatalf("runCommand(--output=json) failed: %v", err)
	}

	var result struct {
		OK             bool         `json:"ok"`
		Status         string       `json:"status"`
		JobID          string       `json:"job_id"`
		DurationMillis int64        `json:"duration_ms"`
		Stats          ingest.Stats `json:"stats"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal JSON output: %v\n%s", err, out.String())
	}
	if !result.OK || result.Status != reindexjob.StatusSucceeded {
		t.Fatalf("result status = %+v", result)
	}
	if result.JobID == "" {
		t.Fatalf("missing job ID in %+v", result)
	}
	if result.Stats.Chunks != 4 || result.Stats.Generation != "gen-json" || result.Stats.IndexSubdir != "code/internal/ingest" {
		t.Fatalf("stats = %+v", result.Stats)
	}
	if strings.Contains(logs.String(), "reindex_complete") {
		t.Fatalf("json mode wrote runtime logs: %s", logs.String())
	}
}

func TestRunBusyDoesNotRetry(t *testing.T) {
	originalLoadConfig := loadConfig
	originalNewIndexer := newIndexer
	t.Cleanup(func() {
		loadConfig = originalLoadConfig
		newIndexer = originalNewIndexer
	})

	indexStateDir := t.TempDir()
	loadConfig = func() (config.Config, error) {
		return testConfig(indexStateDir), nil
	}

	held, err := reindexjob.New(indexStateDir).Start(context.Background(), reindexjob.TriggerCLI)
	if err != nil {
		t.Fatalf("holding reindex lock failed: %v", err)
	}
	defer func() {
		_ = held.Finish(context.Background(), ingest.Stats{}, nil)
	}()

	var attempts atomic.Int32
	newIndexer = func(*config.Config, *ollama.Client, *store.ChromaClient) indexer {
		return fakeIndexer{reindex: func(context.Context) (ingest.Stats, error) {
			attempts.Add(1)
			return ingest.Stats{}, nil
		}}
	}

	err = run(context.Background(), discardLogger())
	if !reindexjob.IsBusy(err) {
		t.Fatalf("run() error = %T %v, want busy", err, err)
	}
	if attempts.Load() != 0 {
		t.Fatalf("reindex attempts = %d, want 0", attempts.Load())
	}
}

func TestRunCLIHumanBusyOutput(t *testing.T) {
	originalLoadConfig := loadConfig
	originalNewIndexer := newIndexer
	t.Cleanup(func() {
		loadConfig = originalLoadConfig
		newIndexer = originalNewIndexer
	})

	indexStateDir := t.TempDir()
	loadConfig = func() (config.Config, error) {
		return testConfig(indexStateDir), nil
	}

	held, err := reindexjob.New(indexStateDir).Start(context.Background(), reindexjob.TriggerMCPTool)
	if err != nil {
		t.Fatalf("holding reindex lock failed: %v", err)
	}
	defer func() {
		_ = held.Finish(context.Background(), ingest.Stats{}, nil)
	}()

	var attempts atomic.Int32
	newIndexer = func(*config.Config, *ollama.Client, *store.ChromaClient) indexer {
		return fakeIndexer{reindex: func(context.Context) (ingest.Stats, error) {
			attempts.Add(1)
			return ingest.Stats{}, nil
		}}
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runCLI(context.Background(), []string{"--output=human"}, &stdout, &stderr)
	if code != exitReindexBusy {
		t.Fatalf("exit code = %d, want %d; stderr:\n%s", code, exitReindexBusy, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	for _, want := range []string{
		"index: blocked",
		"error: already_running",
		"trigger: cli",
		"active_job: " + held.Job.ID,
		"active_trigger: mcp_tool",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr missing %q:\n%s", want, stderr.String())
		}
	}
	if attempts.Load() != 0 {
		t.Fatalf("reindex attempts = %d, want 0", attempts.Load())
	}
}

func TestRunCommandStatusOutputsJSON(t *testing.T) {
	originalLoadConfig := loadConfig
	t.Cleanup(func() {
		loadConfig = originalLoadConfig
	})

	indexStateDir := t.TempDir()
	loadConfig = func() (config.Config, error) {
		return config.Config{IndexStateDir: indexStateDir}, nil
	}
	run, err := reindexjob.New(indexStateDir).Start(context.Background(), reindexjob.TriggerCLI)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	if err := run.Finish(context.Background(), ingest.Stats{Generation: "gen-status"}, nil); err != nil {
		t.Fatalf("Finish() failed: %v", err)
	}

	var out bytes.Buffer
	if err := runCommand(context.Background(), discardLogger(), []string{"--status"}, &out); err != nil {
		t.Fatalf("runCommand(--status) failed: %v", err)
	}
	var status reindexjob.Status
	if err := json.Unmarshal(out.Bytes(), &status); err != nil {
		t.Fatalf("unmarshal status: %v\n%s", err, out.String())
	}
	if status.Status != reindexjob.StatusSucceeded || status.LastRun == nil || status.LastRun.Generation != "gen-status" {
		t.Fatalf("status = %+v", status)
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

	stats, err := reindexWithRetry(ctx, fakeIndexer{reindex: func(context.Context) (ingest.Stats, error) {
		return ingest.Stats{Generation: "gen-draft"}, errors.New("still failing")
	}})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if stats.Generation != "gen-draft" {
		t.Fatalf("Generation after timeout = %q, want gen-draft", stats.Generation)
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

func testConfig(indexStateDir string) config.Config {
	return config.Config{
		OllamaHost:     "http://127.0.0.1:11434",
		ChromaURL:      "http://127.0.0.1:8000",
		ChromaTenant:   "default_tenant",
		ChromaDatabase: "default_database",
		IndexStateDir:  indexStateDir,
		ReindexTimeout: time.Minute,
	}
}
