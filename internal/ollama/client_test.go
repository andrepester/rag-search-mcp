package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbedEmptyInput(t *testing.T) {
	client := New("http://127.0.0.1:11434")
	embeddings, err := client.Embed(context.Background(), "model", nil)
	if err != nil {
		t.Fatalf("Embed() failed: %v", err)
	}
	if embeddings != nil {
		t.Fatalf("embeddings = %v, want nil", embeddings)
	}
}

func TestCheck(t *testing.T) {
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer server.Close()

	client := New(server.URL)
	if err := client.Check(context.Background()); err != nil {
		t.Fatalf("Check() failed: %v", err)
	}
	if path != "/api/tags" {
		t.Fatalf("path = %q, want /api/tags", path)
	}
}

func TestCheckHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	client := New(server.URL)
	err := client.Check(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEmbedHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	client := New(server.URL)
	_, err := client.Embed(context.Background(), "model", []string{"hello"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEmbedSendsNumThreadOption(t *testing.T) {
	var payload struct {
		Model   string         `json:"model"`
		Input   []string       `json:"input"`
		Options map[string]any `json:"options"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"embeddings":[[0.1,0.2]]}`))
	}))
	defer server.Close()

	client := New(server.URL)
	_, err := client.Embed(context.Background(), "model", []string{"hello"}, EmbedOptions{NumThreads: 4})
	if err != nil {
		t.Fatalf("Embed() failed: %v", err)
	}
	if payload.Model != "model" || len(payload.Input) != 1 || payload.Input[0] != "hello" {
		t.Fatalf("unexpected request payload: %+v", payload)
	}
	if payload.Options["num_thread"] != float64(4) {
		t.Fatalf("num_thread option = %v, want 4", payload.Options["num_thread"])
	}
}

func TestEmbedDecodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	defer server.Close()

	client := New(server.URL)
	_, err := client.Embed(context.Background(), "model", []string{"hello"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "decode embed response") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEmbedCountMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"embeddings":[[0.1,0.2]]}`))
	}))
	defer server.Close()

	client := New(server.URL)
	_, err := client.Embed(context.Background(), "model", []string{"one", "two"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unexpected embedding count") {
		t.Fatalf("unexpected error: %v", err)
	}
}
