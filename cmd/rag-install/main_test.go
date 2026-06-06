package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCreatesEnvAndHostDirsWithoutClientConfig(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, ".env.example"), []byte("RAG_HTTP_PORT=8090\n"), 0o644); err != nil {
		t.Fatalf("write .env.example: %v", err)
	}

	var out bytes.Buffer
	if err := run([]string{"--repo-root", repoRoot}, &out); err != nil {
		t.Fatalf("run() failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(repoRoot, ".env")); err != nil {
		t.Fatalf("expected .env to be created: %v", err)
	}
	assertFileMode(t, filepath.Join(repoRoot, ".env"), 0o600)
	if _, err := os.Stat(filepath.Join(repoRoot, "opencode.json")); !os.IsNotExist(err) {
		t.Fatalf("client config should not be created, got err=%v", err)
	}

	for _, dir := range []string{
		filepath.Join(repoRoot, "data", "docs"),
		filepath.Join(repoRoot, "data", "code"),
		filepath.Join(repoRoot, "data", "index"),
		filepath.Join(repoRoot, "data", "models"),
	} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("expected directory %s to exist: %v", dir, err)
		}
	}

	output := out.String()
	if !strings.Contains(output, "http://127.0.0.1:8090/mcp") {
		t.Fatalf("missing generic endpoint in output: %s", output)
	}
	if !strings.Contains(output, "client configuration files are user-managed") {
		t.Fatalf("missing user-managed client config note in output: %s", output)
	}
}

func TestRunPreservesExistingUserClientConfig(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, ".env.example"), []byte("RAG_HTTP_PORT=8090\n"), 0o644); err != nil {
		t.Fatalf("write .env.example: %v", err)
	}
	configPath := filepath.Join(repoRoot, "opencode.json")
	original := []byte("{not managed by rag-install")
	if err := os.WriteFile(configPath, original, 0o644); err != nil {
		t.Fatalf("write existing client config: %v", err)
	}

	var out bytes.Buffer
	if err := run([]string{"--repo-root", repoRoot}, &out); err != nil {
		t.Fatalf("run() failed: %v", err)
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read existing client config: %v", err)
	}
	if string(raw) != string(original) {
		t.Fatalf("client config was modified: %q", string(raw))
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
