package observability

import "testing"

func TestNewReadinessReport(t *testing.T) {
	report := NewReadinessReport([]DependencyStatus{
		{Name: "chroma", Status: StatusOK},
		{Name: "ollama", Status: StatusError, Error: "down"},
	})

	if report.Ready() {
		t.Fatal("report should not be ready with a failed dependency")
	}
	if report.Status != StatusError {
		t.Fatalf("Status = %q, want error", report.Status)
	}
}
