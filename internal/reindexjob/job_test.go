package reindexjob

import (
	"context"
	"errors"
	"testing"

	"github.com/andrepester/rag-search-mcp/internal/ingest"
)

func TestCoordinatorBlocksConcurrentRunsAndRecordsStatus(t *testing.T) {
	ctx := context.Background()
	coord := New(t.TempDir())

	run, err := coord.Start(ctx, TriggerCLI)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	if run.Job.ID == "" {
		t.Fatal("expected job id")
	}

	status, err := coord.Status(ctx)
	if err != nil {
		t.Fatalf("Status() failed: %v", err)
	}
	if status.Status != StatusRunning || status.ActiveJob == nil || status.ActiveJob.ID != run.Job.ID {
		t.Fatalf("running status = %+v, want active job %s", status, run.Job.ID)
	}

	_, err = coord.Start(ctx, TriggerMCPTool)
	if err == nil {
		t.Fatal("expected busy error")
	}
	busy, ok := Busy(err)
	if !ok {
		t.Fatalf("Start() error = %T %v, want BusyError", err, err)
	}
	if busy.BlockedStart.Status != StatusBlocked || busy.BlockedStart.Error != ErrorAlreadyRunning {
		t.Fatalf("blocked status = %+v", busy.BlockedStart)
	}
	if busy.BlockedStart.ActiveJob == nil || busy.BlockedStart.ActiveJob.ID != run.Job.ID {
		t.Fatalf("blocked active job = %+v, want %s", busy.BlockedStart.ActiveJob, run.Job.ID)
	}

	status, err = coord.Status(ctx)
	if err != nil {
		t.Fatalf("Status() after blocked start failed: %v", err)
	}
	if status.LastBlockedStart == nil || status.LastBlockedStart.Status != StatusBlocked {
		t.Fatalf("missing blocked start status: %+v", status)
	}

	stats := ingest.Stats{Files: 2, DocsFiles: 1, CodeFiles: 1, Chunks: 4, Generation: "gen-1"}
	if err := run.Finish(ctx, stats, nil); err != nil {
		t.Fatalf("Finish() failed: %v", err)
	}

	status, err = coord.Status(ctx)
	if err != nil {
		t.Fatalf("Status() after finish failed: %v", err)
	}
	if status.Status != StatusSucceeded || status.ActiveJob != nil {
		t.Fatalf("finished status = %+v, want succeeded without active job", status)
	}
	if status.LastRun == nil || status.LastRun.Generation != "gen-1" || status.LastRun.Chunks != 4 {
		t.Fatalf("last run = %+v", status.LastRun)
	}
	if status.LastBlockedStart == nil {
		t.Fatal("expected blocked start metadata to be preserved")
	}

	next, err := coord.Start(ctx, TriggerCLI)
	if err != nil {
		t.Fatalf("Start() after release failed: %v", err)
	}
	if err := next.Finish(ctx, ingest.Stats{Generation: "gen-2"}, nil); err != nil {
		t.Fatalf("Finish() second run failed: %v", err)
	}
}

func TestRunProgressIsRecordedAndFinished(t *testing.T) {
	ctx := context.Background()
	coord := New(t.TempDir())

	run, err := coord.Start(ctx, TriggerCLI)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	if err := run.UpdateProgress(ctx, Progress{TotalDocuments: 3, ProcessedDocuments: 1}); err != nil {
		t.Fatalf("UpdateProgress() failed: %v", err)
	}

	status, err := coord.Status(ctx)
	if err != nil {
		t.Fatalf("Status() failed: %v", err)
	}
	if status.Progress == nil || status.Progress.TotalDocuments != 3 || status.Progress.ProcessedDocuments != 1 {
		t.Fatalf("progress = %+v, want 1/3", status.Progress)
	}

	if err := run.UpdateProgress(ctx, Progress{TotalDocuments: 3, ProcessedDocuments: 9}); err != nil {
		t.Fatalf("UpdateProgress() clamp case failed: %v", err)
	}
	status, err = coord.Status(ctx)
	if err != nil {
		t.Fatalf("Status() after clamp failed: %v", err)
	}
	if status.Progress == nil || status.Progress.ProcessedDocuments != 3 {
		t.Fatalf("clamped progress = %+v, want 3/3", status.Progress)
	}

	if err := run.Finish(ctx, ingest.Stats{Files: 3, DocsFiles: 3, Generation: "gen-progress"}, nil); err != nil {
		t.Fatalf("Finish() failed: %v", err)
	}
	status, err = coord.Status(ctx)
	if err != nil {
		t.Fatalf("Status() after finish failed: %v", err)
	}
	if status.Progress != nil {
		t.Fatalf("finished status progress = %+v, want nil", status.Progress)
	}
	if status.LastRun == nil || status.LastRun.TotalDocuments != 3 || status.LastRun.ProcessedDocuments != 3 {
		t.Fatalf("last run progress = %+v, want 3/3", status.LastRun)
	}
}

func TestFailedRunPreservesPartialProgress(t *testing.T) {
	ctx := context.Background()
	coord := New(t.TempDir())

	run, err := coord.Start(ctx, TriggerCLI)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	if err := run.UpdateProgress(ctx, Progress{TotalDocuments: 5, ProcessedDocuments: 2}); err != nil {
		t.Fatalf("UpdateProgress() failed: %v", err)
	}

	runErr := errors.New("embed batch: failed")
	if err := run.Finish(ctx, ingest.Stats{Files: 5, DocsFiles: 5}, runErr); err != nil {
		t.Fatalf("Finish() failed: %v", err)
	}

	status, err := coord.Status(ctx)
	if err != nil {
		t.Fatalf("Status() failed: %v", err)
	}
	if status.LastRun == nil || status.LastRun.TotalDocuments != 5 || status.LastRun.ProcessedDocuments != 2 {
		t.Fatalf("failed last run progress = %+v, want 2/5", status.LastRun)
	}
}

func TestCoordinatorRecordsFailedRun(t *testing.T) {
	ctx := context.Background()
	coord := New(t.TempDir())

	run, err := coord.Start(ctx, TriggerCLI)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	runErr := errors.New("embed batch: failed")
	if err := run.Finish(ctx, ingest.Stats{}, runErr); err != nil {
		t.Fatalf("Finish() failed: %v", err)
	}

	status, err := coord.Status(ctx)
	if err != nil {
		t.Fatalf("Status() failed: %v", err)
	}
	if status.Status != StatusFailed {
		t.Fatalf("Status = %q, want failed", status.Status)
	}
	if status.LastRun == nil || status.LastRun.Error != runErr.Error() {
		t.Fatalf("LastRun = %+v, want error %q", status.LastRun, runErr.Error())
	}
}

func TestStatusMarksStaleRunningJobFailed(t *testing.T) {
	ctx := context.Background()
	coord := New(t.TempDir())

	job := Job{
		ID:        "stale-job",
		Trigger:   TriggerCLI,
		PID:       12345,
		StartedAt: "2026-06-05T20:00:00Z",
	}
	if err := coord.recordRunning(job); err != nil {
		t.Fatalf("recordRunning() failed: %v", err)
	}

	status, err := coord.Status(ctx)
	if err != nil {
		t.Fatalf("Status() failed: %v", err)
	}
	if status.Status != StatusFailed || status.ActiveJob != nil {
		t.Fatalf("stale status = %+v, want failed without active job", status)
	}
	if status.LastRun == nil || status.LastRun.Job.ID != job.ID || status.LastRun.Error == "" {
		t.Fatalf("LastRun = %+v, want stale failure", status.LastRun)
	}
}
