package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/andrepester/rag-search-mcp/internal/config"
	"github.com/andrepester/rag-search-mcp/internal/ingest"
	"github.com/andrepester/rag-search-mcp/internal/observability"
	"github.com/andrepester/rag-search-mcp/internal/ollama"
	"github.com/andrepester/rag-search-mcp/internal/rag"
	"github.com/andrepester/rag-search-mcp/internal/reindexjob"
	"github.com/andrepester/rag-search-mcp/internal/store"
)

const (
	defaultReadTimeout       = 15 * time.Second
	defaultWriteTimeout      = 60 * time.Second
	defaultIdleTimeout       = 120 * time.Second
	defaultMaxHeaderBytes    = 1 << 20  // 1 MiB
	defaultMaxMCPBodyBytes   = 2 << 20  // 2 MiB
	defaultMaxUIAPIBodyBytes = 64 << 10 // 64 KiB
	defaultMCPTopK           = 5
	defaultReadHeaderTimeout = 5 * time.Second
	defaultReadinessTimeout  = 3 * time.Second
)

//go:embed web/*
var webAssets embed.FS

type searchInput struct {
	Query        string   `json:"query" jsonschema:"Search query"`
	TopK         int      `json:"top_k,omitempty" jsonschema:"Maximum number of matches"`
	Scope        string   `json:"scope,omitempty" jsonschema:"Search scope: all, docs, code"`
	SourceFilter string   `json:"source_filter,omitempty" jsonschema:"Substring filter for source_path"`
	MaxDistance  *float64 `json:"max_distance,omitempty" jsonschema:"Maximum cosine distance threshold; lower is stricter"`
}

type getChunkInput struct {
	ChunkID string `json:"chunk_id" jsonschema:"Chunk id from rag_search results"`
}

type listSourcesInput struct {
	Scope string `json:"scope,omitempty" jsonschema:"Source scope: all, docs, code"`
}

type ragService interface {
	SearchWithOptions(ctx context.Context, options rag.SearchOptions) (rag.SearchResponse, error)
	SearchSettings() rag.SearchSettings
	GetChunk(ctx context.Context, chunkID string) rag.ChunkResponse
	ListSources(ctx context.Context, scope string) (rag.ListSourcesResponse, error)
	RunReindex(ctx context.Context, trigger string, onStart func(reindexjob.Job)) (ingest.Stats, error)
	ReindexStatus(ctx context.Context) (reindexjob.Status, error)
	CheckReadiness(ctx context.Context) observability.ReadinessReport
}

var (
	loadConfig    = config.Load
	newRAGService = func(cfg *config.Config) (ragService, error) {
		ollamaClient := ollama.New(cfg.OllamaHost)
		chromaClient := store.NewChromaClient(cfg.ChromaURL, cfg.ChromaTenant, cfg.ChromaDatabase)
		ingestSvc := &ingest.Service{Config: cfg, Ollama: ollamaClient, Chroma: chromaClient}
		return rag.NewService(cfg, ingestSvc, ollamaClient, chromaClient)
	}
	serveHTTP = func(server *http.Server) error {
		return server.ListenAndServe()
	}
)

func main() {
	logger := observability.NewFallbackLogger(os.Stdout, os.Getenv("RAG_LOG_LEVEL"), os.Getenv("RAG_LOG_FORMAT"))
	if err := run(logger); err != nil {
		logger.Error("service run failed",
			slog.String("component", "rag-mcp"),
			slog.String("event", "service_error"),
			slog.String("error", err.Error()),
		)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	ragSvc, err := newRAGService(&cfg)
	if err != nil {
		return fmt.Errorf("service init failed: %w", err)
	}

	metrics := observability.NewMetrics()
	httpServer := newHTTPServer(&cfg, newMux(newMCPHandler(ragSvc, logger, metrics), ragSvc, logger, metrics))
	componentLogger(logger, "rag-mcp").Info("rag-mcp listening",
		slog.String("event", "service_start"),
		slog.String("addr", httpServer.Addr),
		slog.String("collection", cfg.CollectionName),
		slog.String("default_scope", cfg.DefaultScope),
		slog.Bool("code_ingest", cfg.EnableCodeIngest),
		slog.String("log_level", cfg.LogLevel),
		slog.String("log_format", cfg.LogFormat),
	)

	if err := serveHTTP(httpServer); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http server failed: %w", err)
	}

	return nil
}

