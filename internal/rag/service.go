package rag

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/andrepester/rag-search-mcp/internal/config"
	"github.com/andrepester/rag-search-mcp/internal/indexstate"
	"github.com/andrepester/rag-search-mcp/internal/ingest"
	"github.com/andrepester/rag-search-mcp/internal/observability"
	"github.com/andrepester/rag-search-mcp/internal/ollama"
	"github.com/andrepester/rag-search-mcp/internal/reindexjob"
	"github.com/andrepester/rag-search-mcp/internal/store"
)

type Service struct {
	Config       *config.Config
	Ingest       *ingest.Service
	IndexState   *indexstate.Store
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
	Query              string        `json:"query"`
	ScopeUsed          string        `json:"scope_used"`
	SourceFilter       string        `json:"source_filter,omitempty"`
	MaxDistance        float64       `json:"max_distance"`
	Matches            []SearchMatch `json:"matches"`
	OmittedWeakMatches int           `json:"omitted_weak_matches,omitempty"`
}

type SearchOptions struct {
	Query        string
	TopK         int
	Scope        string
	SourceFilter string
	MaxDistance  *float64
}

type SearchSettings struct {
	MaxDistance    float64 `json:"max_distance"`
	MinDistance    float64 `json:"min_distance"`
	MaxDistanceCap float64 `json:"max_distance_cap"`
	DistanceStep   float64 `json:"distance_step"`
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
	searchDistanceStep       = 0.01
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
		IndexState:   indexstate.New(cfg.IndexStateDir),
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
	return s.RunReindex(ctx, reindexjob.TriggerMCPTool, nil)
}

func (s *Service) RunReindex(ctx context.Context, trigger string, onStart func(reindexjob.Job)) (ingest.Stats, error) {
	run, err := reindexjob.New(s.Config.IndexStateDir).StartWithOptions(ctx, trigger, reindexjob.StartOptions{IndexSubdir: s.Config.IndexSubdir})
	if err != nil {
		return ingest.Stats{}, err
	}
	if onStart != nil {
		onStart(run.Job)
	}

	progressCtx := ingest.WithDocumentProgressReporter(ctx, func(progress ingest.DocumentProgress) {
		_ = run.UpdateProgress(ctx, reindexjob.Progress{
			TotalDocuments:     progress.TotalDocuments,
			ProcessedDocuments: progress.ProcessedDocuments,
		})
	})

	stats, err := s.Ingest.Reindex(progressCtx)
	if err == nil {
		var collectionID string
		collectionID, err = s.Chroma.EnsureCollection(ctx, s.Config.CollectionName)
		if err == nil {
			s.setCollectionID(collectionID)
		}
	}
	if finishErr := run.Finish(ctx, stats, err); finishErr != nil {
		if err != nil {
			return stats, errors.Join(err, finishErr)
		}
		return stats, finishErr
	}
	return stats, err
}

func (s *Service) ReindexStatus(ctx context.Context) (reindexjob.Status, error) {
	return reindexjob.New(s.Config.IndexStateDir).Status(ctx)
}

func (s *Service) Search(ctx context.Context, query string, topK int, scope string, sourceFilter string) (SearchResponse, error) {
	return s.SearchWithOptions(ctx, SearchOptions{
		Query:        query,
		TopK:         topK,
		Scope:        scope,
		SourceFilter: sourceFilter,
	})
}

