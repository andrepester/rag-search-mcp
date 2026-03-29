package rag

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNormalizeScope(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		fallback string
		want     string
	}{
		{name: "default fallback", input: "", fallback: "all", want: "all"},
		{name: "docs value", input: "docs", fallback: "all", want: "docs"},
		{name: "code value", input: "code", fallback: "all", want: "code"},
		{name: "invalid", input: "foo", fallback: "all", want: "all"},
		{name: "invalid fallback", input: "", fallback: "nope", want: "all"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeScope(tt.input, tt.fallback)
			if got != tt.want {
				t.Fatalf("normalizeScope() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCollectionIDAccessorsAreSafe(t *testing.T) {
	svc := &Service{}

	const workers = 16
	const iterations = 200

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				svc.setCollectionID("col")
				if got := svc.getCollectionID(); got == "" {
					t.Errorf("getCollectionID() returned empty value")
					return
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestEnsureWithRetryEventuallySucceeds(t *testing.T) {
	var attempts atomic.Int32

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	collectionID, err := ensureWithRetry(ctx, func(context.Context) (string, error) {
		if attempts.Add(1) < 3 {
			return "", errors.New("not ready")
		}
		return "col-ready", nil
	})
	if err != nil {
		t.Fatalf("ensureWithRetry() failed: %v", err)
	}
	if collectionID != "col-ready" {
		t.Fatalf("collectionID = %q, want col-ready", collectionID)
	}
	if attempts.Load() < 3 {
		t.Fatalf("attempts = %d, want >= 3", attempts.Load())
	}
}

func TestEnsureWithRetryTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	if _, err := ensureWithRetry(ctx, func(context.Context) (string, error) {
		return "", errors.New("still failing")
	}); err == nil {
		t.Fatal("expected timeout error")
	}
}
