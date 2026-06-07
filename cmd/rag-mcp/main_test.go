package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andrepester/rag-search-mcp/internal/config"
	"github.com/andrepester/rag-search-mcp/internal/ingest"
	"github.com/andrepester/rag-search-mcp/internal/observability"
	"github.com/andrepester/rag-search-mcp/internal/rag"
	"github.com/andrepester/rag-search-mcp/internal/reindexjob"
)

type fakeRAGService struct{}

func (fakeRAGService) SearchWithOptions(context.Context, rag.SearchOptions) (rag.SearchResponse, error) {
	return rag.SearchResponse{}, nil
}

func (fakeRAGService) SearchSettings() rag.SearchSettings {
	return rag.SearchSettings{
		MaxDistance:    config.DefaultMaxSearchDistance,
		MinDistance:    config.MinSearchDistance,
		MaxDistanceCap: config.MaxSearchDistance,
		DistanceStep:   0.01,
	}
}

func (fakeRAGService) GetChunk(context.Context, string) rag.ChunkResponse {
	return rag.ChunkResponse{}
}

func (fakeRAGService) ListSources(context.Context, string) (rag.ListSourcesResponse, error) {
	return rag.ListSourcesResponse{}, nil
}

func (fakeRAGService) RunReindex(context.Context, string, func(reindexjob.Job)) (ingest.Stats, error) {
	return ingest.Stats{}, nil
}

func (fakeRAGService) ReindexStatus(context.Context) (reindexjob.Status, error) {
	return reindexjob.Status{Status: reindexjob.StatusIdle}, nil
}

func (fakeRAGService) CheckReadiness(context.Context) observability.ReadinessReport {
	return observability.NewReadinessReport([]observability.DependencyStatus{
		{Name: "chroma", Status: observability.StatusOK},
		{Name: "ollama", Status: observability.StatusOK},
	})
}

type failingReadinessService struct {
	fakeRAGService
}

func (failingReadinessService) CheckReadiness(context.Context) observability.ReadinessReport {
	return observability.NewReadinessReport([]observability.DependencyStatus{
		{
			Name:   "ollama",
			Status: observability.StatusError,
			Error:  "connection refused",
			Hint:   observability.DependencyHint("ollama"),
		},
	})
}

type recordingRAGService struct {
	fakeRAGService
	searchResponse    rag.SearchResponse
	searchErr         error
	searchQuery       string
	searchTopK        int
	searchScope       string
	searchFilter      string
	searchMaxDistance *float64
	chunkResponse     rag.ChunkResponse
	chunkID           string
	sourcesResponse   rag.ListSourcesResponse
	sourcesErr        error
	sourcesScope      string
}

func (s *recordingRAGService) SearchWithOptions(_ context.Context, options rag.SearchOptions) (rag.SearchResponse, error) {
	s.searchQuery = options.Query
	s.searchTopK = options.TopK
	s.searchScope = options.Scope
	s.searchFilter = options.SourceFilter
	s.searchMaxDistance = options.MaxDistance
	return s.searchResponse, s.searchErr
}

func (s *recordingRAGService) GetChunk(_ context.Context, chunkID string) rag.ChunkResponse {
	s.chunkID = chunkID
	return s.chunkResponse
}

func (s *recordingRAGService) ListSources(_ context.Context, scope string) (rag.ListSourcesResponse, error) {
	s.sourcesScope = scope
	return s.sourcesResponse, s.sourcesErr
}

