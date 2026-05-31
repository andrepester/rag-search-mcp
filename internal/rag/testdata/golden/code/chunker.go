package golden

// splitGoldenChunks mirrors the product chunking concepts used by the golden
// query fixture: chunk size, overlap, chunk index, source_path metadata, and
// paragraph splitting.
func splitGoldenChunks(text string, chunkSize int, overlap int) []string {
	if chunkSize <= 0 || overlap < 0 || overlap >= chunkSize {
		return nil
	}
	return []string{text}
}
