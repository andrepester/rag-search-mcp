package rag

import "testing"

func TestNormalizeScope(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		fallback string
		want     string
	}{
		{name: "default fallback", input: "", fallback: "all", want: "all"},
		{name: "docs value", input: "docs", fallback: "all", want: "docs"},
		{name: "code value", input: "code", fallback: "all", want: "code"},
		{name: "invalid", input: "foo", fallback: "all", want: "all"},
		{name: "invalid fallback", input: "", fallback: "nope", want: "all"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeScope(tt.input, tt.fallback)
			if got != tt.want {
				t.Fatalf("normalizeScope() = %q, want %q", got, tt.want)
			}
		})
	}
}
