package config

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultMaxSearchDistance = 0.50
	MinSearchDistance        = 0.01
	MaxSearchDistance        = 2.00
	DefaultEmbedConcurrency  = 2
	DefaultEmbedNumThreads   = 0
	// DefaultReindexTimeout is only the process fallback; RAG_REINDEX_TIMEOUT
	// from the environment, .env, or Compose takes precedence.
	DefaultReindexTimeout = "60m"
)

type Config struct {
	Host              string
	Port              int
	DocsDir           string
	CodeDir           string
	ChromaURL         string
	ChromaTenant      string
	ChromaDatabase    string
	CollectionName    string
	IndexStateDir     string
	OllamaHost        string
	EmbedModel        string
	ChunkSize         int
	ChunkOverlap      int
	DefaultScope      string
	MaxTopK           int
	MaxSearchDistance float64
	EnableCodeIngest  bool
	FreshIndex        bool
	IndexLimit        int
	IndexSubdir       string
	EmbedConcurrency  int
	EmbedNumThreads   int
	ReindexTimeout    time.Duration
	LogLevel          string
	LogFormat         string
}

func Load() (Config, error) {
	chunkSize, err := envInt("RAG_CHUNK_SIZE", 1200)
	if err != nil {
		return Config{}, err
	}
	chunkOverlap, err := envInt("RAG_CHUNK_OVERLAP", 200)
	if err != nil {
		return Config{}, err
	}
	if chunkSize <= 0 {
		return Config{}, fmt.Errorf("RAG_CHUNK_SIZE must be > 0")
	}
	if chunkOverlap < 0 || chunkOverlap >= chunkSize {
		return Config{}, fmt.Errorf("RAG_CHUNK_OVERLAP must be >= 0 and smaller than RAG_CHUNK_SIZE")
	}

	port, err := envInt("RAG_HTTP_PORT", 8765)
	if err != nil {
		return Config{}, err
	}
	if port < 1 || port > 65535 {
		return Config{}, fmt.Errorf("RAG_HTTP_PORT must be between 1 and 65535")
	}

	maxTopK, err := envInt("RAG_MAX_TOP_K", 50)
	if err != nil {
		return Config{}, err
	}
	if maxTopK <= 0 {
		return Config{}, fmt.Errorf("RAG_MAX_TOP_K must be > 0")
	}

	maxSearchDistance, err := envFloat("RAG_MAX_SEARCH_DISTANCE", DefaultMaxSearchDistance)
	if err != nil {
		return Config{}, err
	}
	if err := validateSearchDistance("RAG_MAX_SEARCH_DISTANCE", maxSearchDistance); err != nil {
		return Config{}, err
	}

	enableCodeIngest, err := envBool("RAG_ENABLE_CODE_INGEST", true)
	if err != nil {
		return Config{}, err
	}
	freshIndex, err := envBool("FRESH_INDEX", false)
	if err != nil {
		return Config{}, err
	}
	indexLimit, err := envInt("RAG_INDEX_LIMIT", 0)
	if err != nil {
		return Config{}, err
	}
	if indexLimit < 0 {
		return Config{}, fmt.Errorf("RAG_INDEX_LIMIT must be >= 0")
	}
	indexSubdir, err := envIndexSubdir("RAG_INDEX_SUBDIR")
	if err != nil {
		return Config{}, err
	}
	if indexSubdir != "" && strings.HasPrefix(indexSubdir, "code/") && !enableCodeIngest {
		return Config{}, fmt.Errorf("RAG_INDEX_SUBDIR uses code scope but RAG_ENABLE_CODE_INGEST is disabled")
	}
	embedConcurrency, err := envInt("RAG_EMBED_CONCURRENCY", DefaultEmbedConcurrency)
	if err != nil {
		return Config{}, err
	}
	if embedConcurrency <= 0 {
		return Config{}, fmt.Errorf("RAG_EMBED_CONCURRENCY must be > 0")
	}
	embedNumThreads, err := envInt("RAG_EMBED_NUM_THREADS", DefaultEmbedNumThreads)
	if err != nil {
		return Config{}, err
	}
	if embedNumThreads < 0 {
		return Config{}, fmt.Errorf("RAG_EMBED_NUM_THREADS must be >= 0")
	}
	reindexTimeout, err := envDuration("RAG_REINDEX_TIMEOUT", DefaultReindexTimeout)
	if err != nil {
		return Config{}, err
	}
	if reindexTimeout <= 0 {
		return Config{}, fmt.Errorf("RAG_REINDEX_TIMEOUT must be > 0")
	}

	defaultScope := strings.ToLower(strings.TrimSpace(env("RAG_SCOPE_DEFAULT", "all")))
	if defaultScope == "" {
		defaultScope = "all"
	}
	if defaultScope != "all" && defaultScope != "docs" && defaultScope != "code" {
		return Config{}, fmt.Errorf("RAG_SCOPE_DEFAULT must be one of all, docs, code")
	}

	logLevel := strings.ToLower(strings.TrimSpace(env("RAG_LOG_LEVEL", "info")))
	if logLevel != "debug" && logLevel != "info" && logLevel != "warn" && logLevel != "error" {
		return Config{}, fmt.Errorf("RAG_LOG_LEVEL must be one of debug, info, warn, error")
	}

	logFormat := strings.ToLower(strings.TrimSpace(env("RAG_LOG_FORMAT", "json")))
	if logFormat != "json" && logFormat != "text" {
		return Config{}, fmt.Errorf("RAG_LOG_FORMAT must be one of json, text")
	}

	docsDir, err := filepath.Abs(env("RAG_DOCS_DIR", "./data/docs"))
	if err != nil {
		return Config{}, fmt.Errorf("failed to resolve RAG_DOCS_DIR: %w", err)
	}
	codeDir, err := filepath.Abs(env("RAG_CODE_DIR", "./data/code"))
	if err != nil {
		return Config{}, fmt.Errorf("failed to resolve RAG_CODE_DIR: %w", err)
	}
	indexStateDir, err := filepath.Abs(env("RAG_INDEX_STATE_DIR", "./data/index-state"))
	if err != nil {
		return Config{}, fmt.Errorf("failed to resolve RAG_INDEX_STATE_DIR: %w", err)
	}
	ollamaHost := strings.TrimRight(env("OLLAMA_HOST", ""), "/")
	if strings.TrimSpace(ollamaHost) == "" {
		return Config{}, fmt.Errorf("OLLAMA_HOST must be set to the shared Ollama endpoint")
	}

	cfg := Config{
		Host:              env("RAG_HTTP_HOST", "127.0.0.1"),
		Port:              port,
		DocsDir:           docsDir,
		CodeDir:           codeDir,
		ChromaURL:         strings.TrimRight(env("RAG_CHROMA_URL", "http://chroma:8000"), "/"),
		ChromaTenant:      env("RAG_CHROMA_TENANT", "default_tenant"),
		ChromaDatabase:    env("RAG_CHROMA_DATABASE", "default_database"),
		CollectionName:    env("RAG_COLLECTION_NAME", "rag"),
		IndexStateDir:     indexStateDir,
		OllamaHost:        ollamaHost,
		EmbedModel:        env("EMBED_MODEL", "nomic-embed-text"),
		ChunkSize:         chunkSize,
		ChunkOverlap:      chunkOverlap,
		DefaultScope:      defaultScope,
		MaxTopK:           maxTopK,
		MaxSearchDistance: maxSearchDistance,
		EnableCodeIngest:  enableCodeIngest,
		FreshIndex:        freshIndex,
		IndexLimit:        indexLimit,
		IndexSubdir:       indexSubdir,
		EmbedConcurrency:  embedConcurrency,
		EmbedNumThreads:   embedNumThreads,
		ReindexTimeout:    reindexTimeout,
		LogLevel:          logLevel,
		LogFormat:         logFormat,
	}

	return cfg, nil
}

