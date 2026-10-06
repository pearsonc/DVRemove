package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// Hazard: H7
func TestGrowingInputIsSkipped(t *testing.T) {
	calls := stubTools(t, "dvhe.07")
	in, out := t.TempDir(), t.TempDir()
	growing := writeInput(t, in, "growing.mkv", 1024)
	writeInput(t, in, "steady.mkv", 1024)
	logs := &lockedBuffer{}
	c := newTestConverter(t, out)
	c.freeSpace = plentyOfSpace
	c.SetTempDir(t.TempDir())
	w := NewWatcher(in, c, 1, nil, zerolog.New(logs))

	var intervals []time.Duration
	w.stableInterval = 7 * time.Millisecond
	w.sleep = func(d time.Duration) {
		intervals = append(intervals, d)
		f, err := os.OpenFile(growing, os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			t.Error(err)
			return
		}
		defer f.Close()
		f.WriteString("more")
	}

	if err := w.ProcessExisting(); err != nil {
		t.Errorf("a growing input made the batch fail: %v", err)
	}
	if len(intervals) == 0 || intervals[0] != 7*time.Millisecond {
		t.Errorf("the check waited %v, want the injected 7ms", intervals)
	}
	if exists(filepath.Join(out, "growing.mkv")) {
		t.Error("a file still growing was converted")
	}
	if !exists(filepath.Join(out, "steady.mkv")) {
		t.Error("a steady file was not converted")
	}
	if n := ffmpegCalls(t, calls); n == 0 {
		t.Error("no conversion ran at all")
	}
	if !exists(growing) {
		t.Error("the growing input was not left in place")
	}
	if got := logs.String(); !containsAll(got, "still growing", "growing.mkv") {
		t.Errorf("the skip was not logged:\n%s", got)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}
