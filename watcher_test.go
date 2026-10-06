package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// batchOf stubs the tools as Profile 7 and returns a watcher over n input files.
func batchOf(t *testing.T, n, workers int) *Watcher {
	t.Helper()
	stubTools(t, "dvhe.07")
	in := t.TempDir()
	for i := 0; i < n; i++ {
		writeInput(t, in, fmt.Sprintf("film-%d.mkv", i), 1024)
	}
	c := newTestConverter(t, t.TempDir())
	c.freeSpace = plentyOfSpace
	return NewWatcher(in, c, workers, nil, zerolog.Nop())
}

// Hazard: H1
func TestProcessExistingReturnsWithFiles(t *testing.T) {
	w := batchOf(t, 5, 1)
	done := make(chan error, 1)
	go func() { done <- w.ProcessExisting() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ProcessExisting: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ProcessExisting did not return within 10s with 5 jobs on 1 worker")
	}
}
