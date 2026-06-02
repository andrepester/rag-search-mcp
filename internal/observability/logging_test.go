package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestNormalizeLogLevel(t *testing.T) {
	level, err := NormalizeLogLevel(" WARN ")
	if err != nil {
		t.Fatalf("NormalizeLogLevel() failed: %v", err)
	}
	if level != "warn" {
		t.Fatalf("level = %q, want warn", level)
	}

	if _, err := NormalizeLogLevel("loud"); err == nil {
		t.Fatal("expected invalid level error")
	}
}

func TestNormalizeLogFormat(t *testing.T) {
	format, err := NormalizeLogFormat("")
	if err != nil {
		t.Fatalf("NormalizeLogFormat() failed: %v", err)
	}
	if format != DefaultLogFormat {
		t.Fatalf("format = %q, want %q", format, DefaultLogFormat)
	}

	if _, err := NormalizeLogFormat("xml"); err == nil {
		t.Fatal("expected invalid format error")
	}
}

func TestNewLoggerWritesJSONToProvidedWriter(t *testing.T) {
	var buf bytes.Buffer
	logger, err := NewLogger(&buf, "info", "json")
	if err != nil {
		t.Fatalf("NewLogger() failed: %v", err)
	}

	logger.InfoContext(context.Background(), "tool call complete", slog.String("event", "tool_call"))

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("log output is not JSON: %v\n%s", err, buf.String())
	}
	if record["event"] != "tool_call" {
		t.Fatalf("event = %v, want tool_call", record["event"])
	}
}

func TestNewLoggerTextFormat(t *testing.T) {
	var buf bytes.Buffer
	logger, err := NewLogger(&buf, "info", "text")
	if err != nil {
		t.Fatalf("NewLogger() failed: %v", err)
	}

	logger.Info("service start", slog.String("event", "service_start"))

	if !strings.Contains(buf.String(), "event=service_start") {
		t.Fatalf("expected text log field, got %q", buf.String())
	}
}
