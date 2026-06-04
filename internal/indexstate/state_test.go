package indexstate

import "testing"

func TestLoadMissingStateReturnsEmptyManifest(t *testing.T) {
	store := New(t.TempDir())

	manifest, err := store.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if manifest.Version != version {
		t.Fatalf("Version = %d, want %d", manifest.Version, version)
	}
	if manifest.ActiveGeneration != "" {
		t.Fatalf("ActiveGeneration = %q, want empty", manifest.ActiveGeneration)
	}
	if len(manifest.Sources) != 0 {
		t.Fatalf("Sources = %v, want empty", manifest.Sources)
	}
}

func TestSaveAndLoadManifest(t *testing.T) {
	store := New(t.TempDir())
	want := Manifest{
		CollectionName:   "rag",
		ActiveGeneration: "gen-1",
		Sources: map[string]SourceManifest{
			"docs/guide.md": {
				Scope:    "docs",
				Hash:     "hash-1",
				ChunkIDs: []string{"docs:abc"},
			},
		},
	}

	if err := store.Save(want); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if got.CollectionName != want.CollectionName || got.ActiveGeneration != want.ActiveGeneration {
		t.Fatalf("loaded manifest = %+v, want %+v", got, want)
	}
	source := got.Sources["docs/guide.md"]
	if source.Scope != "docs" || source.Hash != "hash-1" || len(source.ChunkIDs) != 1 || source.ChunkIDs[0] != "docs:abc" {
		t.Fatalf("source manifest = %+v", source)
	}
	if got.UpdatedAt == "" {
		t.Fatal("expected UpdatedAt to be set")
	}
}
