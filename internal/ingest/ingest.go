package ingest

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ledongthuc/pdf"

	"local-rag/internal/chunk"
	"local-rag/internal/config"
	"local-rag/internal/ollama"
	"local-rag/internal/store"
)

type Service struct {
	Config *config.Config
	Ollama *ollama.Client
	Chroma *store.ChromaClient
}

type Stats struct {
	Files     int `json:"files"`
	Chunks    int `json:"chunks"`
	CodeFiles int `json:"code_files"`
	DocsFiles int `json:"docs_files"`
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

func (s *Service) Reindex(ctx context.Context) (Stats, error) {
	collectionID, err := s.Chroma.EnsureCollection(ctx, s.Config.CollectionName)
	if err != nil {
		return Stats{}, fmt.Errorf("ensure collection before reset: %w", err)
	}
	if err := s.Chroma.DeleteCollection(ctx, collectionID); err != nil && !store.IsNotFound(err) {
		return Stats{}, fmt.Errorf("delete collection before reset: %w", err)
	}

	collectionID, err = s.Chroma.EnsureCollection(ctx, s.Config.CollectionName)
	if err != nil {
		return Stats{}, fmt.Errorf("ensure collection after reset: %w", err)
	}

	documents, stats, err := s.loadDocuments()
	if err != nil {
		return Stats{}, err
	}
	if len(documents) == 0 {
		return stats, nil
	}

	ids := make([]string, 0)
	texts := make([]string, 0)
	metadatas := make([]map[string]any, 0)

	for _, doc := range documents {
		chunks := chunk.Split(doc.Text, s.Config.ChunkSize, s.Config.ChunkOverlap)
		for i, c := range chunks {
			id := scopedChunkID(doc.Scope, doc.SourcePath, i)
			ids = append(ids, id)
			texts = append(texts, c)
			metadatas = append(metadatas, map[string]any{
				"scope":       doc.Scope,
				"source_path": doc.SourcePath,
				"chunk_index": i,
			})
		}
	}

	batchSize := 32
	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		batchTexts := texts[i:end]
		embeddings, err := s.Ollama.Embed(ctx, s.Config.EmbedModel, batchTexts)
		if err != nil {
			return Stats{}, fmt.Errorf("embed batch: %w", err)
		}
		if err := s.Chroma.Add(ctx, collectionID, ids[i:end], batchTexts, metadatas[i:end], embeddings); err != nil {
			return Stats{}, fmt.Errorf("write batch to chroma: %w", err)
		}
	}

	stats.Chunks = len(texts)
	return stats, nil
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
