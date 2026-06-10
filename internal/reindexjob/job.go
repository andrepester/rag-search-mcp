package reindexjob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/andrepester/rag-search-mcp/internal/ingest"
)

const (
	statusFileName      = "reindex-status.json"
	reindexLockName     = "reindex.lock"
	statusLockName      = "reindex-status.lock"
	statusFileVersion   = 1
	StatusIdle          = "idle"
	StatusRunning       = "running"
	StatusSucceeded     = "succeeded"
	StatusFailed        = "failed"
	StatusBlocked       = "blocked"
	ErrorAlreadyRunning = "already_running"
	TriggerCLI          = "cli"
	TriggerMCPTool      = "mcp_tool"
)

type Coordinator struct {
	Dir string
}

type StartOptions struct {
	IndexSubdir    string
	EmbedBatchSize int
}

type Job struct {
	ID             string `json:"job_id"`
	Trigger        string `json:"trigger"`
	PID            int    `json:"pid"`
	Hostname       string `json:"hostname,omitempty"`
	StartedAt      string `json:"started_at"`
	IndexSubdir    string `json:"index_subdir,omitempty"`
	EmbedBatchSize int    `json:"embed_batch_size,omitempty"`
}

type Status struct {
	Version          int           `json:"version"`
	Status           string        `json:"status"`
	ActiveJob        *Job          `json:"active_job,omitempty"`
	Progress         *Progress     `json:"progress,omitempty"`
	LastRun          *RunRecord    `json:"last_run,omitempty"`
	LastBlockedStart *BlockedStart `json:"last_blocked_start,omitempty"`
	UpdatedAt        string        `json:"updated_at,omitempty"`
}

type Progress struct {
	TotalDocuments     int `json:"total_documents"`
	ProcessedDocuments int `json:"processed_documents"`
}

type RunRecord struct {
	Status             string `json:"status"`
	Job                Job    `json:"job"`
	CompletedAt        string `json:"completed_at"`
	DurationMillis     int64  `json:"duration_ms"`
	TotalDocuments     int    `json:"total_documents"`
	ProcessedDocuments int    `json:"processed_documents"`
	Files              int    `json:"files,omitempty"`
	Chunks             int    `json:"chunks,omitempty"`
	CodeFiles          int    `json:"code_files,omitempty"`
	DocsFiles          int    `json:"docs_files,omitempty"`
	ChangedFiles       int    `json:"changed_files,omitempty"`
	DeletedFiles       int    `json:"deleted_files,omitempty"`
	ReusedFiles        int    `json:"reused_files,omitempty"`
	EmbeddedChunks     int    `json:"embedded_chunks,omitempty"`
	ReusedChunks       int    `json:"reused_chunks,omitempty"`
	EmbedBatchSize     int    `json:"embed_batch_size,omitempty"`
	Generation         string `json:"generation,omitempty"`
	IndexSubdir        string `json:"index_subdir,omitempty"`
	Error              string `json:"error,omitempty"`
}

type BlockedStart struct {
	Status      string `json:"status"`
	Error       string `json:"error"`
	Trigger     string `json:"trigger"`
	AttemptedAt string `json:"attempted_at"`
	ActiveJob   *Job   `json:"active_job,omitempty"`
}

type Run struct {
	coord *Coordinator
	lock  *heldLock
	Job   Job
}

type BusyError struct {
	BlockedStart BlockedStart `json:"blocked_start"`
	RecordError  string       `json:"record_error,omitempty"`
}

func New(dir string) *Coordinator {
	return &Coordinator{Dir: dir}
}

func (c *Coordinator) Start(ctx context.Context, trigger string) (*Run, error) {
	return c.StartWithOptions(ctx, trigger, StartOptions{})
}

func (c *Coordinator) StartWithOptions(_ context.Context, trigger string, opts StartOptions) (*Run, error) {
	trigger = normalizeTrigger(trigger)
	job := newJob(trigger, time.Now().UTC(), opts.IndexSubdir, opts.EmbedBatchSize)
	lock, err := c.tryAcquireReindexLock()
	if err != nil {
		if errors.Is(err, errLockBusy) {
			blocked, recordErr := c.recordBlockedStart(trigger)
			busy := &BusyError{BlockedStart: blocked}
			if recordErr != nil {
				busy.RecordError = recordErr.Error()
			}
			return nil, busy
		}
		return nil, err
	}

	if err := lock.writeJob(job); err != nil {
		_ = lock.Close()
		return nil, err
	}
	if err := c.recordRunning(job); err != nil {
		_ = lock.Close()
		return nil, err
	}

	return &Run{coord: c, lock: lock, Job: job}, nil
}

func (r *Run) Finish(_ context.Context, stats ingest.Stats, runErr error) (finishErr error) {
	defer func() {
		if closeErr := r.lock.Close(); finishErr == nil && closeErr != nil {
			finishErr = closeErr
		}
	}()

	finishErr = r.coord.recordFinished(r.Job, stats, runErr)
	return finishErr
}

func (r *Run) UpdateProgress(_ context.Context, progress Progress) error {
	return r.coord.recordProgress(r.Job, progress)
}

