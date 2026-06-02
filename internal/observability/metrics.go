package observability

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

type Metrics struct {
	mu        sync.RWMutex
	toolCalls map[string]int64
	reindexes map[string]int64
	readiness map[string]int
}

func NewMetrics() *Metrics {
	return &Metrics{
		toolCalls: map[string]int64{},
		reindexes: map[string]int64{},
		readiness: map[string]int{},
	}
}

func (m *Metrics) RecordToolCall(tool string, ok bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toolCalls[labelKey(tool, status(ok))]++
}

func (m *Metrics) RecordReindex(trigger string, ok bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reindexes[labelKey(trigger, status(ok))]++
}

func (m *Metrics) RecordReadiness(report ReadinessReport) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, dependency := range report.Dependencies {
		up := 0
		if dependency.Status == StatusOK {
			up = 1
		}
		m.readiness[dependency.Name] = up
	}
}

func (m *Metrics) WritePrometheus(w io.Writer) error {
	if m == nil {
		m = NewMetrics()
	}

	m.mu.RLock()
	toolCalls := copyInt64Map(m.toolCalls)
	reindexes := copyInt64Map(m.reindexes)
	readiness := copyIntMap(m.readiness)
	m.mu.RUnlock()

	if _, err := fmt.Fprintln(w, "# HELP rag_mcp_tool_calls_total MCP tool calls by tool and status."); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "# TYPE rag_mcp_tool_calls_total counter"); err != nil {
		return err
	}
	for _, key := range sortedKeys(toolCalls) {
		parts := splitLabelKey(key)
		if _, err := fmt.Fprintf(w, "rag_mcp_tool_calls_total{tool=%q,status=%q} %d\n", parts[0], parts[1], toolCalls[key]); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintln(w, "# HELP rag_mcp_reindex_runs_total Reindex runs by trigger and status."); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "# TYPE rag_mcp_reindex_runs_total counter"); err != nil {
		return err
	}
	for _, key := range sortedKeys(reindexes) {
		parts := splitLabelKey(key)
		if _, err := fmt.Fprintf(w, "rag_mcp_reindex_runs_total{trigger=%q,status=%q} %d\n", parts[0], parts[1], reindexes[key]); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintln(w, "# HELP rag_mcp_readiness_dependency_up Last readiness state by dependency, 1 for healthy and 0 for unhealthy."); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "# TYPE rag_mcp_readiness_dependency_up gauge"); err != nil {
		return err
	}
	for _, dependency := range sortedKeys(readiness) {
		if _, err := fmt.Fprintf(w, "rag_mcp_readiness_dependency_up{dependency=%q} %d\n", dependency, readiness[dependency]); err != nil {
			return err
		}
	}

	return nil
}

func status(ok bool) string {
	if ok {
		return StatusOK
	}
	return StatusError
}

func labelKey(a, b string) string {
	return sanitizeLabelValue(a) + "\x00" + sanitizeLabelValue(b)
}

func splitLabelKey(key string) [2]string {
	parts := strings.SplitN(key, "\x00", 2)
	if len(parts) != 2 {
		return [2]string{key, ""}
	}
	return [2]string{parts[0], parts[1]}
}

func sanitizeLabelValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}

func copyInt64Map(in map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func copyIntMap(in map[string]int) map[string]int {
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
