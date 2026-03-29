package main

import (
	"context"
	"log"

	"local-rag/internal/config"
	"local-rag/internal/ingest"
	"local-rag/internal/ollama"
	"local-rag/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}

	ctx := context.Background()
	ollamaClient := ollama.New(cfg.OllamaHost)
	chromaClient := store.NewChromaClient(cfg.ChromaURL, cfg.ChromaTenant, cfg.ChromaDatabase)
	ingestSvc := &ingest.Service{Config: &cfg, Ollama: ollamaClient, Chroma: chromaClient}

	stats, err := ingestSvc.Reindex(ctx)
	if err != nil {
		log.Fatalf("reindex failed: %v", err)
	}

	log.Printf("reindex complete: files=%d docs_files=%d code_files=%d chunks=%d", stats.Files, stats.DocsFiles, stats.CodeFiles, stats.Chunks)
}
