package reindexjob

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

var errLockBusy = errors.New("lock busy")

var localLocks = struct {
	mu    sync.Mutex
	paths map[string]struct{}
}{paths: map[string]struct{}{}}

type heldLock struct {
	file      *os.File
	localPath string
}

func (c *Coordinator) tryAcquireReindexLock() (*heldLock, error) {
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("create reindex lock directory: %w", err)
	}
	lockPath := c.reindexLockPath()
	localPath := cleanLockPath(lockPath)
	if !tryAcquireLocalLock(localPath) {
		return nil, errLockBusy
	}
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		releaseLocalLock(localPath)
		return nil, fmt.Errorf("open reindex lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		releaseLocalLock(localPath)
		if isLockBusy(err) {
			return nil, errLockBusy
		}
		return nil, fmt.Errorf("acquire reindex lock: %w", err)
	}
	return &heldLock{file: file, localPath: localPath}, nil
}

func (c *Coordinator) reindexLockState() (bool, *Job, error) {
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return false, nil, fmt.Errorf("create reindex lock directory: %w", err)
	}
	if localLockHeld(cleanLockPath(c.reindexLockPath())) {
		job, _ := c.readLockJob()
		return true, job, nil
	}
	file, err := os.OpenFile(c.reindexLockPath(), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return false, nil, fmt.Errorf("open reindex lock: %w", err)
	}
	defer file.Close()

	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if isLockBusy(err) {
			job, _ := c.readLockJob()
			return true, job, nil
		}
		return false, nil, fmt.Errorf("check reindex lock: %w", err)
	}
	defer func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	}()
	return false, nil, nil
}

func (c *Coordinator) withStatusLock(fn func() error) error {
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return fmt.Errorf("create reindex status lock directory: %w", err)
	}
	file, err := os.OpenFile(c.statusLockPath(), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("open reindex status lock: %w", err)
	}
	defer file.Close()

	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquire reindex status lock: %w", err)
	}
	defer func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	}()

	return fn()
}

func (l *heldLock) writeJob(job Job) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal reindex lock job: %w", err)
	}
	payload = append(payload, '\n')

	if err := l.file.Truncate(0); err != nil {
		return fmt.Errorf("truncate reindex lock: %w", err)
	}
	if _, err := l.file.Seek(0, 0); err != nil {
		return fmt.Errorf("seek reindex lock: %w", err)
	}
	if _, err := l.file.Write(payload); err != nil {
		return fmt.Errorf("write reindex lock job: %w", err)
	}
	if err := l.file.Sync(); err != nil {
		return fmt.Errorf("sync reindex lock job: %w", err)
	}
	return nil
}

func (l *heldLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	var err error
	if truncateErr := l.file.Truncate(0); truncateErr != nil {
		err = truncateErr
	}
	if unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN); unlockErr != nil && err == nil {
		err = unlockErr
	}
	if closeErr := l.file.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	releaseLocalLock(l.localPath)
	l.file = nil
	if err != nil {
		return fmt.Errorf("release reindex lock: %w", err)
	}
	return nil
}

func (c *Coordinator) readLockJob() (*Job, error) {
	raw, err := os.ReadFile(c.reindexLockPath())
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var job Job
	if err := json.Unmarshal(raw, &job); err != nil {
		return nil, err
	}
	if job.ID == "" {
		return nil, nil
	}
	return &job, nil
}

func isLockBusy(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
}

func cleanLockPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}

func tryAcquireLocalLock(path string) bool {
	localLocks.mu.Lock()
	defer localLocks.mu.Unlock()
	if _, ok := localLocks.paths[path]; ok {
		return false
	}
	localLocks.paths[path] = struct{}{}
	return true
}

func releaseLocalLock(path string) {
	if path == "" {
		return
	}
	localLocks.mu.Lock()
	defer localLocks.mu.Unlock()
	delete(localLocks.paths, path)
}

func localLockHeld(path string) bool {
	localLocks.mu.Lock()
	defer localLocks.mu.Unlock()
	_, ok := localLocks.paths[path]
	return ok
}
