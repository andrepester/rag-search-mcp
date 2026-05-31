package golden

// goldenSearchResponse documents the code-scope result contract: every semantic
// search match carries a source path, scope, chunk index, distance, and text.
type goldenSearchResponse struct {
	SourcePath string
	Scope      string
	ChunkIndex int
	Distance   float64
	Text       string
}