func (s *Service) SearchWithOptions(ctx context.Context, options SearchOptions) (SearchResponse, error) {
	query := options.Query
	query = strings.TrimSpace(query)
	scopeUsed := normalizeScope(options.Scope, s.Config.DefaultScope)
	sourceFilter := strings.TrimSpace(options.SourceFilter)
	if options.MaxDistance != nil && !validMaxSearchDistance(*options.MaxDistance) {
		return SearchResponse{}, fmt.Errorf("max_distance must be between %.2f and %.2f", config.MinSearchDistance, config.MaxSearchDistance)
	}
	maxDistance := s.effectiveMaxSearchDistance(options.MaxDistance)

	if query == "" {
		return SearchResponse{Query: query, ScopeUsed: scopeUsed, SourceFilter: sourceFilter, MaxDistance: maxDistance, Matches: []SearchMatch{}}, nil
	}
	if looksLikeNonsenseQuery(query) {
		return SearchResponse{Query: query, ScopeUsed: scopeUsed, SourceFilter: sourceFilter, MaxDistance: maxDistance, Matches: []SearchMatch{}}, nil
	}

	activeGeneration, err := s.activeGeneration()
	if err != nil {
		return SearchResponse{}, err
	}
	if activeGeneration == "" {
		return SearchResponse{Query: query, ScopeUsed: scopeUsed, SourceFilter: sourceFilter, MaxDistance: maxDistance, Matches: []SearchMatch{}}, nil
	}
	topK := options.TopK
	limitResults := topK > 0
	if limitResults && topK > s.Config.MaxTopK {
		topK = s.Config.MaxTopK
	}

	embeddings, err := s.Ollama.Embed(ctx, s.Config.EmbedModel, []string{query}, ollama.EmbedOptions{NumThreads: s.Config.EmbedNumThreads})
	if err != nil {
		return SearchResponse{}, err
	}
	if len(embeddings) == 0 {
		return SearchResponse{Query: query, ScopeUsed: scopeUsed, SourceFilter: sourceFilter, MaxDistance: maxDistance, Matches: []SearchMatch{}}, nil
	}

	where := map[string]any{"index_generation": activeGeneration}
	if scopeUsed == "docs" || scopeUsed == "code" {
		where = store.WhereAnd(where, map[string]any{"scope": scopeUsed})
	}

	candidateLimit := min(topK*8, max(20, s.Config.MaxTopK))
	if !limitResults {
		candidateLimit, err = s.Chroma.CountRecords(ctx, s.getCollectionID(), where)
		if err != nil {
			return SearchResponse{}, err
		}
		if candidateLimit == 0 {
			return SearchResponse{Query: query, ScopeUsed: scopeUsed, SourceFilter: sourceFilter, MaxDistance: maxDistance, Matches: []SearchMatch{}}, nil
		}
	}

	candidates, err := s.Chroma.Query(ctx, s.getCollectionID(), embeddings[0], candidateLimit, where)
	if err != nil {
		return SearchResponse{}, err
	}

	matchCap := len(candidates)
	if limitResults && topK < matchCap {
		matchCap = topK
	}
	matches := make([]SearchMatch, 0, matchCap)
	omittedWeakMatches := 0
	for _, candidate := range candidates {
		sourcePath, _ := candidate.Metadata["source_path"].(string)
		candidateScope, _ := candidate.Metadata["scope"].(string)

		if sourceFilter != "" && !strings.Contains(strings.ToLower(sourcePath), strings.ToLower(sourceFilter)) {
			continue
		}
		if scopeUsed != "all" && candidateScope != scopeUsed {
			continue
		}
		if !isRelevantDistance(candidate.Distance, maxDistance) {
			omittedWeakMatches++
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
			ChunkID:    publicChunkID(candidate),
			SourcePath: sourcePath,
			Scope:      candidateScope,
			ChunkIndex: chunkIdx,
			Distance:   candidate.Distance,
			Text:       candidate.Document,
		})
		if limitResults && len(matches) >= topK {
			break
		}
	}

	return SearchResponse{
		Query:              query,
		ScopeUsed:          scopeUsed,
		SourceFilter:       sourceFilter,
		MaxDistance:        maxDistance,
		Matches:            matches,
		OmittedWeakMatches: omittedWeakMatches,
	}, nil
}

func (s *Service) SearchSettings() SearchSettings {
	return SearchSettings{
		MaxDistance:    s.effectiveMaxSearchDistance(nil),
		MinDistance:    config.MinSearchDistance,
		MaxDistanceCap: config.MaxSearchDistance,
		DistanceStep:   searchDistanceStep,
	}
}

