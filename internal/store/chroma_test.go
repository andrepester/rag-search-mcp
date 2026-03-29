package store

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDoJSONHTTPErrorAndNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "missing"})
	}))
	defer server.Close()

	client := NewChromaClient(server.URL, "default_tenant", "default_database")
	err := client.doJSON(context.Background(), http.MethodGet, "/missing", nil, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsNotFound(err) {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func TestQueryMapsResponse(t *testing.T) {
	base := "/api/v2/tenants/default_tenant/databases/default_database"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != base+"/collections/col-1/query" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ids":       [][]string{{"chunk-1", "chunk-2"}},
			"documents": [][]*string{{ptr("first"), nil}},
			"metadatas": [][]map[string]any{{{"scope": "docs"}, nil}},
			"distances": [][]*float64{{ptrFloat(0.2), ptrFloat(0.5)}},
		})
	}))
	defer server.Close()

	client := NewChromaClient(server.URL, "default_tenant", "default_database")
	matches, err := client.Query(context.Background(), "col-1", []float64{0.1, 0.2}, 2, nil)
	if err != nil {
		t.Fatalf("Query() failed: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("len(matches) = %d, want 2", len(matches))
	}
	if matches[0].ID != "chunk-1" || matches[0].Document != "first" {
		t.Fatalf("unexpected first match: %+v", matches[0])
	}
	if matches[1].Metadata == nil {
		t.Fatal("expected metadata map fallback")
	}
}

func TestListSourcePathsPaginatesDeduplicatesAndSorts(t *testing.T) {
	base := "/api/v2/tenants/default_tenant/databases/default_database"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != base+"/collections/col-1/get" {
			http.NotFound(w, r)
			return
		}

		var payload struct {
			Where  map[string]any `json:"where"`
			Limit  int            `json:"limit"`
			Offset int            `json:"offset"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		scope, _ := payload.Where["scope"].(string)
		if scope == "docs" && payload.Offset == 0 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ids":       []string{"d1", "d2"},
				"metadatas": []map[string]any{{"source_path": "docs/a.md"}, {"source_path": "docs/b.md"}},
			})
			return
		}
		if scope == "docs" {
			_ = json.NewEncoder(w).Encode(map[string]any{"ids": []string{}, "metadatas": []map[string]any{}})
			return
		}
		if scope == "code" && payload.Offset == 0 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ids":       []string{"c1", "c2"},
				"metadatas": []map[string]any{{"source_path": "code/main.go"}, {"source_path": "docs/a.md"}},
			})
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{"ids": []string{}, "metadatas": []map[string]any{}})
	}))
	defer server.Close()

	client := NewChromaClient(server.URL, "default_tenant", "default_database")
	sources, err := client.ListSourcePaths(context.Background(), "col-1", "all")
	if err != nil {
		t.Fatalf("ListSourcePaths() failed: %v", err)
	}

	want := []string{"code/main.go", "docs/a.md", "docs/b.md"}
	if len(sources) != len(want) {
		t.Fatalf("len(sources) = %d, want %d (%v)", len(sources), len(want), sources)
	}
	for i := range want {
		if sources[i] != want[i] {
			t.Fatalf("sources[%d] = %q, want %q", i, sources[i], want[i])
		}
	}
}

func TestIsNotFoundFalseForOtherError(t *testing.T) {
	if IsNotFound(errors.New("nope")) {
		t.Fatal("expected false for non-http error")
	}
}

func ptr(v string) *string {
	return &v
}

func ptrFloat(v float64) *float64 {
	return &v
}
