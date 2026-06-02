package observability

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

const (
	DefaultLogLevel  = "info"
	DefaultLogFormat = "json"
)

func NormalizeLogLevel(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		normalized = DefaultLogLevel
	}
	switch normalized {
	case "debug", "info", "warn", "error":
		return normalized, nil
	default:
		return "", fmt.Errorf("RAG_LOG_LEVEL must be one of debug, info, warn, error")
	}
}

func NormalizeLogFormat(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		normalized = DefaultLogFormat
	}
	switch normalized {
	case "json", "text":
		return normalized, nil
	default:
		return "", fmt.Errorf("RAG_LOG_FORMAT must be one of json, text")
	}
}

func NewLogger(w io.Writer, levelValue, formatValue string) (*slog.Logger, error) {
	level, err := ParseLogLevel(levelValue)
	if err != nil {
		return nil, err
	}
	format, err := NormalizeLogFormat(formatValue)
	if err != nil {
		return nil, err
	}

	opts := &slog.HandlerOptions{Level: level}
	if format == "text" {
		return slog.New(slog.NewTextHandler(w, opts)), nil
	}
	return slog.New(slog.NewJSONHandler(w, opts)), nil
}

func NewFallbackLogger(w io.Writer, levelValue, formatValue string) *slog.Logger {
	logger, err := NewLogger(w, levelValue, formatValue)
	if err == nil {
		return logger
	}
	logger, _ = NewLogger(w, DefaultLogLevel, DefaultLogFormat)
	return logger
}

func ParseLogLevel(value string) (slog.Level, error) {
	normalized, err := NormalizeLogLevel(value)
	if err != nil {
		return slog.LevelInfo, err
	}
	switch normalized {
	case "debug":
		return slog.LevelDebug, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, nil
	}
}

func DependencyHint(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "chroma":
		return "check the chroma service, RAG_CHROMA_URL, tenant, database, and collection settings"
	case "ollama":
		return "check the ollama service, OLLAMA_HOST, and embedding model availability"
	default:
		return "check the dependent service configuration and container status"
	}
}
