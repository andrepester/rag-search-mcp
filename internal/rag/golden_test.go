package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/andrepester/rag-search-mcp/internal/config"
	"github.com/andrepester/rag-search-mcp/internal/ingest"
	"github.com/andrepester/rag-search-mcp/internal/ollama"
	"github.com/andrepester/rag-search-mcp/internal/store"
)

type goldenSuite struct {
	Version  int              `json:"version"`
	Settings goldenSettings   `json:"settings"`
	Queries  []goldenTestCase `json:"queries"`
}

type goldenSettings struct {
	EmbeddingModel string `json:"embedding_model"`
	ChunkSize      int    `json:"chunk_size"`
	ChunkOverlap   int    `json:"chunk_overlap"`
	DefaultTopK    int    `json:"default_top_k"`
	MaxTopK        int    `json:"max_top_k"`
}

type goldenTestCase struct {
	Name         string              `json:"name"`
	Query        string              `json:"query"`
	Scope        string              `json:"scope"`
	SourceFilter string              `json:"source_filter"`
	TopK         int                 `json:"top_k"`
	Expected     []goldenExpectation `json:"expected"`
}

type goldenExpectation struct {
	SourcePath  string   `json:"source_path"`
	Scope       string   `json:"scope"`
	ChunkIndex  int      `json:"chunk_index"`
	MaxRank     int      `json:"max_rank"`
	MustContain []string `json:"must_contain"`
}

func TestGoldenQueries(t *testing.T) {
	ctx := context.Background()
	suite := loadGoldenSuite(t)

	chromaBackend := newGoldenChromaBackend()
	chromaServer := httptest.NewServer(chromaBackend)
	defer chromaServer.Close()

	ollamaServer := newGoldenOllamaServer(suite.Settings.EmbeddingModel)
	defer ollamaServer.Close()

	cfg := &config.Config{
		DocsDir:          filepath.Join("testdata", "golden", "docs"),
		CodeDir:          filepath.Join("testdata", "golden", "code"),
		CollectionName:   "rag-golden",
		EmbedModel:       suite.Settings.EmbeddingModel,
		ChunkSize:        suite.Settings.ChunkSize,
		ChunkOverlap:     suite.Settings.ChunkOverlap,
		DefaultScope:     "all",
		MaxTopK:          suite.Settings.MaxTopK,
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
	if stats.DocsFiles == 0 || stats.CodeFiles == 0 || stats.Chunks == 0 {
		t.Fatalf("golden fixtures did not index docs and code: %+v", stats)
	}

	for _, tc := range suite.Queries {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			topK := tc.TopK
			if topK <= 0 {
				topK = suite.Settings.DefaultTopK
			}

			resp, err := svc.Search(ctx, tc.Query, topK, tc.Scope, tc.SourceFilter)
			if err != nil {
				t.Fatalf("Search() failed: %v", err)
			}
			assertGoldenResponse(t, tc, resp)
		})
	}
}

