package ingest

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ledongthuc/pdf"

	"github.com/andrepester/rag-search-mcp/internal/chunk"
	"github.com/andrepester/rag-search-mcp/internal/config"
	"github.com/andrepester/rag-search-mcp/internal/indexstate"
	"github.com/andrepester/rag-search-mcp/internal/ollama"
	"github.com/andrepester/rag-search-mcp/internal/store"
)

type Service struct {
	Config *config.Config
	Ollama *ollama.Client
	Chroma *store.ChromaClient
}

type Stats struct {
	Files          int    `json:"files"`
	Chunks         int    `json:"chunks"`
	CodeFiles      int    `json:"code_files"`
	DocsFiles      int    `json:"docs_files"`
	ChangedFiles   int    `json:"changed_files"`
	DeletedFiles   int    `json:"deleted_files"`
	ReusedFiles    int    `json:"reused_files"`
	EmbeddedChunks int    `json:"embedded_chunks"`
	ReusedChunks   int    `json:"reused_chunks"`
	EmbedBatchSize int    `json:"embed_batch_size,omitempty"`
	Generation     string `json:"generation"`
	IndexSubdir    string `json:"index_subdir,omitempty"`
}

type DocumentProgress struct {
	TotalDocuments     int
	ProcessedDocuments int
}

type progressReporterKey struct{}

type ProgressReporter func(DocumentProgress)

func WithDocumentProgressReporter(ctx context.Context, reporter ProgressReporter) context.Context {
	if reporter == nil {
		return ctx
	}
	return context.WithValue(ctx, progressReporterKey{}, reporter)
}

type document struct {
	Scope      string
	SourcePath string
	Text       string
}

type documentIndexResult struct {
	SourcePath string
	Source     indexstate.SourceManifest
	Stats      Stats
}

var docsExt = map[string]struct{}{
	".md":  {},
	".txt": {},
	".pdf": {},
}

var codeExt = map[string]struct{}{
	".go":    {},
	".ts":    {},
	".tsx":   {},
	".js":    {},
	".jsx":   {},
	".py":    {},
	".java":  {},
	".cs":    {},
	".rs":    {},
	".cpp":   {},
	".c":     {},
	".h":     {},
	".hpp":   {},
	".kt":    {},
	".swift": {},
	".sql":   {},
	".yaml":  {},
	".yml":   {},
	".json":  {},
	".toml":  {},
	".sh":    {},
}

const chromaWriteBatchSize = 32

