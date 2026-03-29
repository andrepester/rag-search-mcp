package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Host             string
	Port             int
	DocsDir          string
	CodeDir          string
	ChromaURL        string
	ChromaTenant     string
	ChromaDatabase   string
	CollectionName   string
	OllamaHost       string
	EmbedModel       string
	ChunkSize        int
	ChunkOverlap     int
	DefaultScope     string
	MaxTopK          int
	EnableCodeIngest bool
}

func Load() (Config, error) {
	chunkSize := envInt("RAG_CHUNK_SIZE", 1200)
	chunkOverlap := envInt("RAG_CHUNK_OVERLAP", 200)
	if chunkSize <= 0 {
		return Config{}, fmt.Errorf("RAG_CHUNK_SIZE must be > 0")
	}
	if chunkOverlap < 0 || chunkOverlap >= chunkSize {
		return Config{}, fmt.Errorf("RAG_CHUNK_OVERLAP must be >= 0 and smaller than RAG_CHUNK_SIZE")
	}

	defaultScope := strings.ToLower(strings.TrimSpace(env("RAG_SCOPE_DEFAULT", "all")))
	if defaultScope == "" {
		defaultScope = "all"
	}
	if defaultScope != "all" && defaultScope != "docs" && defaultScope != "code" {
		return Config{}, fmt.Errorf("RAG_SCOPE_DEFAULT must be one of all, docs, code")
	}

	docsDir, err := filepath.Abs(env("RAG_DOCS_DIR", "./docs"))
	if err != nil {
		return Config{}, fmt.Errorf("failed to resolve RAG_DOCS_DIR: %w", err)
	}
	codeDir, err := filepath.Abs(env("RAG_CODE_DIR", "./.empty-code"))
	if err != nil {
		return Config{}, fmt.Errorf("failed to resolve RAG_CODE_DIR: %w", err)
	}

	cfg := Config{
		Host:             env("RAG_HTTP_HOST", "0.0.0.0"),
		Port:             envInt("RAG_HTTP_PORT", 8080),
		DocsDir:          docsDir,
		CodeDir:          codeDir,
		ChromaURL:        strings.TrimRight(env("RAG_CHROMA_URL", "http://chroma:8000"), "/"),
		ChromaTenant:     env("RAG_CHROMA_TENANT", "default_tenant"),
		ChromaDatabase:   env("RAG_CHROMA_DATABASE", "default_database"),
		CollectionName:   env("RAG_COLLECTION_NAME", "rag"),
		OllamaHost:       strings.TrimRight(env("OLLAMA_HOST", "http://host.docker.internal:11434"), "/"),
		EmbedModel:       env("EMBED_MODEL", "nomic-embed-text"),
		ChunkSize:        chunkSize,
		ChunkOverlap:     chunkOverlap,
		DefaultScope:     defaultScope,
		MaxTopK:          envInt("RAG_MAX_TOP_K", 50),
		EnableCodeIngest: envBool("RAG_ENABLE_CODE_INGEST", true),
	}

	if cfg.MaxTopK <= 0 {
		cfg.MaxTopK = 50
	}

	return cfg, nil
}

func env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func envInt(key string, fallback int) int {
	raw, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) bool {
	raw, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
