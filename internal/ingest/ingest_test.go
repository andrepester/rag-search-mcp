package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/andrepester/rag-search-mcp/internal/config"
	"github.com/andrepester/rag-search-mcp/internal/indexstate"
	"github.com/andrepester/rag-search-mcp/internal/ollama"
	"github.com/andrepester/rag-search-mcp/internal/store"
)

func TestLoadScopeDocumentsSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	insideFile := filepath.Join(root, "guide.md")
	if err := os.WriteFile(insideFile, []byte("inside"), 0o644); err != nil {
		t.Fatalf("write inside file: %v", err)
	}

	outsideFile := filepath.Join(outside, "secret.md")
	if err := os.WriteFile(outsideFile, []byte("outside"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	symlinkPath := filepath.Join(root, "link.md")
	if err := os.Symlink(outsideFile, symlinkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	docs, err := loadScopeDocuments(root, "docs", docsExt)
	if err != nil {
		t.Fatalf("loadScopeDocuments() failed: %v", err)
	}

	if len(docs) != 1 {
		t.Fatalf("len(docs) = %d, want 1", len(docs))
	}
	if docs[0].SourcePath != "docs/guide.md" {
		t.Fatalf("SourcePath = %q, want docs/guide.md", docs[0].SourcePath)
	}
}

func TestResolvedPathWithinRoot(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "nested", "file.md")
	outside := filepath.Join(filepath.Dir(root), "outside.md")

	if !resolvedPathWithinRoot(root, inside) {
		t.Fatal("expected inside path to be within root")
	}
	if resolvedPathWithinRoot(root, outside) {
		t.Fatal("expected outside path to be rejected")
	}
}

func TestReindexBuildsNewGenerationsAndReusesUnchangedSources(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatalf("create docs dir: %v", err)
	}
	guidePath := filepath.Join(docsDir, "guide.md")
	if err := os.WriteFile(guidePath, []byte("first guide text"), 0o644); err != nil {
		t.Fatalf("write guide: %v", err)
	}

	chromaBackend := newIngestChromaBackend()
	chromaServer := httptest.NewServer(chromaBackend)
	defer chromaServer.Close()

	ollamaBackend := &ingestOllamaBackend{}
	ollamaServer := httptest.NewServer(ollamaBackend)
	defer ollamaServer.Close()

	cfg := &config.Config{
		DocsDir:          docsDir,
		CodeDir:          filepath.Join(root, "missing-code"),
		CollectionName:   "rag",
		IndexStateDir:    filepath.Join(root, "index-state"),
		EmbedModel:       "test-embed",
		ChunkSize:        200,
		ChunkOverlap:     20,
		EnableCodeIngest: false,
	}
	svc := &Service{
		Config: cfg,
		Ollama: ollama.New(ollamaServer.URL),
		Chroma: store.NewChromaClient(chromaServer.URL, "default_tenant", "default_database"),
	}

	first, err := svc.Reindex(ctx)
	if err != nil {
		t.Fatalf("first Reindex() failed: %v", err)
	}
	if first.ChangedFiles != 1 || first.ReusedFiles != 0 || first.EmbeddedChunks == 0 {
		t.Fatalf("unexpected first stats: %+v", first)
	}
	firstEmbedCalls := ollamaBackend.calls()

	second, err := svc.Reindex(ctx)
	if err != nil {
		t.Fatalf("second Reindex() failed: %v", err)
	}
	if second.ChangedFiles != 0 || second.ReusedFiles != 1 || second.EmbeddedChunks != 0 || second.ReusedChunks == 0 {
		t.Fatalf("unexpected second stats: %+v", second)
	}
	if got := ollamaBackend.calls(); got != firstEmbedCalls {
		t.Fatalf("embedding calls after unchanged reindex = %d, want %d", got, firstEmbedCalls)
	}
	if second.Generation == first.Generation {
		t.Fatal("expected a new generation for the second reindex")
	}
	if got := chromaBackend.countGeneration(first.Generation); got == 0 {
		t.Fatalf("first generation records were deleted after second reindex, got %d", got)
	}
	if got := chromaBackend.countGeneration(second.Generation); got == 0 {
		t.Fatalf("second generation records were not written, got %d", got)
	}

	cfg.FreshIndex = true
	fresh, err := svc.Reindex(ctx)
	if err != nil {
		t.Fatalf("fresh Reindex() failed: %v", err)
	}
	if fresh.ChangedFiles != 1 || fresh.ReusedFiles != 0 || fresh.ReusedChunks != 0 || fresh.EmbeddedChunks == 0 {
		t.Fatalf("unexpected fresh stats: %+v", fresh)
	}
	if got := ollamaBackend.calls(); got <= firstEmbedCalls {
		t.Fatalf("embedding calls after fresh index = %d, want more than %d", got, firstEmbedCalls)
	}
	if got := chromaBackend.countGeneration(first.Generation); got == 0 {
		t.Fatal("first generation records were deleted during fresh index")
	}
	if got := chromaBackend.countGeneration(second.Generation); got == 0 {
		t.Fatal("second generation records were deleted during fresh index")
	}
	if got := chromaBackend.countGeneration(fresh.Generation); got == 0 {
		t.Fatalf("fresh generation records were not written, got %d", got)
	}
	cfg.FreshIndex = false

	if err := os.WriteFile(guidePath, []byte("changed guide text"), 0o644); err != nil {
		t.Fatalf("modify guide: %v", err)
	}
	third, err := svc.Reindex(ctx)
	if err != nil {
		t.Fatalf("third Reindex() failed: %v", err)
	}
	if third.ChangedFiles != 1 || third.ReusedFiles != 0 || third.EmbeddedChunks == 0 {
		t.Fatalf("unexpected third stats: %+v", third)
	}

	if err := os.Remove(guidePath); err != nil {
		t.Fatalf("delete guide: %v", err)
	}
	fourth, err := svc.Reindex(ctx)
	if err != nil {
		t.Fatalf("fourth Reindex() failed: %v", err)
	}
	if fourth.Files != 0 || fourth.Chunks != 0 || fourth.DeletedFiles != 1 {
		t.Fatalf("unexpected fourth stats: %+v", fourth)
	}
}

