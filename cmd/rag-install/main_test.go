package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCreatesEnvAndOpenCodeConfig(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, ".env.example"), []byte("RAG_HTTP_PORT=8090\n"), 0o644); err != nil {
		t.Fatalf("write .env.example: %v", err)
	}

	originalArgs := os.Args
	os.Args = []string{"rag-install", "--repo-root", repoRoot}
	t.Cleanup(func() { os.Args = originalArgs })

	var out bytes.Buffer
	if err := run(&out); err != nil {
		t.Fatalf("run() failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(repoRoot, ".env")); err != nil {
		t.Fatalf("expected .env to be created: %v", err)
	}
	assertFileMode(t, filepath.Join(repoRoot, ".env"), 0o600)
	raw, err := os.ReadFile(filepath.Join(repoRoot, "opencode.json"))
	if err != nil {
		t.Fatalf("read opencode.json: %v", err)
	}
	assertFileMode(t, filepath.Join(repoRoot, "opencode.json"), 0o600)
	if !strings.Contains(string(raw), "http://127.0.0.1:8090/mcp") {
		t.Fatalf("opencode.json did not include expected URL: %s", string(raw))
	}

	for _, dir := range []string{
		filepath.Join(repoRoot, "data", "docs"),
		filepath.Join(repoRoot, "data", "code"),
		filepath.Join(repoRoot, "data", "index"),
	} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("expected directory %s to exist: %v", dir, err)
		}
	}
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("permissions for %s = %o, want %o", path, got, want)
	}
}