func (s *Service) Reindex(ctx context.Context) (Stats, error) {
	collectionID, err := s.Chroma.EnsureCollection(ctx, s.Config.CollectionName)
	if err != nil {
		return Stats{}, fmt.Errorf("ensure collection: %w", err)
	}

	documents, stats, err := s.loadDocuments()
	if err != nil {
		return stats, err
	}
	reportDocumentProgress(ctx, stats.Files, 0)

	stateStore := indexstate.New(s.Config.IndexStateDir)
	activeManifest, err := stateStore.Load()
	if err != nil {
		return stats, err
	}

	if activeManifest.CollectionName != "" && activeManifest.CollectionName != s.Config.CollectionName {
		activeManifest = indexstate.Manifest{Sources: map[string]indexstate.SourceManifest{}}
	}

	activeGeneration := activeManifest.ActiveGeneration
	buildGeneration := strings.TrimSpace(activeManifest.ResumeGeneration)
	requestedIndexSubdir := strings.TrimSpace(s.Config.IndexSubdir)
	effectiveFreshIndex := s.Config.FreshIndex
	if buildGeneration != "" {
		if strings.TrimSpace(activeManifest.ResumeIndexSubdir) != requestedIndexSubdir {
			return stats, fmt.Errorf("resume generation %s was started with RAG_INDEX_SUBDIR=%s; current RAG_INDEX_SUBDIR=%s; rerun with the matching INDEX_SUBDIR value or clear the incomplete resume generation before changing the selection", buildGeneration, displayIndexSubdir(activeManifest.ResumeIndexSubdir), displayIndexSubdir(requestedIndexSubdir))
		}
		effectiveFreshIndex = activeManifest.ResumeFreshIndex
		if s.Config.FreshIndex && !activeManifest.ResumeFreshIndex {
			buildGeneration = ""
			effectiveFreshIndex = true
		}
	}
	if buildGeneration == "" {
		buildGeneration = newGeneration()
		activeManifest.CollectionName = s.Config.CollectionName
		activeManifest.ResumeGeneration = buildGeneration
		activeManifest.ResumeFreshIndex = s.Config.FreshIndex
		activeManifest.ResumeIndexSubdir = requestedIndexSubdir
		if activeManifest.Sources == nil {
			activeManifest.Sources = map[string]indexstate.SourceManifest{}
		}
		if err := stateStore.Save(activeManifest); err != nil {
			return stats, err
		}
	}
	stats.Generation = buildGeneration
	stats.EmbedBatchSize = s.effectiveEmbedBatchSize()

	nextSources := map[string]indexstate.SourceManifest{}
	results, err := s.indexDocuments(ctx, collectionID, activeGeneration, buildGeneration, activeManifest, documents, effectiveFreshIndex, func(processedDocuments int) {
		reportDocumentProgress(ctx, stats.Files, processedDocuments)
	})
	if err != nil {
		return stats, err
	}
	for _, result := range results {
		nextSources[result.SourcePath] = result.Source
		stats.ChangedFiles += result.Stats.ChangedFiles
		stats.EmbeddedChunks += result.Stats.EmbeddedChunks
		stats.ReusedFiles += result.Stats.ReusedFiles
		stats.ReusedChunks += result.Stats.ReusedChunks
		stats.Chunks += result.Stats.Chunks
	}

	for sourcePath := range activeManifest.Sources {
		if _, ok := nextSources[sourcePath]; !ok {
			stats.DeletedFiles++
		}
	}
	if err := s.deleteStaleBuildSources(ctx, collectionID, buildGeneration, nextSources); err != nil {
		return stats, err
	}

	nextManifest := indexstate.Manifest{
		CollectionName:    s.Config.CollectionName,
		ActiveGeneration:  buildGeneration,
		ActiveIndexSubdir: requestedIndexSubdir,
		Sources:           nextSources,
	}
	if err := stateStore.Save(nextManifest); err != nil {
		return stats, err
	}

	return stats, nil
}