func loadGoldenSuite(t *testing.T) goldenSuite {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", "golden", "queries.json"))
	if err != nil {
		t.Fatalf("read golden queries: %v", err)
	}

	var suite goldenSuite
	if err := json.Unmarshal(raw, &suite); err != nil {
		t.Fatalf("parse golden queries: %v", err)
	}
	if suite.Version != 1 {
		t.Fatalf("golden suite version = %d, want 1", suite.Version)
	}
	if suite.Settings.EmbeddingModel == "" {
		t.Fatal("golden settings require embedding_model")
	}
	if suite.Settings.ChunkSize <= 0 || suite.Settings.ChunkOverlap < 0 || suite.Settings.ChunkOverlap >= suite.Settings.ChunkSize {
		t.Fatalf("invalid golden chunk settings: %+v", suite.Settings)
	}
	if suite.Settings.DefaultTopK <= 0 || suite.Settings.MaxTopK < suite.Settings.DefaultTopK {
		t.Fatalf("invalid golden top-k settings: %+v", suite.Settings)
	}
	if len(suite.Queries) == 0 {
		t.Fatal("golden suite must contain at least one query")
	}

	names := map[string]struct{}{}
	scopeCounts := map[string]int{}
	for i, tc := range suite.Queries {
		if strings.TrimSpace(tc.Name) == "" {
			t.Fatalf("golden query %d requires name", i)
		}
		if _, exists := names[tc.Name]; exists {
			t.Fatalf("duplicate golden query name %q", tc.Name)
		}
		names[tc.Name] = struct{}{}

		if strings.TrimSpace(tc.Query) == "" {
			t.Fatalf("golden query %q requires query text", tc.Name)
		}
		if !validGoldenScope(tc.Scope) {
			t.Fatalf("golden query %q has invalid scope %q", tc.Name, tc.Scope)
		}
		scopeCounts[tc.Scope]++

		topK := tc.TopK
		if topK <= 0 {
			topK = suite.Settings.DefaultTopK
		}
		if topK > suite.Settings.MaxTopK {
			t.Fatalf("golden query %q top_k=%d exceeds max_top_k=%d", tc.Name, topK, suite.Settings.MaxTopK)
		}
		if len(tc.Expected) == 0 {
			t.Fatalf("golden query %q requires at least one expectation", tc.Name)
		}
		for j, want := range tc.Expected {
			if strings.TrimSpace(want.SourcePath) == "" {
				t.Fatalf("golden query %q expectation %d requires source_path", tc.Name, j)
			}
			if !validGoldenScope(want.Scope) {
				t.Fatalf("golden query %q expectation %d has invalid scope %q", tc.Name, j, want.Scope)
			}
			if tc.Scope != "all" && want.Scope != tc.Scope {
				t.Fatalf("golden query %q expectation %d scope=%q must match query scope=%q", tc.Name, j, want.Scope, tc.Scope)
			}
			if want.MaxRank > topK {
				t.Fatalf("golden query %q expectation %d max_rank=%d exceeds top_k=%d", tc.Name, j, want.MaxRank, topK)
			}
			if len(want.MustContain) == 0 {
				t.Fatalf("golden query %q expectation %d requires at least one must_contain snippet", tc.Name, j)
			}
		}
	}
	if scopeCounts["docs"] == 0 || scopeCounts["code"] == 0 {
		t.Fatalf("golden suite must cover docs and code scopes, got counts: %v", scopeCounts)
	}
	return suite
}

func validGoldenScope(scope string) bool {
	return scope == "all" || scope == "docs" || scope == "code"
}

func assertGoldenResponse(t *testing.T, tc goldenTestCase, resp SearchResponse) {
	t.Helper()

	if resp.ScopeUsed != tc.Scope {
		t.Fatalf("scope_used = %q, want %q\nactual:\n%s", resp.ScopeUsed, tc.Scope, formatGoldenMatches(resp.Matches))
	}
	if len(resp.Matches) == 0 {
		t.Fatalf("no matches returned\nquery: %q\nscope: %s", tc.Query, tc.Scope)
	}

	failures := make([]string, 0)
	for i, want := range tc.Expected {
		maxRank := want.MaxRank
		if maxRank <= 0 {
			maxRank = i + 1
		}

		rank := findGoldenMatch(resp.Matches, want, maxRank)
		if rank == 0 {
			failures = append(failures, fmt.Sprintf(
				"expected source=%s scope=%s chunk_index=%d within rank <= %d, but it was not present",
				want.SourcePath,
				want.Scope,
				want.ChunkIndex,
				maxRank,
			))
			continue
		}

		match := resp.Matches[rank-1]
		for _, snippet := range want.MustContain {
			if !strings.Contains(strings.ToLower(match.Text), strings.ToLower(snippet)) {
				failures = append(failures, fmt.Sprintf(
					"rank %d source=%s is missing text snippet %q",
					rank,
					match.SourcePath,
					snippet,
				))
			}
		}
	}

	if len(failures) > 0 {
		t.Fatalf(
			"golden query %q failed\nquery: %q\nsource_filter: %q\n%s\nactual top results:\n%s",
			tc.Name,
			tc.Query,
			tc.SourceFilter,
			strings.Join(failures, "\n"),
			formatGoldenMatches(resp.Matches),
		)
	}
}

func findGoldenMatch(matches []SearchMatch, want goldenExpectation, maxRank int) int {
	if maxRank > len(matches) {
		maxRank = len(matches)
	}
	for i := 0; i < maxRank; i++ {
		match := matches[i]
		if match.SourcePath == want.SourcePath && match.Scope == want.Scope && match.ChunkIndex == want.ChunkIndex {
			return i + 1
		}
	}
	return 0
}

func formatGoldenMatches(matches []SearchMatch) string {
	if len(matches) == 0 {
		return "  <none>"
	}

	lines := make([]string, 0, len(matches))
	for i, match := range matches {
		distance := "<nil>"
		if match.Distance != nil {
			distance = fmt.Sprintf("%.6f", *match.Distance)
		}
		lines = append(lines, fmt.Sprintf(
			"  %d. source=%s scope=%s chunk_index=%d distance=%s text=%q",
			i+1,
			match.SourcePath,
			match.Scope,
			match.ChunkIndex,
			distance,
			trimGoldenText(match.Text, 120),
		))
	}
	return strings.Join(lines, "\n")
}