func TestReindexResumesIncompleteBuildGeneration(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatalf("create docs dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "a.md"), []byte("first guide text"), 0o644); err != nil {
		t.Fatalf("write first doc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "b.md"), []byte("second guide text"), 0o644); err != nil {
		t.Fatalf("write second doc: %v", err)
	}

	chromaBackend := newIngestChromaBackend()
	chromaServer := httptest.NewServer(chromaBackend)
	defer chromaServer.Close()

	ollamaBackend := &ingestOllamaBackend{failAfter: 1}
	ollamaServer := httptest.NewServer(ollamaBackend)
	defer ollamaServer.Close()

	cfg := &config.Config{
		DocsDir:          docsDir,
		CodeDir:          filepath.Join(root, "missing-code"),
		CollectionName:   "rag",
		IndexStateDir:    filepath.Join(root, "index-state"),
		EmbedModel:       "test-embed",
		ChunkSize:        200,
		ChunkOverlap:     20,
		EnableCodeIngest: false,
		EmbedConcurrency: 1,
	}
	svc := &Service{
		Config: cfg,
		Ollama: ollama.New(ollamaServer.URL),
		Chroma: store.NewChromaClient(chromaServer.URL, "default_tenant", "default_database"),
	}

	failed, err := svc.Reindex(ctx)
	if err == nil {
		t.Fatal("first Reindex() succeeded, want failure")
	}
	if failed.Generation == "" {
		t.Fatal("failed generation is empty")
	}
	if got := chromaBackend.countGeneration(failed.Generation); got == 0 {
		t.Fatal("failed generation did not keep completed records for resume")
	}
	manifest, err := indexstate.New(cfg.IndexStateDir).Load()
	if err != nil {
		t.Fatalf("load manifest after failure: %v", err)
	}
	if manifest.ActiveGeneration != "" {
		t.Fatalf("ActiveGeneration after failure = %q, want empty", manifest.ActiveGeneration)
	}
	if manifest.ResumeGeneration != failed.Generation {
		t.Fatalf("ResumeGeneration = %q, want %q", manifest.ResumeGeneration, failed.Generation)
	}

	callsAfterFailure := ollamaBackend.calls()
	ollamaBackend.setFailAfter(0)
	resumed, err := svc.Reindex(ctx)
	if err != nil {
		t.Fatalf("resumed Reindex() failed: %v", err)
	}
	if resumed.Generation != failed.Generation {
		t.Fatalf("resumed generation = %q, want %q", resumed.Generation, failed.Generation)
	}
	if resumed.ReusedFiles != 1 || resumed.ChangedFiles != 1 || resumed.EmbeddedChunks == 0 {
		t.Fatalf("unexpected resumed stats: %+v", resumed)
	}
	if got := ollamaBackend.calls() - callsAfterFailure; got != 1 {
		t.Fatalf("embedding calls during resume = %d, want 1", got)
	}
	manifest, err = indexstate.New(cfg.IndexStateDir).Load()
	if err != nil {
		t.Fatalf("load manifest after resume: %v", err)
	}
	if manifest.ActiveGeneration != failed.Generation {
		t.Fatalf("ActiveGeneration after resume = %q, want %q", manifest.ActiveGeneration, failed.Generation)
	}
	if manifest.ResumeGeneration != "" {
		t.Fatalf("ResumeGeneration after success = %q, want empty", manifest.ResumeGeneration)
	}
}

func TestFreshReindexKeepsActiveGenerationUntilResumeCompletes(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatalf("create docs dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "a.md"), []byte("first guide text"), 0o644); err != nil {
		t.Fatalf("write first doc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "b.md"), []byte("second guide text"), 0o644); err != nil {
		t.Fatalf("write second doc: %v", err)
	}

	chromaBackend := newIngestChromaBackend()
	chromaServer := httptest.NewServer(chromaBackend)
	defer chromaServer.Close()

	ollamaBackend := &ingestOllamaBackend{}
	ollamaServer := httptest.NewServer(ollamaBackend)
	defer ollamaServer.Close()

	cfg := &config.Config{
		DocsDir:          docsDir,
		CodeDir:          filepath.Join(root, "missing-code"),
		CollectionName:   "rag",
		IndexStateDir:    filepath.Join(root, "index-state"),
		EmbedModel:       "test-embed",
		ChunkSize:        200,
		ChunkOverlap:     20,
		EnableCodeIngest: false,
		EmbedConcurrency: 1,
	}
	svc := &Service{
		Config: cfg,
		Ollama: ollama.New(ollamaServer.URL),
		Chroma: store.NewChromaClient(chromaServer.URL, "default_tenant", "default_database"),
	}

	initial, err := svc.Reindex(ctx)
	if err != nil {
		t.Fatalf("initial Reindex() failed: %v", err)
	}
	cfg.FreshIndex = true
	ollamaBackend.setFailAfter(ollamaBackend.calls() + 1)
	failed, err := svc.Reindex(ctx)
	if err == nil {
		t.Fatal("fresh Reindex() succeeded, want failure")
	}
	if failed.Generation == initial.Generation {
		t.Fatal("fresh failed generation reused active generation")
	}
	manifest, err := indexstate.New(cfg.IndexStateDir).Load()
	if err != nil {
		t.Fatalf("load manifest after failed fresh run: %v", err)
	}
	if manifest.ActiveGeneration != initial.Generation {
		t.Fatalf("ActiveGeneration after failed fresh run = %q, want %q", manifest.ActiveGeneration, initial.Generation)
	}
	if manifest.ResumeGeneration != failed.Generation {
		t.Fatalf("ResumeGeneration after failed fresh run = %q, want %q", manifest.ResumeGeneration, failed.Generation)
	}
	if !manifest.ResumeFreshIndex {
		t.Fatal("ResumeFreshIndex after failed fresh run = false, want true")
	}
	if got := chromaBackend.countGeneration(initial.Generation); got == 0 {
		t.Fatal("active generation records were deleted by failed fresh run")
	}

	callsAfterFailure := ollamaBackend.calls()
	cfg.FreshIndex = false
	ollamaBackend.setFailAfter(0)
	resumed, err := svc.Reindex(ctx)
	if err != nil {
		t.Fatalf("resumed fresh Reindex() failed: %v", err)
	}
	if resumed.Generation != failed.Generation {
		t.Fatalf("resumed fresh generation = %q, want %q", resumed.Generation, failed.Generation)
	}
	if resumed.ReusedFiles != 1 || resumed.ChangedFiles != 1 {
		t.Fatalf("resumed fresh stats = %+v, want one resumed and one embedded source", resumed)
	}
	if got := ollamaBackend.calls() - callsAfterFailure; got != 1 {
		t.Fatalf("embedding calls during fresh resume without FRESH_INDEX = %d, want 1", got)
	}
	manifest, err = indexstate.New(cfg.IndexStateDir).Load()
	if err != nil {
		t.Fatalf("load manifest after resumed fresh run: %v", err)
	}
	if manifest.ActiveGeneration != failed.Generation {
		t.Fatalf("ActiveGeneration after resumed fresh run = %q, want %q", manifest.ActiveGeneration, failed.Generation)
	}
	if manifest.ResumeGeneration != "" {
		t.Fatalf("ResumeGeneration after resumed fresh run = %q, want empty", manifest.ResumeGeneration)
	}
	if manifest.ResumeFreshIndex {
		t.Fatal("ResumeFreshIndex after resumed fresh run = true, want false")
	}
}

func TestReindexReportsDocumentProgress(t *testing.T) {
	root := t.TempDir()
	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatalf("create docs dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "a.md"), []byte("first guide text"), 0o644); err != nil {
		t.Fatalf("write first doc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "b.md"), []byte("second guide text"), 0o644); err != nil {
		t.Fatalf("write second doc: %v", err)
	}

	chromaBackend := newIngestChromaBackend()
	chromaServer := httptest.NewServer(chromaBackend)
	defer chromaServer.Close()

	ollamaBackend := &ingestOllamaBackend{}
	ollamaServer := httptest.NewServer(ollamaBackend)
	defer ollamaServer.Close()

	cfg := &config.Config{
		DocsDir:          docsDir,
		CodeDir:          filepath.Join(root, "missing-code"),
		CollectionName:   "rag",
		IndexStateDir:    filepath.Join(root, "index-state"),
		EmbedModel:       "test-embed",
		ChunkSize:        200,
		ChunkOverlap:     20,
		EnableCodeIngest: false,
	}
	svc := &Service{
		Config: cfg,
		Ollama: ollama.New(ollamaServer.URL),
		Chroma: store.NewChromaClient(chromaServer.URL, "default_tenant", "default_database"),
	}

	var progresses []DocumentProgress
	ctx := WithDocumentProgressReporter(context.Background(), func(progress DocumentProgress) {
		progresses = append(progresses, progress)
	})
	stats, err := svc.Reindex(ctx)
	if err != nil {
		t.Fatalf("Reindex() failed: %v", err)
	}
	if stats.Files != 2 {
		t.Fatalf("Files = %d, want 2", stats.Files)
	}

	want := []DocumentProgress{
		{TotalDocuments: 2, ProcessedDocuments: 0},
		{TotalDocuments: 2, ProcessedDocuments: 1},
		{TotalDocuments: 2, ProcessedDocuments: 2},
	}
	if len(progresses) != len(want) {
		t.Fatalf("progress count = %d, want %d: %+v", len(progresses), len(want), progresses)
	}
	for i := range want {
		if progresses[i] != want[i] {
			t.Fatalf("progress[%d] = %+v, want %+v", i, progresses[i], want[i])
		}
	}
}

func TestReindexReportsEmptyDocumentProgress(t *testing.T) {
	root := t.TempDir()
	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatalf("create docs dir: %v", err)
	}

	chromaBackend := newIngestChromaBackend()
	chromaServer := httptest.NewServer(chromaBackend)
	defer chromaServer.Close()

	ollamaServer := httptest.NewServer(&ingestOllamaBackend{})
	defer ollamaServer.Close()

	cfg := &config.Config{
		DocsDir:          docsDir,
		CodeDir:          filepath.Join(root, "missing-code"),
		CollectionName:   "rag",
		IndexStateDir:    filepath.Join(root, "index-state"),
		EmbedModel:       "test-embed",
		ChunkSize:        200,
		ChunkOverlap:     20,
		EnableCodeIngest: false,
	}
	svc := &Service{
		Config: cfg,
		Ollama: ollama.New(ollamaServer.URL),
		Chroma: store.NewChromaClient(chromaServer.URL, "default_tenant", "default_database"),
	}

	var progresses []DocumentProgress
	ctx := WithDocumentProgressReporter(context.Background(), func(progress DocumentProgress) {
		progresses = append(progresses, progress)
	})
	stats, err := svc.Reindex(ctx)
	if err != nil {
		t.Fatalf("Reindex() failed: %v", err)
	}
	if stats.Files != 0 {
		t.Fatalf("Files = %d, want 0", stats.Files)
	}
	want := []DocumentProgress{{TotalDocuments: 0, ProcessedDocuments: 0}}
	if len(progresses) != len(want) || progresses[0] != want[0] {
		t.Fatalf("progresses = %+v, want %+v", progresses, want)
	}
}

func TestReindexHonorsIndexLimit(t *testing.T) {
	root := t.TempDir()
	docsDir := filepath.Join(root, "docs")
	codeDir := filepath.Join(root, "code")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatalf("create docs dir: %v", err)
	}
	if err := os.MkdirAll(codeDir, 0o755); err != nil {
		t.Fatalf("create code dir: %v", err)
	}
	for _, name := range []string{"a.md", "b.md", "c.md"} {
		if err := os.WriteFile(filepath.Join(docsDir, name), []byte(name+" guide text"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(codeDir, "app.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write code file: %v", err)
	}

	chromaBackend := newIngestChromaBackend()
	chromaServer := httptest.NewServer(chromaBackend)
	defer chromaServer.Close()

	ollamaServer := httptest.NewServer(&ingestOllamaBackend{})
	defer ollamaServer.Close()

	cfg := &config.Config{
		DocsDir:          docsDir,
		CodeDir:          codeDir,
		CollectionName:   "rag",
		IndexStateDir:    filepath.Join(root, "index-state"),
		EmbedModel:       "test-embed",
		ChunkSize:        200,
		ChunkOverlap:     20,
		EnableCodeIngest: true,
		IndexLimit:       2,
	}
	svc := &Service{
		Config: cfg,
		Ollama: ollama.New(ollamaServer.URL),
		Chroma: store.NewChromaClient(chromaServer.URL, "default_tenant", "default_database"),
	}

	stats, err := svc.Reindex(context.Background())
	if err != nil {
		t.Fatalf("Reindex() failed: %v", err)
	}
	if stats.Files != 2 || stats.DocsFiles != 2 || stats.CodeFiles != 0 || stats.ChangedFiles != 2 {
		t.Fatalf("unexpected limited stats: %+v", stats)
	}

	paths := chromaBackend.sourcePathsForGeneration(stats.Generation)
	want := []string{"docs/a.md", "docs/b.md"}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("indexed source paths = %+v, want %+v", paths, want)
	}
}

func TestReindexSplitsChangedSourceIntoEmbedBatches(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatalf("create docs dir: %v", err)
	}

	var builder strings.Builder
	for i := 0; i < embedBatchSize+5; i++ {
		_, _ = fmt.Fprintf(&builder, "biology demo sentence %02d with habitat adaptation ecology. ", i)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "many.md"), []byte(builder.String()), 0o644); err != nil {
		t.Fatalf("write many chunks doc: %v", err)
	}

	chromaBackend := newIngestChromaBackend()
	chromaServer := httptest.NewServer(chromaBackend)
	defer chromaServer.Close()

	ollamaBackend := &ingestOllamaBackend{}
	ollamaServer := httptest.NewServer(ollamaBackend)
	defer ollamaServer.Close()

	cfg := &config.Config{
		DocsDir:          docsDir,
		CodeDir:          filepath.Join(root, "missing-code"),
		CollectionName:   "rag",
		IndexStateDir:    filepath.Join(root, "index-state"),
		EmbedModel:       "test-embed",
		ChunkSize:        40,
		ChunkOverlap:     0,
		EnableCodeIngest: false,
	}
	svc := &Service{
		Config: cfg,
		Ollama: ollama.New(ollamaServer.URL),
		Chroma: store.NewChromaClient(chromaServer.URL, "default_tenant", "default_database"),
	}

	stats, err := svc.Reindex(ctx)
	if err != nil {
		t.Fatalf("Reindex() failed: %v", err)
	}
	if stats.EmbeddedChunks <= embedBatchSize {
		t.Fatalf("EmbeddedChunks = %d, want more than batch size %d", stats.EmbeddedChunks, embedBatchSize)
	}
	if got := ollamaBackend.calls(); got < 2 {
		t.Fatalf("embedding calls = %d, want split batches", got)
	}
	if got := ollamaBackend.maxBatchSize(); got > embedBatchSize {
		t.Fatalf("max embedding batch size = %d, want <= %d", got, embedBatchSize)
	}
}