func (s *Service) GetChunk(ctx context.Context, chunkID string) ChunkResponse {
	chunkID = strings.TrimSpace(chunkID)
	if chunkID == "" {
		return ChunkResponse{Found: false, ChunkID: chunkID, Error: "chunk_id is required"}
	}

	activeGeneration, err := s.activeGeneration()
	if err != nil {
		return ChunkResponse{Found: false, ChunkID: chunkID, Error: err.Error()}
	}
	if activeGeneration == "" {
		return ChunkResponse{Found: false, ChunkID: chunkID}
	}

	match, err := s.Chroma.GetByChunkID(ctx, s.getCollectionID(), activeGeneration, chunkID)
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
	activeGeneration, err := s.activeGeneration()
	if err != nil {
		return ListSourcesResponse{}, err
	}
	if activeGeneration == "" {
		return ListSourcesResponse{ScopeUsed: scopeUsed, Sources: []string{}}, nil
	}
	sources, err := s.Chroma.ListSourcePaths(ctx, s.getCollectionID(), activeGeneration, scopeUsed)
	if err != nil {
		return ListSourcesResponse{}, err
	}
	return ListSourcesResponse{ScopeUsed: scopeUsed, Sources: sources}, nil
}

func (s *Service) CheckReadiness(ctx context.Context) observability.ReadinessReport {
	dependencies := []observability.DependencyStatus{
		s.checkChroma(ctx),
		s.checkOllama(ctx),
	}
	return observability.NewReadinessReport(dependencies)
}

func (s *Service) checkChroma(ctx context.Context) observability.DependencyStatus {
	collectionID, err := s.Chroma.EnsureCollection(ctx, s.Config.CollectionName)
	if err != nil {
		return observability.DependencyStatus{
			Name:   "chroma",
			Status: observability.StatusError,
			Error:  err.Error(),
			Hint:   observability.DependencyHint("chroma"),
		}
	}
	s.setCollectionID(collectionID)
	return observability.DependencyStatus{Name: "chroma", Status: observability.StatusOK}
}

func (s *Service) checkOllama(ctx context.Context) observability.DependencyStatus {
	if err := s.Ollama.Check(ctx); err != nil {
		return observability.DependencyStatus{
			Name:   "ollama",
			Status: observability.StatusError,
			Error:  err.Error(),
			Hint:   observability.DependencyHint("ollama"),
		}
	}
	return observability.DependencyStatus{Name: "ollama", Status: observability.StatusOK}
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

func (s *Service) effectiveMaxSearchDistance(override *float64) float64 {
	if override != nil {
		return *override
	}
	if s.Config != nil && s.Config.MaxSearchDistance >= config.MinSearchDistance {
		return s.Config.MaxSearchDistance
	}
	return config.DefaultMaxSearchDistance
}

func validMaxSearchDistance(value float64) bool {
	return value >= config.MinSearchDistance && value <= config.MaxSearchDistance
}

func isRelevantDistance(distance *float64, maxDistance float64) bool {
	return distance == nil || *distance <= maxDistance
}

func looksLikeNonsenseQuery(query string) bool {
	tokens := strings.Fields(query)
	if len(tokens) != 1 {
		return false
	}

	token := strings.ToLower(strings.Trim(tokens[0], " \t\r\n.,;:!?()[]{}'\"`"))
	if len(token) < 8 {
		return false
	}

	hasLetter := false
	for _, r := range token {
		if r < 'a' || r > 'z' {
			return false
		}
		hasLetter = true
		if strings.ContainsRune("aeiou", r) {
			return false
		}
	}
	return hasLetter
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

func (s *Service) activeGeneration() (string, error) {
	manifest, err := s.IndexState.Load()
	if err != nil {
		return "", err
	}
	if manifest.CollectionName != "" && manifest.CollectionName != s.Config.CollectionName {
		return "", nil
	}
	return strings.TrimSpace(manifest.ActiveGeneration), nil
}

func publicChunkID(match store.QueryMatch) string {
	if chunkID, ok := match.Metadata["chunk_id"].(string); ok && chunkID != "" {
		return chunkID
	}
	return match.ID
}
