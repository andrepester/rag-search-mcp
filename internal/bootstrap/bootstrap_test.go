package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
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
		filepath.Join(repoRoot, "data", "index", "rag-state"),
	} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("expected %s to exist: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("expected %s to be a directory", dir)
		}
	}
	assertFileMode(t, filepath.Join(repoRoot, "data", "index", "rag-state"), 0o777)
}

func TestEnsureHostDataDirsUsesConfiguredHostPaths(t *testing.T) {
	repoRoot := t.TempDir()
	envContent := "HOST_DOCS_DIR=./custom/docs\nHOST_CODE_DIR=./custom/code\nHOST_INDEX_DIR=./custom/index\n"
	if err := os.WriteFile(filepath.Join(repoRoot, ".env"), []byte(envContent), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	if err := EnsureHostDataDirs(repoRoot); err != nil {
		t.Fatalf("EnsureHostDataDirs() failed: %v", err)
	}

	for _, dir := range []string{
		filepath.Join(repoRoot, "custom", "docs"),
		filepath.Join(repoRoot, "custom", "code"),
		filepath.Join(repoRoot, "custom", "index"),
		filepath.Join(repoRoot, "custom", "index", "rag-state"),
	} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("expected %s to exist: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("expected %s to be a directory", dir)
		}
	}
	assertFileMode(t, filepath.Join(repoRoot, "custom", "index", "rag-state"), 0o777)
}

func TestEnsureHostDataDirsPrefersProcessEnvOverDotEnv(t *testing.T) {
	repoRoot := t.TempDir()
	envContent := "HOST_DOCS_DIR=./from-dotenv/docs\nHOST_CODE_DIR=./from-dotenv/code\nHOST_INDEX_DIR=./from-dotenv/index\n"
	if err := os.WriteFile(filepath.Join(repoRoot, ".env"), []byte(envContent), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	t.Setenv("HOST_DOCS_DIR", "./from-env/docs")
	t.Setenv("HOST_CODE_DIR", "./from-env/code")
	t.Setenv("HOST_INDEX_DIR", "./from-env/index")

	if err := EnsureHostDataDirs(repoRoot); err != nil {
		t.Fatalf("EnsureHostDataDirs() failed: %v", err)
	}

	for _, dir := range []string{
		filepath.Join(repoRoot, "from-env", "docs"),
		filepath.Join(repoRoot, "from-env", "code"),
		filepath.Join(repoRoot, "from-env", "index"),
		filepath.Join(repoRoot, "from-env", "index", "rag-state"),
	} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("expected %s to exist: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("expected %s to be a directory", dir)
		}
	}
	assertFileMode(t, filepath.Join(repoRoot, "from-env", "index", "rag-state"), 0o777)

	if _, err := os.Stat(filepath.Join(repoRoot, "from-dotenv", "docs")); !os.IsNotExist(err) {
		t.Fatalf("expected .env docs path to stay absent, got err=%v", err)
	}
}

func TestResolveHostDirFallsBackToDotEnvWhenProcessEnvIsEmpty(t *testing.T) {
	repoRoot := t.TempDir()
	envValues := map[string]string{hostDocsEnvKey: "./from-dotenv/docs"}
	t.Setenv(hostDocsEnvKey, "   ")

	got, err := resolveHostDir(repoRoot, envValues, hostDocsEnvKey, hostDocsDir)
	if err != nil {
		t.Fatalf("resolveHostDir() failed: %v", err)
	}

	want, err := filepath.Abs(filepath.Join(repoRoot, "from-dotenv", "docs"))
	if err != nil {
		t.Fatalf("resolve expected path: %v", err)
	}
	if got != want {
		t.Fatalf("resolved path = %s, want %s", got, want)
	}
}

func TestUpsertHostSourceDirsUpdatesOnlySourceKeys(t *testing.T) {
	repoRoot := t.TempDir()
	content := "RAG_HTTP_PORT=8123\nHOST_DOCS_DIR=./old/docs\nHOST_CODE_DIR=./old/code\nHOST_INDEX_DIR=./data/index\n"
	if err := os.WriteFile(filepath.Join(repoRoot, ".env"), []byte(content), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	if err := UpsertHostSourceDirs(repoRoot, "./new/docs", "./new/code"); err != nil {
		t.Fatalf("UpsertHostSourceDirs() failed: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(repoRoot, ".env"))
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	env := string(raw)
	if !strings.Contains(env, "HOST_DOCS_DIR=./new/docs") {
		t.Fatalf("missing updated HOST_DOCS_DIR: %s", env)
	}
	if !strings.Contains(env, "HOST_CODE_DIR=./new/code") {
		t.Fatalf("missing updated HOST_CODE_DIR: %s", env)
	}
	if !strings.Contains(env, "RAG_HTTP_PORT=8123") {
		t.Fatalf("expected unrelated keys to stay unchanged: %s", env)
	}
	if !strings.Contains(env, "HOST_INDEX_DIR=./data/index") {
		t.Fatalf("expected unrelated HOST_INDEX_DIR to stay unchanged: %s", env)
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