func trimGoldenText(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= limit {
		return text
	}
	return strings.TrimSpace(text[:limit]) + "..."
}

func newGoldenOllamaServer(expectedModel string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/embed" {
			http.NotFound(w, r)
			return
		}

		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Model != expectedModel {
			http.Error(w, fmt.Sprintf("unexpected model %q", req.Model), http.StatusBadRequest)
			return
		}

		embeddings := make([][]float64, 0, len(req.Input))
		for _, input := range req.Input {
			embeddings = append(embeddings, goldenEmbedding(input))
		}

		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": embeddings})
	}))
}

var goldenKeywordFeatures = [][]string{
	{"install", "installation", "bootstrap", "make install", "docker compose", "compose", "reindex"},
	{"architecture", "mcp", "semantic", "retrieval", "ollama", "embedding", "embeddings", "chroma", "vector"},
	{"configuration", "environment", "rag_chunk_size", "rag_chunk_overlap", "rag_scope_default", "rag_max_top_k", "source_filter", "top_k", "scope"},
	{"chunk", "chunking", "chunk size", "overlap", "paragraph", "splitting", "source_path", "chunk index", "metadata"},
	{"security", "localhost", "loopback", "lan", "wan", "token", "threat", "exposure"},
	{"search", "response", "match", "distance", "text", "source path"},
}

func goldenEmbedding(text string) []float64 {
	lower := strings.ToLower(text)
	vector := make([]float64, len(goldenKeywordFeatures)+1)

	for i, terms := range goldenKeywordFeatures {
		for _, term := range terms {
			vector[i] += float64(strings.Count(lower, term))
		}
	}

	vector[len(vector)-1] = 1
	return vector
}

type goldenChromaBackend struct {
	mu           sync.Mutex
	collectionID string
	records      map[string]goldenChromaRecord
	order        []string
}

type goldenChromaRecord struct {
	ID        string
	Document  string
	Metadata  map[string]any
	Embedding []float64
}

type goldenRankedRecord struct {
	record   goldenChromaRecord
	distance float64
}

func newGoldenChromaBackend() *goldenChromaBackend {
	return &goldenChromaBackend{
		collectionID: "col-rag-golden",
		records:      map[string]goldenChromaRecord{},
		order:        []string{},
	}
}

func (f *goldenChromaBackend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	base := "/api/v2/tenants/default_tenant/databases/default_database"

	if r.Method == http.MethodPost && r.URL.Path == base+"/collections" {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": f.collectionID, "name": "rag-golden"})
		return
	}

	if r.Method == http.MethodDelete && r.URL.Path == base+"/collections/"+f.collectionID {
		f.mu.Lock()
		f.records = map[string]goldenChromaRecord{}
		f.order = nil
		f.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method == http.MethodPost && r.URL.Path == base+"/collections/"+f.collectionID+"/upsert" {
		f.handleUpsert(w, r)
		return
	}

	if r.Method == http.MethodPost && r.URL.Path == base+"/collections/"+f.collectionID+"/query" {
		f.handleQuery(w, r)
		return
	}

	if r.Method == http.MethodPost && r.URL.Path == base+"/collections/"+f.collectionID+"/get" {
		f.handleGet(w, r)
		return
	}

	http.Error(w, fmt.Sprintf("unhandled route %s %s", r.Method, r.URL.Path), http.StatusNotFound)
}

func (f *goldenChromaBackend) handleUpsert(w http.ResponseWriter, r *http.Request) {
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

	f.mu.Lock()
	defer f.mu.Unlock()

	for i, id := range payload.IDs {
		if _, exists := f.records[id]; !exists {
			f.order = append(f.order, id)
		}

		record := goldenChromaRecord{
			ID:        id,
			Metadata:  map[string]any{},
			Embedding: []float64{},
		}
		if i < len(payload.Documents) {
			record.Document = payload.Documents[i]
		}
		if i < len(payload.Metadatas) && payload.Metadatas[i] != nil {
			for k, v := range payload.Metadatas[i] {
				record.Metadata[k] = v
			}
		}
		if i < len(payload.Embeddings) {
			record.Embedding = append([]float64(nil), payload.Embeddings[i]...)
		}

		f.records[id] = record
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"upserted": len(payload.IDs)})
}

