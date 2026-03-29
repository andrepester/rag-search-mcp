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

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}

	ollamaClient := ollama.New(cfg.OllamaHost)
	chromaClient := store.NewChromaClient(cfg.ChromaURL, cfg.ChromaTenant, cfg.ChromaDatabase)
	ingestSvc := &ingest.Service{Config: &cfg, Ollama: ollamaClient, Chroma: chromaClient}
	ragSvc, err := rag.NewService(&cfg, ingestSvc, ollamaClient, chromaClient)
	if err != nil {
		log.Fatalf("service init failed: %v", err)
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "rag",
		Version: "1.0.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search",
		Description: "Semantic search over indexed docs and optional code (default scope=all)",
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
		Description: "Rebuild the index from docs and optional code sources",
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

	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	mux.Handle("/mcp/", handler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	log.Printf("rag-mcp listening on %s", addr)

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("http server failed: %v", err)
	}
}
