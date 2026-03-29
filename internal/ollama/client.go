package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	http    *http.Client
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
}

func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (c *Client) Embed(ctx context.Context, model string, inputs []string) ([][]float64, error) {
	if len(inputs) == 0 {
		return nil, nil
	}

	body, err := json.Marshal(embedRequest{Model: model, Input: inputs})
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embed request failed with HTTP %d", resp.StatusCode)
	}

	var payload embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode embed response: %w", err)
	}

	if len(payload.Embeddings) != len(inputs) {
		return nil, fmt.Errorf("unexpected embedding count: got %d want %d", len(payload.Embeddings), len(inputs))
	}

	return payload.Embeddings, nil
}
