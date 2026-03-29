package rag

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
	"github.com/andrepester/rag-search-mcp/internal/ingest"
	"github.com/andrepester/rag-search-mcp/internal/ollama"
	"github.com/andrepester/rag-search-mcp/internal/store"
)

func TestServiceReindexAndSearchScopes(t *testing.T) {
	ctx := context.Background()

	docsDir := t.TempDir()
	codeDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(docsDir, "guide.md"), []byte("Install steps for rag-search-mcp.\n\nUse docker compose."), 0o644); err != nil {
		t.Fatalf("write docs file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codeDir, "main.go"), []byte("package main\n\nfunc chunkText() string { return \"ok\" }\n"), 0o644); err != nil {
		t.Fatalf("write code file: %v", err)
	}

	chromaBackend := newFakeChromaBackend()
	chromaServer := httptest.NewServer(chromaBackend)
	defer chromaServer.Close()

	ollamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		embeddings := make([][]float64, 0, len(req.Input))
		for i, text := range req.Input {
			embeddings = append(embeddings, []float64{float64(len(text)), float64(i + 1)})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": embeddings})
	}))
	defer ollamaServer.Close()

	cfg := &config.Config{
		DocsDir:          docsDir,
		CodeDir:          codeDir,
		CollectionName:   "rag",
		EmbedModel:       "test-embed",
		ChunkSize:        120,
		ChunkOverlap:     20,
		DefaultScope:     "all",
		MaxTopK:          30,
		EnableCodeIngest: true,
	}

	ollamaClient := ollama.New(ollamaServer.URL)
	chromaClient := store.NewChromaClient(chromaServer.URL, "default_tenant", "default_database")
	ingestSvc := &ingest.Service{Config: cfg, Ollama: ollamaClient, Chroma: chromaClient}

	svc, err := NewService(cfg, ingestSvc, ollamaClient, chromaClient)
	if err != nil {
		t.Fatalf("NewService() failed: %v", err)
	}

	stats, err := svc.Reindex(ctx)
	if err != nil {
		t.Fatalf("Reindex() failed: %v", err)
	}
	if stats.DocsFiles != 1 || stats.CodeFiles != 1 {
		t.Fatalf("unexpected file stats: %+v", stats)
	}
	if stats.Chunks == 0 {
		t.Fatalf("expected indexed chunks > 0, got %d", stats.Chunks)
	}

	docsResp, err := svc.Search(ctx, "install", 10, "docs", "")
	if err != nil {
		t.Fatalf("docs search failed: %v", err)
	}
	if len(docsResp.Matches) == 0 {
		t.Fatal("expected docs matches")
	}
	for _, m := range docsResp.Matches {
		if m.Scope != "docs" {
			t.Fatalf("expected docs scope match, got %q", m.Scope)
		}
	}

	codeResp, err := svc.Search(ctx, "chunk", 10, "code", "")
	if err != nil {
		t.Fatalf("code search failed: %v", err)
	}
	if len(codeResp.Matches) == 0 {
		t.Fatal("expected code matches")
	}
	for _, m := range codeResp.Matches {
		if m.Scope != "code" {
			t.Fatalf("expected code scope match, got %q", m.Scope)
		}
	}

	allResp, err := svc.Search(ctx, "project", 50, "all", "")
	if err != nil {
		t.Fatalf("all search failed: %v", err)
	}
	if len(allResp.Matches) < 2 {
		t.Fatalf("expected mixed matches, got %d", len(allResp.Matches))
	}
	hasDocs, hasCode := false, false
	for _, m := range allResp.Matches {
		if m.Scope == "docs" {
			hasDocs = true
		}
		if m.Scope == "code" {
			hasCode = true
		}
	}
	if !hasDocs || !hasCode {
		t.Fatalf("expected all-scope results to include docs and code, got docs=%v code=%v", hasDocs, hasCode)
	}

	sources, err := svc.ListSources(ctx, "all")
	if err != nil {
		t.Fatalf("ListSources() failed: %v", err)
	}
	if !contains(sources.Sources, "docs/guide.md") || !contains(sources.Sources, "code/main.go") {
		t.Fatalf("unexpected sources: %v", sources.Sources)
	}

	chunk := svc.GetChunk(ctx, allResp.Matches[0].ChunkID)
	if !chunk.Found || chunk.Chunk == nil {
		t.Fatalf("expected chunk found, got %+v", chunk)
	}
}

type fakeChromaBackend struct {
	mu           sync.Mutex
	collectionID string
	records      map[string]fakeRecord
	order        []string
}

type fakeRecord struct {
	ID       string
	Document string
	Metadata map[string]any
	Distance float64
}