func TestReindexUsesConfiguredEmbedConcurrency(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatalf("create docs dir: %v", err)
	}
	for _, name := range []string{"a.md", "b.md", "c.md", "d.md"} {
		if err := os.WriteFile(filepath.Join(docsDir, name), []byte(name+" guide text about embedding throughput"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	chromaBackend := newIngestChromaBackend()
	chromaServer := httptest.NewServer(chromaBackend)
	defer chromaServer.Close()

	ollamaBackend := &ingestOllamaBackend{delay: 25 * time.Millisecond}
	ollamaServer := httptest.NewServer(ollamaBackend)
	defer ollamaServer.Close()

	cfg := &config.Config{
		DocsDir:          docsDir,
		CodeDir:          filepath.Join(root, "missing-code"),
		CollectionName:   "rag",
		IndexStateDir:    filepath.Join(root, "index-state"),
		EmbedModel:       "test-embed",
		ChunkSize:        200,
		ChunkOverlap:     20,
		EnableCodeIngest: false,
		EmbedConcurrency: 2,
	}
	svc := &Service{
		Config: cfg,
		Ollama: ollama.New(ollamaServer.URL),
		Chroma: store.NewChromaClient(chromaServer.URL, "default_tenant", "default_database"),
	}

	stats, err := svc.Reindex(ctx)
	if err != nil {
		t.Fatalf("Reindex() failed: %v", err)
	}
	if stats.ChangedFiles != 4 {
		t.Fatalf("ChangedFiles = %d, want 4", stats.ChangedFiles)
	}
	if got := ollamaBackend.maxConcurrentCalls(); got != 2 {
		t.Fatalf("max concurrent embedding calls = %d, want 2", got)
	}
}

type ingestOllamaBackend struct {
	mu        sync.Mutex
	callCount int
	maxBatch  int
	inFlight  int
	maxFlight int
	delay     time.Duration
	failAfter int
}

func (b *ingestOllamaBackend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/api/embed" {
		http.NotFound(w, r)
		return
	}
	var req struct {
		Input []string `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	b.mu.Lock()
	b.callCount++
	callNumber := b.callCount
	failAfter := b.failAfter
	b.inFlight++
	if len(req.Input) > b.maxBatch {
		b.maxBatch = len(req.Input)
	}
	if b.inFlight > b.maxFlight {
		b.maxFlight = b.inFlight
	}
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		b.inFlight--
		b.mu.Unlock()
	}()
	if failAfter > 0 && callNumber > failAfter {
		http.Error(w, "embedding failure", http.StatusInternalServerError)
		return
	}
	if b.delay > 0 {
		time.Sleep(b.delay)
	}

	embeddings := make([][]float64, 0, len(req.Input))
	for _, input := range req.Input {
		embeddings = append(embeddings, []float64{float64(len(input)), 1})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": embeddings})
}

func (b *ingestOllamaBackend) calls() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.callCount
}

func (b *ingestOllamaBackend) setFailAfter(value int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failAfter = value
}

func (b *ingestOllamaBackend) maxBatchSize() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.maxBatch
}

func (b *ingestOllamaBackend) maxConcurrentCalls() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.maxFlight
}

type ingestChromaBackend struct {
	mu           sync.Mutex
	collectionID string
	records      map[string]ingestRecord
	order        []string
}

type ingestRecord struct {
	ID        string
	Document  string
	Metadata  map[string]any
	Embedding []float64
}

func newIngestChromaBackend() *ingestChromaBackend {
	return &ingestChromaBackend{
		collectionID: "col-rag",
		records:      map[string]ingestRecord{},
		order:        []string{},
	}
}

func (b *ingestChromaBackend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	basePath := "/api/v2/tenants/default_tenant/databases/default_database"
	switch {
	case r.Method == http.MethodPost && r.URL.Path == basePath+"/collections":
		_ = json.NewEncoder(w).Encode(map[string]any{"id": b.collectionID, "name": "rag"})
	case r.Method == http.MethodPost && r.URL.Path == basePath+"/collections/"+b.collectionID+"/upsert":
		b.handleUpsert(w, r)
	case r.Method == http.MethodPost && r.URL.Path == basePath+"/collections/"+b.collectionID+"/get":
		b.handleGet(w, r)
	case r.Method == http.MethodPost && r.URL.Path == basePath+"/collections/"+b.collectionID+"/delete":
		b.handleDelete(w, r)
	case r.Method == http.MethodDelete && r.URL.Path == basePath+"/collections/"+b.collectionID:
		b.handleDeleteCollection(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (b *ingestChromaBackend) handleUpsert(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		IDs        []string         `json:"ids"`
		Documents  []string         `json:"documents"`
		Metadatas  []map[string]any `json:"metadatas"`
		Embeddings [][]float64      `json:"embeddings"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	for i, id := range payload.IDs {
		if _, exists := b.records[id]; !exists {
			b.order = append(b.order, id)
		}
		record := ingestRecord{ID: id, Metadata: map[string]any{}}
		if i < len(payload.Documents) {
			record.Document = payload.Documents[i]
		}
		if i < len(payload.Metadatas) {
			for key, value := range payload.Metadatas[i] {
				record.Metadata[key] = value
			}
		}
		if i < len(payload.Embeddings) {
			record.Embedding = append([]float64(nil), payload.Embeddings[i]...)
		}
		b.records[id] = record
	}
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"upserted": len(payload.IDs)})
}