func (c *Coordinator) Status(_ context.Context) (Status, error) {
	var status Status
	if err := c.withStatusLock(func() error {
		current, err := c.loadStatusUnlocked()
		if err != nil {
			return err
		}

		held, lockJob, err := c.reindexLockState()
		if err != nil {
			return err
		}
		switch {
		case held:
			if current.ActiveJob == nil && lockJob != nil {
				current.ActiveJob = lockJob
			}
			current.Status = StatusRunning
		case current.ActiveJob != nil:
			current = c.markStaleRunFailed(current, *current.ActiveJob)
			if err := c.saveStatusUnlocked(current); err != nil {
				return err
			}
		default:
			current = normalizeStatus(current)
		}

		status = current
		return nil
	}); err != nil {
		return Status{}, err
	}
	return status, nil
}

func IsBusy(err error) bool {
	var busy *BusyError
	return errors.As(err, &busy)
}

func Busy(err error) (*BusyError, bool) {
	var busy *BusyError
	if errors.As(err, &busy) {
		return busy, true
	}
	return nil, false
}

func (e *BusyError) Error() string {
	if e.RecordError != "" {
		return fmt.Sprintf("reindex already running: %s", e.RecordError)
	}
	return "reindex already running"
}

func (c *Coordinator) recordRunning(job Job) error {
	return c.withStatusLock(func() error {
		status, err := c.loadStatusUnlocked()
		if err != nil {
			return err
		}
		status = normalizeStatus(status)
		status.Status = StatusRunning
		status.ActiveJob = &job
		status.Progress = &Progress{}
		status.UpdatedAt = nowString()
		return c.saveStatusUnlocked(status)
	})
}

func (c *Coordinator) recordProgress(job Job, progress Progress) error {
	return c.withStatusLock(func() error {
		status, err := c.loadStatusUnlocked()
		if err != nil {
			return err
		}
		status = normalizeStatus(status)
		if status.ActiveJob == nil || status.ActiveJob.ID != job.ID {
			return nil
		}
		progress = normalizeProgress(progress)
		status.Status = StatusRunning
		status.Progress = &progress
		status.UpdatedAt = nowString()
		return c.saveStatusUnlocked(status)
	})
}

func (c *Coordinator) recordFinished(job Job, stats ingest.Stats, runErr error) error {
	return c.withStatusLock(func() error {
		status, err := c.loadStatusUnlocked()
		if err != nil {
			return err
		}
		record := runRecord(job, stats, runErr, time.Now().UTC())
		if status.Progress != nil {
			progress := normalizeProgress(*status.Progress)
			record.TotalDocuments = progress.TotalDocuments
			record.ProcessedDocuments = progress.ProcessedDocuments
		}
		if runErr == nil {
			record.TotalDocuments = stats.Files
			record.ProcessedDocuments = stats.Files
		}
		status.Version = statusFileVersion
		status.Status = record.Status
		status.ActiveJob = nil
		status.Progress = nil
		status.LastRun = &record
		status.UpdatedAt = record.CompletedAt
		return c.saveStatusUnlocked(status)
	})
}

func (c *Coordinator) recordBlockedStart(trigger string) (BlockedStart, error) {
	var blocked BlockedStart
	err := c.withStatusLock(func() error {
		status, err := c.loadStatusUnlocked()
		if err != nil {
			return err
		}
		if status.ActiveJob == nil {
			if lockJob, err := c.readLockJob(); err == nil && lockJob != nil {
				status.ActiveJob = lockJob
			}
		}
		blocked = BlockedStart{
			Status:      StatusBlocked,
			Error:       ErrorAlreadyRunning,
			Trigger:     trigger,
			AttemptedAt: nowString(),
			ActiveJob:   status.ActiveJob,
		}
		status = normalizeStatus(status)
		if status.ActiveJob != nil {
			status.Status = StatusRunning
		}
		status.LastBlockedStart = &blocked
		status.UpdatedAt = blocked.AttemptedAt
		return c.saveStatusUnlocked(status)
	})
	if blocked.Status == "" {
		blocked = BlockedStart{
			Status:      StatusBlocked,
			Error:       ErrorAlreadyRunning,
			Trigger:     trigger,
			AttemptedAt: nowString(),
		}
	}
	return blocked, err
}

func (c *Coordinator) markStaleRunFailed(status Status, job Job) Status {
	completedAt := time.Now().UTC()
	progress := Progress{}
	if status.Progress != nil {
		progress = normalizeProgress(*status.Progress)
	}
	record := RunRecord{
		Status:             StatusFailed,
		Job:                job,
		CompletedAt:        completedAt.Format(time.RFC3339Nano),
		DurationMillis:     durationMillis(job.StartedAt, completedAt),
		TotalDocuments:     progress.TotalDocuments,
		ProcessedDocuments: progress.ProcessedDocuments,
		Error:              "reindex process exited before updating job status",
	}
	status.Version = statusFileVersion
	status.Status = StatusFailed
	status.ActiveJob = nil
	status.Progress = nil
	status.LastRun = &record
	status.UpdatedAt = record.CompletedAt
	return status
}