func TestLimitRequestBodyMiddleware(t *testing.T) {
	h := limitRequestBodyMiddleware(8, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("0123456789"))
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestRunConfigError(t *testing.T) {
	originalLoadConfig := loadConfig
	originalNewRAGService := newRAGService
	originalServeHTTP := serveHTTP
	t.Cleanup(func() {
		loadConfig = originalLoadConfig
		newRAGService = originalNewRAGService
		serveHTTP = originalServeHTTP
	})

	loadConfig = func() (config.Config, error) {
		return config.Config{}, errors.New("broken env")
	}

	err := run(discardLogger())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "invalid configuration") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunServiceInitError(t *testing.T) {
	originalLoadConfig := loadConfig
	originalNewRAGService := newRAGService
	originalServeHTTP := serveHTTP
	t.Cleanup(func() {
		loadConfig = originalLoadConfig
		newRAGService = originalNewRAGService
		serveHTTP = originalServeHTTP
	})

	loadConfig = func() (config.Config, error) {
		return config.Config{Host: "127.0.0.1", Port: 8765}, nil
	}
	newRAGService = func(*config.Config) (ragService, error) {
		return nil, errors.New("chroma down")
	}

	err := run(discardLogger())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "service init failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunServerError(t *testing.T) {
	originalLoadConfig := loadConfig
	originalNewRAGService := newRAGService
	originalServeHTTP := serveHTTP
	t.Cleanup(func() {
		loadConfig = originalLoadConfig
		newRAGService = originalNewRAGService
		serveHTTP = originalServeHTTP
	})

	loadConfig = func() (config.Config, error) {
		return config.Config{Host: "127.0.0.1", Port: 8765}, nil
	}
	newRAGService = func(*config.Config) (ragService, error) {
		return fakeRAGService{}, nil
	}
	serveHTTP = func(*http.Server) error {
		return errors.New("bind failed")
	}

	err := run(discardLogger())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "http server failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunServerClosedIsSuccess(t *testing.T) {
	originalLoadConfig := loadConfig
	originalNewRAGService := newRAGService
	originalServeHTTP := serveHTTP
	t.Cleanup(func() {
		loadConfig = originalLoadConfig
		newRAGService = originalNewRAGService
		serveHTTP = originalServeHTTP
	})

	loadConfig = func() (config.Config, error) {
		return config.Config{Host: "127.0.0.1", Port: 8765}, nil
	}
	newRAGService = func(*config.Config) (ragService, error) {
		return fakeRAGService{}, nil
	}
	serveHTTP = func(*http.Server) error {
		return http.ErrServerClosed
	}

	if err := run(discardLogger()); err != nil {
		t.Fatalf("run() failed: %v", err)
	}
}

func TestNewMuxHealthz(t *testing.T) {
	mux := newMux(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}), fakeRAGService{}, discardLogger(), observability.NewMetrics())

	healthReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthRes := httptest.NewRecorder()
	mux.ServeHTTP(healthRes, healthReq)
	if healthRes.Code != http.StatusOK {
		t.Fatalf("health status = %d, want %d", healthRes.Code, http.StatusOK)
	}
	if strings.TrimSpace(healthRes.Body.String()) != "ok" {
		t.Fatalf("health body = %q, want ok", healthRes.Body.String())
	}

	mcpReq := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
	mcpRes := httptest.NewRecorder()
	mux.ServeHTTP(mcpRes, mcpReq)
	if mcpRes.Code != http.StatusTeapot {
		t.Fatalf("mcp status = %d, want %d", mcpRes.Code, http.StatusTeapot)
	}
}