func newFakeChromaBackend() *fakeChromaBackend {
	return &fakeChromaBackend{
		collectionID: "col-rag",
		records:      map[string]fakeRecord{},
		order:        []string{},
	}
}

func (f *fakeChromaBackend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	base := "/api/v2/tenants/default_tenant/databases/default_database"

	if r.Method == http.MethodPost && r.URL.Path == base+"/collections" {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": f.collectionID, "name": "rag"})
		return
	}

	if r.Method == http.MethodDelete && r.URL.Path == base+"/collections/"+f.collectionID {
		f.mu.Lock()
		f.records = map[string]fakeRecord{}
		f.order = nil
		f.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method == http.MethodPost && r.URL.Path == base+"/collections/"+f.collectionID+"/upsert" {
		var payload struct {
			IDs       []string         `json:"ids"`
			Documents []string         `json:"documents"`
			Metadatas []map[string]any `json:"metadatas"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		f.mu.Lock()
		defer f.mu.Unlock()
		for i, id := range payload.IDs {
			if _, exists := f.records[id]; !exists {
				f.order = append(f.order, id)
			}
			meta := map[string]any{}
			if i < len(payload.Metadatas) && payload.Metadatas[i] != nil {
				for k, v := range payload.Metadatas[i] {
					meta[k] = v
				}
			}
			doc := ""
			if i < len(payload.Documents) {
				doc = payload.Documents[i]
			}
			f.records[id] = fakeRecord{ID: id, Document: doc, Metadata: meta, Distance: float64(i) / 10}
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"upserted": len(payload.IDs)})
		return
	}

	if r.Method == http.MethodPost && r.URL.Path == base+"/collections/"+f.collectionID+"/query" {
		var payload struct {
			NResults int            `json:"n_results"`
			Where    map[string]any `json:"where"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		matches := f.filterRecords(payload.Where)
		if payload.NResults > 0 && len(matches) > payload.NResults {
			matches = matches[:payload.NResults]
		}

		ids := make([]string, 0, len(matches))
		docs := make([]*string, 0, len(matches))
		metas := make([]map[string]any, 0, len(matches))
		dists := make([]*float64, 0, len(matches))
		for i := range matches {
			rec := matches[i]
			ids = append(ids, rec.ID)
			doc := rec.Document
			docs = append(docs, &doc)
			metas = append(metas, rec.Metadata)
			d := rec.Distance
			dists = append(dists, &d)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"ids":       [][]string{ids},
			"documents": [][]*string{docs},
			"metadatas": [][]map[string]any{metas},
			"distances": [][]*float64{dists},
		})
		return
	}

	if r.Method == http.MethodPost && r.URL.Path == base+"/collections/"+f.collectionID+"/get" {
		var payload struct {
			IDs    []string       `json:"ids"`
			Where  map[string]any `json:"where"`
			Limit  int            `json:"limit"`
			Offset int            `json:"offset"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var matches []fakeRecord
		if len(payload.IDs) > 0 {
			f.mu.Lock()
			for _, id := range payload.IDs {
				if rec, ok := f.records[id]; ok {
					matches = append(matches, rec)
				}
			}
			f.mu.Unlock()
		} else {
			matches = f.filterRecords(payload.Where)
			if payload.Offset > 0 && payload.Offset < len(matches) {
				matches = matches[payload.Offset:]
			} else if payload.Offset >= len(matches) {
				matches = nil
			}
			if payload.Limit > 0 && len(matches) > payload.Limit {
				matches = matches[:payload.Limit]
			}
		}

		ids := make([]string, 0, len(matches))
		docs := make([]*string, 0, len(matches))
		metas := make([]map[string]any, 0, len(matches))
		for i := range matches {
			rec := matches[i]
			ids = append(ids, rec.ID)
			doc := rec.Document
			docs = append(docs, &doc)
			metas = append(metas, rec.Metadata)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"ids":       ids,
			"documents": docs,
			"metadatas": metas,
			"include":   []string{"documents", "metadatas"},
		})
		return
	}

	http.Error(w, fmt.Sprintf("unhandled route %s %s", r.Method, r.URL.Path), http.StatusNotFound)
}

func (f *fakeChromaBackend) filterRecords(where map[string]any) []fakeRecord {
	f.mu.Lock()
	defer f.mu.Unlock()

	var scopeFilter string
	if where != nil {
		if scope, ok := where["scope"].(string); ok {
			scopeFilter = scope
		}
	}

	out := make([]fakeRecord, 0, len(f.order))
	for _, id := range f.order {
		rec := f.records[id]
		if scopeFilter != "" {
			scope, _ := rec.Metadata["scope"].(string)
			if !strings.EqualFold(scope, scopeFilter) {
				continue
			}
		}
		out = append(out, rec)
	}
	return out
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
