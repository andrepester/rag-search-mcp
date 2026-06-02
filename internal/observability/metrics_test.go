package observability

import (
	"bytes"
	"strings"
	"testing"
)

func TestMetricsWritePrometheus(t *testing.T) {
	metrics := NewMetrics()
	metrics.RecordToolCall("search", true)
	metrics.RecordToolCall("search", false)
	metrics.RecordReindex("mcp_tool", true)
	metrics.RecordReadiness(NewReadinessReport([]DependencyStatus{
		{Name: "chroma", Status: StatusOK},
		{Name: "ollama", Status: StatusError},
	}))

	var out bytes.Buffer
	if err := metrics.WritePrometheus(&out); err != nil {
		t.Fatalf("WritePrometheus() failed: %v", err)
	}

	output := out.String()
	for _, want := range []string{
		`rag_mcp_tool_calls_total{tool="search",status="ok"} 1`,
		`rag_mcp_tool_calls_total{tool="search",status="error"} 1`,
		`rag_mcp_reindex_runs_total{trigger="mcp_tool",status="ok"} 1`,
		`rag_mcp_readiness_dependency_up{dependency="chroma"} 1`,
		`rag_mcp_readiness_dependency_up{dependency="ollama"} 0`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q in\n%s", want, output)
		}
	}
}