func newMCPHandler(ragSvc ragService, logger *slog.Logger, metrics *observability.Metrics) http.Handler {
	logger = componentLogger(logger, "rag-mcp")

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "rag",
		Version: "1.0.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search",
		Description: "Semantic search over indexed docs and code (default scope=all)",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input *searchInput) (*mcp.CallToolResult, any, error) {
		if input == nil {
			err := errors.New("input is required")
			logToolError(ctx, logger, "search", time.Now(), err, slog.String("scope", ""))
			metrics.RecordToolCall("search", false)
			return nil, nil, err
		}
		start := time.Now()
		scope := input.Scope
		topK := input.TopK
		if topK <= 0 {
			topK = defaultMCPTopK
		}
		response, err := ragSvc.SearchWithOptions(ctx, rag.SearchOptions{
			Query:        input.Query,
			TopK:         topK,
			Scope:        input.Scope,
			SourceFilter: input.SourceFilter,
			MaxDistance:  input.MaxDistance,
		})
		if err != nil {
			logToolError(ctx, logger, "search", start, err,
				slog.String("scope", scope),
				slog.Int("top_k", topK),
				slog.Bool("source_filter_set", strings.TrimSpace(input.SourceFilter) != ""),
			)
			metrics.RecordToolCall("search", false)
			return nil, nil, err
		}
		metrics.RecordToolCall("search", true)
		logger.InfoContext(ctx, "tool call complete",
			slog.String("event", "tool_call"),
			slog.String("tool", "search"),
			slog.String("scope", response.ScopeUsed),
			slog.Int("top_k", topK),
			slog.Bool("source_filter_set", response.SourceFilter != ""),
			slog.Int("matches", len(response.Matches)),
			slog.Int64("duration_ms", durationMillis(start)),
		)
		return nil, response, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_chunk",
		Description: "Fetch a specific indexed chunk by chunk_id",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input *getChunkInput) (*mcp.CallToolResult, any, error) {
		if input == nil {
			err := errors.New("input is required")
			logToolError(ctx, logger, "get_chunk", time.Now(), err)
			metrics.RecordToolCall("get_chunk", false)
			return nil, nil, err
		}
		start := time.Now()
		response := ragSvc.GetChunk(ctx, input.ChunkID)
		if response.Error != "" {
			metrics.RecordToolCall("get_chunk", false)
			attrs := []slog.Attr{
				slog.String("event", "tool_error"),
				slog.String("tool", "get_chunk"),
				slog.String("error", response.Error),
				slog.Bool("found", response.Found),
				slog.Int64("duration_ms", durationMillis(start)),
			}
			if strings.TrimSpace(response.ChunkID) != "" {
				attrs = append(attrs,
					slog.String("dependency", "chroma"),
					slog.String("hint", observability.DependencyHint("chroma")),
				)
			}
			logger.LogAttrs(ctx, slog.LevelError, "tool call returned error response", attrs...)
		} else {
			metrics.RecordToolCall("get_chunk", true)
		}
		logger.InfoContext(ctx, "tool call complete",
			slog.String("event", "tool_call"),
			slog.String("tool", "get_chunk"),
			slog.Bool("found", response.Found),
			slog.Int64("duration_ms", durationMillis(start)),
		)
		return nil, response, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_sources",
		Description: "List indexed source paths for docs/code/all scopes",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input *listSourcesInput) (*mcp.CallToolResult, any, error) {
		scope := ""
		if input != nil {
			scope = input.Scope
		}
		start := time.Now()
		response, err := ragSvc.ListSources(ctx, scope)
		if err != nil {
			logToolError(ctx, logger, "list_sources", start, err, slog.String("scope", scope))
			metrics.RecordToolCall("list_sources", false)
			return nil, nil, err
		}
		metrics.RecordToolCall("list_sources", true)
		logger.InfoContext(ctx, "tool call complete",
			slog.String("event", "tool_call"),
			slog.String("tool", "list_sources"),
			slog.String("scope", response.ScopeUsed),
			slog.Int("sources", len(response.Sources)),
			slog.Int64("duration_ms", durationMillis(start)),
		)
		return nil, response, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "reindex",
		Description: "Rebuild the index from docs and code sources",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ *struct{}) (*mcp.CallToolResult, any, error) {
		start := time.Now()
		var jobID string
		stats, err := ragSvc.RunReindex(ctx, reindexjob.TriggerMCPTool, func(job reindexjob.Job) {
			jobID = job.ID
			logger.InfoContext(ctx, "reindex started",
				slog.String("event", "reindex_start"),
				slog.String("trigger", "mcp_tool"),
				slog.String("job_id", job.ID),
				slog.String("index_subdir", job.IndexSubdir),
				slog.Int("embed_batch_size", job.EmbedBatchSize),
			)
		})
		if err != nil {
			if busy, ok := reindexjob.Busy(err); ok {
				logReindexBlocked(ctx, logger, busy)
				metrics.RecordToolCall("reindex", false)
				metrics.RecordReindex("mcp_tool", false)
				return nil, map[string]any{
					"ok":                  false,
					"status":              busy.BlockedStart.Status,
					"error":               busy.BlockedStart.Error,
					"active_job":          busy.BlockedStart.ActiveJob,
					"last_blocked_start":  busy.BlockedStart,
					"status_record_error": busy.RecordError,
					"duration_ms":         durationMillis(start),
				}, nil
			}
			logToolError(ctx, logger, "reindex", start, err)
			metrics.RecordToolCall("reindex", false)
			metrics.RecordReindex("mcp_tool", false)
			return nil, nil, err
		}
		metrics.RecordToolCall("reindex", true)
		metrics.RecordReindex("mcp_tool", true)
		logger.InfoContext(ctx, "reindex complete",
			slog.String("event", "reindex_complete"),
			slog.String("trigger", "mcp_tool"),
			slog.Int("files", stats.Files),
			slog.Int("docs_files", stats.DocsFiles),
			slog.Int("code_files", stats.CodeFiles),
			slog.Int("chunks", stats.Chunks),
			slog.Int("changed_files", stats.ChangedFiles),
			slog.Int("deleted_files", stats.DeletedFiles),
			slog.Int("reused_files", stats.ReusedFiles),
			slog.Int("embedded_chunks", stats.EmbeddedChunks),
			slog.Int("reused_chunks", stats.ReusedChunks),
			slog.Int("embed_batch_size", stats.EmbedBatchSize),
			slog.String("generation", stats.Generation),
			slog.String("index_subdir", stats.IndexSubdir),
			slog.String("job_id", jobID),
			slog.Int64("duration_ms", durationMillis(start)),
		)
		return nil, map[string]any{
			"ok":               true,
			"files":            stats.Files,
			"docs_files":       stats.DocsFiles,
			"code_files":       stats.CodeFiles,
			"chunks":           stats.Chunks,
			"changed_files":    stats.ChangedFiles,
			"deleted_files":    stats.DeletedFiles,
			"reused_files":     stats.ReusedFiles,
			"embedded_chunks":  stats.EmbeddedChunks,
			"reused_chunks":    stats.ReusedChunks,
			"embed_batch_size": stats.EmbedBatchSize,
			"generation":       stats.Generation,
			"index_subdir":     stats.IndexSubdir,
			"job_id":           jobID,
		}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "reindex_status",
		Description: "Return the current and last reindex job status",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ *struct{}) (*mcp.CallToolResult, any, error) {
		start := time.Now()
		status, err := ragSvc.ReindexStatus(ctx)
		if err != nil {
			logToolError(ctx, logger, "reindex_status", start, err)
			metrics.RecordToolCall("reindex_status", false)
			return nil, nil, err
		}
		metrics.RecordToolCall("reindex_status", true)
		logger.InfoContext(ctx, "tool call complete",
			slog.String("event", "tool_call"),
			slog.String("tool", "reindex_status"),
			slog.String("status", status.Status),
			slog.Int64("duration_ms", durationMillis(start)),
		)
		return nil, status, nil
	})

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, nil)
	return wrapMCPHandler(handler, defaultMaxMCPBodyBytes)
}