func env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func envInt(key string, fallback int) (int, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return value, nil
}

func envFloat(key string, fallback float64) (float64, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number", key)
	}
	return value, nil
}

func envDuration(key string, fallback string) (time.Duration, error) {
	raw := fallback
	if value, ok := os.LookupEnv(key); ok && strings.TrimSpace(value) != "" {
		raw = value
	}
	value, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration such as 60m or 1h", key)
	}
	return value, nil
}

func envIndexSubdir(key string) (string, error) {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return "", nil
	}
	return normalizeIndexSubdir(key, raw)
}

func normalizeIndexSubdir(key, raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("%s must not be empty when set", key)
	}

	normalized := strings.ReplaceAll(trimmed, "\\", "/")
	if path.IsAbs(normalized) || filepath.IsAbs(trimmed) {
		return "", fmt.Errorf("%s must be a scoped relative directory such as docs/demo/technology or code/internal/ingest", key)
	}
	for _, segment := range strings.Split(normalized, "/") {
		if segment == ".." {
			return "", fmt.Errorf("%s must not contain .. path segments", key)
		}
	}

	cleaned := path.Clean(normalized)
	scope, rel, ok := strings.Cut(cleaned, "/")
	if !ok || rel == "" || rel == "." {
		return "", fmt.Errorf("%s must start with docs/ or code/ followed by a subdirectory", key)
	}
	if scope != "docs" && scope != "code" {
		return "", fmt.Errorf("%s must start with docs/ or code/", key)
	}
	return scope + "/" + rel, nil
}

func validateSearchDistance(key string, value float64) error {
	if value < MinSearchDistance || value > MaxSearchDistance {
		return fmt.Errorf("%s must be between %.2f and %.2f", key, MinSearchDistance, MaxSearchDistance)
	}
	return nil
}

func envBool(key string, fallback bool) (bool, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be a boolean", key)
	}
}