func (b *ingestChromaBackend) handleGet(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Where  map[string]any `json:"where"`
		Limit  int            `json:"limit"`
		Offset int            `json:"offset"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	matches := b.filter(payload.Where)
	if payload.Offset > 0 && payload.Offset < len(matches) {
		matches = matches[payload.Offset:]
	} else if payload.Offset >= len(matches) {
		matches = nil
	}
	if payload.Limit > 0 && len(matches) > payload.Limit {
		matches = matches[:payload.Limit]
	}

	ids := make([]string, 0, len(matches))
	docs := make([]*string, 0, len(matches))
	metas := make([]map[string]any, 0, len(matches))
	embeddings := make([][]float64, 0, len(matches))
	for _, record := range matches {
		ids = append(ids, record.ID)
		doc := record.Document
		docs = append(docs, &doc)
		metas = append(metas, record.Metadata)
		embeddings = append(embeddings, record.Embedding)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ids":        ids,
		"documents":  docs,
		"metadatas":  metas,
		"embeddings": embeddings,
	})
}

func (b *ingestChromaBackend) handleDelete(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Where map[string]any `json:"where"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	nextOrder := make([]string, 0, len(b.order))
	for _, id := range b.order {
		record := b.records[id]
		if ingestMetadataMatches(record.Metadata, payload.Where) {
			delete(b.records, id)
			continue
		}
		nextOrder = append(nextOrder, id)
	}
	b.order = nextOrder
	b.mu.Unlock()
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"deleted": true})
}