func TestNewMuxServesUIAndPreservesRoutes(t *testing.T) {
	mux := newMux(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}), fakeRAGService{}, discardLogger(), observability.NewMetrics())

	rootReq := httptest.NewRequest(http.MethodGet, "/", nil)
	rootRes := httptest.NewRecorder()
	mux.ServeHTTP(rootRes, rootReq)
	if rootRes.Code != http.StatusOK {
		t.Fatalf("root status = %d, want %d", rootRes.Code, http.StatusOK)
	}
	if !strings.Contains(rootRes.Body.String(), `id="search-form"`) {
		t.Fatalf("expected root UI index, got %s", rootRes.Body.String())
	}

	cssReq := httptest.NewRequest(http.MethodGet, "/styles.css", nil)
	cssRes := httptest.NewRecorder()
	mux.ServeHTTP(cssRes, cssReq)
	if cssRes.Code != http.StatusOK || !strings.Contains(cssRes.Body.String(), ".search-form") {
		t.Fatalf("root css status/body = %d/%q", cssRes.Code, cssRes.Body.String())
	}

	jsReq := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	jsRes := httptest.NewRecorder()
	mux.ServeHTTP(jsRes, jsReq)
	if jsRes.Code != http.StatusOK || !strings.Contains(jsRes.Body.String(), "runSearch") {
		t.Fatalf("root js status/body = %d/%q", jsRes.Code, jsRes.Body.String())
	}

	uiReq := httptest.NewRequest(http.MethodGet, "/ui/", nil)
	uiRes := httptest.NewRecorder()
	mux.ServeHTTP(uiRes, uiReq)
	if uiRes.Code != http.StatusOK {
		t.Fatalf("ui status = %d, want %d", uiRes.Code, http.StatusOK)
	}
	if !strings.Contains(uiRes.Body.String(), `id="search-form"`) {
		t.Fatalf("expected UI index, got %s", uiRes.Body.String())
	}
	if strings.Contains(uiRes.Body.String(), "Max results") || strings.Contains(uiRes.Body.String(), "technical Top K value") {
		t.Fatalf("unexpected max-results control in UI, got %s", uiRes.Body.String())
	}
	if !strings.Contains(uiRes.Body.String(), "Search") || !strings.Contains(uiRes.Body.String(), "query that gets embedded") {
		t.Fatalf("expected search label and help text, got %s", uiRes.Body.String())
	}
	if !strings.Contains(uiRes.Body.String(), "Scope") || !strings.Contains(uiRes.Body.String(), "scope parameter") {
		t.Fatalf("expected scope label and help text, got %s", uiRes.Body.String())
	}
	if !strings.Contains(uiRes.Body.String(), "Directory") || !strings.Contains(uiRes.Body.String(), "source filter") {
		t.Fatalf("expected directory label and help text, got %s", uiRes.Body.String())
	}
	if !strings.Contains(uiRes.Body.String(), "Relevance") || !strings.Contains(uiRes.Body.String(), "max_distance 0.50") || !strings.Contains(uiRes.Body.String(), "max_distance 0.40") {
		t.Fatalf("expected relevance label and help text, got %s", uiRes.Body.String())
	}
	if !strings.Contains(uiRes.Body.String(), "Strict") || !strings.Contains(uiRes.Body.String(), "Normal") {
		t.Fatalf("expected relevance mode controls, got %s", uiRes.Body.String())
	}
	if strings.Contains(uiRes.Body.String(), "rag-search-mcp") {
		t.Fatalf("unexpected product slug in UI header: %s", uiRes.Body.String())
	}
	if got := uiRes.Header().Get("Content-Security-Policy"); !strings.Contains(got, "connect-src 'self'") {
		t.Fatalf("missing UI CSP, got %q", got)
	}
	if got := uiRes.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unexpected CORS header %q", got)
	}

	mcpReq := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
	mcpRes := httptest.NewRecorder()
	mux.ServeHTTP(mcpRes, mcpReq)
	if mcpRes.Code != http.StatusTeapot {
		t.Fatalf("mcp status = %d, want %d", mcpRes.Code, http.StatusTeapot)
	}

	missingReq := httptest.NewRequest(http.MethodGet, "/missing", nil)
	missingRes := httptest.NewRecorder()
	mux.ServeHTTP(missingRes, missingReq)
	if missingRes.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d, want %d", missingRes.Code, http.StatusNotFound)
	}
}

func TestUIAPISearchDelegatesToRAGService(t *testing.T) {
	distance := 0.42
	maxDistance := 0.38
	svc := &recordingRAGService{
		searchResponse: rag.SearchResponse{
			Query:        "routing",
			ScopeUsed:    "docs",
			SourceFilter: "README",
			Matches: []rag.SearchMatch{
				{
					ChunkID:    "docs:README.md:0",
					SourcePath: "README.md",
					Scope:      "docs",
					ChunkIndex: 0,
					Distance:   &distance,
					Text:       "route documentation",
				},
			},
		},
	}
	mux := newMux(http.NotFoundHandler(), svc, discardLogger(), observability.NewMetrics())

	req := httptest.NewRequest(http.MethodPost, "/api/search", strings.NewReader(`{"query":"  routing  ","scope":"docs","source_filter":" README ","max_distance":0.38}`))
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("search status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	if svc.searchQuery != "routing" || svc.searchTopK != 0 || svc.searchScope != "docs" || svc.searchFilter != "README" {
		t.Fatalf("unexpected search call: query=%q topK=%d scope=%q filter=%q", svc.searchQuery, svc.searchTopK, svc.searchScope, svc.searchFilter)
	}
	if svc.searchMaxDistance == nil || *svc.searchMaxDistance < maxDistance-0.000001 || *svc.searchMaxDistance > maxDistance+0.000001 {
		t.Fatalf("search max distance = %v, want %f", svc.searchMaxDistance, maxDistance)
	}
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unexpected CORS header %q", got)
	}

	var response rag.SearchResponse
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal search response: %v", err)
	}
	if len(response.Matches) != 1 || response.Matches[0].ChunkID != "docs:README.md:0" {
		t.Fatalf("unexpected matches: %+v", response.Matches)
	}
}

