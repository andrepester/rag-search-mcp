package bootstrap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureEnvFileCreatesFromExample(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, ".env.example"), []byte("RAG_HTTP_PORT=9090\n"), 0o644); err != nil {
		t.Fatalf("write env example: %v", err)
	}

	created, err := EnsureEnvFile(repoRoot)
	if err != nil {
		t.Fatalf("EnsureEnvFile() failed: %v", err)
	}
	if !created {
		t.Fatal("expected .env to be created")
	}

	raw, err := os.ReadFile(filepath.Join(repoRoot, ".env"))
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	if string(raw) != "RAG_HTTP_PORT=9090\n" {
		t.Fatalf("unexpected .env content: %q", string(raw))
	}
	assertFileMode(t, filepath.Join(repoRoot, ".env"), 0o600)
}

func TestEnsureEnvFilePreservesExisting(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, ".env.example"), []byte("RAG_HTTP_PORT=9090\n"), 0o644); err != nil {
		t.Fatalf("write env example: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, ".env"), []byte("RAG_HTTP_PORT=7070\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	created, err := EnsureEnvFile(repoRoot)
	if err != nil {
		t.Fatalf("EnsureEnvFile() failed: %v", err)
	}
	if created {
		t.Fatal("did not expect .env to be created")
	}

	raw, err := os.ReadFile(filepath.Join(repoRoot, ".env"))
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	if string(raw) != "RAG_HTTP_PORT=7070\n" {
		t.Fatalf(".env was overwritten: %q", string(raw))
	}
}

func TestResolvePortPrefersEnvironmentVariable(t *testing.T) {
	t.Setenv("RAG_HTTP_PORT", "8099")
	repoRoot := t.TempDir()

	port, err := ResolvePort(repoRoot)
	if err != nil {
		t.Fatalf("ResolvePort() failed: %v", err)
	}
	if port != 8099 {
		t.Fatalf("port = %d, want 8099", port)
	}
}

func TestResolvePortFromDotEnv(t *testing.T) {
	t.Setenv("RAG_HTTP_PORT", "")
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, ".env"), []byte("# test\nRAG_HTTP_PORT=8123\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	port, err := ResolvePort(repoRoot)
	if err != nil {
		t.Fatalf("ResolvePort() failed: %v", err)
	}
	if port != 8123 {
		t.Fatalf("port = %d, want 8123", port)
	}
}

func TestResolvePortUsesDefaultWhenUnset(t *testing.T) {
	t.Setenv("RAG_HTTP_PORT", "")
	repoRoot := t.TempDir()

	port, err := ResolvePort(repoRoot)
	if err != nil {
		t.Fatalf("ResolvePort() failed: %v", err)
	}
	if port != 8765 {
		t.Fatalf("port = %d, want 8765", port)
	}
}

func TestEnsureHostDataDirsCreatesDefaults(t *testing.T) {
	repoRoot := t.TempDir()

	if err := EnsureHostDataDirs(repoRoot); err != nil {
		t.Fatalf("EnsureHostDataDirs() failed: %v", err)
	}

	for _, dir := range []string{
		filepath.Join(repoRoot, "data", "docs"),
		filepath.Join(repoRoot, "data", "code"),
		filepath.Join(repoRoot, "data", "index"),
		filepath.Join(repoRoot, "data", "models"),
	} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("expected %s to exist: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("expected %s to be a directory", dir)
		}
	}
}

func TestEnsureHostDataDirsUsesConfiguredDocAndCodePaths(t *testing.T) {
	repoRoot := t.TempDir()
	envContent := "HOST_DOCS_DIR=./custom/docs\nHOST_CODE_DIR=./custom/code\n"
	if err := os.WriteFile(filepath.Join(repoRoot, ".env"), []byte(envContent), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	if err := EnsureHostDataDirs(repoRoot); err != nil {
		t.Fatalf("EnsureHostDataDirs() failed: %v", err)
	}

	for _, dir := range []string{
		filepath.Join(repoRoot, "custom", "docs"),
		filepath.Join(repoRoot, "custom", "code"),
		filepath.Join(repoRoot, "data", "index"),
		filepath.Join(repoRoot, "data", "models"),
	} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("expected %s to exist: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("expected %s to be a directory", dir)
		}
	}
}

func TestUpsertOpenCodeConfigCreatesFile(t *testing.T) {
	repoRoot := t.TempDir()

	if err := UpsertOpenCodeConfig(repoRoot, 8088); err != nil {
		t.Fatalf("UpsertOpenCodeConfig() failed: %v", err)
	}

	cfg := readOpenCodeConfig(t, filepath.Join(repoRoot, "opencode.json"))
	mcp := cfg["mcp"].(map[string]any)
	service := mcp["rag-search-mcp"].(map[string]any)
	if service["url"] != "http://127.0.0.1:8088/mcp" {
		t.Fatalf("url = %v, want http://127.0.0.1:8088/mcp", service["url"])
	}
	assertFileMode(t, filepath.Join(repoRoot, "opencode.json"), 0o600)
}

func TestUpsertOpenCodeConfigPreservesExistingKeys(t *testing.T) {
	repoRoot := t.TempDir()
	original := `{
	  "custom": {"keep": true},
	  "mcp": {
	    "github": {"type": "local", "enabled": true},
	    "rag-search-mcp": {"type": "remote", "url": "http://127.0.0.1:1111/mcp", "enabled": false, "timeout": 1}
	  }
	}`
	if err := os.WriteFile(filepath.Join(repoRoot, "opencode.json"), []byte(original), 0o644); err != nil {
		t.Fatalf("write opencode.json: %v", err)
	}

	if err := UpsertOpenCodeConfig(repoRoot, 8765); err != nil {
		t.Fatalf("UpsertOpenCodeConfig() failed: %v", err)
	}

	cfg := readOpenCodeConfig(t, filepath.Join(repoRoot, "opencode.json"))
	custom := cfg["custom"].(map[string]any)
	if custom["keep"] != true {
		t.Fatal("custom key was not preserved")
	}
	mcp := cfg["mcp"].(map[string]any)
	if _, ok := mcp["github"]; !ok {
		t.Fatal("existing mcp.github key was not preserved")
	}
	service := mcp["rag-search-mcp"].(map[string]any)
	if service["enabled"] != true {
		t.Fatalf("rag-search-mcp.enabled = %v, want true", service["enabled"])
	}
	if service["url"] != "http://127.0.0.1:8765/mcp" {
		t.Fatalf("rag-search-mcp.url = %v, want updated url", service["url"])
	}
	assertFileMode(t, filepath.Join(repoRoot, "opencode.json"), 0o600)
}

func TestUpsertOpenCodeConfigBacksUpInvalidJSON(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "opencode.json"), []byte("{invalid"), 0o644); err != nil {
		t.Fatalf("write invalid opencode.json: %v", err)
	}

	if err := UpsertOpenCodeConfig(repoRoot, 8765); err != nil {
		t.Fatalf("UpsertOpenCodeConfig() failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(repoRoot, "opencode.json.invalid")); err != nil {
		t.Fatalf("expected invalid backup file: %v", err)
	}
	readOpenCodeConfig(t, filepath.Join(repoRoot, "opencode.json"))
}

func readOpenCodeConfig(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	return cfg
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
