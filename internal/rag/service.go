package rag

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"local-rag/internal/config"
	"local-rag/internal/ingest"
	"local-rag/internal/ollama"
	"local-rag/internal/store"
)

type Service struct {
	Config       *config.Config
	Ingest       *ingest.Service
	Ollama       *ollama.Client
	Chroma       *store.ChromaClient
	collectionMu sync.RWMutex
	collectionID string
}

type SearchMatch struct {
	ChunkID    string   `json:"chunk_id"`
	SourcePath string   `json:"source_path"`
	Scope      string   `json:"scope"`
	ChunkIndex int      `json:"chunk_index,omitempty"`
	Distance   *float64 `json:"distance,omitempty"`
	Text       string   `json:"text"`
}

type SearchResponse struct {
	Query        string        `json:"query"`
	ScopeUsed    string        `json:"scope_used"`
	SourceFilter string        `json:"source_filter,omitempty"`
	Matches      []SearchMatch `json:"matches"`
}

type ListSourcesResponse struct {
	ScopeUsed string   `json:"scope_used"`
	Sources   []string `json:"sources"`
}

type ChunkResponse struct {
	Found   bool           `json:"found"`
	ChunkID string         `json:"chunk_id"`
	Chunk   *SearchMatch   `json:"chunk,omitempty"`
	Error   string         `json:"error,omitempty"`
	Meta    map[string]any `json:"metadata,omitempty"`
}

const (
	collectionInitTimeout    = 45 * time.Second
	collectionInitMinBackoff = 250 * time.Millisecond
	collectionInitMaxBackoff = 3 * time.Second
)

func NewService(cfg *config.Config, ingestSvc *ingest.Service, ollamaClient *ollama.Client, chromaClient *store.ChromaClient) (*Service, error) {
	ctx, cancel := context.WithTimeout(context.Background(), collectionInitTimeout)
	defer cancel()

	collectionID, err := ensureWithRetry(ctx, func(callCtx context.Context) (string, error) {
		return chromaClient.EnsureCollection(callCtx, cfg.CollectionName)
	})
	if err != nil {
		return nil, err
	}

	return &Service{
		Config:       cfg,
		Ingest:       ingestSvc,
		Ollama:       ollamaClient,
		Chroma:       chromaClient,
		collectionID: collectionID,
	}, nil
}

func ensureWithRetry(ctx context.Context, ensure func(context.Context) (string, error)) (string, error) {
	backoff := collectionInitMinBackoff
	var lastErr error

	for {
		collectionID, err := ensure(ctx)
		if err == nil {
			return collectionID, nil
		}
		lastErr = err

		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return "", fmt.Errorf("ensure collection timeout after retries: %w", lastErr)
			}
			return "", fmt.Errorf("ensure collection canceled: %w", ctx.Err())
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > collectionInitMaxBackoff {
			backoff = collectionInitMaxBackoff
		}
	}
}

func (s *Service) Reindex(ctx context.Context) (ingest.Stats, error) {
	stats, err := s.Ingest.Reindex(ctx)
	if err != nil {
		return ingest.Stats{}, err
	}
	collectionID, err := s.Chroma.EnsureCollection(ctx, s.Config.CollectionName)
	if err != nil {
		return stats, err
	}
	s.setCollectionID(collectionID)
	return stats, nil
}

