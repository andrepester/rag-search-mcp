package ingest

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	Generation     string `json:"generation"`
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

const (
	embedBatchSize       = 8
	chromaWriteBatchSize = 32
)

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

	if s.Config.FreshIndex {
		if err := s.Chroma.DeleteCollection(ctx, collectionID); err != nil && !store.IsNotFound(err) {
			return stats, fmt.Errorf("reset collection for fresh index: %w", err)
		}
		collectionID, err = s.Chroma.EnsureCollection(ctx, s.Config.CollectionName)
		if err != nil {
			return stats, fmt.Errorf("ensure collection after fresh reset: %w", err)
		}
		activeManifest = indexstate.Manifest{Sources: map[string]indexstate.SourceManifest{}}
	} else if activeManifest.CollectionName != "" && activeManifest.CollectionName != s.Config.CollectionName {
		activeManifest = indexstate.Manifest{Sources: map[string]indexstate.SourceManifest{}}
	}

	activeGeneration := activeManifest.ActiveGeneration
	buildGeneration := newGeneration()
	stats.Generation = buildGeneration

	switched := false
	defer func() {
		if switched {
			return
		}
		_ = s.Chroma.DeleteWhere(ctx, collectionID, map[string]any{"index_generation": buildGeneration})
	}()

	nextSources := map[string]indexstate.SourceManifest{}
	processedDocuments := 0
	for _, doc := range documents {
		sourceHash := sourceFingerprint(doc, s.Config)
		if activeGeneration != "" {
			if activeSource, ok := activeManifest.Sources[doc.SourcePath]; ok && activeSource.Hash == sourceHash {
				chunkIDs, copied, err := s.copyUnchangedSource(ctx, collectionID, activeGeneration, buildGeneration, doc, sourceHash)
				if err != nil {
					return stats, err
				}
				if copied {
					stats.ReusedFiles++
					stats.ReusedChunks += len(chunkIDs)
					stats.Chunks += len(chunkIDs)
					nextSources[doc.SourcePath] = indexstate.SourceManifest{
						Scope:    doc.Scope,
						Hash:     sourceHash,
						ChunkIDs: chunkIDs,
					}
					processedDocuments++
					reportDocumentProgress(ctx, stats.Files, processedDocuments)
					continue
				}
			}
		}

		chunkIDs, err := s.indexChangedSource(ctx, collectionID, buildGeneration, doc, sourceHash)
		if err != nil {
			return stats, err
		}
		stats.ChangedFiles++
		stats.EmbeddedChunks += len(chunkIDs)
		stats.Chunks += len(chunkIDs)
		nextSources[doc.SourcePath] = indexstate.SourceManifest{
			Scope:    doc.Scope,
			Hash:     sourceHash,
			ChunkIDs: chunkIDs,
		}
		processedDocuments++
		reportDocumentProgress(ctx, stats.Files, processedDocuments)
	}

	for sourcePath := range activeManifest.Sources {
		if _, ok := nextSources[sourcePath]; !ok {
			stats.DeletedFiles++
		}
	}

	nextManifest := indexstate.Manifest{
		CollectionName:   s.Config.CollectionName,
		ActiveGeneration: buildGeneration,
		Sources:          nextSources,
	}
	if err := stateStore.Save(nextManifest); err != nil {
		return stats, err
	}
	switched = true

	return stats, nil
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

func (s *Service) indexChangedSource(ctx context.Context, collectionID, generation string, doc document, sourceHash string) ([]string, error) {
	chunks := chunk.Split(doc.Text, s.Config.ChunkSize, s.Config.ChunkOverlap)
	if len(chunks) == 0 {
		return nil, nil
	}

	ids := make([]string, 0, len(chunks))
	metadatas := make([]map[string]any, 0, len(chunks))
	chunkIDs := make([]string, 0, len(chunks))
	for i := range chunks {
		chunkID := scopedChunkID(doc.Scope, doc.SourcePath, i)
		chunkIDs = append(chunkIDs, chunkID)
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

	for i := 0; i < len(chunks); i += embedBatchSize {
		end := i + embedBatchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		batchTexts := chunks[i:end]
		embeddings, err := s.Ollama.Embed(ctx, s.Config.EmbedModel, batchTexts)
		if err != nil {
			return nil, fmt.Errorf("embed batch: %w", err)
		}
		if err := s.Chroma.Add(ctx, collectionID, ids[i:end], batchTexts, metadatas[i:end], embeddings); err != nil {
			return nil, fmt.Errorf("write batch to chroma: %w", err)
		}
	}

	return chunkIDs, nil
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
	stats := Stats{}

	if docs, err := loadScopeDocuments(s.Config.DocsDir, "docs", docsExt); err != nil {
		return nil, stats, err
	} else {
		stats.DocsFiles = len(docs)
		out = append(out, docs...)
	}

	if s.Config.EnableCodeIngest {
		if code, err := loadScopeDocuments(s.Config.CodeDir, "code", codeExt); err != nil {
			return nil, stats, err
		} else {
			stats.CodeFiles = len(code)
			out = append(out, code...)
		}
	}

	stats.Files = len(out)
	return out, stats, nil
}

func loadScopeDocuments(root, scope string, allowedExt map[string]struct{}) ([]document, error) {
	if strings.TrimSpace(root) == "" {
		return nil, nil
	}

	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat %s directory: %w", scope, err)
	}

	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve %s root: %w", scope, err)
	}

	docs := make([]document, 0)
	err = filepath.WalkDir(root, func(filePath string, d os.DirEntry, walkErr error) error {
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
