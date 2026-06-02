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
)

type fakeRAGService struct{}

func (fakeRAGService) Search(context.Context, string, int, string, string) (rag.SearchResponse, error) {
	return rag.SearchResponse{}, nil
}

func (fakeRAGService) GetChunk(context.Context, string) rag.ChunkResponse {
	return rag.ChunkResponse{}
}

func (fakeRAGService) ListSources(context.Context, string) (rag.ListSourcesResponse, error) {
	return rag.ListSourcesResponse{}, nil
}

func (fakeRAGService) Reindex(context.Context) (ingest.Stats, error) {
	return ingest.Stats{}, nil
}

func (fakeRAGService) CheckReadiness(context.Context) observability.ReadinessReport {
	return observability.NewReadinessReport([]observability.DependencyStatus{
		{Name: "chroma", Status: observability.StatusOK},
		{Name: "ollama", Status: observability.StatusOK},
	})
}

type failingReadinessService struct{}

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