func TestUIAPISearchValidation(t *testing.T) {
	mux := newMux(http.NotFoundHandler(), fakeRAGService{}, discardLogger(), observability.NewMetrics())

	tests := []struct {
		name   string
		method string
		body   string
		want   int
	}{
		{name: "method", method: http.MethodGet, body: "", want: http.StatusMethodNotAllowed},
		{name: "empty query", method: http.MethodPost, body: `{"query":"   "}`, want: http.StatusBadRequest},
		{name: "bad scope", method: http.MethodPost, body: `{"query":"x","scope":"bad"}`, want: http.StatusBadRequest},
		{name: "bad top k", method: http.MethodPost, body: `{"query":"x","top_k":-1}`, want: http.StatusBadRequest},
		{name: "bad max distance", method: http.MethodPost, body: `{"query":"x","max_distance":2.01}`, want: http.StatusBadRequest},
		{name: "unknown field", method: http.MethodPost, body: `{"query":"x","unexpected":true}`, want: http.StatusBadRequest},
		{name: "too large", method: http.MethodPost, body: `{"query":"` + strings.Repeat("x", defaultMaxUIAPIBodyBytes) + `"}`, want: http.StatusRequestEntityTooLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/search", strings.NewReader(tt.body))
			res := httptest.NewRecorder()
			mux.ServeHTTP(res, req)
			if res.Code != tt.want {
				t.Fatalf("status = %d, want %d: %s", res.Code, tt.want, res.Body.String())
			}
			if got := res.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
				t.Fatalf("Content-Type = %q, want JSON", got)
			}
		})
	}
}

func TestUIAPISearchSettings(t *testing.T) {
	mux := newMux(http.NotFoundHandler(), fakeRAGService{}, discardLogger(), observability.NewMetrics())

	req := httptest.NewRequest(http.MethodGet, "/api/search-settings", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("settings status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var response rag.SearchSettings
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal settings response: %v", err)
	}
	if response.MaxDistance != config.DefaultMaxSearchDistance || response.MinDistance != config.MinSearchDistance || response.MaxDistanceCap != config.MaxSearchDistance {
		t.Fatalf("unexpected settings: %+v", response)
	}
}

func TestUIAPIChunkDelegatesAndMapsStates(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		svc := &recordingRAGService{
			chunkResponse: rag.ChunkResponse{
				Found:   true,
				ChunkID: "chunk-1",
				Chunk: &rag.SearchMatch{
					ChunkID:    "chunk-1",
					SourcePath: "README.md",
					Scope:      "docs",
					Text:       "full chunk",
				},
			},
		}
		mux := newMux(http.NotFoundHandler(), svc, discardLogger(), observability.NewMetrics())

		req := httptest.NewRequest(http.MethodPost, "/api/chunk", strings.NewReader(`{"chunk_id":" chunk-1 "}`))
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)

		if res.Code != http.StatusOK {
			t.Fatalf("chunk status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
		}
		if svc.chunkID != "chunk-1" {
			t.Fatalf("chunkID = %q, want chunk-1", svc.chunkID)
		}
	})

	t.Run("not found", func(t *testing.T) {
		svc := &recordingRAGService{chunkResponse: rag.ChunkResponse{Found: false, ChunkID: "missing"}}
		mux := newMux(http.NotFoundHandler(), svc, discardLogger(), observability.NewMetrics())

		req := httptest.NewRequest(http.MethodPost, "/api/chunk", strings.NewReader(`{"chunk_id":"missing"}`))
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)

		if res.Code != http.StatusNotFound {
			t.Fatalf("chunk status = %d, want %d: %s", res.Code, http.StatusNotFound, res.Body.String())
		}
	})

	t.Run("service error", func(t *testing.T) {
		svc := &recordingRAGService{chunkResponse: rag.ChunkResponse{Found: false, ChunkID: "chunk-1", Error: "chroma down"}}
		mux := newMux(http.NotFoundHandler(), svc, discardLogger(), observability.NewMetrics())

		req := httptest.NewRequest(http.MethodPost, "/api/chunk", strings.NewReader(`{"chunk_id":"chunk-1"}`))
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)

		if res.Code != http.StatusBadGateway {
			t.Fatalf("chunk status = %d, want %d: %s", res.Code, http.StatusBadGateway, res.Body.String())
		}
		if strings.Contains(res.Body.String(), "chroma down") {
			t.Fatalf("response leaked service error detail: %s", res.Body.String())
		}
	})
}

