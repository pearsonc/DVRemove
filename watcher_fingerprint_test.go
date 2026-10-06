package main

import (
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
)

// growingAfterWrite runs isGrowing over a 64 MiB file, performing write on each wait, with
// statTimes replaced by times (nil keeps the real ones).
func growingAfterWrite(t *testing.T, times func(os.FileInfo) (int64, int64), write func(path string, n int)) bool {
	t.Helper()
	path := writeInput(t, t.TempDir(), "f.mkv", 64<<20)
	if times != nil {
		old := statTimes
		statTimes = times
		t.Cleanup(func() { statTimes = old })
	}
	w := NewWatcher(t.TempDir(), nil, 1, nil, zerolog.Nop())
	n := 0
	w.sleep = func(time.Duration) { write(path, n); n++ }
	grown, err := w.isGrowing(path)
	if err != nil {
		t.Fatal(err)
	}
	return grown
}

func writeAt(t *testing.T, path string, off int64, b string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteAt([]byte(b), off); err != nil {
		t.Fatal(err)
	}
}

// Hazard: H7
// The sampled bytes alone: the times held still, a write inside the head block is seen.
func TestGrowingSeenBySampledBytesAlone(t *testing.T) {
	still := func(os.FileInfo) (int64, int64) { return 1, 1 }
	if !growingAfterWrite(t, still, func(p string, n int) { writeAt(t, p, int64(n), "x") }) {
		t.Error("a write into a sampled block, the times still, was not seen as growing")
	}
}

// Hazard: H7
// The times alone: the same bytes written over themselves, so the size and the sampled bytes
// hold still and only the real modification and change times move.
func TestGrowingSeenByTimesAlone(t *testing.T) {
	if !growingAfterWrite(t, nil, func(p string, n int) {
		time.Sleep(20 * time.Millisecond)
		writeAt(t, p, 0, "\x00")
	}) {
		t.Error("a rewrite that moved only the times was not seen as growing")
	}
}

// Hazard: H7
// Each time alone: the file is untouched and the injected time moves, once for each of the two.
func TestGrowingSeenByEachTimeAlone(t *testing.T) {
	for name, move := range map[string]func(c int64) (int64, int64){
		"mtime": func(c int64) (int64, int64) { return c, 1 },
		"ctime": func(c int64) (int64, int64) { return 1, c },
	} {
		var calls atomic.Int64
		times := func(os.FileInfo) (int64, int64) { return move(calls.Add(1)) }
		if !growingAfterWrite(t, times, func(string, int) {}) {
			t.Errorf("%s moving alone was not seen as growing", name)
		}
	}
}

// Hazard: H7
// A file that holds still on every count is not growing.
func TestSteadyFileIsNotGrowing(t *testing.T) {
	if growingAfterWrite(t, nil, func(string, int) {}) {
		t.Error("an untouched file was seen as growing")
	}
}

// Hazard: H1
// A FIFO named *.mkv that appears in a Watch event is skipped, unopened.
func TestWatchSkipsFifoEvent(t *testing.T) {
	logs := &lockedBuffer{}
	in := t.TempDir()
	fifo := in + "/b.mkv"
	if err := mkfifo(fifo); err != nil {
		t.Fatal(err)
	}
	w := NewWatcher(in, nil, 1, nil, zerolog.New(logs))
	w.handleEvent(fsEvent(fifo))
	if _, busy := w.processing.Load(fifo); busy {
		t.Error("a FIFO was taken up for processing")
	}
	if !strings.Contains(logs.String(), "not a regular file") {
		t.Errorf("the skip was not logged:\n%s", logs.String())
	}
}

func mkfifo(path string) error { return syscall.Mkfifo(path, 0o644) }

func fsEvent(name string) fsnotify.Event { return fsnotify.Event{Name: name, Op: fsnotify.Create} }
