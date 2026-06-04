package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/andrepester/rag-search-mcp/internal/config"
	"github.com/andrepester/rag-search-mcp/internal/ollama"
	"github.com/andrepester/rag-search-mcp/internal/store"
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

func TestReindexBuildsNewGenerationsAndReusesUnchangedSources(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatalf("create docs dir: %v", err)
	}
	guidePath := filepath.Join(docsDir, "guide.md")
	if err := os.WriteFile(guidePath, []byte("first guide text"), 0o644); err != nil {
		t.Fatalf("write guide: %v", err)
	}

	chromaBackend := newIngestChromaBackend()
	chromaServer := httptest.NewServer(chromaBackend)
	defer chromaServer.Close()

	ollamaBackend := &ingestOllamaBackend{}
	ollamaServer := httptest.NewServer(ollamaBackend)
	defer ollamaServer.Close()

	cfg := &config.Config{
		DocsDir:          docsDir,
		CodeDir:          filepath.Join(root, "missing-code"),
		CollectionName:   "rag",
		IndexStateDir:    filepath.Join(root, "index-state"),
		EmbedModel:       "test-embed",
		ChunkSize:        200,
		ChunkOverlap:     20,
		EnableCodeIngest: false,
	}
	svc := &Service{
		Config: cfg,
		Ollama: ollama.New(ollamaServer.URL),
		Chroma: store.NewChromaClient(chromaServer.URL, "default_tenant", "default_database"),
	}

	first, err := svc.Reindex(ctx)
	if err != nil {
		t.Fatalf("first Reindex() failed: %v", err)
	}
	if first.ChangedFiles != 1 || first.ReusedFiles != 0 || first.EmbeddedChunks == 0 {
		t.Fatalf("unexpected first stats: %+v", first)
	}
	firstEmbedCalls := ollamaBackend.calls()

	second, err := svc.Reindex(ctx)
	if err != nil {
		t.Fatalf("second Reindex() failed: %v", err)
	}
	if second.ChangedFiles != 0 || second.ReusedFiles != 1 || second.EmbeddedChunks != 0 || second.ReusedChunks == 0 {
		t.Fatalf("unexpected second stats: %+v", second)
	}
	if got := ollamaBackend.calls(); got != firstEmbedCalls {
		t.Fatalf("embedding calls after unchanged reindex = %d, want %d", got, firstEmbedCalls)
	}
	if second.Generation == first.Generation {
		t.Fatal("expected a new generation for the second reindex")
	}

	if err := os.WriteFile(guidePath, []byte("changed guide text"), 0o644); err != nil {
		t.Fatalf("modify guide: %v", err)
	}
	third, err := svc.Reindex(ctx)
	if err != nil {
		t.Fatalf("third Reindex() failed: %v", err)
	}
	if third.ChangedFiles != 1 || third.ReusedFiles != 0 || third.EmbeddedChunks == 0 {
		t.Fatalf("unexpected third stats: %+v", third)
	}

	if err := os.Remove(guidePath); err != nil {
		t.Fatalf("delete guide: %v", err)
	}
	fourth, err := svc.Reindex(ctx)
	if err != nil {
		t.Fatalf("fourth Reindex() failed: %v", err)
	}
	if fourth.Files != 0 || fourth.Chunks != 0 || fourth.DeletedFiles != 1 {
		t.Fatalf("unexpected fourth stats: %+v", fourth)
	}
}

type ingestOllamaBackend struct {
	mu        sync.Mutex
	callCount int
}