func TestUIAPISourcesDelegatesToRAGService(t *testing.T) {
	svc := &recordingRAGService{
		sourcesResponse: rag.ListSourcesResponse{ScopeUsed: "code", Sources: []string{"cmd/rag-mcp/main.go"}},
	}
	mux := newMux(http.NotFoundHandler(), svc, discardLogger(), observability.NewMetrics())

	req := httptest.NewRequest(http.MethodGet, "/api/sources?scope=code", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("sources status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	if svc.sourcesScope != "code" {
		t.Fatalf("sources scope = %q, want code", svc.sourcesScope)
	}
	var response rag.ListSourcesResponse
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal sources response: %v", err)
	}
	if len(response.Sources) != 1 || response.Sources[0] != "cmd/rag-mcp/main.go" {
		t.Fatalf("unexpected sources: %+v", response.Sources)
	}
}

func TestUIAPISourcesValidationAndErrors(t *testing.T) {
	t.Run("bad scope", func(t *testing.T) {
		mux := newMux(http.NotFoundHandler(), fakeRAGService{}, discardLogger(), observability.NewMetrics())
		req := httptest.NewRequest(http.MethodGet, "/api/sources?scope=bad", nil)
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusBadRequest {
			t.Fatalf("sources status = %d, want %d", res.Code, http.StatusBadRequest)
		}
	})

	t.Run("service error", func(t *testing.T) {
		svc := &recordingRAGService{sourcesErr: errors.New("chroma down")}
		mux := newMux(http.NotFoundHandler(), svc, discardLogger(), observability.NewMetrics())
		req := httptest.NewRequest(http.MethodGet, "/api/sources?scope=docs", nil)
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusBadGateway {
			t.Fatalf("sources status = %d, want %d", res.Code, http.StatusBadGateway)
		}
		if strings.Contains(res.Body.String(), "chroma down") {
			t.Fatalf("response leaked service error detail: %s", res.Body.String())
		}
	})
}

func TestNewMuxReadyz(t *testing.T) {
	var logs bytes.Buffer
	metrics := observability.NewMetrics()
	mux := newMux(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}), failingReadinessService{}, slog.New(slog.NewJSONHandler(&logs, nil)), metrics)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz status = %d, want %d", res.Code, http.StatusServiceUnavailable)
	}

	var report observability.ReadinessReport
	if err := json.Unmarshal(res.Body.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal readyz response: %v\n%s", err, res.Body.String())
	}
	if report.Ready() {
		t.Fatal("readyz report should not be ready")
	}
	if !strings.Contains(logs.String(), `"event":"dependency_unhealthy"`) {
		t.Fatalf("expected dependency_unhealthy log, got %s", logs.String())
	}

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRes := httptest.NewRecorder()
	mux.ServeHTTP(metricsRes, metricsReq)
	if metricsRes.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want %d", metricsRes.Code, http.StatusOK)
	}
	if !strings.Contains(metricsRes.Body.String(), `rag_mcp_readiness_dependency_up{dependency="ollama"} 0`) {
		t.Fatalf("expected readiness metric, got %s", metricsRes.Body.String())
	}
}

func TestNewHTTPServerDefaults(t *testing.T) {
	cfg := &config.Config{Host: "127.0.0.1", Port: 8081}
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	srv := newHTTPServer(cfg, h)

	if srv.Addr != "127.0.0.1:8081" {
		t.Fatalf("Addr = %q, want 127.0.0.1:8081", srv.Addr)
	}
	if srv.Handler == nil {
		t.Fatal("expected handler")
	}
	if srv.ReadTimeout != defaultReadTimeout {
		t.Fatalf("ReadTimeout = %s, want %s", srv.ReadTimeout, defaultReadTimeout)
	}
	if srv.WriteTimeout != defaultWriteTimeout {
		t.Fatalf("WriteTimeout = %s, want %s", srv.WriteTimeout, defaultWriteTimeout)
	}
	if srv.IdleTimeout != defaultIdleTimeout {
		t.Fatalf("IdleTimeout = %s, want %s", srv.IdleTimeout, defaultIdleTimeout)
	}
	if srv.ReadHeaderTimeout != defaultReadHeaderTimeout {
		t.Fatalf("ReadHeaderTimeout = %s, want %s", srv.ReadHeaderTimeout, defaultReadHeaderTimeout)
	}
	if srv.MaxHeaderBytes != defaultMaxHeaderBytes {
		t.Fatalf("MaxHeaderBytes = %d, want %d", srv.MaxHeaderBytes, defaultMaxHeaderBytes)
	}
}

func TestDependencyForToolError(t *testing.T) {
	if got := dependencyForToolError("search", errors.New("embed request failed")); got != "ollama" {
		t.Fatalf("dependency = %q, want ollama", got)
	}
	if got := dependencyForToolError("list_sources", errors.New("request failed")); got != "chroma" {
		t.Fatalf("dependency = %q, want chroma", got)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
