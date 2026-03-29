package ingest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"local-rag/internal/config"
	"local-rag/internal/store"
)

func TestLoadScopeDocumentsSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	insideFile := filepath.Join(root, "guide.md")
	if err := os.WriteFile(insideFile, []byte("inside"), 0o644); err != nil {
		t.Fatalf("write inside file: %v", err)
	}

	outsideFile := filepath.Join(outside, "secret.md")
	if err := os.WriteFile(outsideFile, []byte("outside"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	symlinkPath := filepath.Join(root, "link.md")
	if err := os.Symlink(outsideFile, symlinkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	docs, err := loadScopeDocuments(root, "docs", docsExt)
	if err != nil {
		t.Fatalf("loadScopeDocuments() failed: %v", err)
	}

	if len(docs) != 1 {
		t.Fatalf("len(docs) = %d, want 1", len(docs))
	}
	if docs[0].SourcePath != "docs/guide.md" {
		t.Fatalf("SourcePath = %q, want docs/guide.md", docs[0].SourcePath)
	}
}

func TestResolvedPathWithinRoot(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "nested", "file.md")
	outside := filepath.Join(filepath.Dir(root), "outside.md")

	if !resolvedPathWithinRoot(root, inside) {
		t.Fatal("expected inside path to be within root")
	}
	if resolvedPathWithinRoot(root, outside) {
		t.Fatal("expected outside path to be rejected")
	}
}

func TestReindexDeleteCollectionHandling(t *testing.T) {
	t.Run("not found delete is ignored", func(t *testing.T) {
		svc := newIngestServiceForDeleteStatus(t, http.StatusNotFound)
		if _, err := svc.Reindex(context.Background()); err != nil {
			t.Fatalf("Reindex() failed: %v", err)
		}
	})

	t.Run("delete errors are returned", func(t *testing.T) {
		svc := newIngestServiceForDeleteStatus(t, http.StatusInternalServerError)
		if _, err := svc.Reindex(context.Background()); err == nil {
			t.Fatal("expected Reindex() to fail on delete error")
		}
	})
}

func newIngestServiceForDeleteStatus(t *testing.T, deleteStatus int) *Service {
	t.Helper()

	basePath := "/api/v2/tenants/default_tenant/databases/default_database"
	chroma := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == basePath+"/collections":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "col-rag", "name": "rag"})
		case r.Method == http.MethodDelete && r.URL.Path == basePath+"/collections/col-rag":
			w.WriteHeader(deleteStatus)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "delete failed"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(chroma.Close)

	root := t.TempDir()
	cfg := &config.Config{
		DocsDir:          filepath.Join(root, "missing-docs"),
		CodeDir:          filepath.Join(root, "missing-code"),
		CollectionName:   "rag",
		EnableCodeIngest: false,
	}

	return &Service{
		Config: cfg,
		Chroma: store.NewChromaClient(chroma.URL, "default_tenant", "default_database"),
	}
}