func runRecord(job Job, stats ingest.Stats, runErr error, completedAt time.Time) RunRecord {
	status := StatusSucceeded
	errText := ""
	if runErr != nil {
		status = StatusFailed
		errText = runErr.Error()
	}
	indexSubdir := stats.IndexSubdir
	if indexSubdir == "" {
		indexSubdir = job.IndexSubdir
	}
	embedBatchSize := stats.EmbedBatchSize
	if embedBatchSize <= 0 {
		embedBatchSize = job.EmbedBatchSize
	}
	processedDocuments := stats.Files
	if runErr != nil {
		processedDocuments = stats.ChangedFiles + stats.ReusedFiles
	}
	return RunRecord{
		Status:             status,
		Job:                job,
		CompletedAt:        completedAt.Format(time.RFC3339Nano),
		DurationMillis:     durationMillis(job.StartedAt, completedAt),
		TotalDocuments:     stats.Files,
		ProcessedDocuments: processedDocuments,
		Files:              stats.Files,
		Chunks:             stats.Chunks,
		CodeFiles:          stats.CodeFiles,
		DocsFiles:          stats.DocsFiles,
		ChangedFiles:       stats.ChangedFiles,
		DeletedFiles:       stats.DeletedFiles,
		ReusedFiles:        stats.ReusedFiles,
		EmbeddedChunks:     stats.EmbeddedChunks,
		ReusedChunks:       stats.ReusedChunks,
		EmbedBatchSize:     embedBatchSize,
		Generation:         stats.Generation,
		IndexSubdir:        indexSubdir,
		Error:              errText,
	}
}

func newJob(trigger string, now time.Time, indexSubdir string, embedBatchSize int) Job {
	hostname, _ := os.Hostname()
	return Job{
		ID:             fmt.Sprintf("reindex-%s-%d", now.Format("20060102T150405.000000000Z"), os.Getpid()),
		Trigger:        trigger,
		PID:            os.Getpid(),
		Hostname:       hostname,
		StartedAt:      now.Format(time.RFC3339Nano),
		IndexSubdir:    strings.TrimSpace(indexSubdir),
		EmbedBatchSize: embedBatchSize,
	}
}

func normalizeTrigger(trigger string) string {
	trigger = strings.TrimSpace(trigger)
	if trigger == "" {
		return TriggerCLI
	}
	return trigger
}

func normalizeStatus(status Status) Status {
	status.Version = statusFileVersion
	if status.Status == "" {
		switch {
		case status.ActiveJob != nil:
			status.Status = StatusRunning
		case status.LastRun != nil:
			status.Status = status.LastRun.Status
		default:
			status.Status = StatusIdle
		}
	}
	if status.Status != StatusRunning {
		status.Progress = nil
	} else if status.Progress != nil {
		progress := normalizeProgress(*status.Progress)
		status.Progress = &progress
	}
	return status
}

func normalizeProgress(progress Progress) Progress {
	if progress.TotalDocuments < 0 {
		progress.TotalDocuments = 0
	}
	if progress.ProcessedDocuments < 0 {
		progress.ProcessedDocuments = 0
	}
	if progress.TotalDocuments == 0 {
		progress.ProcessedDocuments = 0
	} else if progress.ProcessedDocuments > progress.TotalDocuments {
		progress.ProcessedDocuments = progress.TotalDocuments
	}
	return progress
}

func durationMillis(startedAt string, completedAt time.Time) int64 {
	start, err := time.Parse(time.RFC3339Nano, startedAt)
	if err != nil {
		return 0
	}
	return completedAt.Sub(start).Milliseconds()
}

func nowString() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func (c *Coordinator) loadStatusUnlocked() (Status, error) {
	raw, err := os.ReadFile(c.statusPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Status{Version: statusFileVersion, Status: StatusIdle}, nil
		}
		return Status{}, fmt.Errorf("read reindex status: %w", err)
	}

	var status Status
	if err := json.Unmarshal(raw, &status); err != nil {
		return Status{}, fmt.Errorf("parse reindex status: %w", err)
	}
	return normalizeStatus(status), nil
}

func (c *Coordinator) saveStatusUnlocked(status Status) error {
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return fmt.Errorf("create reindex status directory: %w", err)
	}
	status = normalizeStatus(status)
	if status.UpdatedAt == "" {
		status.UpdatedAt = nowString()
	}

	payload, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal reindex status: %w", err)
	}
	payload = append(payload, '\n')

	tmp, err := os.CreateTemp(c.Dir, ".reindex-status-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary reindex status: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary reindex status: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary reindex status: %w", err)
	}
	if err := os.Rename(tmpPath, c.statusPath()); err != nil {
		return fmt.Errorf("activate reindex status: %w", err)
	}
	return nil
}

func (c *Coordinator) statusPath() string {
	return filepath.Join(c.Dir, statusFileName)
}

func (c *Coordinator) reindexLockPath() string {
	return filepath.Join(c.Dir, reindexLockName)
}

func (c *Coordinator) statusLockPath() string {
	return filepath.Join(c.Dir, statusLockName)
}
