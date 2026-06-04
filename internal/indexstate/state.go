package indexstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	stateFileName = "active-index.json"
	version       = 1
)

type Store struct {
	Dir string
}

type Manifest struct {
	Version          int                       `json:"version"`
	CollectionName   string                    `json:"collection_name"`
	ActiveGeneration string                    `json:"active_generation"`
	UpdatedAt        string                    `json:"updated_at"`
	Sources          map[string]SourceManifest `json:"sources"`
}

type SourceManifest struct {
	Scope    string   `json:"scope"`
	Hash     string   `json:"hash"`
	ChunkIDs []string `json:"chunk_ids"`
}

func New(dir string) *Store {
	return &Store{Dir: dir}
}

func (s *Store) Load() (Manifest, error) {
	path := s.path()
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return emptyManifest(), nil
		}
		return Manifest{}, fmt.Errorf("read index state: %w", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("parse index state: %w", err)
	}
	if manifest.Version == 0 {
		manifest.Version = version
	}
	if manifest.Sources == nil {
		manifest.Sources = map[string]SourceManifest{}
	}
	return manifest, nil
}

func (s *Store) Save(manifest Manifest) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return fmt.Errorf("create index state directory: %w", err)
	}

	manifest.Version = version
	manifest.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if manifest.Sources == nil {
		manifest.Sources = map[string]SourceManifest{}
	}

	payload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal index state: %w", err)
	}
	payload = append(payload, '\n')

	tmp, err := os.CreateTemp(s.Dir, ".active-index-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary index state: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary index state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary index state: %w", err)
	}
	if err := os.Rename(tmpPath, s.path()); err != nil {
		return fmt.Errorf("activate index state: %w", err)
	}
	return nil
}

func (s *Store) path() string {
	return filepath.Join(s.Dir, stateFileName)
}

func emptyManifest() Manifest {
	return Manifest{Version: version, Sources: map[string]SourceManifest{}}
}