func (s *Service) indexDocuments(ctx context.Context, collectionID, activeGeneration, buildGeneration string, activeManifest indexstate.Manifest, documents []document, freshIndex bool, onProgress func(int)) ([]documentIndexResult, error) {
	if len(documents) == 0 {
		return nil, nil
	}

	workerCount := s.Config.EmbedConcurrency
	if workerCount <= 0 {
		workerCount = config.DefaultEmbedConcurrency
	}
	if workerCount > len(documents) {
		workerCount = len(documents)
	}

	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan document)
	results := make(chan struct {
		result documentIndexResult
		err    error
	}, len(documents))

	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for doc := range jobs {
				result, err := s.indexDocument(workCtx, collectionID, activeGeneration, buildGeneration, activeManifest, doc, freshIndex)
				if err != nil {
					cancel()
				}
				results <- struct {
					result documentIndexResult
					err    error
				}{result: result, err: err}
				if err != nil {
					return
				}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, doc := range documents {
			select {
			case jobs <- doc:
			case <-workCtx.Done():
				return
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	out := make([]documentIndexResult, 0, len(documents))
	var firstErr error
	processedDocuments := 0
	for item := range results {
		if item.err != nil {
			if firstErr == nil {
				firstErr = item.err
			}
			continue
		}
		out = append(out, item.result)
		processedDocuments++
		if onProgress != nil {
			onProgress(processedDocuments)
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}
	if len(out) != len(documents) {
		if err := workCtx.Err(); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("indexed %d documents, want %d", len(out), len(documents))
	}
	return out, nil
}

func (s *Service) indexDocument(ctx context.Context, collectionID, activeGeneration, buildGeneration string, activeManifest indexstate.Manifest, doc document, freshIndex bool) (documentIndexResult, error) {
	sourceHash := sourceFingerprint(doc, s.Config)
	chunks := chunk.Split(doc.Text, s.Config.ChunkSize, s.Config.ChunkOverlap)
	chunkIDs := sourceChunkIDs(doc, len(chunks))

	resumed, resumeOK, hasBuildRecords, err := s.reuseBuildSource(ctx, collectionID, buildGeneration, doc, sourceHash, chunkIDs)
	if err != nil {
		return documentIndexResult{}, err
	}
	if resumeOK {
		return documentIndexResult{
			SourcePath: doc.SourcePath,
			Source: indexstate.SourceManifest{
				Scope:    doc.Scope,
				Hash:     sourceHash,
				ChunkIDs: resumed,
			},
			Stats: Stats{
				ReusedFiles:  1,
				ReusedChunks: len(resumed),
				Chunks:       len(resumed),
			},
		}, nil
	}

	if activeGeneration != "" && !freshIndex {
		if activeSource, ok := activeManifest.Sources[doc.SourcePath]; ok && activeSource.Hash == sourceHash {
			if hasBuildRecords {
				if err := s.deleteBuildSourceRecords(ctx, collectionID, buildGeneration, doc); err != nil {
					return documentIndexResult{}, err
				}
			}
			chunkIDs, copied, err := s.copyUnchangedSource(ctx, collectionID, activeGeneration, buildGeneration, doc, sourceHash)
			if err != nil {
				return documentIndexResult{}, err
			}
			if copied {
				return documentIndexResult{
					SourcePath: doc.SourcePath,
					Source: indexstate.SourceManifest{
						Scope:    doc.Scope,
						Hash:     sourceHash,
						ChunkIDs: chunkIDs,
					},
					Stats: Stats{
						ReusedFiles:  1,
						ReusedChunks: len(chunkIDs),
						Chunks:       len(chunkIDs),
					},
				}, nil
			}
		}
	}

	chunkIDs, err = s.indexChangedSource(ctx, collectionID, buildGeneration, doc, sourceHash, chunks, chunkIDs, hasBuildRecords)
	if err != nil {
		return documentIndexResult{}, err
	}
	return documentIndexResult{
		SourcePath: doc.SourcePath,
		Source: indexstate.SourceManifest{
			Scope:    doc.Scope,
			Hash:     sourceHash,
			ChunkIDs: chunkIDs,
		},
		Stats: Stats{
			ChangedFiles:   1,
			EmbeddedChunks: len(chunkIDs),
			Chunks:         len(chunkIDs),
		},
	}, nil
}

func reportDocumentProgress(ctx context.Context, totalDocuments, processedDocuments int) {
	reporter, ok := ctx.Value(progressReporterKey{}).(ProgressReporter)
	if !ok || reporter == nil {
		return
	}
	reporter(normalizeDocumentProgress(totalDocuments, processedDocuments))
}

func normalizeDocumentProgress(totalDocuments, processedDocuments int) DocumentProgress {
	if totalDocuments < 0 {
		totalDocuments = 0
	}
	if processedDocuments < 0 {
		processedDocuments = 0
	}
	if totalDocuments == 0 {
		processedDocuments = 0
	} else if processedDocuments > totalDocuments {
		processedDocuments = totalDocuments
	}
	return DocumentProgress{
		TotalDocuments:     totalDocuments,
		ProcessedDocuments: processedDocuments,
	}
}

func (s *Service) copyUnchangedSource(ctx context.Context, collectionID, activeGeneration, buildGeneration string, doc document, sourceHash string) ([]string, bool, error) {
	records, err := s.Chroma.GetRecordsBySource(ctx, collectionID, activeGeneration, doc.Scope, doc.SourcePath)
	if err != nil {
		return nil, false, fmt.Errorf("load unchanged source records: %w", err)
	}
	if len(records) == 0 {
		return nil, false, nil
	}
	sort.SliceStable(records, func(i, j int) bool {
		return metadataInt(records[i].Metadata["chunk_index"]) < metadataInt(records[j].Metadata["chunk_index"])
	})

	ids := make([]string, 0, len(records))
	texts := make([]string, 0, len(records))
	metadatas := make([]map[string]any, 0, len(records))
	embeddings := make([][]float64, 0, len(records))
	chunkIDs := make([]string, 0, len(records))

	for _, record := range records {
		if len(record.Embedding) == 0 {
			return nil, false, nil
		}
		chunkID, _ := record.Metadata["chunk_id"].(string)
		if chunkID == "" {
			chunkID = scopedChunkID(doc.Scope, doc.SourcePath, metadataInt(record.Metadata["chunk_index"]))
		}
		meta := copyMetadata(record.Metadata)
		meta["scope"] = doc.Scope
		meta["source_path"] = doc.SourcePath
		meta["chunk_id"] = chunkID
		meta["index_generation"] = buildGeneration
		meta["source_hash"] = sourceHash

		ids = append(ids, generationChunkID(buildGeneration, chunkID))
		texts = append(texts, record.Document)
		metadatas = append(metadatas, meta)
		embeddings = append(embeddings, record.Embedding)
		chunkIDs = append(chunkIDs, chunkID)
	}

	if err := writeBatches(ctx, s.Chroma, collectionID, ids, texts, metadatas, embeddings); err != nil {
		return nil, false, fmt.Errorf("copy unchanged source records: %w", err)
	}
	return chunkIDs, true, nil
}

func (s *Service) reuseBuildSource(ctx context.Context, collectionID, generation string, doc document, sourceHash string, expectedChunkIDs []string) ([]string, bool, bool, error) {
	if generation == "" {
		return nil, false, false, nil
	}
	records, err := s.Chroma.GetRecordsBySource(ctx, collectionID, generation, doc.Scope, doc.SourcePath)
	if err != nil {
		return nil, false, false, fmt.Errorf("load resumable source records: %w", err)
	}
	hasRecords := len(records) > 0
	if len(records) == 0 || len(records) != len(expectedChunkIDs) {
		return nil, false, hasRecords, nil
	}
	sort.SliceStable(records, func(i, j int) bool {
		return metadataInt(records[i].Metadata["chunk_index"]) < metadataInt(records[j].Metadata["chunk_index"])
	})

	for i, record := range records {
		if fmt.Sprint(record.Metadata["source_hash"]) != sourceHash {
			return nil, false, hasRecords, nil
		}
		chunkID, _ := record.Metadata["chunk_id"].(string)
		if chunkID == "" {
			chunkID = scopedChunkID(doc.Scope, doc.SourcePath, metadataInt(record.Metadata["chunk_index"]))
		}
		if chunkID != expectedChunkIDs[i] || len(record.Embedding) == 0 {
			return nil, false, hasRecords, nil
		}
	}
	return append([]string(nil), expectedChunkIDs...), true, hasRecords, nil
}

func (s *Service) indexChangedSource(ctx context.Context, collectionID, generation string, doc document, sourceHash string, chunks []string, chunkIDs []string, deleteExisting bool) ([]string, error) {
	if deleteExisting {
		if err := s.deleteBuildSourceRecords(ctx, collectionID, generation, doc); err != nil {
			return nil, err
		}
	}
	if len(chunks) == 0 {
		return nil, nil
	}

	ids := make([]string, 0, len(chunks))
	metadatas := make([]map[string]any, 0, len(chunks))
	for i := range chunks {
		chunkID := chunkIDs[i]
		ids = append(ids, generationChunkID(generation, chunkID))
		metadatas = append(metadatas, map[string]any{
			"scope":            doc.Scope,
			"source_path":      doc.SourcePath,
			"chunk_index":      i,
			"chunk_id":         chunkID,
			"index_generation": generation,
			"source_hash":      sourceHash,
		})
	}

	embedBatchSize := s.effectiveEmbedBatchSize()
	for i := 0; i < len(chunks); i += embedBatchSize {
		end := i + embedBatchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		batchTexts := chunks[i:end]
		embeddings, err := s.Ollama.Embed(ctx, s.Config.EmbedModel, batchTexts, ollama.EmbedOptions{NumThreads: s.Config.EmbedNumThreads})
		if err != nil {
			return nil, fmt.Errorf("embed batch: %w", err)
		}
		if err := s.Chroma.Add(ctx, collectionID, ids[i:end], batchTexts, metadatas[i:end], embeddings); err != nil {
			return nil, fmt.Errorf("write batch to chroma: %w", err)
		}
	}

	return chunkIDs, nil
}

func (s *Service) deleteBuildSourceRecords(ctx context.Context, collectionID, generation string, doc document) error {
	if generation == "" {
		return nil
	}
	where := store.WhereAnd(
		map[string]any{"index_generation": generation},
		map[string]any{"scope": doc.Scope},
		map[string]any{"source_path": doc.SourcePath},
	)
	if err := s.Chroma.DeleteWhere(ctx, collectionID, where); err != nil {
		return fmt.Errorf("delete stale build source records: %w", err)
	}
	return nil
}

func (s *Service) deleteStaleBuildSources(ctx context.Context, collectionID, generation string, nextSources map[string]indexstate.SourceManifest) error {
	sourcePaths, err := s.Chroma.ListSourcePaths(ctx, collectionID, generation, "all")
	if err != nil {
		return fmt.Errorf("list build generation sources: %w", err)
	}
	for _, sourcePath := range sourcePaths {
		if _, ok := nextSources[sourcePath]; ok {
			continue
		}
		where := store.WhereAnd(
			map[string]any{"index_generation": generation},
			map[string]any{"source_path": sourcePath},
		)
		if err := s.Chroma.DeleteWhere(ctx, collectionID, where); err != nil {
			return fmt.Errorf("delete stale build source %s: %w", sourcePath, err)
		}
	}
	return nil
}

func writeBatches(ctx context.Context, chroma *store.ChromaClient, collectionID string, ids []string, documents []string, metadatas []map[string]any, embeddings [][]float64) error {
	for i := 0; i < len(ids); i += chromaWriteBatchSize {
		end := i + chromaWriteBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		if err := chroma.Add(ctx, collectionID, ids[i:end], documents[i:end], metadatas[i:end], embeddings[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) loadDocuments() ([]document, Stats, error) {
	out := make([]document, 0)
	stats := Stats{
		EmbedBatchSize: s.effectiveEmbedBatchSize(),
		IndexSubdir:    strings.TrimSpace(s.Config.IndexSubdir),
	}

	if s.Config.IndexSubdir != "" {
		docs, err := s.loadIndexSubdirDocuments()
		if err != nil {
			return nil, stats, err
		}
		out = append(out, docs...)
	} else {
		if docs, err := loadScopeDocuments(s.Config.DocsDir, "docs", docsExt, ""); err != nil {
			return nil, stats, err
		} else {
			out = append(out, docs...)
		}

		if s.Config.EnableCodeIngest {
			if code, err := loadScopeDocuments(s.Config.CodeDir, "code", codeExt, ""); err != nil {
				return nil, stats, err
			} else {
				out = append(out, code...)
			}
		}
	}

	if s.Config.IndexLimit > 0 && len(out) > s.Config.IndexLimit {
		out = out[:s.Config.IndexLimit]
	}

	stats = documentStats(out)
	stats.EmbedBatchSize = s.effectiveEmbedBatchSize()
	stats.IndexSubdir = strings.TrimSpace(s.Config.IndexSubdir)
	return out, stats, nil
}

func (s *Service) effectiveEmbedBatchSize() int {
	if s == nil || s.Config == nil || s.Config.EmbedBatchSize <= 0 {
		return config.DefaultEmbedBatchSize
	}
	return s.Config.EmbedBatchSize
}

func (s *Service) loadIndexSubdirDocuments() ([]document, error) {
	scope, rel, ok := splitIndexSubdir(s.Config.IndexSubdir)
	if !ok {
		return nil, fmt.Errorf("RAG_INDEX_SUBDIR must start with docs/ or code/ followed by a subdirectory")
	}
	switch scope {
	case "docs":
		return loadScopeDocuments(s.Config.DocsDir, "docs", docsExt, rel)
	case "code":
		if !s.Config.EnableCodeIngest {
			return nil, fmt.Errorf("RAG_INDEX_SUBDIR=%s requires RAG_ENABLE_CODE_INGEST=true", s.Config.IndexSubdir)
		}
		return loadScopeDocuments(s.Config.CodeDir, "code", codeExt, rel)
	default:
		return nil, fmt.Errorf("RAG_INDEX_SUBDIR must start with docs/ or code/")
	}
}

func documentStats(docs []document) Stats {
	stats := Stats{Files: len(docs)}
	for _, doc := range docs {
		switch doc.Scope {
		case "docs":
			stats.DocsFiles++
		case "code":
			stats.CodeFiles++
		}
	}
	return stats
}

func loadScopeDocuments(root, scope string, allowedExt map[string]struct{}, subdir string) ([]document, error) {
	if strings.TrimSpace(root) == "" {
		if subdir != "" {
			return nil, fmt.Errorf("%s root is empty for RAG_INDEX_SUBDIR=%s/%s", scope, scope, subdir)
		}
		return nil, nil
	}

	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			if subdir != "" {
				return nil, fmt.Errorf("%s root does not exist for RAG_INDEX_SUBDIR=%s/%s", scope, scope, subdir)
			}
			return nil, nil
		}
		return nil, fmt.Errorf("stat %s directory: %w", scope, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s root %s is not a directory", scope, root)
	}

	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve %s root: %w", scope, err)
	}
	walkRoot := root
	if subdir != "" {
		target, err := validateIndexSubdirTarget(root, resolvedRoot, scope, subdir)
		if err != nil {
			return nil, err
		}
		walkRoot = target
	}

	docs := make([]document, 0)
	err = filepath.WalkDir(walkRoot, func(filePath string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == ".venv" || name == "node_modules" || name == ".idea" || name == ".vscode" {
				return filepath.SkipDir
			}
			return nil
		}

		resolvedFilePath, err := filepath.EvalSymlinks(filePath)
		if err != nil {
			return fmt.Errorf("resolve path %s: %w", filePath, err)
		}
		if !resolvedPathWithinRoot(resolvedRoot, resolvedFilePath) {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(filePath))
		if _, ok := allowedExt[ext]; !ok {
			return nil
		}

		text, err := readText(filePath, ext)
		if err != nil {
			return fmt.Errorf("read %s: %w", filePath, err)
		}
		if strings.TrimSpace(text) == "" {
			return nil
		}

		rel, err := filepath.Rel(root, filePath)
		if err != nil {
			return err
		}

		docs = append(docs, document{
			Scope:      scope,
			SourcePath: filepath.ToSlash(filepath.Join(scope, rel)),
			Text:       text,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	return docs, nil
}

func validateIndexSubdirTarget(root, resolvedRoot, scope, subdir string) (string, error) {
	target := filepath.Join(root, filepath.FromSlash(subdir))
	info, err := os.Lstat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("RAG_INDEX_SUBDIR=%s/%s does not exist", scope, subdir)
		}
		return "", fmt.Errorf("stat RAG_INDEX_SUBDIR=%s/%s: %w", scope, subdir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("RAG_INDEX_SUBDIR=%s/%s must not be a symlink", scope, subdir)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("RAG_INDEX_SUBDIR=%s/%s is not a directory", scope, subdir)
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", fmt.Errorf("resolve RAG_INDEX_SUBDIR=%s/%s: %w", scope, subdir, err)
	}
	if !resolvedPathWithinRoot(resolvedRoot, resolvedTarget) {
		return "", fmt.Errorf("RAG_INDEX_SUBDIR=%s/%s escapes the configured %s root", scope, subdir, scope)
	}
	return target, nil
}

func splitIndexSubdir(indexSubdir string) (scope, rel string, ok bool) {
	trimmed := strings.TrimSpace(indexSubdir)
	normalized := strings.ReplaceAll(trimmed, "\\", "/")
	if normalized == "" || path.IsAbs(normalized) || filepath.IsAbs(trimmed) {
		return "", "", false
	}
	for _, segment := range strings.Split(normalized, "/") {
		if segment == ".." {
			return "", "", false
		}
	}
	cleaned := path.Clean(normalized)
	scope, rel, ok = strings.Cut(cleaned, "/")
	if !ok || rel == "" || scope == "" {
		return "", "", false
	}
	return scope, rel, true
}

func displayIndexSubdir(indexSubdir string) string {
	indexSubdir = strings.TrimSpace(indexSubdir)
	if indexSubdir == "" {
		return "<full-index>"
	}
	return indexSubdir
}

func resolvedPathWithinRoot(rootResolvedPath, fileResolvedPath string) bool {
	rel, err := filepath.Rel(rootResolvedPath, fileResolvedPath)
	if err != nil {
		return false
	}
	rel = filepath.Clean(rel)
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func readText(filePath, ext string) (string, error) {
	if ext == ".pdf" {
		f, r, err := pdf.Open(filePath)
		if err != nil {
			return "", err
		}
		defer f.Close()

		var b strings.Builder
		total := r.NumPage()
		for i := 1; i <= total; i++ {
			p := r.Page(i)
			if p.V.IsNull() {
				continue
			}
			content, err := p.GetPlainText(nil)
			if err != nil {
				continue
			}
			b.WriteString(content)
			b.WriteString("\n\n")
		}
		return b.String(), nil
	}

	raw, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func scopedChunkID(scope, sourcePath string, index int) string {
	seed := fmt.Sprintf("%s|%s|%d", scope, sourcePath, index)
	h := sha1.Sum([]byte(seed))
	return fmt.Sprintf("%s:%s", scope, hex.EncodeToString(h[:])[:16])
}

func sourceChunkIDs(doc document, count int) []string {
	chunkIDs := make([]string, 0, count)
	for i := 0; i < count; i++ {
		chunkIDs = append(chunkIDs, scopedChunkID(doc.Scope, doc.SourcePath, i))
	}
	return chunkIDs
}

func generationChunkID(generation, chunkID string) string {
	seed := fmt.Sprintf("%s|%s", generation, chunkID)
	h := sha1.Sum([]byte(seed))
	return fmt.Sprintf("%s:%s", generation, hex.EncodeToString(h[:])[:16])
}

func sourceFingerprint(doc document, cfg *config.Config) string {
	seed := strings.Join([]string{
		"rag-index-v1",
		doc.Scope,
		doc.SourcePath,
		cfg.EmbedModel,
		fmt.Sprintf("chunk_size=%d", cfg.ChunkSize),
		fmt.Sprintf("chunk_overlap=%d", cfg.ChunkOverlap),
		doc.Text,
	}, "\x00")
	h := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(h[:])
}

func newGeneration() string {
	return fmt.Sprintf("gen-%d", time.Now().UTC().UnixNano())
}

func copyMetadata(in map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range in {
		out[key] = value
	}
	return out
}

func metadataInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}