func (s *Service) Search(ctx context.Context, query string, topK int, scope string, sourceFilter string) (SearchResponse, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return SearchResponse{Query: query, ScopeUsed: normalizeScope(scope, s.Config.DefaultScope), Matches: []SearchMatch{}}, nil
	}

	scopeUsed := normalizeScope(scope, s.Config.DefaultScope)
	if topK <= 0 {
		topK = 5
	}
	if topK > s.Config.MaxTopK {
		topK = s.Config.MaxTopK
	}

	embeddings, err := s.Ollama.Embed(ctx, s.Config.EmbedModel, []string{query})
	if err != nil {
		return SearchResponse{}, err
	}
	if len(embeddings) == 0 {
		return SearchResponse{Query: query, ScopeUsed: scopeUsed, SourceFilter: sourceFilter, Matches: []SearchMatch{}}, nil
	}

	where := map[string]any{}
	if scopeUsed == "docs" || scopeUsed == "code" {
		where["scope"] = scopeUsed
	}

	candidates, err := s.Chroma.Query(ctx, s.getCollectionID(), embeddings[0], min(topK*8, max(20, s.Config.MaxTopK)), where)
	if err != nil {
		return SearchResponse{}, err
	}

	matches := make([]SearchMatch, 0, topK)
	for _, candidate := range candidates {
		sourcePath, _ := candidate.Metadata["source_path"].(string)
		candidateScope, _ := candidate.Metadata["scope"].(string)

		if sourceFilter != "" && !strings.Contains(strings.ToLower(sourcePath), strings.ToLower(sourceFilter)) {
			continue
		}
		if scopeUsed != "all" && candidateScope != scopeUsed {
			continue
		}

		chunkIdx := 0
		switch v := candidate.Metadata["chunk_index"].(type) {
		case float64:
			chunkIdx = int(v)
		case int:
			chunkIdx = v
		}

		matches = append(matches, SearchMatch{
			ChunkID:    candidate.ID,
			SourcePath: sourcePath,
			Scope:      candidateScope,
			ChunkIndex: chunkIdx,
			Distance:   candidate.Distance,
			Text:       candidate.Document,
		})
		if len(matches) >= topK {
			break
		}
	}

	return SearchResponse{
		Query:        query,
		ScopeUsed:    scopeUsed,
		SourceFilter: sourceFilter,
		Matches:      matches,
	}, nil
}

func (s *Service) GetChunk(ctx context.Context, chunkID string) ChunkResponse {
	chunkID = strings.TrimSpace(chunkID)
	if chunkID == "" {
		return ChunkResponse{Found: false, ChunkID: chunkID, Error: "chunk_id is required"}
	}

	match, err := s.Chroma.GetByID(ctx, s.getCollectionID(), chunkID)
	if err != nil {
		return ChunkResponse{Found: false, ChunkID: chunkID, Error: err.Error()}
	}
	if match == nil {
		return ChunkResponse{Found: false, ChunkID: chunkID}
	}

	sourcePath, _ := match.Metadata["source_path"].(string)
	scope, _ := match.Metadata["scope"].(string)
	chunkIdx := 0
	switch v := match.Metadata["chunk_index"].(type) {
	case float64:
		chunkIdx = int(v)
	case int:
		chunkIdx = v
	}

	return ChunkResponse{
		Found:   true,
		ChunkID: chunkID,
		Chunk: &SearchMatch{
			ChunkID:    chunkID,
			SourcePath: sourcePath,
			Scope:      scope,
			ChunkIndex: chunkIdx,
			Text:       match.Document,
		},
		Meta: match.Metadata,
	}
}

func (s *Service) ListSources(ctx context.Context, scope string) (ListSourcesResponse, error) {
	scopeUsed := normalizeScope(scope, s.Config.DefaultScope)
	sources, err := s.Chroma.ListSourcePaths(ctx, s.getCollectionID(), scopeUsed)
	if err != nil {
		return ListSourcesResponse{}, err
	}
	return ListSourcesResponse{ScopeUsed: scopeUsed, Sources: sources}, nil
}

func normalizeScope(input, fallback string) string {
	v := strings.ToLower(strings.TrimSpace(input))
	if v == "" {
		v = strings.ToLower(strings.TrimSpace(fallback))
	}
	if v != "docs" && v != "code" && v != "all" {
		return "all"
	}
	return v
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (s *Service) getCollectionID() string {
	s.collectionMu.RLock()
	defer s.collectionMu.RUnlock()
	return s.collectionID
}

func (s *Service) setCollectionID(collectionID string) {
	s.collectionMu.Lock()
	defer s.collectionMu.Unlock()
	s.collectionID = collectionID
}