func (b *ingestOllamaBackend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/api/embed" {
		http.NotFound(w, r)
		return
	}
	var req struct {
		Input []string `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	b.mu.Lock()
	b.callCount++
	b.mu.Unlock()

	embeddings := make([][]float64, 0, len(req.Input))
	for _, input := range req.Input {
		embeddings = append(embeddings, []float64{float64(len(input)), 1})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": embeddings})
}

func (b *ingestOllamaBackend) calls() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.callCount
}

type ingestChromaBackend struct {
	mu           sync.Mutex
	collectionID string
	records      map[string]ingestRecord
	order        []string
}

type ingestRecord struct {
	ID        string
	Document  string
	Metadata  map[string]any
	Embedding []float64
}

func newIngestChromaBackend() *ingestChromaBackend {
	return &ingestChromaBackend{
		collectionID: "col-rag",
		records:      map[string]ingestRecord{},
		order:        []string{},
	}
}

func (b *ingestChromaBackend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	basePath := "/api/v2/tenants/default_tenant/databases/default_database"
	switch {
	case r.Method == http.MethodPost && r.URL.Path == basePath+"/collections":
		_ = json.NewEncoder(w).Encode(map[string]any{"id": b.collectionID, "name": "rag"})
	case r.Method == http.MethodPost && r.URL.Path == basePath+"/collections/"+b.collectionID+"/upsert":
		b.handleUpsert(w, r)
	case r.Method == http.MethodPost && r.URL.Path == basePath+"/collections/"+b.collectionID+"/get":
		b.handleGet(w, r)
	case r.Method == http.MethodPost && r.URL.Path == basePath+"/collections/"+b.collectionID+"/delete":
		b.handleDelete(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (b *ingestChromaBackend) handleUpsert(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		IDs        []string         `json:"ids"`
		Documents  []string         `json:"documents"`
		Metadatas  []map[string]any `json:"metadatas"`
		Embeddings [][]float64      `json:"embeddings"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	for i, id := range payload.IDs {
		if _, exists := b.records[id]; !exists {
			b.order = append(b.order, id)
		}
		record := ingestRecord{ID: id, Metadata: map[string]any{}}
		if i < len(payload.Documents) {
			record.Document = payload.Documents[i]
		}
		if i < len(payload.Metadatas) {
			for key, value := range payload.Metadatas[i] {
				record.Metadata[key] = value
			}
		}
		if i < len(payload.Embeddings) {
			record.Embedding = append([]float64(nil), payload.Embeddings[i]...)
		}
		b.records[id] = record
	}
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"upserted": len(payload.IDs)})
}

func (b *ingestChromaBackend) handleGet(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Where  map[string]any `json:"where"`
		Limit  int            `json:"limit"`
		Offset int            `json:"offset"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	matches := b.filter(payload.Where)
	if payload.Offset > 0 && payload.Offset < len(matches) {
		matches = matches[payload.Offset:]
	} else if payload.Offset >= len(matches) {
		matches = nil
	}
	if payload.Limit > 0 && len(matches) > payload.Limit {
		matches = matches[:payload.Limit]
	}

	ids := make([]string, 0, len(matches))
	docs := make([]*string, 0, len(matches))
	metas := make([]map[string]any, 0, len(matches))
	embeddings := make([][]float64, 0, len(matches))
	for _, record := range matches {
		ids = append(ids, record.ID)
		doc := record.Document
		docs = append(docs, &doc)
		metas = append(metas, record.Metadata)
		embeddings = append(embeddings, record.Embedding)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ids":        ids,
		"documents":  docs,
		"metadatas":  metas,
		"embeddings": embeddings,
	})
}

func (b *ingestChromaBackend) handleDelete(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Where map[string]any `json:"where"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	nextOrder := make([]string, 0, len(b.order))
	for _, id := range b.order {
		record := b.records[id]
		if ingestMetadataMatches(record.Metadata, payload.Where) {
			delete(b.records, id)
			continue
		}
		nextOrder = append(nextOrder, id)
	}
	b.order = nextOrder
	b.mu.Unlock()
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"deleted": true})
}

func (b *ingestChromaBackend) filter(where map[string]any) []ingestRecord {
	b.mu.Lock()
	defer b.mu.Unlock()

	out := make([]ingestRecord, 0, len(b.order))
	for _, id := range b.order {
		record := b.records[id]
		if ingestMetadataMatches(record.Metadata, where) {
			out = append(out, record)
		}
	}
	return out
}

func ingestMetadataMatches(metadata map[string]any, where map[string]any) bool {
	if len(where) == 0 {
		return true
	}
	for key, want := range where {
		if key == "$and" {
			clauses, ok := want.([]any)
			if !ok {
				return false
			}
			for _, clause := range clauses {
				clauseMap, ok := clause.(map[string]any)
				if !ok || !ingestMetadataMatches(metadata, clauseMap) {
					return false
				}
			}
			continue
		}
		got, ok := metadata[key]
		if !ok || !strings.EqualFold(fmt.Sprint(got), fmt.Sprint(want)) {
			return false
		}
	}
	return true
}
