package chunk

import "testing"

func TestSplit(t *testing.T) {
	text := "alpha\n\n" +
		"beta beta beta beta\n\n" +
		"gamma gamma gamma"

	parts := Split(text, 20, 5)
	if len(parts) == 0 {
		t.Fatal("expected non-empty chunks")
	}

	for _, part := range parts {
		if len(part) > 20 {
			t.Fatalf("chunk too large: %d", len(part))
		}
	}
}