func (b *ingestChromaBackend) handleDeleteCollection(w http.ResponseWriter, _ *http.Request) {
	b.mu.Lock()
	b.records = map[string]ingestRecord{}
	b.order = nil
	b.mu.Unlock()
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"deleted": true})
}

func (b *ingestChromaBackend) filter(where map[string]any) []ingestRecord {
	b.mu.Lock()
	defer b.mu.Unlock()

	out := make([]ingestRecord, 0, len(b.order))
	for _, id := range b.order {
		record := b.records[id]
		if ingestMetadataMatches(record.Metadata, where) {
			out = append(out, record)
		}
	}
	return out
}

func (b *ingestChromaBackend) countGeneration(generation string) int {
	b.mu.Lock()
	defer b.mu.Unlock()

	count := 0
	for _, record := range b.records {
		if fmt.Sprint(record.Metadata["index_generation"]) == generation {
			count++
		}
	}
	return count
}

func (b *ingestChromaBackend) sourcePathsForGeneration(generation string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()

	seen := map[string]struct{}{}
	for _, record := range b.records {
		if fmt.Sprint(record.Metadata["index_generation"]) != generation {
			continue
		}
		sourcePath := fmt.Sprint(record.Metadata["source_path"])
		if sourcePath != "" {
			seen[sourcePath] = struct{}{}
		}
	}
	paths := make([]string, 0, len(seen))
	for sourcePath := range seen {
		paths = append(paths, sourcePath)
	}
	sort.Strings(paths)
	return paths
}

func ingestMetadataMatches(metadata map[string]any, where map[string]any) bool {
	if len(where) == 0 {
		return true
	}
	for key, want := range where {
		if key == "$and" {
			clauses, ok := want.([]any)
			if !ok {
				return false
			}
			for _, clause := range clauses {
				clauseMap, ok := clause.(map[string]any)
				if !ok || !ingestMetadataMatches(metadata, clauseMap) {
					return false
				}
			}
			continue
		}
		got, ok := metadata[key]
		if !ok || !strings.EqualFold(fmt.Sprint(got), fmt.Sprint(want)) {
			return false
		}
	}
	return true
}
