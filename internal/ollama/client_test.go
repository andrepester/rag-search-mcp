package ollama

import (
	"context"
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