func newMux(mcpHandler http.Handler, ragSvc ragService, logger *slog.Logger, metrics *observability.Metrics) *http.ServeMux {
	logger = componentLogger(logger, "rag-mcp")
	api := uiAPI{ragSvc: ragSvc}

	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpHandler)
	mux.Handle("/mcp/", mcpHandler)
	mux.Handle("/api/search", withUISecurityHeaders(http.HandlerFunc(api.handleSearch)))
	mux.Handle("/api/search-settings", withUISecurityHeaders(http.HandlerFunc(api.handleSearchSettings)))
	mux.Handle("/api/chunk", withUISecurityHeaders(http.HandlerFunc(api.handleChunk)))
	mux.Handle("/api/sources", withUISecurityHeaders(http.HandlerFunc(api.handleSources)))
	mux.Handle("/ui/", withUISecurityHeaders(http.StripPrefix("/ui/", newUIHandler())))
	mux.HandleFunc("/ui", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), defaultReadinessTimeout)
		defer cancel()

		report := ragSvc.CheckReadiness(ctx)
		metrics.RecordReadiness(report)
		for _, dependency := range report.Dependencies {
			if dependency.Status == observability.StatusOK {
				continue
			}
			logger.WarnContext(r.Context(), "dependency unhealthy",
				slog.String("event", "dependency_unhealthy"),
				slog.String("dependency", dependency.Name),
				slog.String("error", dependency.Error),
				slog.String("hint", dependency.Hint),
			)
		}

		w.Header().Set("Content-Type", "application/json")
		if !report.Ready() {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(report)
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_ = metrics.WritePrometheus(w)
	})
	mux.Handle("/", withUISecurityHeaders(newUIHandler()))
	return mux
}

