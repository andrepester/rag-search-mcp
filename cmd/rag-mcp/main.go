package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"local-rag/internal/config"
	"local-rag/internal/ingest"
	"local-rag/internal/ollama"
	"local-rag/internal/rag"
	"local-rag/internal/store"
)

const (
	defaultReadTimeout       = 15 * time.Second
	defaultWriteTimeout      = 60 * time.Second
	defaultIdleTimeout       = 120 * time.Second
	defaultMaxHeaderBytes    = 1 << 20 // 1 MiB
	defaultMaxMCPBodyBytes   = 2 << 20 // 2 MiB
	defaultReadHeaderTimeout = 5 * time.Second
)

type searchInput struct {
	Query        string `json:"query" jsonschema:"Search query"`
	TopK         int    `json:"top_k,omitempty" jsonschema:"Maximum number of matches"`
	Scope        string `json:"scope,omitempty" jsonschema:"Search scope: all, docs, code"`
	SourceFilter string `json:"source_filter,omitempty" jsonschema:"Substring filter for source_path"`
}

type getChunkInput struct {
	ChunkID string `json:"chunk_id" jsonschema:"Chunk id from rag_search results"`
}

type listSourcesInput struct {
	Scope string `json:"scope,omitempty" jsonschema:"Source scope: all, docs, code"`
}

type ragService interface {
	Search(ctx context.Context, query string, topK int, scope string, sourceFilter string) (rag.SearchResponse, error)
	GetChunk(ctx context.Context, chunkID string) rag.ChunkResponse
	ListSources(ctx context.Context, scope string) (rag.ListSourcesResponse, error)
	Reindex(ctx context.Context) (ingest.Stats, error)
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
	if err := run(log.Printf); err != nil {
		log.Fatalf("service run failed: %v", err)
	}
}

func run(logf func(string, ...any)) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	ragSvc, err := newRAGService(&cfg)
	if err != nil {
		return fmt.Errorf("service init failed: %w", err)
	}

	httpServer := newHTTPServer(&cfg, newMux(newMCPHandler(ragSvc)))
	logf("rag-mcp listening on %s", httpServer.Addr)

	if err := serveHTTP(httpServer); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http server failed: %w", err)
	}

	return nil
}

func newMCPHandler(ragSvc ragService) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "rag",
		Version: "1.0.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search",
		Description: "Semantic search over indexed docs and code (default scope=all)",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input *searchInput) (*mcp.CallToolResult, any, error) {
		if input == nil {
			return nil, nil, errors.New("input is required")
		}
		response, err := ragSvc.Search(ctx, input.Query, input.TopK, input.Scope, input.SourceFilter)
		if err != nil {
			return nil, nil, err
		}
		return nil, response, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_chunk",
		Description: "Fetch a specific indexed chunk by chunk_id",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input *getChunkInput) (*mcp.CallToolResult, any, error) {
		if input == nil {
			return nil, nil, errors.New("input is required")
		}
		response := ragSvc.GetChunk(ctx, input.ChunkID)
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
		response, err := ragSvc.ListSources(ctx, scope)
		if err != nil {
			return nil, nil, err
		}
		return nil, response, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "reindex",
		Description: "Rebuild the index from docs and code sources",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ *struct{}) (*mcp.CallToolResult, any, error) {
		stats, err := ragSvc.Reindex(ctx)
		if err != nil {
			return nil, nil, err
		}
		return nil, map[string]any{
			"ok":         true,
			"files":      stats.Files,
			"docs_files": stats.DocsFiles,
			"code_files": stats.CodeFiles,
			"chunks":     stats.Chunks,
		}, nil
	})

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, nil)
	return wrapMCPHandler(handler, defaultMaxMCPBodyBytes)
}

func newMux(mcpHandler http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpHandler)
	mux.Handle("/mcp/", mcpHandler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
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