func (f *goldenChromaBackend) handleQuery(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		QueryEmbeddings [][]float64      `json:"query_embeddings"`
		NResults        int              `json:"n_results"`
		Where           map[string]any   `json:"where"`
		Include         []string         `json:"include"`
		Metadatas       []map[string]any `json:"metadatas"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(payload.QueryEmbeddings) == 0 {
		http.Error(w, "query_embeddings is required", http.StatusBadRequest)
		return
	}

	records := f.filterRecords(payload.Where)
	ranked := make([]goldenRankedRecord, 0, len(records))
	for _, record := range records {
		ranked = append(ranked, goldenRankedRecord{
			record:   record,
			distance: goldenCosineDistance(payload.QueryEmbeddings[0], record.Embedding),
		})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if math.Abs(ranked[i].distance-ranked[j].distance) > 1e-12 {
			return ranked[i].distance < ranked[j].distance
		}
		return goldenRecordSortKey(ranked[i].record) < goldenRecordSortKey(ranked[j].record)
	})
	if payload.NResults > 0 && len(ranked) > payload.NResults {
		ranked = ranked[:payload.NResults]
	}

	ids := make([]string, 0, len(ranked))
	docs := make([]*string, 0, len(ranked))
	metas := make([]map[string]any, 0, len(ranked))
	dists := make([]*float64, 0, len(ranked))
	for _, item := range ranked {
		ids = append(ids, item.record.ID)
		doc := item.record.Document
		docs = append(docs, &doc)
		metas = append(metas, item.record.Metadata)
		distance := item.distance
		dists = append(dists, &distance)
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"ids":       [][]string{ids},
		"documents": [][]*string{docs},
		"metadatas": [][]map[string]any{metas},
		"distances": [][]*float64{dists},
	})
}

func (f *goldenChromaBackend) handleGet(w http.ResponseWriter, r *http.Request) {
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

	var matches []goldenChromaRecord
	if len(payload.IDs) > 0 {
		matches = f.recordsByID(payload.IDs)
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
	for _, record := range matches {
		ids = append(ids, record.ID)
		doc := record.Document
		docs = append(docs, &doc)
		metas = append(metas, record.Metadata)
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"ids":       ids,
		"documents": docs,
		"metadatas": metas,
		"include":   []string{"documents", "metadatas"},
	})
}

func (f *goldenChromaBackend) recordsByID(ids []string) []goldenChromaRecord {
	f.mu.Lock()
	defer f.mu.Unlock()

	matches := make([]goldenChromaRecord, 0, len(ids))
	for _, id := range ids {
		record, ok := f.records[id]
		if !ok {
			continue
		}
		matches = append(matches, copyGoldenRecord(record))
	}
	return matches
}

func (f *goldenChromaBackend) filterRecords(where map[string]any) []goldenChromaRecord {
	f.mu.Lock()
	defer f.mu.Unlock()

	var scopeFilter string
	if where != nil {
		if scope, ok := where["scope"].(string); ok {
			scopeFilter = scope
		}
	}

	out := make([]goldenChromaRecord, 0, len(f.order))
	for _, id := range f.order {
		record := f.records[id]
		if scopeFilter != "" {
			scope, _ := record.Metadata["scope"].(string)
			if !strings.EqualFold(scope, scopeFilter) {
				continue
			}
		}
		out = append(out, copyGoldenRecord(record))
	}
	return out
}

func copyGoldenRecord(record goldenChromaRecord) goldenChromaRecord {
	out := goldenChromaRecord{
		ID:        record.ID,
		Document:  record.Document,
		Metadata:  map[string]any{},
		Embedding: append([]float64(nil), record.Embedding...),
	}
	for k, v := range record.Metadata {
		out.Metadata[k] = v
	}
	return out
}

func goldenRecordSortKey(record goldenChromaRecord) string {
	source, _ := record.Metadata["source_path"].(string)
	return fmt.Sprintf("%s/%06d/%s", source, goldenMetadataInt(record.Metadata["chunk_index"]), record.ID)
}

func goldenMetadataInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}

func goldenCosineDistance(a, b []float64) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 1
	}

	n := len(a)
	if len(b) < n {
		n = len(b)
	}

	var dot, normA, normB float64
	for i := 0; i < n; i++ {
		dot += a[i] * b[i]
	}
	for _, value := range a {
		normA += value * value
	}
	for _, value := range b {
		normB += value * value
	}
	if normA == 0 || normB == 0 {
		return 1
	}

	similarity := dot / (math.Sqrt(normA) * math.Sqrt(normB))
	if similarity > 1 {
		similarity = 1
	}
	if similarity < -1 {
		similarity = -1
	}
	return 1 - similarity
}