type uiAPI struct {
	ragSvc ragService
}

func (api uiAPI) handleSearch(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var input searchInput
	if !decodeUIJSON(w, r, &input) {
		return
	}

	query := strings.TrimSpace(input.Query)
	if query == "" {
		writeAPIError(w, http.StatusBadRequest, "query is required")
		return
	}

	scope, ok := normalizeUIScope(w, input.Scope)
	if !ok {
		return
	}

	if input.TopK < 0 {
		writeAPIError(w, http.StatusBadRequest, "top_k must be zero or positive")
		return
	}
	if input.MaxDistance != nil && !validSearchDistance(*input.MaxDistance) {
		writeAPIError(w, http.StatusBadRequest, fmt.Sprintf("max_distance must be between %.2f and %.2f", config.MinSearchDistance, config.MaxSearchDistance))
		return
	}

	response, err := api.ragSvc.SearchWithOptions(r.Context(), rag.SearchOptions{
		Query:        query,
		TopK:         input.TopK,
		Scope:        scope,
		SourceFilter: strings.TrimSpace(input.SourceFilter),
		MaxDistance:  input.MaxDistance,
	})
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "search service unavailable")
		return
	}
	writeAPIJSON(w, http.StatusOK, response)
}

func (api uiAPI) handleSearchSettings(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	writeAPIJSON(w, http.StatusOK, api.ragSvc.SearchSettings())
}

func (api uiAPI) handleChunk(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var input getChunkInput
	if !decodeUIJSON(w, r, &input) {
		return
	}

	chunkID := strings.TrimSpace(input.ChunkID)
	if chunkID == "" {
		writeAPIError(w, http.StatusBadRequest, "chunk_id is required")
		return
	}

	response := api.ragSvc.GetChunk(r.Context(), chunkID)
	if response.Error != "" {
		writeAPIError(w, http.StatusBadGateway, "chunk service unavailable")
		return
	}
	if !response.Found {
		writeAPIJSON(w, http.StatusNotFound, response)
		return
	}
	writeAPIJSON(w, http.StatusOK, response)
}

