package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
)

type ChromaClient struct {
	baseURL    string
	tenant     string
	database   string
	httpClient *http.Client
}

type collection struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type queryResponse struct {
	IDs       [][]string         `json:"ids"`
	Documents [][]*string        `json:"documents"`
	Metadatas [][]map[string]any `json:"metadatas"`
	Distances [][]*float64       `json:"distances"`
}

type getResponse struct {
	IDs       []string         `json:"ids"`
	Documents []*string        `json:"documents"`
	Metadatas []map[string]any `json:"metadatas"`
	Include   []string         `json:"include"`
}

type QueryMatch struct {
	ID       string
	Document string
	Metadata map[string]any
	Distance *float64
}

func NewChromaClient(baseURL, tenant, database string) *ChromaClient {
	return &ChromaClient{
		baseURL:  strings.TrimRight(baseURL, "/"),
		tenant:   tenant,
		database: database,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (c *ChromaClient) EnsureCollection(ctx context.Context, name string) (string, error) {
	payload := map[string]any{
		"name":          name,
		"get_or_create": true,
		"metadata": map[string]any{
			"hnsw:space": "cosine",
		},
	}

	var resp collection
	if err := c.doJSON(ctx, http.MethodPost, c.collectionPath("collections"), nil, payload, &resp); err != nil {
		return "", err
	}
	if resp.ID == "" {
		return "", fmt.Errorf("chroma collection response missing id")
	}
	return resp.ID, nil
}

func (c *ChromaClient) Add(ctx context.Context, collectionID string, ids []string, documents []string, metadatas []map[string]any, embeddings [][]float64) error {
	payload := map[string]any{
		"ids":        ids,
		"documents":  documents,
		"metadatas":  metadatas,
		"embeddings": embeddings,
	}
	return c.doJSON(ctx, http.MethodPost, c.collectionPath("collections", collectionID, "upsert"), nil, payload, nil)
}

func (c *ChromaClient) Query(ctx context.Context, collectionID string, embedding []float64, nResults int, where map[string]any) ([]QueryMatch, error) {
	payload := map[string]any{
		"query_embeddings": [][]float64{embedding},
		"n_results":        nResults,
		"include":          []string{"documents", "metadatas", "distances"},
	}
	if len(where) > 0 {
		payload["where"] = where
	}

	var resp queryResponse
	if err := c.doJSON(ctx, http.MethodPost, c.collectionPath("collections", collectionID, "query"), nil, payload, &resp); err != nil {
		return nil, err
	}

	if len(resp.IDs) == 0 {
		return nil, nil
	}

	ids := resp.IDs[0]
	docs := make([]*string, len(ids))
	metas := make([]map[string]any, len(ids))
	dists := make([]*float64, len(ids))

	if len(resp.Documents) > 0 {
		docs = resp.Documents[0]
	}
	if len(resp.Metadatas) > 0 {
		metas = resp.Metadatas[0]
	}
	if len(resp.Distances) > 0 {
		dists = resp.Distances[0]
	}

	matches := make([]QueryMatch, 0, len(ids))
	for i, id := range ids {
		match := QueryMatch{ID: id}
		if i < len(docs) && docs[i] != nil {
			match.Document = *docs[i]
		}
		if i < len(metas) && metas[i] != nil {
			match.Metadata = metas[i]
		} else {
			match.Metadata = map[string]any{}
		}
		if i < len(dists) {
			match.Distance = dists[i]
		}
		matches = append(matches, match)
	}

	return matches, nil
}

func (c *ChromaClient) GetByID(ctx context.Context, collectionID, chunkID string) (*QueryMatch, error) {
	payload := map[string]any{
		"ids":     []string{chunkID},
		"include": []string{"documents", "metadatas"},
	}

	var resp getResponse
	if err := c.doJSON(ctx, http.MethodPost, c.collectionPath("collections", collectionID, "get"), nil, payload, &resp); err != nil {
		return nil, err
	}
	if len(resp.IDs) == 0 {
		return nil, nil
	}

	match := &QueryMatch{ID: resp.IDs[0], Metadata: map[string]any{}}
	if len(resp.Documents) > 0 && resp.Documents[0] != nil {
		match.Document = *resp.Documents[0]
	}
	if len(resp.Metadatas) > 0 && resp.Metadatas[0] != nil {
		match.Metadata = resp.Metadatas[0]
	}
	return match, nil
}

func (c *ChromaClient) ListSourcePaths(ctx context.Context, collectionID string, scope string) ([]string, error) {
	all := make(map[string]struct{})
	var scopes []string
	switch scope {
	case "docs", "code":
		scopes = []string{scope}
	default:
		scopes = []string{"docs", "code"}
	}

	for _, currentScope := range scopes {
		offset := 0
		for {
			payload := map[string]any{
				"where":   map[string]any{"scope": currentScope},
				"include": []string{"metadatas"},
				"limit":   500,
				"offset":  offset,
			}

			var resp getResponse
			if err := c.doJSON(ctx, http.MethodPost, c.collectionPath("collections", collectionID, "get"), nil, payload, &resp); err != nil {
				if isNotFound(err) {
					break
				}
				return nil, err
			}

			if len(resp.IDs) == 0 {
				break
			}

			for _, meta := range resp.Metadatas {
				if meta == nil {
					continue
				}
				if source, ok := meta["source_path"].(string); ok && source != "" {
					all[source] = struct{}{}
				}
			}

			offset += len(resp.IDs)
		}
	}

	out := make([]string, 0, len(all))
	for source := range all {
		out = append(out, source)
	}
	sort.Strings(out)
	return out, nil
}

func (c *ChromaClient) DeleteCollection(ctx context.Context, collectionID string) error {
	return c.doJSON(ctx, http.MethodDelete, c.collectionPath("collections", collectionID), nil, nil, nil)
}

func (c *ChromaClient) collectionPath(parts ...string) string {
	base := path.Join("/api/v2/tenants", c.tenant, "databases", c.database)
	all := append([]string{base}, parts...)
	return strings.Join(all, "/")
}

func (c *ChromaClient) doJSON(ctx context.Context, method, endpoint string, query url.Values, requestBody any, out any) error {
	fullURL := c.baseURL + endpoint
	if query != nil {
		fullURL += "?" + query.Encode()
	}

	var body *bytes.Reader
	if requestBody == nil {
		body = bytes.NewReader(nil)
	} else {
		payload, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("marshal request payload: %w", err)
		}
		body = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		var errBody map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		return &HTTPError{StatusCode: resp.StatusCode, Body: errBody}
	}

	if out == nil {
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

type HTTPError struct {
	StatusCode int
	Body       map[string]any
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("chroma HTTP %d", e.StatusCode)
}

func isNotFound(err error) bool {
	h, ok := err.(*HTTPError)
	return ok && h.StatusCode == http.StatusNotFound
}

func IsNotFound(err error) bool {
	return isNotFound(err)
}