func (api uiAPI) handleSources(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	scope, ok := normalizeUIScope(w, r.URL.Query().Get("scope"))
	if !ok {
		return
	}

	response, err := api.ragSvc.ListSources(r.Context(), scope)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "sources service unavailable")
		return
	}
	writeAPIJSON(w, http.StatusOK, response)
}

func newUIHandler() http.Handler {
	uiFS, err := fs.Sub(webAssets, "web")
	if err != nil {
		panic(fmt.Sprintf("web assets unavailable: %v", err))
	}
	return http.FileServer(http.FS(uiFS))
}

func withUISecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	w.Header().Set("Allow", method)
	writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
	return false
}

func decodeUIJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, defaultMaxUIAPIBodyBytes)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeAPIError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		writeAPIError(w, http.StatusBadRequest, "invalid json body")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeAPIError(w, http.StatusBadRequest, "request body must contain a single json object")
		return false
	}
	return true
}

func normalizeUIScope(w http.ResponseWriter, input string) (string, bool) {
	scope := strings.ToLower(strings.TrimSpace(input))
	if scope == "" {
		return "all", true
	}
	switch scope {
	case "all", "docs", "code":
		return scope, true
	default:
		writeAPIError(w, http.StatusBadRequest, "scope must be one of all, docs, code")
		return "", false
	}
}

func validSearchDistance(value float64) bool {
	return value >= config.MinSearchDistance && value <= config.MaxSearchDistance
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	writeAPIJSON(w, status, map[string]string{"error": message})
}

func writeAPIJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func newHTTPServer(cfg *config.Config, handler http.Handler) *http.Server {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadTimeout:       defaultReadTimeout,
		WriteTimeout:      defaultWriteTimeout,
		IdleTimeout:       defaultIdleTimeout,
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		MaxHeaderBytes:    defaultMaxHeaderBytes,
	}
}

func wrapMCPHandler(next http.Handler, maxBodyBytes int64) http.Handler {
	return limitRequestBodyMiddleware(maxBodyBytes, next)
}

func limitRequestBodyMiddleware(maxBodyBytes int64, next http.Handler) http.Handler {
	if maxBodyBytes <= 0 {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > maxBodyBytes {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

func componentLogger(logger *slog.Logger, component string) *slog.Logger {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return logger.With(slog.String("component", component))
}

func logToolError(ctx context.Context, logger *slog.Logger, tool string, start time.Time, err error, attrs ...slog.Attr) {
	all := []slog.Attr{
		slog.String("event", "tool_error"),
		slog.String("tool", tool),
		slog.String("error", err.Error()),
		slog.Int64("duration_ms", durationMillis(start)),
	}
	if dependency := dependencyForToolError(tool, err); dependency != "" {
		all = append(all,
			slog.String("dependency", dependency),
			slog.String("hint", observability.DependencyHint(dependency)),
		)
	}
	all = append(all, attrs...)
	logger.LogAttrs(ctx, slog.LevelError, "tool call failed", all...)
}

func logReindexBlocked(ctx context.Context, logger *slog.Logger, busy *reindexjob.BusyError) {
	attrs := []slog.Attr{
		slog.String("event", "reindex_blocked"),
		slog.String("tool", "reindex"),
		slog.String("trigger", busy.BlockedStart.Trigger),
		slog.String("status", busy.BlockedStart.Status),
		slog.String("error", busy.Error()),
	}
	if busy.BlockedStart.ActiveJob != nil {
		attrs = append(attrs,
			slog.String("active_job_id", busy.BlockedStart.ActiveJob.ID),
			slog.String("active_trigger", busy.BlockedStart.ActiveJob.Trigger),
		)
	}
	logger.LogAttrs(ctx, slog.LevelWarn, "reindex already running", attrs...)
}

func dependencyForToolError(tool string, err error) string {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "ollama") || strings.Contains(msg, "embed") {
		return "ollama"
	}
	if strings.Contains(msg, "chroma") {
		return "chroma"
	}
	switch tool {
	case "get_chunk", "list_sources":
		return "chroma"
	default:
		return ""
	}
}

func durationMillis(start time.Time) int64 {
	return time.Since(start).Milliseconds()
}
